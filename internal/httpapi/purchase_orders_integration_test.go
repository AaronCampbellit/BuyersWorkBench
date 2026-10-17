package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"buyersworkbench/internal/domain"
	"buyersworkbench/internal/store"
	"buyersworkbench/internal/workbench"
)

func decodePurchaseOrderAPI(t *testing.T, res *httptest.ResponseRecorder, status int) workbench.PurchaseOrder {
	t.Helper()
	if res.Code != status {
		t.Fatalf("purchase order returned %d, want %d: %s", res.Code, status, res.Body.String())
	}
	var order workbench.PurchaseOrder
	if err := json.Unmarshal(res.Body.Bytes(), &order); err != nil || !uuidPattern.MatchString(order.ID) || order.Number == "" || order.Revision < 1 {
		t.Fatal("purchase order omitted identity/revision", err)
	}
	return order
}

func purchaseOrderAPIInput(supplierID, key string) map[string]any {
	return map[string]any{"generation": 1, "idempotency_key": key, "supplier_id": supplierID, "description": "Internally managed purchasing line", "quantity": "10.5", "uom": "EA", "currency": "USD", "unit_price": "2.50", "freight": "1.00", "other_charges": "0.25", "required_date": "2026-10-25", "notes": "Private internal terms"}
}

func TestPurchaseOrderAPIPrivateLifecycleAndReceipts(t *testing.T) {
	f := newBackendFixture(t)
	cookie, csrf, _ := f.login(t, "backend-owner@example.test", "Backend owner test passphrase")
	base := "/api/v1/workspaces/" + f.owner.PrivateWorkspaceID
	supplier := decodeSupplierAPI(t, f.json("POST", base+"/suppliers", supplierAPIInput(1, "Private purchasing supplier"), cookie, csrf), 201)
	input := purchaseOrderAPIInput(supplier.ID, "private-manual-order")
	order := decodePurchaseOrderAPI(t, f.json("POST", base+"/purchase-orders", input, cookie, csrf), 201)
	if order.Status != "draft" || order.Origin != "manual_private" || order.ModeledTotal != "27.50" || order.QuoteSource != nil {
		t.Fatal("manual draft has incorrect origin/state/costs")
	}
	retry := decodePurchaseOrderAPI(t, f.json("POST", base+"/purchase-orders", input, cookie, csrf), 201)
	if retry.ID != order.ID {
		t.Fatal("create retry duplicated a purchase order")
	}
	delete(input, "idempotency_key")
	input["revision"] = order.Revision
	input["unit_price"] = "2.25"
	order = decodePurchaseOrderAPI(t, f.json("PATCH", base+"/purchase-orders/"+order.ID, input, cookie, csrf), 200)
	if order.ModeledTotal != "24.875" {
		t.Fatal("fractional-cent modeled total was rounded or calculated incorrectly")
	}
	action := func(key, kind, quantity, reference, note string, confirm bool, want int) *httptest.ResponseRecorder {
		return f.json("POST", base+"/purchase-orders/"+order.ID+"/actions", map[string]any{"generation": 1, "revision": order.Revision, "idempotency_key": key, "action": kind, "quantity": quantity, "reference": reference, "note": note, "confirm": confirm}, cookie, csrf)
	}
	if r := action("premature-receipt", "receive", "1", "Receipt-0", "", false, 422); r.Code != 422 {
		t.Fatal("draft accepted a receipt")
	}
	order = decodePurchaseOrderAPI(t, action("approve-private-po", "approve", "", "", "", false, 200), 200)
	if order.Status != "approved" {
		t.Fatal("approval did not advance draft")
	}
	input["revision"] = order.Revision
	if r := f.json("PATCH", base+"/purchase-orders/"+order.ID, input, cookie, csrf); r.Code != 422 {
		t.Fatal("approved terms were editable without returning to draft")
	}
	if r := action("missing-placement-proof", "place", "", "External-PO-1", "", false, 422); r.Code != 422 {
		t.Fatal("placement accepted without outside-app acknowledgement")
	}
	order = decodePurchaseOrderAPI(t, action("place-private-order", "place", "", "External-PO-1", "Placed by owner outside the workbench", true, 200), 200)
	if order.Status != "placed" || order.PlacementReference != "External-PO-1" {
		t.Fatal("placement did not preserve external reference")
	}
	if r := action("too-large-receipt", "receive", "11", "Receipt-too-large", "", false, 422); r.Code != 422 {
		t.Fatal("over-receipt exceeded ordered quantity")
	}
	firstReceiptBody := map[string]any{"generation": 1, "revision": order.Revision, "idempotency_key": "first-receipt-event", "action": "receive", "quantity": "4.25", "reference": "Receipt-1"}
	order = decodePurchaseOrderAPI(t, f.json("POST", base+"/purchase-orders/"+order.ID+"/actions", firstReceiptBody, cookie, csrf), 200)
	if order.Status != "partially_received" {
		t.Fatal("partial receipt was treated as complete")
	}
	replayed := decodePurchaseOrderAPI(t, f.json("POST", base+"/purchase-orders/"+order.ID+"/actions", firstReceiptBody, cookie, csrf), 200)
	if replayed.Revision != order.Revision || replayed.ReceivedQuantity != order.ReceivedQuantity {
		t.Fatal("receipt retry counted goods twice")
	}
	if r := action("premature-close-order", "close", "", "", "", false, 422); r.Code != 422 {
		t.Fatal("partially received purchase order closed")
	}
	order = decodePurchaseOrderAPI(t, action("final-receipt-event", "receive", "6.25", "Receipt-2", "", false, 200), 200)
	if order.Status != "received" {
		t.Fatal("full explicit receipt did not advance lifecycle")
	}
	order = decodePurchaseOrderAPI(t, action("close-private-order", "close", "", "", "Receipt reconciled", false, 200), 200)
	if order.Status != "closed" || len(order.AllowedActions) != 0 {
		t.Fatal("closed order remained mutable")
	}
	if r := action("reopen-closed-order", "revise", "", "", "", false, 422); r.Code != 422 {
		t.Fatal("terminal purchase order reopened")
	}
	res := f.json("GET", base+"/purchase-orders/"+order.ID, nil, cookie, "")
	var detail workbench.PurchaseOrderDetail
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &detail) != nil || detail.Order.Status != "closed" {
		t.Fatal("persisted purchase order detail missing")
	}
	receipts := 0
	for _, event := range detail.Events {
		if event.Action == "receive" {
			receipts++
		}
		if event.ActorName == "" {
			t.Fatal("event omitted accountable actor")
		}
	}
	if receipts != 2 {
		t.Fatal("receipt event log was duplicated or lost")
	}
	var view workbench.Overview
	res = f.json("GET", base+"/overview", nil, cookie, "")
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &view) != nil || len(view.PurchaseOrders) != 1 || len(view.Orders) != 0 || len(view.Connections) != 0 || view.Quotes != nil {
		t.Fatal("private purchasing lifecycle fabricated supplier observations or required a connector")
	}
	if r := f.json("GET", base+"/purchase-orders", nil, cookie, ""); r.Code != 200 {
		t.Fatal("internal order directory unavailable")
	}
}

func TestPurchaseOrderAPIQuoteProvenanceAndBoundaries(t *testing.T) {
	f := newBackendFixture(t)
	cookie, csrf, _ := f.login(t, "backend-owner@example.test", "Backend owner test passphrase")
	svc := workbench.New(f.app)
	run, err := svc.Seed(f.ctx, f.owner.OwnerID, f.owner.PresentationWorkspaceID, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, f.owner.OwnerID, f.owner.PresentationWorkspaceID, run)
	base := "/api/v1/workspaces/" + f.owner.PresentationWorkspaceID
	var view workbench.Overview
	res := f.json("GET", base+"/overview", nil, cookie, "")
	if res.Code != 200 || json.Unmarshal(res.Body.Bytes(), &view) != nil || view.Quotes == nil {
		t.Fatal("quote fixture unavailable")
	}
	convert := map[string]any{"generation": 1, "revision": view.Quotes.Revision, "idempotency_key": "quote-meridian-draft"}
	if r := f.json("POST", base+"/quotes/harbor/purchase-order", convert, cookie, csrf); r.Code != 422 {
		t.Fatal("ineligible quote created a purchase order")
	}
	order := decodePurchaseOrderAPI(t, f.json("POST", base+"/quotes/meridian/purchase-order", convert, cookie, csrf), 201)
	if order.Status != "draft" || order.SourceQuoteID != "meridian" || order.QuoteSource == nil || order.QuoteSource.Revision != view.Quotes.Revision || order.ModeledTotal != "1125.00" || order.SupplierAlias != "fictional_meridian" {
		t.Fatal("quote conversion did not snapshot an eligible draft with stable supplier binding")
	}
	var quote domain.Quote
	for _, q := range view.Quotes.Comparison.Quotes {
		if q.ID == "meridian" {
			quote = q
		}
	}
	quote.UnitPrice = "11.00"
	if r := f.json("PATCH", base+"/quotes/meridian", map[string]any{"generation": 1, "revision": view.Quotes.Revision, "quote": quote}, cookie, csrf); r.Code != 200 {
		t.Fatal("quote input edit failed")
	}
	convert["revision"] = view.Quotes.Revision + 1
	convert["idempotency_key"] = "same-quote-new-request"
	again := decodePurchaseOrderAPI(t, f.json("POST", base+"/quotes/meridian/purchase-order", convert, cookie, csrf), 201)
	if again.ID != order.ID || again.QuoteSource.Quote.UnitPrice != "10.50" || again.ModeledTotal != "1125.00" {
		t.Fatal("quote edits rewrote old purchasing terms or duplicated conversion")
	}
	if r := f.json("POST", base+"/quotes/atlas/purchase-order", map[string]any{"generation": 1, "revision": 1, "idempotency_key": "stale-quote-revision"}, cookie, csrf); r.Code != 409 {
		t.Fatal("stale unconverted quote revision was accepted")
	}
	privateBase := "/api/v1/workspaces/" + f.owner.PrivateWorkspaceID
	if r := f.json("GET", privateBase+"/purchase-orders/"+order.ID, nil, cookie, ""); r.Code != 404 {
		t.Fatal("purchase order UUID crossed workspace")
	}
	action := map[string]any{"generation": 1, "revision": order.Revision, "idempotency_key": "approval-without-csrf", "action": "approve"}
	if r := f.json("POST", base+"/purchase-orders/"+order.ID+"/actions", action, cookie, ""); r.Code != 403 {
		t.Fatal("purchase order action bypassed CSRF")
	}
	body, _ := json.Marshal(action)
	req := httptest.NewRequest("POST", base+"/purchase-orders/"+order.ID+"/actions", bytes.NewReader(body))
	req.Header.Set("Origin", "https://untrusted.example.test")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", csrf)
	req.AddCookie(cookie)
	badOrigin := httptest.NewRecorder()
	f.handler.ServeHTTP(badOrigin, req)
	if badOrigin.Code != 403 {
		t.Fatal("purchase order action bypassed Origin")
	}
	reviewer, err := store.CreateReviewer(f.ctx, f.admin, store.ReviewerInput{Email: "purchasing-reviewer@example.test", DisplayName: "Purchasing reviewer", Password: "Initial purchasing reviewer passphrase"})
	if err != nil {
		t.Fatal(err)
	}
	rc, rt, _ := f.login(t, "purchasing-reviewer@example.test", "Initial purchasing reviewer passphrase")
	if r := f.json("POST", "/api/password", map[string]string{"current_password": "Initial purchasing reviewer passphrase", "new_password": "Changed purchasing reviewer passphrase"}, rc, rt); r.Code != 200 {
		t.Fatal("reviewer first-use password change failed")
	}
	reviewerBase := "/api/v1/workspaces/" + reviewer.WorkspaceID
	if r := f.json("GET", reviewerBase+"/purchase-orders", nil, rc, ""); r.Code != 200 {
		t.Fatal("reviewer cannot read own internal-order directory")
	}
	if r := f.json("POST", reviewerBase+"/purchase-orders", purchaseOrderAPIInput(order.SupplierID, "reviewer-cannot-create"), rc, rt); r.Code != 404 {
		t.Fatal("reviewer created an internal purchase order")
	}
	if r := f.json("GET", base+"/purchase-orders/"+order.ID, nil, rc, ""); r.Code != 404 {
		t.Fatal("reviewer read owner purchasing terms")
	}
	if r := f.json("POST", privateBase+"/purchase-orders/"+order.ID+"/actions", action, cookie, csrf); r.Code != 404 {
		t.Fatal("foreign order mutation crossed workspace")
	}
	reset := f.json("POST", base+"/demo/reset", map[string]any{"generation": 1, "confirm": true, "idempotency_key": "reset-internal-orders"}, cookie, csrf)
	var resetResult workbench.ResetResult
	if reset.Code != 202 || json.Unmarshal(reset.Body.Bytes(), &resetResult) != nil {
		t.Fatal("reset internal order fixture")
	}
	f.collect(t, f.owner.OwnerID, f.owner.PresentationWorkspaceID, resetResult.Run)
	if r := f.json("GET", base+"/purchase-orders/"+order.ID, nil, cookie, ""); r.Code != 404 {
		t.Fatal("prior-generation purchase order remained visible")
	}
	if r := f.json("POST", base+"/purchase-orders/"+order.ID+"/actions", action, cookie, csrf); r.Code != 409 {
		t.Fatal("stale-generation order action was accepted")
	}
	var accepted int
	if err := f.admin.QueryRow(f.ctx, "SELECT count(*) FROM wb_orders WHERE workspace_id=$1 AND generation=$2", f.owner.PresentationWorkspaceID, resetResult.Generation).Scan(&accepted); err != nil || accepted != 4 {
		t.Fatal("internal orders altered source order publication", err)
	}
}
