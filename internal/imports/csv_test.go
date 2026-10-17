package imports

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"buyersworkbench/internal/domain"
)

func TestFixtureTemplateAndValidation(t *testing.T) {
	seed := domain.SeedRequirements()
	var out bytes.Buffer
	if err := Template(&out, seed); err != nil {
		t.Fatal(err)
	}
	rows, err := ParseCSV(&out)
	if err != nil || !reflect.DeepEqual(rows, seed) {
		t.Fatalf("CSV and JSON requirements differ: %v", err)
	}
}
func TestInvalidCSVNeverProducesPartialRows(t *testing.T) {
	var out bytes.Buffer
	_ = Template(&out, domain.SeedRequirements())
	valid := out.String()
	cases := []string{
		strings.Replace(valid, "2026-10-25", "10/25/2026", 1),
		strings.Replace(valid, "24,EA", "-24,EA", 1),
		strings.Replace(valid, "24,EA", "1e2,EA", 1),
		strings.Replace(valid, "24,EA", "0,EA", 1),
		strings.Replace(valid, "DEMO-PART-001", "=HYPERLINK()", 1),
		strings.Replace(valid, "demo_buyer", "unknown_buyer", 1),
		strings.Replace(valid, "SRC-MERIDIAN-10503:1", "SRC-ATLAS-10482:3", 1),
		strings.Replace(valid, "internal_po", "internal_po,internal_po", 1),
		valid + strings.Repeat("x", MaxBytes),
	}
	for i, input := range cases {
		if rows, err := ParseCSV(strings.NewReader(input)); err == nil || rows != nil {
			t.Fatalf("case %d accepted invalid CSV or partially returned rows", i)
		}
	}
}
