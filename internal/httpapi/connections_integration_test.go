package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"buyersworkbench/internal/domain"
	"buyersworkbench/internal/imports"
	"buyersworkbench/internal/store"
	"buyersworkbench/internal/workbench"
)

func decodeConnection(t *testing.T, data []byte) workbench.Connection {
	t.Helper()
	var connection workbench.Connection
	if err := json.Unmarshal(data, &connection); err != nil || connection.ID == "" || connection.Revision < 1 {
		t.Fatal("invalid connection response", err)
	}
	return connection
}

func TestConnectionsAPIAndFictionalSourcePublication(t *testing.T) {
	f := newBackendFixture(t)
	cookie, csrf, _ := f.login(t, "backend-owner@example.test", "Backend owner test passphrase")
	svc := workbench.New(f.app)
	baseline, err := svc.Seed(f.ctx, f.owner.OwnerID, f.owner.PresentationWorkspaceID, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, f.owner.OwnerID, f.owner.PresentationWorkspaceID, baseline)
	base := "/api/v1/workspaces/" + f.owner.PresentationWorkspaceID
	input := map[string]any{"generation": 1, "idempotency_key": "http-extra-feed", "name": "Separate supplier account", "method": "fictional", "cadence_seconds": 300, "minimum_cadence_seconds": 60, "buyer_user_id": f.owner.OwnerID}
	created := f.json(http.MethodPost, base+"/connections", input, cookie, csrf)
	if created.Code != 201 {
		t.Fatal(created.Code, created.Body.String())
	}
	c := decodeConnection(t, created.Body.Bytes())
	if !c.Runnable || c.IsDemoDefault || c.Method != "fictional" {
		t.Fatal("added demo connection incorrect", c)
	}
	repeated := f.json(http.MethodPost, base+"/connections", input, cookie, csrf)
	if repeated.Code != 201 || decodeConnection(t, repeated.Body.Bytes()).ID != c.ID {
		t.Fatal("connection creation was not idempotent")
	}
	path := base + "/connections/" + c.ID
	source := map[string]any{"generation": 1, "revision": c.Revision, "idempotency_key": "http-source-baseline", "records": domain.SeedRecords()[:1], "requirements": domain.SeedRequirements()[:1]}
	staged := f.json(http.MethodPost, path+"/source", source, cookie, csrf)
	if staged.Code != 202 {
		t.Fatal(staged.Code, staged.Body.String())
	}
	var run workbench.Run
	_ = json.Unmarshal(staged.Body.Bytes(), &run)
	duplicate := f.json(http.MethodPost, path+"/source", source, cookie, csrf)
	var same workbench.Run
	_ = json.Unmarshal(duplicate.Body.Bytes(), &same)
	if duplicate.Code != 202 || same.ID != run.ID {
		t.Fatal("source retry duplicated collection", duplicate.Code, duplicate.Body.String())
	}
	f.collect(t, f.owner.OwnerID, f.owner.PresentationWorkspaceID, run)
	overview, err := svc.Overview(f.ctx, f.owner.OwnerID, f.owner.PresentationWorkspaceID)
	if err != nil || len(overview.Orders) != 5 {
		t.Fatal("source did not publish independently", len(overview.Orders), err)
	}
	var added workbench.Order
	for _, o := range overview.Orders {
		if o.ConnectionID == c.ID {
			added = o
		}
	}
	if added.ID == "" || added.SourceKey != domain.SeedRecords()[0].SourceKey {
		t.Fatal("source identity collision corrupted order")
	}
	for _, item := range overview.Connections {
		if item.ID == c.ID {
			c = item
		}
	}
	badRecords := domain.SeedRecords()[:1]
	badRecords[0].Origin = "live_private"
	rejected := f.json(http.MethodPost, path+"/source", map[string]any{"generation": 1, "revision": c.Revision, "idempotency_key": "reject-private-source", "records": badRecords}, cookie, csrf)
	if rejected.Code != 422 {
		t.Fatal("fictional source accepted private origin", rejected.Code)
	}
	update := map[string]any{"generation": 1, "revision": c.Revision, "name": "Renamed account", "method": "fictional", "minimum_cadence_seconds": 60, "cadence_seconds": 300}
	saved := f.json(http.MethodPatch, path, update, cookie, csrf)
	if saved.Code != 200 {
		t.Fatal(saved.Code, saved.Body.String())
	}
	stale := f.json(http.MethodPatch, path, update, cookie, csrf)
	if stale.Code != 409 {
		t.Fatal("stale settings accepted", stale.Code)
	}
	c = decodeConnection(t, saved.Body.Bytes())
	tooFast := f.json(http.MethodPatch, path+"/schedule", map[string]any{"generation": 1, "revision": c.Revision, "enabled": true, "cadence_seconds": 30}, cookie, csrf)
	if tooFast.Code != 422 {
		t.Fatal("minimum cadence bypassed", tooFast.Code)
	}
	schedule := f.json(http.MethodPatch, path+"/schedule", map[string]any{"generation": 1, "revision": c.Revision, "enabled": true, "cadence_seconds": 300}, cookie, csrf)
	if schedule.Code != 200 {
		t.Fatal(schedule.Code, schedule.Body.String())
	}
	c = decodeConnection(t, schedule.Body.Bytes())
	pause := f.json(http.MethodPatch, path+"/pause", map[string]any{"generation": 1, "revision": c.Revision, "paused": true}, cookie, csrf)
	if pause.Code != 200 {
		t.Fatal(pause.Code, pause.Body.String())
	}
	if refresh := f.json(http.MethodPost, path+"/refresh", map[string]int{"generation": 1}, cookie, csrf); refresh.Code != 422 {
		t.Fatal("paused source ran", refresh.Code)
	}
	if noCSRF := f.json(http.MethodPost, base+"/connections", input, cookie, ""); noCSRF.Code != 403 {
		t.Fatal("missing CSRF accepted", noCSRF.Code)
	}
	if anonymous := f.json(http.MethodPost, base+"/connections", input, nil, ""); anonymous.Code != 401 {
		t.Fatal("anonymous connection creation accepted", anonymous.Code)
	}

}

func TestConnectionsAPIPrivateSetupAndReviewerDenial(t *testing.T) {
	f := newBackendFixture(t)
	cookie, csrf, _ := f.login(t, "backend-owner@example.test", "Backend owner test passphrase")
	base := "/api/v1/workspaces/" + f.owner.PrivateWorkspaceID
	for _, method := range []string{"api_polling", "webhook", "browser", "public_scrape", "email"} {
		input := map[string]any{"generation": 1, "idempotency_key": "private-method-" + method, "name": "Pending " + method, "method": method}
		result := f.json(http.MethodPost, base+"/connections", input, cookie, csrf)
		if result.Code != 201 {
			t.Fatal(method, result.Code, result.Body.String())
		}
		c := decodeConnection(t, result.Body.Bytes())
		if c.Runnable || c.LastSuccessAt != nil || c.Status != "not_configured" || c.Origin != "live_private" {
			t.Fatal("pending configuration claimed live success", c)
		}
		path := base + "/connections/" + c.ID
		for _, action := range []string{"refresh", "source"} {
			payload := map[string]any{"generation": 1}
			if action == "source" {
				payload["revision"] = c.Revision
				payload["idempotency_key"] = "reject-live"
			}
			result = f.json(http.MethodPost, path+"/"+action, payload, cookie, csrf)
			if result.Code != 422 {
				t.Fatal("unconfigured live access enabled", action, result.Code)
			}
		}
	}
	overview := f.json(http.MethodGet, base+"/overview", nil, cookie, "")
	var data workbench.Overview
	_ = json.Unmarshal(overview.Body.Bytes(), &data)
	if overview.Code != 200 || len(data.Connections) != 5 || len(data.Orders) != 0 || data.Origin != "no_supplier_data" {
		t.Fatal("private setup was not visible or invented orders")
	}
	reviewer, err := store.CreateReviewer(f.ctx, f.admin, store.ReviewerInput{Email: "connections-reviewer@example.test", DisplayName: "Connection reviewer", Password: "Initial connection reviewer passphrase"})
	if err != nil {
		t.Fatal(err)
	}
	reviewerCookie, reviewerCSRF, _ := f.login(t, "connections-reviewer@example.test", "Initial connection reviewer passphrase")
	changed := f.json(http.MethodPost, "/api/password", map[string]string{"current_password": "Initial connection reviewer passphrase", "new_password": "Changed connection reviewer passphrase"}, reviewerCookie, reviewerCSRF)
	if changed.Code != 200 && changed.Code != 204 {
		t.Fatal(changed.Code, changed.Body.String())
	}
	reviewerCookie, reviewerCSRF, _ = f.login(t, "connections-reviewer@example.test", "Changed connection reviewer passphrase")
	for _, workspace := range []string{reviewer.WorkspaceID, f.owner.PrivateWorkspaceID} {
		res := f.json(http.MethodPost, fmt.Sprintf("/api/v1/workspaces/%s/connections", workspace), map[string]any{"generation": 1, "idempotency_key": "reviewer-denied-feed", "name": "Denied", "method": "fictional"}, reviewerCookie, reviewerCSRF)
		if res.Code != 404 {
			t.Fatal("reviewer created a connection", res.Code, res.Body.String())
		}
	}
}

// Preview validation must query the selected connection, rather than relying on
// the 500-row UI overview or selecting a same-named line from another feed.
func TestConnectionCSVPreviewBeyondOverviewWindow(t *testing.T) {
	f := newBackendFixture(t)
	cookie, csrf, _ := f.login(t, "backend-owner@example.test", "Backend owner test passphrase")
	svc := workbench.New(f.app)
	w := f.owner.PresentationWorkspaceID
	initial, err := svc.Seed(f.ctx, f.owner.OwnerID, w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, f.owner.OwnerID, w, initial)
	c, err := svc.CreateConnection(f.ctx, f.owner.OwnerID, w, 1, "csv-window-feed", workbench.ConnectionInput{Name: "Large independent source", Method: "fictional"})
	if err != nil {
		t.Fatal(err)
	}
	records := make([]domain.Record, 501)
	for i := range records {
		records[i] = domain.SeedRecords()[0]
		records[i].SourceKey = fmt.Sprintf("AAA-%04d:1", i)
		records[i].SourceOrderID = fmt.Sprintf("AAA-%04d", i)
	}
	run, err := svc.StageConnectionSource(f.ctx, f.owner.OwnerID, w, c.ID, 1, c.Revision, "csv-window-source", records, nil)
	if err != nil {
		t.Fatal(err)
	}
	var fence int64
	if err = f.admin.QueryRow(f.ctx, "SELECT fence FROM wb_runs WHERE id=$1", run.ID).Scan(&fence); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	collectionContext, cancel := context.WithTimeout(f.ctx, 45*time.Second)
	err = svc.Collect(collectionContext, workbench.CollectionArgs{SchemaVersion: 1, RunID: run.ID, ConnectionID: run.ConnectionID, WorkspaceID: w, Generation: 1, ActorUserID: f.owner.OwnerID, Fence: fence})
	cancel()
	if err != nil {
		t.Fatal("large fixture exceeded collection worker budget or failed", err)
	}
	collectionDuration := time.Since(started)
	started = time.Now()
	response := f.json(http.MethodGet, "/api/v1/workspaces/"+w+"/overview", nil, cookie, "")
	if response.Code != http.StatusOK {
		t.Fatal("large overview did not complete within API request budget", response.Code, response.Body.String())
	}
	t.Logf("501-record collection %.3fs; bounded HTTP overview %.3fs", collectionDuration.Seconds(), time.Since(started).Seconds())
	overview, err := svc.Overview(f.ctx, f.owner.OwnerID, w)
	if err != nil || len(overview.Orders) != 500 {
		t.Fatal("bounded fixture not established", len(overview.Orders), err)
	}
	for _, order := range overview.Orders {
		if order.SourceKey == domain.SeedRecords()[0].SourceKey {
			t.Fatal("default key should be outside overview window")
		}
	}
	var csv bytes.Buffer
	if err = imports.Template(&csv, domain.SeedRequirements()); err != nil {
		t.Fatal(err)
	}
	base := "/api/v1/workspaces/" + w
	preview := f.request(http.MethodPost, base+"/imports/internal-orders/preview", "text/csv", csv.Bytes(), cookie, csrf)
	if preview.Code != 201 {
		t.Fatal("valid rows outside overview window rejected", preview.Code, preview.Body.String())
	}
	var result struct {
		ID           string `json:"id"`
		ConnectionID string `json:"connection_id"`
	}
	_ = json.Unmarshal(preview.Body.Bytes(), &result)
	if result.ConnectionID != initial.ConnectionID {
		t.Fatal("CSV preview changed its default source binding")
	}
	wrong := f.request(http.MethodPost, base+"/imports/internal-orders/preview?connection_id="+c.ID, "text/csv", csv.Bytes(), cookie, csrf)
	if wrong.Code != 422 {
		t.Fatal("CSV selected rows from a different feed", wrong.Code)
	}
	published := f.json(http.MethodPost, base+"/imports/internal-orders/"+result.ID+"/publish", map[string]any{"generation": 1, "confirm": true}, cookie, csrf)
	if published.Code != 202 {
		t.Fatal(published.Code, published.Body.String())
	}
	var imported workbench.Run
	_ = json.Unmarshal(published.Body.Bytes(), &imported)
	if imported.ConnectionID != initial.ConnectionID {
		t.Fatal("CSV publication queued wrong feed")
	}
}
