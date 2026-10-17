package workbench

import (
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"buyersworkbench/internal/domain"
	"github.com/jackc/pgx/v5/pgconn"
)

func purchaseOrderFixture(t *testing.T) (fixture, domain.PurchaseOrderInput) {
	t.Helper()
	f := testFoundation(t)
	if _, err := f.admin.Exec(f.ctx, "UPDATE workspaces SET kind='presentation' WHERE id=$1", f.w); err != nil {
		t.Fatal("prepare owner purchasing workspace")
	}
	run, err := f.service.Seed(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal("seed purchasing fixture:", err)
	}
	f.collect(t, run)
	var supplier string
	if err = f.admin.QueryRow(f.ctx, "SELECT id::text FROM wb_supplier_profiles WHERE workspace_id=$1 AND alias='fictional_atlas'", f.w).Scan(&supplier); err != nil {
		t.Fatal("load active supplier")
	}
	return f, domain.PurchaseOrderInput{SupplierID: supplier, Description: "Locally maintained purchasing line", Quantity: "100", UOM: "EA", Currency: "USD", UnitPrice: "2.50", Freight: "5", OtherCharges: "0", RequiredDate: "2026-10-25", Notes: "Internal intent; supplier submission is manual."}
}

func actPurchaseOrder(t *testing.T, f fixture, order PurchaseOrder, key string, input domain.PurchaseOrderActionInput) PurchaseOrder {
	t.Helper()
	next, err := f.service.ActOnPurchaseOrder(f.ctx, f.actor, f.w, order.ID, order.Generation, order.Revision, key, input)
	if err != nil {
		t.Fatalf("purchase action %s: %v", input.Action, err)
	}
	if next.Revision != order.Revision+1 {
		t.Fatal("accepted purchase action did not advance revision")
	}
	return next
}

func TestPurchaseOrderLifecycleReceiptIdempotencyAndSourceIndependence(t *testing.T) {
	f, input := purchaseOrderFixture(t)
	var before string
	if err := f.admin.QueryRow(f.ctx, "SELECT jsonb_agg(record ORDER BY source_key)::text FROM wb_orders WHERE workspace_id=$1", f.w).Scan(&before); err != nil {
		t.Fatal(err)
	}
	order, err := f.service.CreatePurchaseOrder(f.ctx, f.actor, f.w, 1, "manual-order-create", input)
	if err != nil || order.Status != "draft" || order.ModeledTotal != "255.00" || order.ReceivedQuantity != "0" || order.Number == "" {
		t.Fatalf("create draft: %+v %v", order, err)
	}
	repeated, err := f.service.CreatePurchaseOrder(f.ctx, f.actor, f.w, 1, "manual-order-create", input)
	if err != nil || repeated.ID != order.ID || repeated.Revision != order.Revision {
		t.Fatal("repeated create duplicated purchasing intent")
	}
	different := input
	different.Quantity = "120"
	if _, err = f.service.CreatePurchaseOrder(f.ctx, f.actor, f.w, 1, "manual-order-create", different); !errors.Is(err, ErrStale) {
		t.Fatal("idempotency key accepted a different create payload")
	}
	order = actPurchaseOrder(t, f, order, "approve-initial-order", domain.PurchaseOrderActionInput{Action: "approve"})
	if _, err = f.service.UpdatePurchaseOrder(f.ctx, f.actor, f.w, order.ID, 1, order.Revision, different); !errors.Is(err, ErrInvalid) {
		t.Fatal("approved order was silently edited")
	}
	order = actPurchaseOrder(t, f, order, "revise-approved-order", domain.PurchaseOrderActionInput{Action: "revise"})
	priorRevision := order.Revision
	order, err = f.service.UpdatePurchaseOrder(f.ctx, f.actor, f.w, order.ID, 1, order.Revision, different)
	if err != nil || order.Quantity != "120" || order.ModeledTotal != "305.00" {
		t.Fatal("revised draft did not persist its new terms:", err)
	}
	if _, err = f.service.UpdatePurchaseOrder(f.ctx, f.actor, f.w, order.ID, 1, priorRevision, input); !errors.Is(err, ErrStale) {
		t.Fatal("stale draft overwrote accepted terms")
	}
	order = actPurchaseOrder(t, f, order, "approve-revised-order", domain.PurchaseOrderActionInput{Action: "approve"})
	if _, err = f.service.ActOnPurchaseOrder(f.ctx, f.actor, f.w, order.ID, 1, order.Revision, "unconfirmed-place", domain.PurchaseOrderActionInput{Action: "place", Reference: "MANUAL-PLACE-001"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("placement accepted without explicit confirmation")
	}
	order = actPurchaseOrder(t, f, order, "record-manual-placement", domain.PurchaseOrderActionInput{Action: "place", Reference: "MANUAL-PLACE-001", Confirm: true})
	if order.PlacementReference != "MANUAL-PLACE-001" || order.Status != "placed" {
		t.Fatal("manual placement was not recorded")
	}
	beforeReceipt := order
	receipt := domain.PurchaseOrderActionInput{Action: "receive", Quantity: "40", Reference: "RECEIPT-001"}
	order = actPurchaseOrder(t, f, order, "record-first-receipt", receipt)
	if order.Status != "partially_received" || order.ReceivedQuantity != "40" {
		t.Fatal("partial receipt failed")
	}
	repeated, err = f.service.ActOnPurchaseOrder(f.ctx, f.actor, f.w, order.ID, 1, beforeReceipt.Revision, "record-first-receipt", receipt)
	if err != nil || repeated.ID != order.ID || repeated.Revision != order.Revision || repeated.ReceivedQuantity != "40" {
		t.Fatal("receipt retry counted received quantity twice")
	}
	if _, err = f.service.ActOnPurchaseOrder(f.ctx, f.actor, f.w, order.ID, 1, order.Revision, "reject-over-receipt", domain.PurchaseOrderActionInput{Action: "receive", Quantity: "81", Reference: "EXCESS"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("receipt exceeded ordered quantity")
	}
	order = actPurchaseOrder(t, f, order, "record-final-receipt", domain.PurchaseOrderActionInput{Action: "receive", Quantity: "80", Reference: "RECEIPT-002"})
	if order.Status != "received" || order.ReceivedQuantity != "120" {
		t.Fatal("complete receipt did not reach received state")
	}
	order = actPurchaseOrder(t, f, order, "close-received-order", domain.PurchaseOrderActionInput{Action: "close"})
	if order.Status != "closed" || len(order.AllowedActions) != 0 {
		t.Fatal("closed order remained mutable")
	}
	if _, err = f.service.ActOnPurchaseOrder(f.ctx, f.actor, f.w, order.ID, 1, order.Revision, "cannot-cancel-closed", domain.PurchaseOrderActionInput{Action: "cancel", Note: "Too late"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("closed order was cancelled")
	}
	detail, err := f.service.PurchaseOrder(f.ctx, f.actor, f.w, order.ID)
	if err != nil || detail.Order.Status != "closed" || len(detail.Events) != 9 {
		t.Fatalf("durable lifecycle/event history: %d events %v", len(detail.Events), err)
	}
	var after string
	if err = f.admin.QueryRow(f.ctx, "SELECT jsonb_agg(record ORDER BY source_key)::text FROM wb_orders WHERE workspace_id=$1", f.w).Scan(&after); err != nil || after != before {
		t.Fatal("local purchase actions changed supplier observations")
	}
	run, err := f.service.Refresh(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal("refresh after internal lifecycle:", err)
	}
	f.collect(t, run)
	afterRefresh, err := f.service.PurchaseOrder(f.ctx, f.actor, f.w, order.ID)
	if err != nil || afterRefresh.Order.Status != "closed" || afterRefresh.Order.ReceivedQuantity != "120" || afterRefresh.Order.PlacementReference != order.PlacementReference || len(afterRefresh.Events) != len(detail.Events) {
		t.Fatal("collection overwrote internal purchasing state or history")
	}
}

func TestPurchaseOrderQuoteSnapshotAndUniqueConversion(t *testing.T) {
	f, _ := purchaseOrderFixture(t)
	if _, err := f.service.CreatePurchaseOrderFromQuote(f.ctx, f.actor, f.w, "harbor", 1, 1, "ineligible-quote-order"); !errors.Is(err, ErrInvalid) {
		t.Fatal("ineligible quote became an order")
	}
	if _, err := f.service.CreatePurchaseOrderFromQuote(f.ctx, f.actor, f.w, "meridian", 1, 99, "stale-quote-order"); !errors.Is(err, ErrStale) {
		t.Fatal("stale comparison revision became an order")
	}
	order, err := f.service.CreatePurchaseOrderFromQuote(f.ctx, f.actor, f.w, "meridian", 1, 1, "eligible-quote-order")
	if err != nil || order.Status != "draft" || order.ModeledTotal != "1125.00" || order.SourceQuoteID != "meridian" || order.QuoteSource == nil || order.SupplierAlias != "fictional_meridian" {
		t.Fatalf("eligible quote conversion: %+v %v", order, err)
	}
	snapshot, _ := json.Marshal(order.QuoteSource)
	quotes, err := quotesTxForTest(f)
	if err != nil {
		t.Fatal(err)
	}
	quote := quotes.Comparison.Quotes[1]
	quote.UnitPrice = "11.00"
	if _, err = f.service.UpdateQuote(f.ctx, f.actor, f.w, quote.ID, 1, quotes.Revision, quote); err != nil {
		t.Fatal("edit comparison after conversion:", err)
	}
	repeat, err := f.service.CreatePurchaseOrderFromQuote(f.ctx, f.actor, f.w, "meridian", 1, 1, "eligible-quote-order")
	if err != nil || repeat.ID != order.ID {
		t.Fatal("same conversion retry failed after comparison changed")
	}
	repeat, err = f.service.CreatePurchaseOrderFromQuote(f.ctx, f.actor, f.w, "meridian", 1, 1, "new-key-same-quote")
	if err != nil || repeat.ID != order.ID {
		t.Fatal("another click created a duplicate order for the quote")
	}
	changed := order.PurchaseOrderInput
	changed.UnitPrice = "12"
	changed.Quantity = "90"
	order, err = f.service.UpdatePurchaseOrder(f.ctx, f.actor, f.w, order.ID, 1, order.Revision, changed)
	if err != nil || order.ModeledTotal != "1155.00" {
		t.Fatal("edit internal draft independently of quote:", err)
	}
	detail, err := f.service.PurchaseOrder(f.ctx, f.actor, f.w, order.ID)
	after, _ := json.Marshal(detail.Order.QuoteSource)
	if err != nil || string(after) != string(snapshot) || detail.Order.SourceQuoteID != "meridian" {
		t.Fatal("commercial draft edit changed immutable source offer snapshot")
	}
	var count int
	if err = f.admin.QueryRow(f.ctx, "SELECT count(*) FROM wb_purchase_orders WHERE workspace_id=$1", f.w).Scan(&count); err != nil || count != 1 {
		t.Fatal("conversion created duplicate durable orders")
	}
}

func quotesTxForTest(f fixture) (Quotes, error) {
	view, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil {
		return Quotes{}, err
	}
	return *view.Quotes, nil
}

func TestPurchaseOrderCancelPrivateAndGenerationIsolation(t *testing.T) {
	f, input := purchaseOrderFixture(t)
	order, err := f.service.CreatePurchaseOrder(f.ctx, f.actor, f.w, 1, "cancel-order-create", input)
	if err != nil {
		t.Fatal(err)
	}
	order = actPurchaseOrder(t, f, order, "cancel-order-approve", domain.PurchaseOrderActionInput{Action: "approve"})
	order = actPurchaseOrder(t, f, order, "cancel-order-place", domain.PurchaseOrderActionInput{Action: "place", Confirm: true, Reference: "MANUAL-002"})
	order = actPurchaseOrder(t, f, order, "cancel-order-receive", domain.PurchaseOrderActionInput{Action: "receive", Quantity: "10.125", Reference: "RECEIPT-FRACTION"})
	if _, err = f.service.ActOnPurchaseOrder(f.ctx, f.actor, f.w, order.ID, 1, order.Revision, "cancel-without-reason", domain.PurchaseOrderActionInput{Action: "cancel"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("cancellation accepted without reason")
	}
	order = actPurchaseOrder(t, f, order, "cancel-with-reason", domain.PurchaseOrderActionInput{Action: "cancel", Note: "Remaining requirement withdrawn"})
	if order.Status != "cancelled" || order.ReceivedQuantity != "10.125" || len(order.AllowedActions) != 0 {
		t.Fatal("cancellation discarded receipt evidence")
	}
	if _, err = f.service.PurchaseOrder(f.ctx, f.otherActor, f.otherW, order.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign order UUID leaked across workspace scope")
	}
	if _, err = f.service.CreatePurchaseOrder(f.ctx, f.otherActor, f.otherW, 1, "reviewer-create-denial", input); !errors.Is(err, ErrNotFound) {
		t.Fatal("reviewer created local purchasing intent")
	}
	tx, err := f.app.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(f.ctx, "SELECT set_config('bwb.actor_id',$1,true)", f.otherActor); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = tx.QueryRow(f.ctx, "SELECT count(*) FROM wb_purchase_orders WHERE workspace_id=$1", f.w).Scan(&count); err != nil || count != 0 {
		t.Fatal("actual runtime role leaked foreign purchase orders")
	}
	changed, err := tx.Exec(f.ctx, "UPDATE wb_purchase_orders SET status='draft' WHERE workspace_id=$1", f.w)
	if err != nil || changed.RowsAffected() != 0 {
		t.Fatal("runtime role changed a foreign purchase order")
	}
	_, err = tx.Exec(f.ctx, `INSERT INTO wb_purchase_orders(workspace_id,generation,supplier_id,data,supplier_alias,supplier_name,origin,created_by)
		VALUES($1,1,$2,$3,'forged','forged','fictional',$4)`, f.otherW, input.SupplierID, raw(input), f.otherActor)
	var denied *pgconn.PgError
	if !errors.As(err, &denied) || denied.Code != "42501" {
		t.Fatal("runtime RLS did not reject reviewer purchasing writes before publication")
	}
	tx.Rollback(f.ctx)
	var canEditEvents, canEditProvenance bool
	if err = f.app.QueryRow(f.ctx, "SELECT has_table_privilege(current_user,'wb_purchase_order_events','UPDATE'),has_column_privilege(current_user,'wb_purchase_orders','quote_source','UPDATE')").Scan(&canEditEvents, &canEditProvenance); err != nil || canEditEvents || canEditProvenance {
		t.Fatal("runtime role may rewrite receipt history or quote provenance")
	}
	var privateID string
	if err = f.admin.QueryRow(f.ctx, "INSERT INTO workspaces(id,kind,owner_user_id,name) VALUES(gen_random_uuid(),'private',$1,'Private purchasing') RETURNING id::text", f.actor).Scan(&privateID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.admin.Exec(f.ctx, "INSERT INTO memberships(workspace_id,user_id,role) VALUES($1,$2,'owner')", privateID, f.actor); err != nil {
		t.Fatal(err)
	}
	privateSupplier, err := f.service.CreateSupplier(f.ctx, f.actor, privateID, 1, SupplierInput{Name: "Private manually maintained supplier", Status: "active"})
	if err != nil {
		t.Fatal(err)
	}
	privateInput := input
	privateInput.SupplierID = privateSupplier.ID
	private, err := f.service.CreatePurchaseOrder(f.ctx, f.actor, privateID, 1, "private-order-create", privateInput)
	if err != nil || private.Origin != "manual_private" {
		t.Fatal("private purchase order required a live source:", err)
	}
	reset, err := f.service.Reset(f.ctx, f.actor, f.w, 1, "purchase-order-reset")
	if err != nil || reset.Generation != 2 {
		t.Fatal("reset purchasing workspace:", err)
	}
	f.collect(t, reset.Run)
	if _, err = f.service.PurchaseOrder(f.ctx, f.actor, f.w, order.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("old-generation local order remained visible")
	}
	if _, err = f.service.CreatePurchaseOrder(f.ctx, f.actor, f.w, 1, "stale-generation-order", input); !errors.Is(err, ErrStale) {
		t.Fatal("stale browser created a local order after reset")
	}
	privateDetail, err := f.service.PurchaseOrder(f.ctx, f.actor, privateID, private.ID)
	if err != nil || privateDetail.Order.ID != private.ID || privateDetail.Order.Status != "draft" {
		t.Fatal("demo reset changed private purchase orders")
	}
	if err = f.admin.QueryRow(f.ctx, "SELECT count(*) FROM wb_connections WHERE workspace_id=$1", privateID).Scan(&count); err != nil || count != 0 {
		t.Fatal("private order lifecycle created a supplier connection")
	}
}

func TestPurchaseOrderConcurrentCreateAndInactiveSupplier(t *testing.T) {
	f, input := purchaseOrderFixture(t)
	var wg sync.WaitGroup
	orders := make(chan PurchaseOrder, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			order, err := f.service.CreatePurchaseOrder(f.ctx, f.actor, f.w, 1, "concurrent-create-key", input)
			orders <- order
			errs <- err
		})
	}
	wg.Wait()
	close(orders)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal("concurrent purchasing create:", err)
		}
	}
	var order PurchaseOrder
	for result := range orders {
		if order.ID != "" && result.ID != order.ID {
			t.Fatal("concurrent retry created multiple orders")
		}
		order = result
	}
	var count int
	if err := f.admin.QueryRow(f.ctx, "SELECT count(*) FROM wb_purchase_order_events WHERE workspace_id=$1", f.w).Scan(&count); err != nil || count != 1 {
		t.Fatal("concurrent create duplicated lifecycle history")
	}
	if _, err := f.admin.Exec(f.ctx, "UPDATE wb_supplier_profiles SET status='inactive' WHERE id=$1", input.SupplierID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreatePurchaseOrder(f.ctx, f.actor, f.w, 1, "inactive-new-order", input); !errors.Is(err, ErrInvalid) {
		t.Fatal("inactive supplier accepted a new order")
	}
	if _, err := f.service.ActOnPurchaseOrder(f.ctx, f.actor, f.w, order.ID, 1, order.Revision, "inactive-approval", domain.PurchaseOrderActionInput{Action: "approve"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("inactive supplier order was approved")
	}
	if _, err := f.admin.Exec(f.ctx, "UPDATE wb_supplier_profiles SET status='active' WHERE id=$1", input.SupplierID); err != nil {
		t.Fatal(err)
	}
	order = actPurchaseOrder(t, f, order, "concurrent-receipt-approve", domain.PurchaseOrderActionInput{Action: "approve"})
	order = actPurchaseOrder(t, f, order, "concurrent-receipt-place", domain.PurchaseOrderActionInput{Action: "place", Confirm: true, Reference: "MANUAL-CONCURRENT"})
	errorsByReceipt := make(chan error, 2)
	for _, key := range []string{"concurrent-receipt-one", "concurrent-receipt-two"} {
		wg.Go(func() {
			_, err := f.service.ActOnPurchaseOrder(f.ctx, f.actor, f.w, order.ID, 1, order.Revision, key, domain.PurchaseOrderActionInput{Action: "receive", Quantity: "40", Reference: key})
			errorsByReceipt <- err
		})
	}
	wg.Wait()
	close(errorsByReceipt)
	succeeded, conflicted := 0, 0
	for err := range errorsByReceipt {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrStale):
			conflicted++
		default:
			t.Fatal("unexpected concurrent receipt failure:", err)
		}
	}
	detail, err := f.service.PurchaseOrder(f.ctx, f.actor, f.w, order.ID)
	if err != nil || succeeded != 1 || conflicted != 1 || detail.Order.ReceivedQuantity != "40" || detail.Order.Status != "partially_received" {
		t.Fatal("concurrent stale receipt silently double-counted received quantity")
	}
}
