package domain

import (
	"errors"
	"math/big"
	"regexp"
	"strings"
)

// Decimal inputs are bounded, nonnegative base-10 values with at most four
// fractional places. No authoritative calculation uses binary floating point.
var decimalPattern = regexp.MustCompile(`^(0|[1-9][0-9]{0,17})(\.[0-9]{1,4})?$`)

func decimal(s string) (*big.Rat, error) {
	if !decimalPattern.MatchString(s) {
		return nil, errors.New("expected a nonnegative decimal with at most four fractional places")
	}
	v, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, errors.New("invalid decimal")
	}
	return v, nil
}

// ValidDecimal is shared with import and quote-edit boundaries.
func ValidDecimal(s string) bool { _, err := decimal(s); return err == nil }

func formatDecimal(v *big.Rat, minimum int) string {
	// Products of accepted decimal inputs have at most eight fractional digits.
	// Keep fractional cents rather than silently rounding a modeled cost.
	s := v.FloatString(8)
	if p := strings.IndexByte(s, '.'); p >= 0 {
		for len(s)-p-1 > minimum && strings.HasSuffix(s, "0") {
			s = s[:len(s)-1]
		}
		s = strings.TrimSuffix(s, ".")
	}
	return s
}

// Balance returns only balances justified by known source facts. An unavailable
// cancelled/delivered value stays unknown; shipment never implies receipt.
func Balance(r Record) (Quantities, error) {
	ordered, err := decimal(r.Ordered)
	if err != nil {
		return Quantities{}, err
	}
	values := map[string]*big.Rat{}
	for name, raw := range map[string]string{"cancelled": r.Cancelled, "shipped": r.Shipped, "delivered": r.Delivered} {
		if raw == "" {
			continue
		}
		v, err := decimal(raw)
		if err != nil {
			return Quantities{}, errors.New("invalid " + name + " quantity")
		}
		if v.Cmp(ordered) > 0 {
			return Quantities{}, errors.New(name + " quantity exceeds ordered quantity")
		}
		values[name] = v
	}
	if values["delivered"] != nil && values["shipped"] != nil && values["delivered"].Cmp(values["shipped"]) > 0 {
		return Quantities{}, errors.New("delivered quantity exceeds shipped quantity")
	}
	if values["cancelled"] == nil {
		return Quantities{}, nil
	}
	net := new(big.Rat).Sub(ordered, values["cancelled"])
	result := Quantities{NetOrdered: formatDecimal(net, 0)}
	for _, name := range []string{"shipped", "delivered"} {
		if values[name] == nil {
			continue
		}
		if values[name].Cmp(net) > 0 {
			return Quantities{}, errors.New(name + " quantity exceeds net ordered quantity")
		}
		remaining := formatDecimal(new(big.Rat).Sub(net, values[name]), 0)
		if name == "shipped" {
			result.Unshipped = remaining
		} else {
			result.Unreceived = remaining
		}
	}
	return result, nil
}
