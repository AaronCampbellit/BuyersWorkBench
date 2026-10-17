// Package imports validates fictional internal requirements before publication.
package imports

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"buyersworkbench/internal/domain"
)

const MaxBytes = 1024 * 1024
const MaxRows = 1000

var decimal = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})(\.[0-9]{1,4})?$`)
var headers = []string{"internal_po", "line", "supplier_alias", "internal_part", "quantity", "uom", "required_arrival_date", "source_order_key", "buyer_alias"}

func ParseCSV(input io.Reader) ([]domain.Requirement, error) {
	contents, err := io.ReadAll(io.LimitReader(input, MaxBytes+1))
	if err != nil || len(contents) > MaxBytes {
		return nil, errors.New("CSV exceeds the size limit or could not be read")
	}
	reader := csv.NewReader(strings.NewReader(string(contents)))
	reader.ReuseRecord = false
	header, err := reader.Read()
	if err != nil {
		return nil, errors.New("CSV header is required")
	}
	positions := map[string]int{}
	for i, h := range header {
		h = strings.TrimPrefix(h, "\ufeff")
		if _, exists := positions[h]; exists {
			return nil, errors.New("duplicate CSV header")
		}
		known := false
		for _, valid := range headers {
			if h == valid {
				known = true
			}
		}
		if !known {
			return nil, errors.New("unsupported CSV header")
		}
		positions[h] = i
	}
	for _, h := range headers[:8] {
		if _, ok := positions[h]; !ok {
			return nil, fmt.Errorf("missing CSV header %q", h)
		}
	}
	seen := map[string]bool{}
	result := []domain.Requirement{}
	for row := 2; ; row++ {
		values, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("row %d: malformed CSV", row)
		}
		if len(result) >= MaxRows {
			return nil, errors.New("CSV exceeds 1000 rows")
		}
		for _, v := range values {
			if !utf8.ValidString(v) || len(v) > 240 || strings.ContainsAny(v, "\x00\r\n\t") {
				return nil, fmt.Errorf("row %d: invalid text", row)
			}
			v = strings.TrimSpace(v)
			if v != "" && strings.ContainsRune("=+-@", rune(v[0])) {
				return nil, fmt.Errorf("row %d: formula-like text is not accepted", row)
			}
		}
		get := func(key string) string {
			if i, ok := positions[key]; ok {
				return strings.TrimSpace(values[i])
			}
			return ""
		}
		r := domain.Requirement{InternalPO: get("internal_po"), Line: get("line"), SupplierAlias: get("supplier_alias"), InternalPart: get("internal_part"), Quantity: get("quantity"), UOM: get("uom"), RequiredDate: get("required_arrival_date"), SourceKey: get("source_order_key"), BuyerAlias: get("buyer_alias"), Origin: "fictional"}
		if r.BuyerAlias == "" {
			r.BuyerAlias = "demo_buyer"
		}
		if r.InternalPO == "" || r.Line == "" || r.SupplierAlias == "" || r.InternalPart == "" || r.SourceKey == "" || r.UOM != "EA" || r.BuyerAlias != "demo_buyer" || !decimal.MatchString(r.Quantity) || strings.Trim(r.Quantity, "0.") == "" {
			return nil, fmt.Errorf("row %d: required values, positive decimal quantity, EA unit, or buyer alias are invalid", row)
		}
		if d, err := time.Parse("2006-01-02", r.RequiredDate); err != nil || d.Format("2006-01-02") != r.RequiredDate {
			return nil, fmt.Errorf("row %d: date must be YYYY-MM-DD", row)
		}
		if seen[r.SourceKey] {
			return nil, fmt.Errorf("row %d: duplicate source mapping", row)
		}
		seen[r.SourceKey] = true
		result = append(result, r)
	}
	if len(result) == 0 {
		return nil, errors.New("CSV contains no requirements")
	}
	return result, nil
}

// Template has a deterministic column order and no source-provided formulas.
func Template(out io.Writer, requirements []domain.Requirement) error {
	writer := csv.NewWriter(out)
	if err := writer.Write(headers); err != nil {
		return err
	}
	for _, r := range requirements {
		if err := writer.Write([]string{r.InternalPO, r.Line, r.SupplierAlias, r.InternalPart, r.Quantity, r.UOM, r.RequiredDate, r.SourceKey, r.BuyerAlias}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}
