package workbench

import (
	"time"

	"buyersworkbench/internal/domain"
)

// PurchaseOrder is an internal purchasing record. It is never a supplier
// observation and changing its lifecycle cannot modify accepted source facts.
type PurchaseOrder struct {
	ID         string `json:"id"`
	Number     string `json:"number"`
	Generation int64  `json:"generation"`
	domain.PurchaseOrderInput
	SupplierAlias      string                    `json:"supplier_alias"`
	SupplierName       string                    `json:"supplier_name"`
	Origin             string                    `json:"origin"`
	Status             string                    `json:"status"`
	Revision           int64                     `json:"revision"`
	ReceivedQuantity   string                    `json:"received_quantity"`
	PlacementReference string                    `json:"placement_reference"`
	ModeledTotal       string                    `json:"modeled_total"`
	SourceQuoteID      string                    `json:"source_quote_id"`
	QuoteSource        *PurchaseOrderQuoteSource `json:"quote_source"`
	AllowedActions     []string                  `json:"allowed_actions"`
	CreatedAt          time.Time                 `json:"created_at"`
	UpdatedAt          time.Time                 `json:"updated_at"`
}

// The source snapshot remains fixed when comparison inputs or draft terms edit.
type PurchaseOrderQuoteSource struct {
	ComparisonID  string       `json:"comparison_id"`
	Revision      int64        `json:"revision"`
	Quote         domain.Quote `json:"quote"`
	Quantity      string       `json:"quantity"`
	Description   string       `json:"description"`
	RequiredDate  string       `json:"required_date"`
	ExcludedCosts []string     `json:"excluded_costs"`
}

type PurchaseOrderEvent struct {
	ID         string    `json:"id"`
	Action     string    `json:"action"`
	FromStatus string    `json:"from_status"`
	ToStatus   string    `json:"to_status"`
	Quantity   string    `json:"quantity"`
	Reference  string    `json:"reference"`
	Note       string    `json:"note"`
	ActorName  string    `json:"actor_name"`
	CreatedAt  time.Time `json:"created_at"`
}

type PurchaseOrderDetail struct {
	Order  PurchaseOrder        `json:"order"`
	Events []PurchaseOrderEvent `json:"events"`
}
