package domain

import (
	"errors"
	"math/big"
	"slices"
	"strings"
)

// ValidateQuote is used for user edits. Unknown optional cost/delivery inputs
// are allowed so they can be represented as insufficient information. Malformed
// or negative known inputs are rejected rather than silently becoming zero.
func ValidateQuote(q Quote) error {
	if !validText(q.ID, 240) || !validText(q.Supplier, 240) {
		return errors.New("quote identity is required")
	}
	for _, v := range []string{q.UnitPrice, q.Freight, q.OtherCharges} {
		if v != "" && !ValidDecimal(v) {
			return errors.New("cost must be a nonnegative decimal with at most four fractional places")
		}
	}
	if !slices.Contains([]string{"approved", "pending", "rejected", "unknown"}, q.Approval) {
		return errors.New("invalid quote approval")
	}
	if q.ArrivalDate != "" && !ValidDate(q.ArrivalDate) {
		return errors.New("invalid quote arrival date")
	}
	if q.UOM != "" && !validText(q.UOM, 24) || q.Currency != "" && (len(q.Currency) != 3 || strings.ToUpper(q.Currency) != q.Currency) {
		return errors.New("invalid quote currency or unit")
	}
	return nil
}

func ValidateQuoteComparison(c QuoteComparison) error {
	quantity, err := decimal(c.Quantity)
	if !validText(c.ID, 240) || c.Origin != "fictional" || err != nil || quantity.Sign() <= 0 || !validText(c.UOM, 24) || !ValidDate(c.RequiredDate) || len(c.Quotes) == 0 || len(c.Quotes) > 100 {
		return errors.New("invalid fictional quote comparison")
	}
	seen := map[string]bool{}
	for _, q := range c.Quotes {
		if err := ValidateQuote(q); err != nil {
			return err
		}
		if seen[q.ID] {
			return errors.New("duplicate quote identity")
		}
		seen[q.ID] = true
	}
	return nil
}

// CalculateQuotes uses exact decimal multiplication and addition. Currency is
// USD in v1. Costs may retain fractional cents: no implicit tax, currency
// conversion, or rounding policy is invented. Rank zero means ineligible.
func CalculateQuotes(c QuoteComparison) QuoteCalculation {
	calculation := QuoteCalculation{Results: []QuoteResult{}}
	quantity, quantityErr := decimal(c.Quantity)
	if quantityErr == nil && quantity.Sign() <= 0 {
		quantityErr = errors.New("nonpositive demand")
	}
	validRequired := ValidDate(c.RequiredDate)
	totals := map[string]*big.Rat{}
	for _, q := range c.Quotes {
		r := QuoteResult{Quote: q, Reasons: []string{}}
		if err := ValidateQuote(q); err != nil {
			r.Reasons = append(r.Reasons, "Invalid quote inputs: "+err.Error())
		}
		if quantityErr != nil {
			r.Reasons = append(r.Reasons, "Demand quantity is unavailable or invalid")
		}
		if c.Currency != "USD" || q.Currency != "USD" {
			r.Reasons = append(r.Reasons, "Supported matching USD currency is required")
		}
		if c.UOM == "" || q.UOM == "" || c.UOM != q.UOM {
			r.Reasons = append(r.Reasons, "A matching quantity unit is required")
		}
		if q.Approval != "approved" {
			r.Reasons = append(r.Reasons, "Supplier approval is not confirmed")
		}
		if !validRequired || !ValidDate(q.ArrivalDate) {
			r.Reasons = append(r.Reasons, "A known arrival and required date are needed")
		} else if q.ArrivalDate > c.RequiredDate {
			r.Reasons = append(r.Reasons, "Arrival is later than the required date")
		}
		price, pErr := decimal(q.UnitPrice)
		freight, fErr := decimal(q.Freight)
		other, oErr := decimal(q.OtherCharges)
		if pErr != nil || fErr != nil || oErr != nil {
			r.Reasons = append(r.Reasons, "Complete nonnegative cost inputs are required")
		} else if quantityErr == nil {
			total := new(big.Rat).Mul(quantity, price)
			total.Add(total, freight).Add(total, other)
			r.ModeledTotal = formatDecimal(total, 2)
			totals[q.ID] = total
		}
		r.Eligible = len(r.Reasons) == 0
		calculation.Results = append(calculation.Results, r)
	}
	indices := []int{}
	for i, result := range calculation.Results {
		if result.Eligible {
			indices = append(indices, i)
		}
	}
	slices.SortFunc(indices, func(i, j int) int {
		a, b := calculation.Results[i], calculation.Results[j]
		if comparison := totals[a.ID].Cmp(totals[b.ID]); comparison != 0 {
			return comparison
		}
		return strings.Compare(a.ID, b.ID)
	})
	for rank, i := range indices {
		calculation.Results[i].Rank = rank + 1
	}
	if len(indices) > 0 {
		winner := calculation.Results[indices[0]]
		calculation.WinnerID, calculation.WinnerSupplier = winner.ID, winner.Supplier
		if atlas, ok := totals["atlas"]; ok {
			difference := new(big.Rat).Sub(atlas, totals[winner.ID])
			if difference.Sign() >= 0 {
				calculation.SavingsVersusAtlas = formatDecimal(difference, 2)
			}
		}
	}
	return calculation
}
