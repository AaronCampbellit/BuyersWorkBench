package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"buyersworkbench/internal/store"
	"buyersworkbench/internal/workbench"
)

func issueResolutionAPIBody(issue workbench.Exception, key string) map[string]any {
	return map[string]any{
		"generation": 1, "revision": issue.Revision, "idempotency_key": key,
		"resolution_type": "confirmation_received", "action_taken": "Contacted the supplier purchasing desk.",
		"result": "Supplier supplied a written order confirmation.", "reference": "Supplier email CONF-2048",
		"supplier_contact": "Order support", "revised_date": "2026-10-25", "confirmed_quantity": "10",
		"follow_up_date": "2026-10-22",
	}
}

func decodeIssueAPI(t *testing.T, response *httptest.ResponseRecorder, status string) workbench.Exception {
	t.Helper()
	if response.Code != 200 {
		t.Fatalf("issue action returned %d: %s", response.Code, response.Body.String())
	}
	var issue workbench.Exception
	if err := json.Unmarshal(response.Body.Bytes(), &issue); err != nil || issue.Status != status || issue.ID == "" {
		t.Fatalf("issue action omitted expected %s state: %v", status, err)
	}
	return issue
}

func issueAPICondition(t *testing.T, f backendFixture, rule string) workbench.Exception {
	t.Helper()
	view, err := workbench.New(f.app).Overview(f.ctx, f.owner.OwnerID, f.owner.PresentationWorkspaceID)
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range view.Priorities {
		if issue.Rule == rule {
			return issue
		}
	}
	t.Fatalf("missing issue rule %s", rule)
	return workbench.Exception{}
}

func TestIssueResolutionAPIAtomicCloseoutVerificationAndRecurrence(t *testing.T) {
	f := newBackendFixture(t)
	cookie, csrf, _ := f.login(t, "backend-owner@example.test", "Backend owner test passphrase")
	svc := workbench.New(f.app)
	w := f.owner.PresentationWorkspaceID
	run, err := svc.Seed(f.ctx, f.owner.OwnerID, w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, f.owner.OwnerID, w, run)
	base := "/api/v1/workspaces/" + w
	issue := issueAPICondition(t, f, "R03")
	path := base + "/exceptions/" + issue.ID
	if res := f.json("POST", path+"/notes", map[string]any{"generation": 1, "revision": issue.Revision, "body": "Existing buyer context remains intact."}, cookie, csrf); res.Code != 201 {
		t.Fatal("could not record initial buyer context")
	}
	issue = issueAPICondition(t, f, "R03")
	original, err := svc.Order(f.ctx, f.owner.OwnerID, w, issue.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	if res := f.json("PATCH", path+"/workflow", map[string]any{"generation": 1, "revision": issue.Revision, "status": "completed"}, cookie, csrf); res.Code != 422 || !bytes.Contains(res.Body.Bytes(), []byte("resolution_required")) {
		t.Fatal("task completion bypassed structured closeout")
	}
	invalid := issueResolutionAPIBody(issue, "missing-resolution-result")
	delete(invalid, "result")
	if res := f.json("POST", path+"/resolve", invalid, cookie, csrf); res.Code != 422 {
		t.Fatal("resolution without an outcome was accepted")
	}
	invalid = issueResolutionAPIBody(issue, "missing-active-followup")
	delete(invalid, "follow_up_date")
	if res := f.json("POST", path+"/resolve", invalid, cookie, csrf); res.Code != 422 {
		t.Fatal("active risk accepted closeout without a verification follow-up")
	}
	body := issueResolutionAPIBody(issue, "closeout-confirmation-once")
	issue = decodeIssueAPI(t, f.json("POST", path+"/resolve", body, cookie, csrf), "awaiting_verification")
	if issue.Resolution == nil || issue.Resolution.Reference != body["reference"] || issue.Resolution.SubmittedBy != "Backend owner" || issue.Resolution.SubmittedAt.IsZero() || !issue.Active || issue.ResolvedAt != nil {
		t.Fatal("resolution lost required evidence, actor, timestamp or active-source distinction")
	}
	replayed := decodeIssueAPI(t, f.json("POST", path+"/resolve", body, cookie, csrf), "awaiting_verification")
	if replayed.Revision != issue.Revision || replayed.Resolution.ID != issue.Resolution.ID {
		t.Fatal("response retry duplicated the resolution")
	}
	altered := issueResolutionAPIBody(issue, "closeout-confirmation-once")
	altered["result"] = "A different outcome cannot reuse an accepted request key."
	if res := f.json("POST", path+"/resolve", altered, cookie, csrf); res.Code != 409 {
		t.Fatal("accepted request key was reused with a different closeout")
	}
	var detail workbench.OrderDetail
	res := f.json("GET", base+"/orders/"+issue.OrderID, nil, cookie, "")
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &detail) != nil || len(detail.Notes) != 1 || detail.Notes[0].Body != "Existing buyer context remains intact." {
		t.Fatal("closeout lost preexisting buyer notes")
	}
	beforeFacts, _ := json.Marshal(original.Order.Record)
	afterFacts, _ := json.Marshal(detail.Order.Record)
	if !bytes.Equal(beforeFacts, afterFacts) {
		t.Fatal("buyer closeout changed supplier facts")
	}
	submissions := 0
	for _, event := range detail.IssueEvents {
		if event.Action == "resolution_submitted" {
			submissions++
			if event.ActorName != "Backend owner" || event.CreatedAt.IsZero() || event.Resolution == nil {
				t.Fatal("closeout history omitted accountable evidence")
			}
		}
	}
	if submissions != 1 {
		t.Fatal("closeout was missing or duplicated in history")
	}
	run, err = svc.Scenario(f.ctx, f.owner.OwnerID, w, 1, "SC04", "verify-confirmation-source")
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, f.owner.OwnerID, w, run)
	issue = issueAPICondition(t, f, "R03")
	if issue.Active || issue.Status != "resolved" || issue.ResolvedAt == nil || issue.Resolution == nil {
		t.Fatal("accepted supplier confirmation did not verify and resolve the waiting issue")
	}
	verifiedDetail, err := svc.Order(f.ctx, f.owner.OwnerID, w, issue.OrderID)
	if err != nil {
		t.Fatal(err)
	}
	foundVerification := false
	for _, event := range verifiedDetail.IssueEvents {
		if event.ExceptionID == issue.ID && event.Action == "source_verified" {
			foundVerification = event.Evidence["confirmation"] == "confirmed" && event.Evidence["source_key"] == issue.SourceKey
		}
	}
	if !foundVerification {
		t.Fatal("verification history omitted the accepted confirmation evidence")
	}
	run, err = svc.Scenario(f.ctx, f.owner.OwnerID, w, 1, "SC01", "recur-confirmation-source")
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, f.owner.OwnerID, w, run)
	issue = issueAPICondition(t, f, "R03")
	if !issue.Active || issue.Status != "open" || issue.ResolvedAt != nil || issue.Resolution == nil {
		t.Fatal("recurring risk did not reopen while retaining the original closeout")
	}
}

func TestIssueResolutionAPIAuthorizationReopenAndStaleRequests(t *testing.T) {
	f := newBackendFixture(t)
	cookie, csrf, _ := f.login(t, "backend-owner@example.test", "Backend owner test passphrase")
	svc := workbench.New(f.app)
	w := f.owner.PresentationWorkspaceID
	run, err := svc.Seed(f.ctx, f.owner.OwnerID, w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, f.owner.OwnerID, w, run)
	issue := issueAPICondition(t, f, "R03")
	base := "/api/v1/workspaces/" + w
	path := base + "/exceptions/" + issue.ID
	body := issueResolutionAPIBody(issue, "closeout-authenticated-only")
	if res := f.json("POST", path+"/resolve", body, nil, ""); res.Code != 401 {
		t.Fatal("anonymous closeout accepted")
	}
	if res := f.json("POST", path+"/resolve", body, cookie, ""); res.Code != 403 {
		t.Fatal("closeout bypassed CSRF")
	}
	encoded, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", path+"/resolve", bytes.NewReader(encoded))
	req.Header.Set("Origin", "https://untrusted.example.test")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	req.AddCookie(cookie)
	res := httptest.NewRecorder()
	f.handler.ServeHTTP(res, req)
	if res.Code != 403 {
		t.Fatal("closeout bypassed Origin")
	}
	_, err = store.CreateReviewer(f.ctx, f.admin, store.ReviewerInput{Email: "issue-reviewer@example.test", DisplayName: "Issue reviewer", Password: "Initial issue reviewer passphrase"})
	if err != nil {
		t.Fatal(err)
	}
	rc, rt, _ := f.login(t, "issue-reviewer@example.test", "Initial issue reviewer passphrase")
	if res = f.json("POST", "/api/password", map[string]string{"current_password": "Initial issue reviewer passphrase", "new_password": "Changed issue reviewer passphrase"}, rc, rt); res.Code != 200 {
		t.Fatal("reviewer password change failed")
	}
	if res = f.json("POST", path+"/resolve", body, rc, rt); res.Code != 404 {
		t.Fatal("reviewer changed an owner issue")
	}
	issue = decodeIssueAPI(t, f.json("POST", path+"/resolve", body, cookie, csrf), "awaiting_verification")
	if res = f.json("POST", path+"/resolve", issueResolutionAPIBody(workbench.Exception{Revision: 1}, "stale-resolution-revision"), cookie, csrf); res.Code != 409 {
		t.Fatal("stale closeout revision accepted")
	}
	reopen := map[string]any{"generation": 1, "revision": issue.Revision, "idempotency_key": "reopen-with-recorded-reason", "reason": ""}
	if res = f.json("POST", path+"/reopen", reopen, cookie, csrf); res.Code != 422 {
		t.Fatal("manual reopening accepted without an explanation")
	}
	reopen["reason"] = "Supplier withdrew the written confirmation."
	issue = decodeIssueAPI(t, f.json("POST", path+"/reopen", reopen, cookie, csrf), "open")
	if issue.Resolution == nil || issue.ResolvedAt != nil {
		t.Fatal("reopening lost the previous closeout or kept a resolved timestamp")
	}
	replayed := decodeIssueAPI(t, f.json("POST", path+"/reopen", reopen, cookie, csrf), "open")
	if replayed.Revision != issue.Revision {
		t.Fatal("manual reopening replay duplicated the action")
	}
	var detail workbench.OrderDetail
	res = f.json("GET", base+"/orders/"+issue.OrderID, nil, cookie, "")
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &detail) != nil {
		t.Fatal("issue detail unavailable")
	}
	found := false
	for _, event := range detail.IssueEvents {
		if event.Action == "reopened" && event.ActorName == "Backend owner" && event.Reason == reopen["reason"] {
			found = true
		}
	}
	if !found {
		t.Fatal("manual reopening omitted the actor and reason")
	}
}
