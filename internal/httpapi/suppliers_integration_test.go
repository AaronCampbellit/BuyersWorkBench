package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"buyersworkbench/internal/store"
	"buyersworkbench/internal/workbench"
)

type supplierAPIRecord struct {
	ID               string `json:"id"`
	Alias            string `json:"alias"`
	Name             string `json:"name"`
	ContactName      string `json:"contact_name"`
	Email            string `json:"email"`
	Phone            string `json:"phone"`
	Website          string `json:"website"`
	Notes            string `json:"notes"`
	Status           string `json:"status"`
	Revision         int64  `json:"revision"`
	OrderCount       int    `json:"order_count"`
	ActiveConditions int    `json:"active_conditions"`
}

func supplierAPIInput(generation int64, name string) map[string]any {
	return map[string]any{
		"generation": generation, "name": name, "contact_name": "Local contact",
		"email": "contact@example.test", "phone": "+1 555 0100", "website": "https://supplier.example.test",
		"notes": "Manually entered local contact details.", "status": "active",
	}
}

func decodeSupplierAPI(t *testing.T, res *httptest.ResponseRecorder, status int) supplierAPIRecord {
	t.Helper()
	if res.Code != status {
		t.Fatalf("supplier status %d, want %d: %s", res.Code, status, res.Body.String())
	}
	var record supplierAPIRecord
	if err := json.Unmarshal(res.Body.Bytes(), &record); err != nil || !uuidPattern.MatchString(record.ID) || record.Revision < 1 || record.Alias == "" {
		t.Fatal("supplier response omitted its stable identity or revision")
	}
	return record
}

func listSupplierAPI(t *testing.T, f backendFixture, path string, cookie *http.Cookie) ([]supplierAPIRecord, int64) {
	t.Helper()
	res := f.json(http.MethodGet, path, nil, cookie, "")
	if res.Code != http.StatusOK {
		t.Fatalf("supplier list returned %d: %s", res.Code, res.Body.String())
	}
	var page struct {
		Suppliers  []supplierAPIRecord `json:"suppliers"`
		Generation int64               `json:"generation"`
	}
	if json.Unmarshal(res.Body.Bytes(), &page) != nil {
		t.Fatal("decode supplier list")
	}
	return page.Suppliers, page.Generation
}

func findSupplierAPI(t *testing.T, records []supplierAPIRecord, id string) supplierAPIRecord {
	t.Helper()
	for _, record := range records {
		if record.ID == id {
			return record
		}
	}
	t.Fatal("persisted supplier missing from its workspace")
	return supplierAPIRecord{}
}

func TestSupplierAPIPersistencePrivateScopeAndReset(t *testing.T) {
	f := newBackendFixture(t)
	cookie, csrf, _ := f.login(t, "backend-owner@example.test", "Backend owner test passphrase")
	svc := workbench.New(f.app)
	run, err := svc.Seed(f.ctx, f.owner.OwnerID, f.owner.PresentationWorkspaceID, 1)
	if err != nil {
		t.Fatal("seed supplier fixture")
	}
	f.collect(t, f.owner.OwnerID, f.owner.PresentationWorkspaceID, run)
	base := "/api/v1/workspaces/" + f.owner.PresentationWorkspaceID
	path := base + "/suppliers"
	seeded, generation := listSupplierAPI(t, f, path, cookie)
	if len(seeded) == 0 || generation != 1 {
		t.Fatal("seeded supplier fixture missing")
	}
	var seed supplierAPIRecord
	for _, candidate := range seeded {
		if candidate.OrderCount > 0 {
			seed = candidate
			break
		}
	}
	if !uuidPattern.MatchString(seed.ID) || seed.Alias == "" || seed.OrderCount == 0 {
		t.Fatal("seeded supplier missing stable identity or observed order association")
	}
	manualInput := supplierAPIInput(1, "New locally entered supplier")
	manual := decodeSupplierAPI(t, f.json(http.MethodPost, path, manualInput, cookie, csrf), http.StatusCreated)
	if manual.Name != manualInput["name"] || manual.ContactName != manualInput["contact_name"] || manual.Email != manualInput["email"] || manual.Website != manualInput["website"] || manual.Phone != manualInput["phone"] || manual.Notes != manualInput["notes"] {
		t.Fatal("create response lost entered supplier details")
	}
	if manual.OrderCount != 0 || manual.ActiveConditions != 0 {
		t.Fatal("manual supplier invented purchasing activity")
	}
	seedInput := supplierAPIInput(1, "Edited display name for seeded supplier")
	seedInput["revision"] = seed.Revision
	seedInput["contact_name"] = "Updated seed contact"
	editedSeed := decodeSupplierAPI(t, f.json(http.MethodPatch, path+"/"+seed.ID, seedInput, cookie, csrf), http.StatusOK)
	if editedSeed.ID != seed.ID || editedSeed.Alias != seed.Alias || editedSeed.Name != seedInput["name"] || editedSeed.Revision <= seed.Revision {
		t.Fatal("editing seeded supplier changed its source identity or failed to advance revision")
	}
	manualInput["revision"] = manual.Revision
	manualInput["name"] = "Edited manual supplier"
	manualInput["contact_name"] = "Updated manual contact"
	manualInput["status"] = "inactive"
	editedManual := decodeSupplierAPI(t, f.json(http.MethodPatch, path+"/"+manual.ID, manualInput, cookie, csrf), http.StatusOK)
	refresh := f.json(http.MethodPost, base+"/refresh", map[string]any{"generation": 1}, cookie, csrf)
	if refresh.Code != http.StatusAccepted || json.Unmarshal(refresh.Body.Bytes(), &run) != nil {
		t.Fatal("queue supplier refresh")
	}
	f.collect(t, f.owner.OwnerID, f.owner.PresentationWorkspaceID, run)
	after, _ := listSupplierAPI(t, f, path, cookie)
	persistedSeed := findSupplierAPI(t, after, seed.ID)
	persistedManual := findSupplierAPI(t, after, manual.ID)
	if persistedSeed.Alias != seed.Alias || persistedSeed.Name != editedSeed.Name || persistedSeed.ContactName != editedSeed.ContactName || persistedSeed.OrderCount != seed.OrderCount {
		t.Fatal("collection overwrote supplier profile or broke its source order association")
	}
	if persistedManual.Name != editedManual.Name || persistedManual.ContactName != editedManual.ContactName || persistedManual.Status != "inactive" || persistedManual.Revision != editedManual.Revision || persistedManual.OrderCount != 0 || persistedManual.ActiveConditions != 0 {
		t.Fatal("collection changed manually maintained supplier state")
	}
	privatePath := "/api/v1/workspaces/" + f.owner.PrivateWorkspaceID + "/suppliers"
	private := decodeSupplierAPI(t, f.json(http.MethodPost, privatePath, supplierAPIInput(1, "Private manual supplier"), cookie, csrf), http.StatusCreated)
	privateRows, privateGeneration := listSupplierAPI(t, f, privatePath, cookie)
	if privateGeneration != 1 || len(privateRows) != 1 || privateRows[0].ID != private.ID || privateRows[0].OrderCount != 0 {
		t.Fatal("private manual supplier requires invented purchasing data")
	}
	var connections int
	if err := f.admin.QueryRow(f.ctx, "SELECT count(*) FROM wb_connections WHERE workspace_id=$1", f.owner.PrivateWorkspaceID).Scan(&connections); err != nil || connections != 0 {
		t.Fatal("maintaining private supplier contacts created a source connection")
	}
	reset := f.json(http.MethodPost, base+"/demo/reset", map[string]any{"generation": 1, "confirm": true, "idempotency_key": "supplier-reset-boundary"}, cookie, csrf)
	var result workbench.ResetResult
	if reset.Code != http.StatusAccepted || json.Unmarshal(reset.Body.Bytes(), &result) != nil || result.Generation != 2 {
		t.Fatal("reset supplier workspace")
	}
	f.collect(t, f.owner.OwnerID, f.owner.PresentationWorkspaceID, result.Run)
	resetRows, resetGeneration := listSupplierAPI(t, f, path, cookie)
	if resetGeneration != 2 || len(resetRows) != len(seeded) {
		t.Fatal("reset did not restore the pinned supplier seed")
	}
	for _, record := range resetRows {
		if record.ID == manual.ID || record.Name == editedSeed.Name || record.Name == editedManual.Name {
			t.Fatal("reset retained prior-generation supplier edits")
		}
	}
	privateRows, privateGeneration = listSupplierAPI(t, f, privatePath, cookie)
	if privateGeneration != 1 || len(privateRows) != 1 || privateRows[0].ID != private.ID || privateRows[0].Name != private.Name {
		t.Fatal("demo reset modified private supplier contacts")
	}
	if stale := f.json(http.MethodPost, path, supplierAPIInput(1, "Stale browser supplier"), cookie, csrf); stale.Code != http.StatusConflict {
		t.Fatalf("stale generation accepted after reset: %d", stale.Code)
	}
}

func TestSupplierAPIAuthorizationValidationAndConflicts(t *testing.T) {
	f := newBackendFixture(t)
	cookie, csrf, _ := f.login(t, "backend-owner@example.test", "Backend owner test passphrase")
	path := "/api/v1/workspaces/" + f.owner.PresentationWorkspaceID + "/suppliers"
	created := decodeSupplierAPI(t, f.json(http.MethodPost, path, supplierAPIInput(1, "Original contact name"), cookie, csrf), http.StatusCreated)
	input := supplierAPIInput(1, "Accepted revision")
	input["revision"] = created.Revision
	edited := decodeSupplierAPI(t, f.json(http.MethodPatch, path+"/"+created.ID, input, cookie, csrf), http.StatusOK)
	if res := f.json(http.MethodPatch, path+"/"+created.ID, input, cookie, csrf); res.Code != http.StatusConflict {
		t.Fatalf("stale supplier edit silently overwrote accepted data: %d", res.Code)
	}
	for _, tc := range []struct {
		name, field string
		value       any
	}{
		{"blank name", "name", "  "},
		{"invalid email", "email", "not-an-email"},
		{"unsafe website", "website", "javascript:alert(1)"},
		{"stale generation", "generation", int64(2)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			invalid := supplierAPIInput(1, "Rejected supplier")
			invalid[tc.field] = tc.value
			want := http.StatusUnprocessableEntity
			if tc.field == "generation" {
				want = http.StatusConflict
			}
			if res := f.json(http.MethodPost, path, invalid, cookie, csrf); res.Code != want {
				t.Fatalf("invalid supplier returned %d, want %d", res.Code, want)
			}
		})
	}
	if res := f.json(http.MethodPost, path, supplierAPIInput(1, "Missing CSRF"), cookie, ""); res.Code != http.StatusForbidden {
		t.Fatal("supplier creation accepted without CSRF")
	}
	badOriginInput, _ := json.Marshal(supplierAPIInput(1, "Wrong Origin"))
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(badOriginInput))
	req.Header.Set("Origin", "https://untrusted.example.test")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	req.AddCookie(cookie)
	wrongOrigin := httptest.NewRecorder()
	f.handler.ServeHTTP(wrongOrigin, req)
	if wrongOrigin.Code != http.StatusForbidden {
		t.Fatal("supplier creation accepted a foreign Origin")
	}
	unchanged, _ := listSupplierAPI(t, f, path, cookie)
	if len(unchanged) != 1 || unchanged[0].ID != edited.ID || unchanged[0].Name != edited.Name || unchanged[0].Revision != edited.Revision {
		t.Fatal("rejected requests changed supplier data")
	}
	privatePath := "/api/v1/workspaces/" + f.owner.PrivateWorkspaceID + "/suppliers"
	private := decodeSupplierAPI(t, f.json(http.MethodPost, privatePath, supplierAPIInput(1, "Foreign workspace UUID marker"), cookie, csrf), http.StatusCreated)
	input["revision"] = private.Revision
	if res := f.json(http.MethodPatch, path+"/"+private.ID, input, cookie, csrf); res.Code != http.StatusNotFound || strings.Contains(res.Body.String(), private.Name) {
		t.Fatal("supplier UUID escaped its workspace boundary")
	}
	reviewer, err := store.CreateReviewer(f.ctx, f.admin, store.ReviewerInput{Email: "supplier-reviewer@example.test", DisplayName: "Supplier reviewer", Password: "Initial supplier reviewer passphrase"})
	if err != nil {
		t.Fatal("create read-only supplier reviewer")
	}
	svc := workbench.New(f.app)
	run, err := svc.Seed(f.ctx, reviewer.UserID, reviewer.WorkspaceID, 1)
	if err != nil {
		t.Fatal("seed reviewer supplier fixture")
	}
	f.collect(t, reviewer.UserID, reviewer.WorkspaceID, run)
	reviewerCookie, reviewerCSRF, _ := f.login(t, "supplier-reviewer@example.test", "Initial supplier reviewer passphrase")
	changed := f.json(http.MethodPost, "/api/password", map[string]string{"current_password": "Initial supplier reviewer passphrase", "new_password": "Changed supplier reviewer passphrase"}, reviewerCookie, reviewerCSRF)
	if changed.Code != http.StatusOK {
		t.Fatal("complete reviewer first-use password change")
	}
	reviewerPath := "/api/v1/workspaces/" + reviewer.WorkspaceID + "/suppliers"
	reviewerSuppliers, _ := listSupplierAPI(t, f, reviewerPath, reviewerCookie)
	if len(reviewerSuppliers) == 0 {
		t.Fatal("reviewer cannot read own supplier list")
	}
	if res := f.json(http.MethodPost, reviewerPath, supplierAPIInput(1, "Reviewer attempted creation"), reviewerCookie, reviewerCSRF); res.Code != http.StatusNotFound {
		t.Fatalf("reviewer supplier creation returned %d, want owner-only denial", res.Code)
	}
	reviewerEdit := supplierAPIInput(1, "Reviewer attempted edit")
	reviewerEdit["revision"] = reviewerSuppliers[0].Revision
	if res := f.json(http.MethodPatch, reviewerPath+"/"+reviewerSuppliers[0].ID, reviewerEdit, reviewerCookie, reviewerCSRF); res.Code != http.StatusNotFound {
		t.Fatalf("reviewer supplier edit returned %d, want owner-only denial", res.Code)
	}
	for _, foreignPath := range []string{path, privatePath} {
		if res := f.json(http.MethodGet, foreignPath, nil, reviewerCookie, ""); res.Code != http.StatusNotFound {
			t.Fatal("reviewer read foreign supplier workspace")
		}
		if res := f.json(http.MethodPost, foreignPath, supplierAPIInput(1, "Foreign reviewer creation"), reviewerCookie, reviewerCSRF); res.Code != http.StatusNotFound {
			t.Fatalf("foreign supplier mutation disclosed scope: %d", res.Code)
		}
	}
}
