package domain

import "testing"

func TestObservedOrderLifecycleKeepsQuantitiesAndConfirmationSeparate(t *testing.T) {
	base := Record{Ordered: "100", Cancelled: "0", Shipped: "0", Delivered: "0", Status: "open", Confirmation: "unconfirmed"}
	tests := []struct{ name, shipped, delivered, cancelled, status, want, cancellation string }{
		{"open", "0.0000", "0", "0", "open", "open", "none"},
		{"partial shipment", "40", "0", "0", "partially_shipped", "partially_shipped", "none"},
		{"shipped not received", "100", "0", "0", "shipped", "shipped", "none"},
		{"partial delivery", "100", "25", "0", "shipped", "partially_delivered", "none"},
		{"delivered", "100", "100", "0", "delivered", "delivered", "none"},
		{"partial cancellation remains open", "0", "0", "20", "open", "open", "partial"},
		{"partial cancellation remaining shipped", "80", "0", "20", "shipped", "shipped", "partial"},
		{"fully cancelled", "0", "0", "100", "cancelled", "cancelled", "full"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := base
			r.Shipped, r.Delivered, r.Cancelled, r.Status = tt.shipped, tt.delivered, tt.cancelled, tt.status
			life := DeriveObservedOrderLifecycle(r)
			if life.FulfillmentState != tt.want || life.CancellationState != tt.cancellation || life.ConfirmationState != "unconfirmed" || life.ReceiptState != "unavailable" || life.Evidence != "quantities" || len(life.Issues) != 0 {
				t.Fatalf("wrong observed lifecycle: %+v", life)
			}
		})
	}
	partial := base
	partial.Shipped = "40"
	partial.Status = "partially_shipped"
	life := DeriveObservedOrderLifecycle(partial)
	if life.NetOrdered != "100" || life.Unshipped != "60" || life.Unreceived != "100" {
		t.Fatalf("partial shipment fabricated receipt: %+v", life)
	}
}

func TestObservedLifecycleUnknownFactsAndContradictions(t *testing.T) {
	r := Record{Ordered: "100", Status: "shipped", Confirmation: "unavailable"}
	life := DeriveObservedOrderLifecycle(r)
	if life.FulfillmentState != "shipped" || life.Evidence != "source_status" || life.NetOrdered != "" || life.Unshipped != "" || life.Unreceived != "" || life.ReceiptState != "unavailable" {
		t.Fatalf("unknown balances were fabricated: %+v", life)
	}
	r.Status = "unknown"
	life = DeriveObservedOrderLifecycle(r)
	if life.FulfillmentState != "unknown" || life.Evidence != "insufficient" || len(life.Issues) == 0 {
		t.Fatal("unknown source appeared fulfilled")
	}
	for _, r := range []Record{
		{Ordered: "100", Cancelled: "0", Shipped: "110", Delivered: "0", Status: "shipped"},
		{Ordered: "100", Cancelled: "0", Shipped: "0", Delivered: "20", Status: "delivered"},
		{Ordered: "100", Cancelled: "20", Shipped: "0", Delivered: "0", Status: "cancelled"},
		{Ordered: "100", Cancelled: "0", Shipped: "40", Delivered: "0", Status: "shipped"},
		{Ordered: "100", Cancelled: "0", Shipped: "0", Status: "delivered"},
		{Ordered: "100", Cancelled: "0", Shipped: "40", Status: "delivered"},
	} {
		life := DeriveObservedOrderLifecycle(r)
		if life.FulfillmentState != "unknown" || len(life.Issues) == 0 || life.ReceiptState != "unavailable" {
			t.Fatalf("contradictory source became a trusted lifecycle: %+v", life)
		}
	}
}

func TestReportedDeliveryIsNotDemotedByUnavailableDeliveredQuantity(t *testing.T) {
	for _, shipped := range []string{"100", ""} {
		r := Record{Ordered: "100", Cancelled: "0", Shipped: shipped, Delivered: "", Status: "delivered", Confirmation: "confirmed"}
		life := DeriveObservedOrderLifecycle(r)
		if life.FulfillmentState != "delivered" || life.Evidence != "source_status" || life.Unreceived != "" || life.ReceiptState != "unavailable" || len(life.Issues) != 0 {
			t.Fatalf("reported delivery was demoted or unknown receipt invented: %+v", life)
		}
	}
}
