package workbench

import (
	"errors"
	"strings"
	"testing"
)

func TestSupplierValidationAndNormalization(t *testing.T) {
	in := SupplierInput{Name: "  Sample supplier  ", ContactName: " Buyer ", Email: " buyer@example.test ", Phone: " +1 555 0100 ", Website: " https://example.test/contact ", Notes: " First line\n\tSecond line  "}
	got, err := normalizeSupplier(in, true)
	if err != nil || got.Name != "Sample supplier" || got.ContactName != "Buyer" || got.Email != "buyer@example.test" || got.Phone != "+1 555 0100" || got.Website != "https://example.test/contact" || got.Notes != "First line\n\tSecond line" || got.Status != "active" {
		t.Fatalf("normalization changed allowed input incorrectly: %+v %v", got, err)
	}
	got.Name = strings.Repeat("界", 100)
	got.ContactName = strings.Repeat("界", 100)
	got.Phone = strings.Repeat("界", 50)
	got.Notes = strings.Repeat("界", 4000)
	got.Status = "inactive"
	if _, err = normalizeSupplier(got, false); err != nil {
		t.Fatal("rune-based limits rejected valid Unicode fields", err)
	}
	if _, err = normalizeSupplier(SupplierInput{Name: "Supplier"}, false); !errors.Is(err, ErrInvalid) {
		t.Fatal("update silently defaulted missing status")
	}
}

func TestSupplierRejectsUnsafeOrOversizedFields(t *testing.T) {
	valid := SupplierInput{Name: "Sample supplier", Status: "active"}
	tests := []struct {
		name string
		edit func(*SupplierInput)
	}{
		{"empty name", func(in *SupplierInput) { in.Name = " \t " }},
		{"long name", func(in *SupplierInput) { in.Name = strings.Repeat("x", 101) }},
		{"name control", func(in *SupplierInput) { in.Name = "Sample\nSupplier" }},
		{"invalid utf8", func(in *SupplierInput) { in.Name = string([]byte{0xff}) }},
		{"long contact", func(in *SupplierInput) { in.ContactName = strings.Repeat("x", 101) }},
		{"long phone", func(in *SupplierInput) { in.Phone = strings.Repeat("x", 51) }},
		{"email display name", func(in *SupplierInput) { in.Email = "Buyer <buyer@example.test>" }},
		{"email control", func(in *SupplierInput) { in.Email = "buyer@exam\nple.test" }},
		{"email length", func(in *SupplierInput) { in.Email = strings.Repeat("x", 250) + "@test.invalid" }},
		{"website relative", func(in *SupplierInput) { in.Website = "example.test/contact" }},
		{"website scheme", func(in *SupplierInput) { in.Website = "javascript:alert(1)" }},
		{"website host", func(in *SupplierInput) { in.Website = "https:///contact" }},
		{"website credentials", func(in *SupplierInput) { in.Website = "https://user:secret@example.test/" }},
		{"website control", func(in *SupplierInput) { in.Website = "https://exam\nple.test/" }},
		{"website length", func(in *SupplierInput) { in.Website = "https://example.test/" + strings.Repeat("x", 2048) }},
		{"notes length", func(in *SupplierInput) { in.Notes = strings.Repeat("x", 4001) }},
		{"notes nul", func(in *SupplierInput) { in.Notes = "Unsafe\x00note" }},
		{"status", func(in *SupplierInput) { in.Status = "deleted" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := valid
			tt.edit(&in)
			if _, err := normalizeSupplier(in, true); !errors.Is(err, ErrInvalid) {
				t.Fatal("invalid supplier field accepted", err)
			}
		})
	}
}
