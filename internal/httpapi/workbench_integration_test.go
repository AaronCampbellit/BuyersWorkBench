package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"buyersworkbench/internal/domain"
	"buyersworkbench/internal/imports"
	"buyersworkbench/internal/store"
	"buyersworkbench/internal/workbench"
	"buyersworkbench/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type backendFixture struct {
	ctx        context.Context
	admin, app *pgxpool.Pool
	handler    http.Handler
	owner      store.BootstrapResult
}

func newBackendFixture(t *testing.T) backendFixture {
	t.Helper()
	adminURL, appURL := os.Getenv("BWB_TEST_DATABASE_URL"), os.Getenv("BWB_TEST_APP_DATABASE_URL")
	if adminURL == "" || appURL == "" {
		t.Skip("disposable database URLs required for backend acceptance tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	base, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatal("open test database")
	}
	var id string
	if err = base.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	schema := "http_test_" + strings.ReplaceAll(id, "-", "")
	quoted := pgx.Identifier{schema}.Sanitize()
	if _, err = base.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, _ = base.Exec(cleanup, "DROP SCHEMA "+quoted+" CASCADE")
		_ = base.Close(cleanup)
	})
	newPool := func(url string) *pgxpool.Pool {
		cfg, err := pgxpool.ParseConfig(url)
		if err != nil {
			t.Fatal("parse test database")
		}
		cfg.ConnConfig.RuntimeParams["search_path"] = schema
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(pool.Close)
		return pool
	}
	admin := newPool(adminURL)
	if err = migrations.Apply(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if err = store.MigrateQueue(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if _, err = base.Exec(ctx, "GRANT USAGE ON SCHEMA "+quoted+" TO bwb_app"); err != nil {
		t.Fatal(err)
	}
	app := newPool(appURL)
	owner, err := store.BootstrapOwner(ctx, admin, store.OwnerInput{Email: "backend-owner@example.test", DisplayName: "Backend owner", Password: "Backend owner test passphrase"})
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(app, Config{Origin: "http://127.0.0.1:4180"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return backendFixture{ctx, admin, app, handler, owner}
}
func (f backendFixture) request(method, path, contentType string, body []byte, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Origin", "http://127.0.0.1:4180")
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("X-CSRF-Token", csrf)
	if cookie != nil {
		req.AddCookie(cookie)
	}
	result := httptest.NewRecorder()
	f.handler.ServeHTTP(result, req)
	return result
}
func (f backendFixture) json(method, path string, body any, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
	data, _ := json.Marshal(body)
	return f.request(method, path, "application/json", data, cookie, csrf)
}
func (f backendFixture) login(t *testing.T, email, password string) (*http.Cookie, string, User) {
	t.Helper()
	res := f.json("POST", "/api/login", map[string]string{"email": email, "password": password}, nil, "")
	if res.Code != 200 {
		t.Fatalf("login status %d", res.Code)
	}
	var result struct {
		User User   `json:"user"`
		CSRF string `json:"csrf_token"`
	}
	if json.Unmarshal(res.Body.Bytes(), &result) != nil {
		t.Fatal("decode session")
	}
	cookies := res.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing session cookie")
	}
	return cookies[0], result.CSRF, result.User
}
func (f backendFixture) collect(t *testing.T, actor, w string, run workbench.Run) {
	t.Helper()
	var fence int64
	if err := f.admin.QueryRow(f.ctx, `SELECT fence FROM wb_runs WHERE id=$1`, run.ID).Scan(&fence); err != nil {
		t.Fatal(err)
	}
	err := workbench.New(f.app).Collect(f.ctx, workbench.CollectionArgs{SchemaVersion: 1, RunID: run.ID, ConnectionID: run.ConnectionID, WorkspaceID: w, Generation: run.Generation, ActorUserID: actor, Fence: fence})
	if err != nil {
		t.Fatal(err)
	}
}

func TestReviewerLifecycleAndDirectAPIBoundaries(t *testing.T) {
	f := newBackendFixture(t)
	first, err := store.CreateReviewer(f.ctx, f.admin, store.ReviewerInput{Email: "reviewer-one@example.test", DisplayName: "Reviewer one", Password: "Initial reviewer one passphrase"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateReviewer(f.ctx, f.admin, store.ReviewerInput{Email: "reviewer-two@example.test", DisplayName: strings.Repeat("R", 100), Password: "Initial reviewer two passphrase"})
	if err != nil {
		t.Fatal(err)
	}
	svc := workbench.New(f.app)
	run, err := svc.Seed(f.ctx, first.UserID, first.WorkspaceID, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, first.UserID, first.WorkspaceID, run)
	cookie, csrf, user := f.login(t, "reviewer-one@example.test", "Initial reviewer one passphrase")
	if !user.MustChangePassword {
		t.Fatal("temporary password was not flagged")
	}
	path := "/api/v1/workspaces/" + first.WorkspaceID
	if r := f.json("GET", path+"/overview", nil, cookie, ""); r.Code != 403 {
		t.Fatal("first-use password change was bypassed")
	}
	if r := f.json("POST", "/api/password", map[string]string{"current_password": "Initial reviewer one passphrase", "new_password": "New reviewer one unique passphrase"}, cookie, ""); r.Code != 403 {
		t.Fatal("password change accepted without CSRF")
	}
	if r := f.json("POST", "/api/password", map[string]string{"current_password": "Initial reviewer one passphrase", "new_password": "New reviewer one unique passphrase"}, cookie, csrf); r.Code != 200 {
		t.Fatalf("change password %d %s", r.Code, r.Body.String())
	}
	if r := f.json("GET", path+"/overview", nil, cookie, ""); r.Code != 200 || !strings.Contains(r.Body.String(), "SRC-ATLAS") {
		t.Fatal("reviewer cannot see own seeded orders")
	}
	for _, foreign := range []string{second.WorkspaceID, f.owner.PrivateWorkspaceID, f.owner.PresentationWorkspaceID} {
		if r := f.json("GET", "/api/v1/workspaces/"+foreign+"/overview", nil, cookie, ""); r.Code != 404 {
			t.Fatalf("foreign workspace returned %d", r.Code)
		}
		if r := f.json("POST", "/api/v1/workspaces/"+foreign+"/refresh", map[string]int{"generation": 1}, cookie, csrf); r.Code != 404 {
			t.Fatal("foreign mutation authorized")
		}
		foreignPath := "/api/v1/workspaces/" + foreign
		for _, suffix := range []string{"/orders", "/priorities", "/connections", "/notifications", "/quotes", "/suppliers", "/runs", "/snapshots", "/orders/00000000-0000-4000-8000-000000000001", "/orders/00000000-0000-4000-8000-000000000001/history", "/runs/00000000-0000-4000-8000-000000000001", "/imports/internal-orders/template.csv"} {
			if r := f.json("GET", foreignPath+suffix, nil, cookie, ""); r.Code != 404 {
				t.Fatalf("foreign %s returned %d", suffix, r.Code)
			}
		}
		for _, suffix := range []string{"/seed", "/demo/scenarios/SC02", "/demo/reset", "/notifications/00000000-0000-4000-8000-000000000001/read", "/exceptions/00000000-0000-4000-8000-000000000001/notes"} {
			if r := f.json("POST", foreignPath+suffix, map[string]any{"generation": 1, "revision": 1, "idempotency_key": "boundary-unique-key", "confirm": true, "body": "test note"}, cookie, csrf); r.Code != 404 {
				t.Fatalf("foreign mutation %s returned %d", suffix, r.Code)
			}
		}
		if r := f.json("PATCH", foreignPath+"/exceptions/00000000-0000-4000-8000-000000000001/workflow", map[string]any{"generation": 1, "revision": 1, "acknowledged": true, "status": "open"}, cookie, csrf); r.Code != 404 {
			t.Fatalf("foreign workflow returned %d", r.Code)
		}
	}
	for _, field := range []string{"role", "origin", "workspace_id", "actor_user_id"} {
		if r := f.json("POST", path+"/refresh", map[string]any{"generation": 1, field: "owner"}, cookie, csrf); r.Code != 400 {
			t.Fatalf("client supplied %s accepted", field)
		}
	}
	own, err := svc.Overview(f.ctx, first.UserID, first.WorkspaceID)
	if err != nil || len(own.Priorities) == 0 {
		t.Fatal("own condition unavailable")
	}
	condition := own.Priorities[0]
	if saved := f.json("POST", path+"/exceptions/"+condition.ID+"/notes", map[string]any{"generation": 1, "revision": condition.Revision, "body": strings.Repeat("界", 3000)}, cookie, csrf); saved.Code != 201 {
		t.Fatalf("valid Unicode note rejected: %d", saved.Code)
	}
	var deniedAudits int
	if err = f.admin.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE actor_user_id=$1 AND action='request.denied'`, first.UserID).Scan(&deniedAudits); err != nil || deniedAudits < 6 {
		t.Fatal("authenticated access denials were not durably audited")
	}
	if r := f.json("POST", "/api/v1/admin/sanitized-exports/preview", map[string]string{}, cookie, csrf); r.Code != 404 {
		t.Fatal("reviewer reached private sanitizer")
	}
	if _, err = store.CreateReviewer(f.ctx, f.admin, store.ReviewerInput{Email: "reviewer-one@example.test", DisplayName: "Changed", Password: "Unwanted replacement passphrase"}); err != nil {
		t.Fatal(err)
	}
	_, _, _ = f.login(t, "reviewer-one@example.test", "New reviewer one unique passphrase")
	if _, err = f.admin.Exec(f.ctx, `UPDATE users SET expires_at=now()-interval '1 minute' WHERE id=$1`, first.UserID); err != nil {
		t.Fatal(err)
	}
	if r := f.json("GET", path+"/overview", nil, cookie, ""); r.Code != 401 {
		t.Fatal("expired account still authenticated")
	}
	if _, err = f.admin.Exec(f.ctx, `UPDATE users SET expires_at=now()+interval '1 day' WHERE id=$1`, first.UserID); err != nil {
		t.Fatal(err)
	}
	if err = store.DisableReviewer(f.ctx, f.admin, first.UserID); err != nil {
		t.Fatal(err)
	}
	if r := f.json("GET", path+"/overview", nil, cookie, ""); r.Code != 401 {
		t.Fatal("disabled account still authenticated")
	}
	if err = store.DisableReviewer(f.ctx, f.admin, f.owner.OwnerID); err == nil {
		t.Fatal("reviewer disable command disabled owner")
	}
}

func TestCSVPreviewPublicationAndSnapshotCanaryBoundary(t *testing.T) {
	f := newBackendFixture(t)
	cookie, csrf, _ := f.login(t, "backend-owner@example.test", "Backend owner test passphrase")
	svc := workbench.New(f.app)
	run, err := svc.Seed(f.ctx, f.owner.OwnerID, f.owner.PresentationWorkspaceID, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, f.owner.OwnerID, f.owner.PresentationWorkspaceID, run)
	path := "/api/v1/workspaces/" + f.owner.PresentationWorkspaceID
	contract := f.json("GET", "/api/v1/openapi.json", nil, cookie, "")
	if contract.Code != 200 || !strings.Contains(contract.Body.String(), `"Overview"`) || !strings.Contains(contract.Body.String(), "/api/v1/workspaces/{w}/refresh") {
		t.Fatal("generated handler contract is incomplete")
	}
	firstPage := f.json("GET", path+"/orders?limit=1", nil, cookie, "")
	var page struct {
		Orders []workbench.Order `json:"orders"`
		Next   string            `json:"next_cursor"`
	}
	if err = json.Unmarshal(firstPage.Body.Bytes(), &page); err != nil || len(page.Orders) != 1 || page.Next == "" {
		t.Fatal("bounded list did not return a cursor")
	}
	firstID := page.Orders[0].ID
	secondPage := f.json("GET", path+"/orders?limit=1&cursor="+page.Next, nil, cookie, "")
	if err = json.Unmarshal(secondPage.Body.Bytes(), &page); err != nil || len(page.Orders) != 1 || page.Orders[0].ID == firstID {
		t.Fatal("cursor repeated or skipped list page")
	}
	var csv bytes.Buffer
	requirements := domain.SeedRequirements()
	requirements[0].RequiredDate = "2026-10-20"
	_ = imports.Template(&csv, requirements)
	preview := f.request("POST", path+"/imports/internal-orders/preview", "text/csv", csv.Bytes(), cookie, csrf)
	if preview.Code != 201 {
		t.Fatalf("preview %d %s", preview.Code, preview.Body.String())
	}
	var staged struct {
		ID         string `json:"id"`
		Generation int64  `json:"generation"`
	}
	_ = json.Unmarshal(preview.Body.Bytes(), &staged)
	before, err := svc.Overview(f.ctx, f.owner.OwnerID, f.owner.PresentationWorkspaceID)
	if err != nil || before.Orders[0].Requirement.RequiredDate == "2026-10-20" {
		t.Fatal("preview altered current requirements")
	}
	publish := f.json("POST", path+"/imports/internal-orders/"+staged.ID+"/publish", map[string]any{"generation": 1, "confirm": true}, cookie, csrf)
	if publish.Code != 202 {
		t.Fatalf("publish %d %s", publish.Code, publish.Body.String())
	}
	_ = json.Unmarshal(publish.Body.Bytes(), &run)
	f.collect(t, f.owner.OwnerID, f.owner.PresentationWorkspaceID, run)
	after, err := svc.Overview(f.ctx, f.owner.OwnerID, f.owner.PresentationWorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	risk := false
	for _, condition := range after.Priorities {
		if condition.Rule == "R01" && condition.Active {
			risk = true
		}
	}
	if !risk {
		t.Fatal("published requirement did not drive pipeline rules")
	}
	// Private fixture canaries exercise the sanitizer; this is not a live collection.
	record := domain.SeedRecords()[0]
	record.Origin = "live_private"
	record.SourceKey = "canary-private-account"
	record.SourceOrderID = "canary-private-order"
	record.Description = "canary-owner@example.test"
	record.SupplierPart = "canary-serial-123"
	recordJSON, _ := json.Marshal(record)
	privateConnection, err := svc.CreateConnection(f.ctx, f.owner.OwnerID, f.owner.PrivateWorkspaceID, 1, "private-canary-connection", workbench.ConnectionInput{Name: "Private fixture connection", Method: "api_polling"})
	if err != nil {
		t.Fatal(err)
	}
	var privateID string
	err = f.admin.QueryRow(f.ctx, `INSERT INTO wb_orders(workspace_id,generation,connection_id,source_key,record,first_record,first_observed_at,last_observed_at) VALUES($1,1,$2,$3,$4,$4,now(),now()) RETURNING id::text`, f.owner.PrivateWorkspaceID, privateConnection.ID, record.SourceKey, recordJSON).Scan(&privateID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.admin.Exec(f.ctx, `UPDATE sessions SET reauthenticated_at=now()-interval '6 minutes' WHERE user_id=$1`, f.owner.OwnerID); err != nil {
		t.Fatal(err)
	}
	exportInput := map[string]any{"workspace_id": f.owner.PrivateWorkspaceID, "source_record_id": privateID, "generation": 1, "safe_description": "Generic computer component", "include_status": true, "include_quantities": true, "include_arrival": true}
	if denied := f.json("POST", "/api/v1/admin/sanitized-exports/preview", exportInput, cookie, csrf); denied.Code != 403 {
		t.Fatal("private sharing accepted without recent credential confirmation")
	}
	if confirmed := f.json("POST", "/api/reauth", map[string]string{"password": "Backend owner test passphrase"}, cookie, csrf); confirmed.Code != 204 {
		t.Fatalf("reauthenticate returned %d", confirmed.Code)
	}
	export := f.json("POST", "/api/v1/admin/sanitized-exports/preview", exportInput, cookie, csrf)
	if export.Code != 201 {
		t.Fatalf("snapshot preview %d %s", export.Code, export.Body.String())
	}
	var snapshot struct {
		ID string `json:"export_id"`
	}
	_ = json.Unmarshal(export.Body.Bytes(), &snapshot)
	shared := f.json("GET", path+"/snapshots", nil, cookie, "")
	if strings.Contains(shared.Body.String(), "Generic computer") {
		t.Fatal("unapproved preview published")
	}
	approved := f.json("POST", "/api/v1/admin/sanitized-exports/"+snapshot.ID+"/publish", map[string]any{"target_workspace_id": f.owner.PresentationWorkspaceID, "generation": 1, "approve": true}, cookie, csrf)
	if approved.Code != 200 {
		t.Fatalf("approve %d %s", approved.Code, approved.Body.String())
	}
	shared = f.json("GET", path+"/snapshots", nil, cookie, "")
	if shared.Code != 200 || !strings.Contains(shared.Body.String(), "sanitized_snapshot") {
		t.Fatal("approved snapshot missing")
	}
	for _, canary := range []string{privateID, snapshot.ID, record.SourceKey, record.SourceOrderID, record.Description, record.SupplierPart, f.owner.PrivateWorkspaceID} {
		if strings.Contains(shared.Body.String(), canary) {
			t.Fatalf("private lineage or canary crossed public boundary: %s", fmt.Sprintf("%d-byte marker", len(canary)))
		}
	}
	clone, err := store.CreateReviewer(f.ctx, f.admin, store.ReviewerInput{Email: "snapshot-reviewer@example.test", DisplayName: "Snapshot reviewer", Password: "Snapshot reviewer unique passphrase"})
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CopyApprovedSnapshots(f.ctx, f.admin, clone.WorkspaceID); err != nil {
		t.Fatal(err)
	}
	var copies int
	if err = f.admin.QueryRow(f.ctx, `SELECT count(*) FROM wb_snapshot_copies WHERE workspace_id=$1`, clone.WorkspaceID).Scan(&copies); err != nil || copies != 1 {
		t.Fatal("explicit reviewer clone omitted approved safe snapshot")
	}
}
