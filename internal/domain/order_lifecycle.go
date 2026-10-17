package domain

// ObservedOrderLifecycle describes supplier observations, independently from an
// internally authored purchase order's approval, placement, and receipt workflow.
type ObservedOrderLifecycle struct {
	FulfillmentState  string   `json:"fulfillment_state"`
	ConfirmationState string   `json:"confirmation_state"`
	CancellationState string   `json:"cancellation_state"`
	ReceiptState      string   `json:"receipt_state"`
	NetOrdered        string   `json:"net_ordered"`
	Unshipped         string   `json:"unshipped"`
	Unreceived        string   `json:"unreceived"`
	Evidence          string   `json:"evidence"`
	Issues            []string `json:"issues"`
}

func DeriveObservedOrderLifecycle(r Record) ObservedOrderLifecycle {
	l := ObservedOrderLifecycle{FulfillmentState: "unknown", ConfirmationState: r.Confirmation, CancellationState: "unknown", ReceiptState: "unavailable", Evidence: "insufficient", Issues: []string{}}
	if r.Confirmation != "confirmed" && r.Confirmation != "unconfirmed" && r.Confirmation != "unavailable" {
		l.ConfirmationState = "unavailable"
		l.Issues = append(l.Issues, "Source confirmation is unavailable or unsupported.")
	}
	balance, err := Balance(r)
	if err != nil {
		l.Issues = append(l.Issues, "Source quantities are inconsistent or invalid; fulfillment cannot be determined.")
		return l
	}
	if CompareDecimals(r.Ordered, "0") <= 0 {
		l.Issues = append(l.Issues, "A positive ordered quantity is required to determine fulfillment.")
		return l
	}
	l.NetOrdered, l.Unshipped, l.Unreceived = balance.NetOrdered, balance.Unshipped, balance.Unreceived
	if r.Cancelled != "" {
		switch {
		case CompareDecimals(r.Cancelled, "0") == 0:
			l.CancellationState = "none"
		case CompareDecimals(r.Cancelled, r.Ordered) == 0:
			l.CancellationState = "full"
		default:
			l.CancellationState = "partial"
		}
	}
	// Reject contradictions rather than letting a reported status override known
	// line quantities. "open" is a broad source state and can coexist with a
	// partial shipment or delivery.
	contradiction := false
	if (r.Status == "shipped" || r.Status == "partially_shipped") && r.Shipped != "" && CompareDecimals(r.Shipped, "0") == 0 {
		contradiction = true
	}
	if r.Status == "delivered" && ((r.Delivered != "" && CompareDecimals(r.Delivered, "0") == 0) || (r.Shipped != "" && CompareDecimals(r.Shipped, "0") == 0)) {
		contradiction = true
	}
	if l.NetOrdered != "" {
		switch r.Status {
		case "shipped":
			contradiction = contradiction || (r.Shipped != "" && CompareDecimals(r.Shipped, l.NetOrdered) != 0)
		case "partially_shipped":
			contradiction = contradiction || (r.Shipped != "" && (CompareDecimals(r.Shipped, "0") <= 0 || CompareDecimals(r.Shipped, l.NetOrdered) >= 0))
		case "delivered":
			contradiction = contradiction || (r.Delivered != "" && CompareDecimals(r.Delivered, l.NetOrdered) != 0) || (r.Shipped != "" && CompareDecimals(r.Shipped, l.NetOrdered) != 0)
		case "cancelled":
			contradiction = contradiction || l.CancellationState != "full"
		}
	} else if r.Status == "cancelled" {
		contradiction = (r.Shipped != "" && CompareDecimals(r.Shipped, "0") > 0) || (r.Delivered != "" && CompareDecimals(r.Delivered, "0") > 0)
	}
	if contradiction {
		l.Issues = append(l.Issues, "Reported source status conflicts with known quantities; review the source facts.")
		return l
	}
	// A known shipment quantity is a lower bound on progress. It cannot demote
	// an explicit delivered source status merely because delivered quantity is
	// unavailable. Preserve that reported state without inventing a balance or
	// an internal receipt; contradictory known quantities were rejected above.
	if r.Status == "delivered" && r.Delivered == "" {
		l.FulfillmentState = "delivered"
		l.Evidence = "source_status"
		return l
	}
	if l.NetOrdered != "" {
		switch {
		case l.CancellationState == "full":
			l.FulfillmentState = "cancelled"
		case r.Delivered != "" && CompareDecimals(r.Delivered, l.NetOrdered) == 0:
			l.FulfillmentState = "delivered"
		case r.Delivered != "" && CompareDecimals(r.Delivered, "0") > 0:
			l.FulfillmentState = "partially_delivered"
		case r.Shipped != "" && CompareDecimals(r.Shipped, l.NetOrdered) == 0:
			l.FulfillmentState = "shipped"
		case r.Shipped != "" && CompareDecimals(r.Shipped, "0") > 0:
			l.FulfillmentState = "partially_shipped"
		case r.Shipped != "" && CompareDecimals(r.Shipped, "0") == 0:
			l.FulfillmentState = "open"
		}
		if l.FulfillmentState != "unknown" {
			l.Evidence = "quantities"
			return l
		}
	}
	// Explicit source status remains labeled as such when balances are unknown.
	// It does not manufacture quantities, timestamps, or internal receipt facts.
	switch r.Status {
	case "open", "partially_shipped", "shipped", "delivered", "cancelled":
		l.FulfillmentState = r.Status
		l.Evidence = "source_status"
	default:
		l.Issues = append(l.Issues, "Source fulfillment status is unknown or unsupported.")
	}
	return l
}
