package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"buyersworkbench/internal/store"
	"buyersworkbench/internal/workbench"
)

func TestNotificationPreferencesAPIDefaultRevisionCSRFAndScope(t *testing.T) {
	f := newBackendFixture(t)
	cookie, csrf, _ := f.login(t, "backend-owner@example.test", "Backend owner test passphrase")
	base := "/api/v1/workspaces/" + f.owner.PresentationWorkspaceID
	path := base + "/notification-preferences"
	res := f.json(http.MethodGet, path, nil, cookie, "")
	var preferences workbench.NotificationPreferences
	if res.Code != http.StatusOK || json.Unmarshal(res.Body.Bytes(), &preferences) != nil || !preferences.StatusChanges || !preferences.SupplierConditions || preferences.Revision != 0 {
		t.Fatal("GET omitted default current-member preferences", res.Code, res.Body.String())
	}
	payload := map[string]any{"generation": 1, "revision": 0, "status_changes": false, "supplier_conditions": true}
	if noCSRF := f.json(http.MethodPatch, path, payload, cookie, ""); noCSRF.Code != http.StatusForbidden {
		t.Fatal("preferences accepted without CSRF", noCSRF.Code)
	}
	res = f.json(http.MethodPatch, path, payload, cookie, csrf)
	if res.Code != http.StatusOK || json.Unmarshal(res.Body.Bytes(), &preferences) != nil || preferences.StatusChanges || !preferences.SupplierConditions || preferences.Revision != 1 {
		t.Fatal("PATCH failed to persist member preferences", res.Code, res.Body.String())
	}
	if replay := f.json(http.MethodPatch, path, payload, cookie, csrf); replay.Code != http.StatusOK {
		t.Fatal("preference retry was not idempotent", replay.Code)
	}
	payload["supplier_conditions"] = false
	if stale := f.json(http.MethodPatch, path, payload, cookie, csrf); stale.Code != http.StatusConflict {
		t.Fatal("stale revision silently overwrote preferences", stale.Code)
	}
	for _, payload := range []map[string]any{
		{"generation": 1, "status_changes": true, "supplier_conditions": true},
		{"generation": 1, "revision": 1, "status_changes": true},
		{"generation": 1, "revision": -1, "status_changes": true, "supplier_conditions": true},
	} {
		if invalid := f.json(http.MethodPatch, path, payload, cookie, csrf); invalid.Code != http.StatusUnprocessableEntity {
			t.Fatal("incomplete preference replacement was accepted", invalid.Code)
		}
	}
	private := f.json(http.MethodGet, "/api/v1/workspaces/"+f.owner.PrivateWorkspaceID+"/notification-preferences", nil, cookie, "")
	if private.Code != http.StatusOK || json.Unmarshal(private.Body.Bytes(), &preferences) != nil || !preferences.StatusChanges || preferences.Revision != 0 {
		t.Fatal("preference change crossed workspace boundary", private.Code, private.Body.String())
	}
	if anonymous := f.json(http.MethodGet, path, nil, nil, ""); anonymous.Code != http.StatusUnauthorized {
		t.Fatal("anonymous member preferences accepted", anonymous.Code)
	}
	members := f.json(http.MethodGet, base+"/members", nil, cookie, "")
	var page struct {
		Members []workbench.WorkspaceMember `json:"members"`
	}
	if members.Code != http.StatusOK || json.Unmarshal(members.Body.Bytes(), &page) != nil || len(page.Members) != 1 || page.Members[0].ID != f.owner.OwnerID || strings.Contains(members.Body.String(), "email") || strings.Contains(members.Body.String(), "@") {
		t.Fatal("workspace members omitted identities or exposed emails", members.Code, members.Body.String())
	}
}

func TestNotificationPreferencesAPIReviewerSelfOnly(t *testing.T) {
	f := newBackendFixture(t)
	reviewer, err := store.CreateReviewer(f.ctx, f.admin, store.ReviewerInput{Email: "preferences-reviewer@example.test", DisplayName: "Preferences reviewer", Password: "Initial preferences reviewer passphrase"})
	if err != nil {
		t.Fatal(err)
	}
	cookie, csrf, _ := f.login(t, "preferences-reviewer@example.test", "Initial preferences reviewer passphrase")
	changed := f.json(http.MethodPost, "/api/password", map[string]string{"current_password": "Initial preferences reviewer passphrase", "new_password": "Changed preferences reviewer passphrase"}, cookie, csrf)
	if changed.Code != http.StatusOK {
		t.Fatal("complete first-use reviewer flow", changed.Code)
	}
	payload := map[string]any{"generation": 1, "revision": 0, "status_changes": false, "supplier_conditions": false}
	path := "/api/v1/workspaces/" + reviewer.WorkspaceID + "/notification-preferences"
	res := f.json(http.MethodPatch, path, payload, cookie, csrf)
	if res.Code != http.StatusOK {
		t.Fatal("reviewer cannot edit their own notice preferences", res.Code, res.Body.String())
	}
	for _, workspace := range []string{f.owner.PresentationWorkspaceID, f.owner.PrivateWorkspaceID} {
		foreign := "/api/v1/workspaces/" + workspace
		for _, suffix := range []string{"/notification-preferences", "/members"} {
			if denied := f.json(http.MethodGet, foreign+suffix, nil, cookie, ""); denied.Code != http.StatusNotFound {
				t.Fatal("reviewer read foreign preferences or members", suffix, denied.Code)
			}
		}
		if denied := f.json(http.MethodPatch, foreign+"/notification-preferences", payload, cookie, csrf); denied.Code != http.StatusNotFound {
			t.Fatal("reviewer edited foreign preferences", denied.Code)
		}
	}
}
