package domain

import (
	"errors"
	"math/big"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// PurchaseOrderInput is locally recorded purchasing intent. Supplier observations
// and receipts remain separate facts; recording an action sends nothing outside
// the workbench. Supplier UUID, scope, active state, and generation are enforced
// by the transactional service rather than this pure calculation package.
type PurchaseOrderInput struct {
	SupplierID   string `json:"supplier_id"`
	Description  string `json:"description"`
	Quantity     string `json:"quantity"`
	UOM          string `json:"uom"`
	Currency     string `json:"currency"`
	UnitPrice    string `json:"unit_price"`
	Freight      string `json:"freight"`
	OtherCharges string `json:"other_charges"`
	RequiredDate string `json:"required_date"`
	Notes        string `json:"notes"`
}
type PurchaseOrderActionInput struct {
	Action    string `json:"action"`
	Quantity  string `json:"quantity"`
	Reference string `json:"reference"`
	Note      string `json:"note"`
	Confirm   bool   `json:"confirm"`
}

func purchaseOrderText(value string, max int, multiline bool) bool {
	return utf8.ValidString(value) && utf8.RuneCountInString(value) <= max && !strings.ContainsFunc(value, func(r rune) bool { return unicode.IsControl(r) && !(multiline && (r == '\n' || r == '\t')) })
}
func NormalizePurchaseOrder(in PurchaseOrderInput) (PurchaseOrderInput, error) {
	in.SupplierID = strings.TrimSpace(in.SupplierID)
	in.Description = strings.TrimSpace(in.Description)
	in.Quantity = strings.TrimSpace(in.Quantity)
	in.UOM = strings.TrimSpace(in.UOM)
	in.Currency = strings.TrimSpace(in.Currency)
	in.UnitPrice = strings.TrimSpace(in.UnitPrice)
	in.Freight = strings.TrimSpace(in.Freight)
	in.OtherCharges = strings.TrimSpace(in.OtherCharges)
	in.RequiredDate = strings.TrimSpace(in.RequiredDate)
	in.Notes = strings.TrimSpace(in.Notes)
	if in.SupplierID == "" || in.Description == "" || !purchaseOrderText(in.Description, 500, false) || in.UOM == "" || !purchaseOrderText(in.UOM, 24, false) || in.Currency != "USD" || !ValidDate(in.RequiredDate) || !purchaseOrderText(in.Notes, 4000, true) {
		return in, errors.New("invalid purchase order supplier, description, unit, currency, date, or notes")
	}
	quantity, err := decimal(in.Quantity)
	if err != nil || quantity.Sign() <= 0 {
		return in, errors.New("purchase order quantity must be a positive exact decimal")
	}
	for _, value := range []string{in.UnitPrice, in.Freight, in.OtherCharges} {
		if !ValidDecimal(value) {
			return in, errors.New("purchase order cost inputs must be complete nonnegative exact decimals")
		}
	}
	return in, nil
}
func PurchaseOrderTotal(in PurchaseOrderInput) (string, error) {
	normalized, err := NormalizePurchaseOrder(in)
	if err != nil {
		return "", err
	}
	quantity, _ := decimal(normalized.Quantity)
	price, _ := decimal(normalized.UnitPrice)
	freight, _ := decimal(normalized.Freight)
	other, _ := decimal(normalized.OtherCharges)
	total := new(big.Rat).Mul(quantity, price)
	total.Add(total, freight).Add(total, other)
	return formatDecimal(total, 2), nil
}
func PurchaseOrderActions(status string) []string {
	switch status {
	case "draft":
		return []string{"approve", "cancel"}
	case "approved":
		return []string{"revise", "place", "cancel"}
	case "placed", "partially_received":
		return []string{"receive", "cancel"}
	case "received":
		return []string{"close"}
	default:
		return []string{}
	}
}
func ApplyPurchaseOrderAction(status, ordered, received string, in PurchaseOrderActionInput) (string, string, error) {
	invalid := func() (string, string, error) {
		return status, received, errors.New("purchase order action is invalid for the current state or quantity")
	}
	quantity, err := decimal(ordered)
	if err != nil || quantity.Sign() <= 0 {
		return invalid()
	}
	current, err := decimal(received)
	if err != nil || current.Cmp(quantity) > 0 {
		return invalid()
	}
	in.Action = strings.TrimSpace(in.Action)
	in.Quantity = strings.TrimSpace(in.Quantity)
	in.Reference = strings.TrimSpace(in.Reference)
	in.Note = strings.TrimSpace(in.Note)
	if !slices.Contains(PurchaseOrderActions(status), in.Action) || !purchaseOrderText(in.Reference, 100, false) || !purchaseOrderText(in.Note, 4000, true) {
		return invalid()
	}
	if in.Quantity != "" && in.Action != "receive" {
		return invalid()
	}
	if (status == "draft" || status == "approved" || status == "placed") && current.Sign() != 0 {
		return invalid()
	}
	if status == "partially_received" && (current.Sign() <= 0 || current.Cmp(quantity) >= 0) {
		return invalid()
	}
	if status == "received" && current.Cmp(quantity) != 0 {
		return invalid()
	}
	switch in.Action {
	case "approve":
		return "approved", formatDecimal(current, 0), nil
	case "revise":
		return "draft", formatDecimal(current, 0), nil
	case "place":
		if !in.Confirm || in.Reference == "" {
			return invalid()
		}
		return "placed", formatDecimal(current, 0), nil
	case "receive":
		increment, err := decimal(in.Quantity)
		if err != nil || increment.Sign() <= 0 || in.Reference == "" {
			return invalid()
		}
		next := new(big.Rat).Add(current, increment)
		if next.Cmp(quantity) > 0 {
			return invalid()
		}
		state := "partially_received"
		if next.Cmp(quantity) == 0 {
			state = "received"
		}
		return state, formatDecimal(next, 0), nil
	case "close":
		return "closed", formatDecimal(current, 0), nil
	case "cancel":
		if in.Note == "" {
			return invalid()
		}
		return "cancelled", formatDecimal(current, 0), nil
	default:
		return invalid()
	}
}
