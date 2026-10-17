package httpapi

import (
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

	"buyersworkbench/internal/auth"
	"buyersworkbench/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
)

// This test requires a disposable migrated database and explicitly exercises
// the constrained runtime role, rather than an administrator-backed API.
func TestDatabaseSessionAndWorkspaceBoundary(t *testing.T) {
	adminURL, appURL := os.Getenv("BWB_TEST_DATABASE_URL"), os.Getenv("BWB_TEST_APP_DATABASE_URL")
	if adminURL == "" || appURL == "" {
		t.Skip("set BWB_TEST_DATABASE_URL and BWB_TEST_APP_DATABASE_URL for disposable database integration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgxpool.New(ctx, adminURL)
	if err != nil {
		t.Fatal("open administrator test pool")
	}
	t.Cleanup(admin.Close)
	app, err := pgxpool.New(ctx, appURL)
	if err != nil {
		t.Fatal("open runtime test pool")
	}
	t.Cleanup(app.Close)
	if err := migrations.Check(ctx, admin); err != nil {
		t.Fatal("test database must already be migrated")
	}
	var super, bypass, editUsers, editIdentities, createSchema, readAudit bool
	err = app.QueryRow(ctx, `SELECT r.rolsuper,r.rolbypassrls,
		has_table_privilege(current_user,'users','UPDATE'),has_table_privilege(current_user,'identities','UPDATE'),
		has_schema_privilege(current_user,'public','CREATE'),has_table_privilege(current_user,'audit_events','SELECT')
		FROM pg_roles r WHERE r.rolname=current_user`).Scan(&super, &bypass, &editUsers, &editIdentities, &createSchema, &readAudit)
	if err != nil || super || bypass || editUsers || editIdentities || createSchema || readAudit {
		t.Fatal("API test role is not constrained")
	}
	var prefix string
	if err = admin.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&prefix); err != nil {
		t.Fatal("generate test identity")
	}
	password := "database-test-password-unique-" + prefix
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	userIDs := make([]string, 2)
	workspaceIDs := make([]string, 2)
	for i := range userIDs {
		if err = admin.QueryRow(ctx, `INSERT INTO users(id,display_name) VALUES(gen_random_uuid(),$1) RETURNING id::text`, fmt.Sprintf("Boundary user %d", i)).Scan(&userIDs[i]); err != nil {
			t.Fatal("create test user")
		}
		if _, err = admin.Exec(ctx, `INSERT INTO identities(id,user_id,provider,issuer,subject,password_hash) VALUES(gen_random_uuid(),$1,'local','local',$2,$3)`, userIDs[i], fmt.Sprintf("boundary-%d-%s@example.test", i, prefix), hash); err != nil {
			t.Fatal("create test identity")
		}
		if err = admin.QueryRow(ctx, `INSERT INTO workspaces(id,kind,owner_user_id,name,seed_version,business_date) VALUES(gen_random_uuid(),'reviewer',$1,$2,'test-foundation','2026-10-19') RETURNING id::text`, userIDs[i], fmt.Sprintf("Boundary workspace %d", i)).Scan(&workspaceIDs[i]); err != nil {
			t.Fatal("create test workspace")
		}
		if _, err = admin.Exec(ctx, `INSERT INTO memberships(workspace_id,user_id,role) VALUES($1,$2,'reviewer')`, workspaceIDs[i], userIDs[i]); err != nil {
			t.Fatal("create test membership")
		}
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, id := range userIDs {
			for _, sql := range []string{`DELETE FROM sessions WHERE user_id=$1`, `DELETE FROM memberships WHERE user_id=$1`, `DELETE FROM workspaces WHERE owner_user_id=$1`, `DELETE FROM identities WHERE user_id=$1`, `DELETE FROM audit_events WHERE actor_user_id=$1`, `DELETE FROM users WHERE id=$1`} {
				if _, err := admin.Exec(cleanupCtx, sql, id); err != nil {
					t.Error("clean up test identity failed")
					break
				}
			}
		}
	})
	h, err := New(app, Config{Origin: "http://127.0.0.1:4180"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string, cookie *http.Cookie, csrf string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Origin", "http://127.0.0.1:4180")
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-CSRF-Token", csrf)
		if cookie != nil {
			r.AddCookie(cookie)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	email := fmt.Sprintf("boundary-0-%s@example.test", prefix)
	loginBody, _ := json.Marshal(map[string]string{"email": email, "password": password})
	login := func() (*http.Cookie, string) {
		w := request("POST", "/api/login", string(loginBody), nil, "")
		if w.Code != 200 {
			t.Fatalf("sign-in failed: %d %s", w.Code, w.Body.String())
		}
		var body struct {
			User User   `json:"user"`
			CSRF string `json:"csrf_token"`
		}
		if json.Unmarshal(w.Body.Bytes(), &body) != nil || body.User.ID != userIDs[0] || body.User.Role != "reviewer" || !validToken(body.CSRF) {
			t.Fatal("invalid authenticated response")
		}
		cookies := w.Result().Cookies()
		if len(cookies) != 1 {
			t.Fatal("missing session cookie")
		}
		return cookies[0], body.CSRF
	}
	knownBad := request("POST", "/api/login", fmt.Sprintf(`{"email":%q,"password":"incorrect"}`, email), nil, "")
	unknownBad := request("POST", "/api/login", `{"email":"unknown@example.test","password":"incorrect"}`, nil, "")
	if knownBad.Code != 401 || unknownBad.Code != 401 || knownBad.Body.String() != unknownBad.Body.String() {
		t.Fatal("credential errors must be generic")
	}
	cookie, csrf := login()
	w := request("GET", "/readyz", "", nil, "")
	if w.Code != 200 {
		t.Fatalf("runtime readiness failed: %d", w.Code)
	}
	w = request("GET", "/api/session", "", cookie, "")
	if w.Code != 200 {
		t.Fatal("session did not persist")
	}
	w = request("GET", "/api/workspaces", "", cookie, "")
	var list struct {
		Workspaces []Workspace `json:"workspaces"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &list) != nil || len(list.Workspaces) != 1 || list.Workspaces[0].ID != workspaceIDs[0] || list.Workspaces[0].DataOrigin != "fictional" {
		t.Fatalf("workspace list exposed another scope: %d %s", w.Code, w.Body.String())
	}
	if w = request("GET", "/api/workspaces/"+workspaceIDs[0], "", cookie, ""); w.Code != 200 {
		t.Fatal("own workspace inaccessible")
	}
	foreign := request("GET", "/api/workspaces/"+workspaceIDs[1], "", cookie, "")
	missing := request("GET", "/api/workspaces/00000000-0000-0000-0000-000000000000", "", cookie, "")
	if foreign.Code != 404 || missing.Code != 404 || foreign.Body.String() != missing.Body.String() {
		t.Fatal("foreign workspace detail must not be disclosed")
	}
	if w = request("POST", "/api/logout", "{}", cookie, ""); w.Code != 403 {
		t.Fatal("logout accepted without CSRF")
	}
	if w = request("POST", "/api/logout", "{}", cookie, csrf); w.Code != 204 {
		t.Fatal("logout failed")
	}
	if w = request("GET", "/api/session", "", cookie, ""); w.Code != 401 {
		t.Fatal("logout failed to revoke the server session")
	}
	cookie, _ = login()
	if _, err = admin.Exec(ctx, `UPDATE sessions SET last_activity_at=now()-interval '31 minutes' WHERE token_hash=$1`, tokenHash(cookie.Value)); err != nil {
		t.Fatal("age idle session")
	}
	if w = request("GET", "/api/session", "", cookie, ""); w.Code != 401 {
		t.Fatal("idle session did not expire")
	}
	cookie, _ = login()
	if _, err = admin.Exec(ctx, `UPDATE sessions SET created_at=now()-interval '13 hours' WHERE token_hash=$1`, tokenHash(cookie.Value)); err != nil {
		t.Fatal("age absolute session")
	}
	if w = request("GET", "/api/session", "", cookie, ""); w.Code != 401 {
		t.Fatal("absolute session did not expire")
	}
	cookie, _ = login()
	if _, err = admin.Exec(ctx, `UPDATE users SET active=false,disabled_at=now() WHERE id=$1`, userIDs[0]); err != nil {
		t.Fatal("disable test user")
	}
	if w = request("GET", "/api/session", "", cookie, ""); w.Code != 401 {
		t.Fatal("disabled account retained session access")
	}
	if w = request("POST", "/api/login", string(loginBody), nil, ""); w.Code != 401 {
		t.Fatal("disabled account signed in")
	}
}
