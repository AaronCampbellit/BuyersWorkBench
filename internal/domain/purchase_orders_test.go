package domain

import (
	"strings"
	"testing"
)

func validPurchaseOrderInput() PurchaseOrderInput {
	return PurchaseOrderInput{SupplierID: "supplier-validated-by-service", Description: "Fictional component", Quantity: "100", UOM: "EA", Currency: "USD", UnitPrice: "9.80", Freight: "350", OtherCharges: "0", RequiredDate: "2026-10-25"}
}

func TestPurchaseOrderNormalizationAndExactCosts(t *testing.T) {
	in := validPurchaseOrderInput()
	in.Description = "  Fictional component  "
	in.Notes = "  First line\n\tSecond line  "
	got, err := NormalizePurchaseOrder(in)
	if err != nil || got.Description != "Fictional component" || got.Notes != "First line\n\tSecond line" {
		t.Fatal("valid draft normalization failed", err)
	}
	total, err := PurchaseOrderTotal(got)
	if err != nil || total != "1330.00" {
		t.Fatal("modeled total is incorrect", total, err)
	}
	in.Quantity, in.UnitPrice, in.Freight, in.OtherCharges = "0.125", "0.25", "0.0100", "0.0001"
	total, err = PurchaseOrderTotal(in)
	if err != nil || total != "0.04135" {
		t.Fatal("fractional cents were rounded or calculated with floats", total, err)
	}
	in.Description = strings.Repeat("界", 500)
	in.Notes = strings.Repeat("界", 4000)
	if _, err = NormalizePurchaseOrder(in); err != nil {
		t.Fatal("rune-based text limits rejected valid Unicode", err)
	}
}
func TestPurchaseOrderRejectsIncompleteAndInvalidDrafts(t *testing.T) {
	tests := []struct {
		name string
		edit func(*PurchaseOrderInput)
	}{
		{"missing supplier", func(in *PurchaseOrderInput) { in.SupplierID = "" }},
		{"missing description", func(in *PurchaseOrderInput) { in.Description = " " }},
		{"description limit", func(in *PurchaseOrderInput) { in.Description = strings.Repeat("x", 501) }},
		{"description control", func(in *PurchaseOrderInput) { in.Description = "a\nb" }},
		{"quantity zero", func(in *PurchaseOrderInput) { in.Quantity = "0" }},
		{"quantity negative", func(in *PurchaseOrderInput) { in.Quantity = "-1" }},
		{"quantity exponent", func(in *PurchaseOrderInput) { in.Quantity = "1e3" }},
		{"quantity precision", func(in *PurchaseOrderInput) { in.Quantity = "1.00001" }},
		{"unknown unit", func(in *PurchaseOrderInput) { in.UOM = "" }},
		{"currency", func(in *PurchaseOrderInput) { in.Currency = "EUR" }},
		{"unknown price", func(in *PurchaseOrderInput) { in.UnitPrice = "" }},
		{"negative freight", func(in *PurchaseOrderInput) { in.Freight = "-0.01" }},
		{"unknown extra charges", func(in *PurchaseOrderInput) { in.OtherCharges = "" }},
		{"bad date", func(in *PurchaseOrderInput) { in.RequiredDate = "2026-02-30" }},
		{"timestamp as date", func(in *PurchaseOrderInput) { in.RequiredDate = "2026-10-25T00:00:00Z" }},
		{"notes limit", func(in *PurchaseOrderInput) { in.Notes = strings.Repeat("x", 4001) }},
		{"notes null", func(in *PurchaseOrderInput) { in.Notes = "note\x00" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := validPurchaseOrderInput()
			tt.edit(&in)
			if _, err := NormalizePurchaseOrder(in); err == nil {
				t.Fatal("invalid draft accepted")
			}
			if _, err := PurchaseOrderTotal(in); err == nil {
				t.Fatal("invalid draft produced an authoritative total")
			}
		})
	}
}
func TestPurchaseOrderActionsCompleteLifecycle(t *testing.T) {
	status, received := "draft", "0"
	steps := []struct {
		in             PurchaseOrderActionInput
		want, quantity string
	}{
		{PurchaseOrderActionInput{Action: "approve"}, "approved", "0"},
		{PurchaseOrderActionInput{Action: "revise"}, "draft", "0"},
		{PurchaseOrderActionInput{Action: "approve"}, "approved", "0"},
		{PurchaseOrderActionInput{Action: "place", Confirm: true, Reference: "Internal placement reference"}, "placed", "0"},
		{PurchaseOrderActionInput{Action: "receive", Quantity: "0.1", Reference: "Receipt 1"}, "partially_received", "0.1"},
		{PurchaseOrderActionInput{Action: "receive", Quantity: "0.2", Reference: "Receipt 2"}, "partially_received", "0.3"},
		{PurchaseOrderActionInput{Action: "receive", Quantity: "0.7", Reference: "Receipt 3"}, "received", "1"},
		{PurchaseOrderActionInput{Action: "close"}, "closed", "1"},
	}
	for _, step := range steps {
		next, quantity, err := ApplyPurchaseOrderAction(status, "1", received, step.in)
		if err != nil || next != step.want || quantity != step.quantity {
			t.Fatalf("%s transition: got %s/%s error=%v", step.in.Action, next, quantity, err)
		}
		status, received = next, quantity
	}
	if len(PurchaseOrderActions(status)) != 0 {
		t.Fatal("closed purchase order still has lifecycle actions")
	}
	status, received, err := ApplyPurchaseOrderAction("partially_received", "100", "40", PurchaseOrderActionInput{Action: "cancel", Note: "Cancel the unfulfilled remainder"})
	if err != nil || status != "cancelled" || received != "40" {
		t.Fatal("partial cancellation erased receipt history", err)
	}
}
func TestPurchaseOrderActionsRejectMissingEvidenceAndInvalidTransitions(t *testing.T) {
	tests := []struct {
		status, ordered, received string
		in                        PurchaseOrderActionInput
	}{
		{"draft", "100", "0", PurchaseOrderActionInput{Action: "place", Confirm: true, Reference: "x"}},
		{"approved", "100", "0", PurchaseOrderActionInput{Action: "place", Reference: "x"}},
		{"approved", "100", "0", PurchaseOrderActionInput{Action: "place", Confirm: true}},
		{"placed", "100", "0", PurchaseOrderActionInput{Action: "receive", Quantity: "1"}},
		{"placed", "100", "0", PurchaseOrderActionInput{Action: "receive", Quantity: "0", Reference: "x"}},
		{"partially_received", "100", "40", PurchaseOrderActionInput{Action: "receive", Quantity: "61", Reference: "x"}},
		{"placed", "100", "0", PurchaseOrderActionInput{Action: "close"}},
		{"received", "100", "40", PurchaseOrderActionInput{Action: "close"}},
		{"draft", "100", "1", PurchaseOrderActionInput{Action: "approve"}},
		{"partially_received", "100", "0", PurchaseOrderActionInput{Action: "cancel", Note: "reason"}},
		{"placed", "100", "0", PurchaseOrderActionInput{Action: "cancel"}},
		{"received", "100", "100", PurchaseOrderActionInput{Action: "cancel", Note: "reason"}},
		{"closed", "100", "100", PurchaseOrderActionInput{Action: "receive", Quantity: "1", Reference: "x"}},
		{"cancelled", "100", "40", PurchaseOrderActionInput{Action: "revise"}},
		{"draft", "100", "0", PurchaseOrderActionInput{Action: "approve", Quantity: "10"}},
		{"approved", "100", "0", PurchaseOrderActionInput{Action: "place", Confirm: true, Reference: "bad\nreference"}},
		{"draft", "100", "0", PurchaseOrderActionInput{Action: "cancel", Note: "bad\x00reason"}},
		{"draft", "0", "0", PurchaseOrderActionInput{Action: "approve"}},
		{"draft", "100", "-1", PurchaseOrderActionInput{Action: "approve"}},
		{"draft", "100", "101", PurchaseOrderActionInput{Action: "approve"}},
	}
	for _, tt := range tests {
		if next, received, err := ApplyPurchaseOrderAction(tt.status, tt.ordered, tt.received, tt.in); err == nil || next != tt.status || received != tt.received {
			t.Fatalf("invalid action mutated state: %s %+v -> %s/%s err=%v", tt.status, tt.in, next, received, err)
		}
	}
}
