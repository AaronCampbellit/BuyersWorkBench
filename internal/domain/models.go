// Package domain implements pure purchasing calculations and normalized source
// validation. Fictional fixtures exercise the same rules as accepted source
// observations; a fixture is never evidence of a live supplier integration.
package domain

import "time"

const (
	SchemaVersion = "1.0"
	SeedVersion   = "seed-1.0"
	BusinessDate  = "2026-10-19"
)

// Record stores normalized facts. Empty optional decimal strings mean unknown;
// callers must never convert an unknown quantity to zero. ArrivalKind may also
// describe a ship date; such a date cannot be used as an arrival commitment.
type Record struct {
	SourceKey         string     `json:"source_key"`
	SourceOrderID     string     `json:"source_order_id"`
	SourceLineID      string     `json:"source_line_id"`
	SupplierAlias     string     `json:"supplier_alias"`
	SupplierPart      string     `json:"supplier_part"`
	Description       string     `json:"description"`
	Origin            string     `json:"origin"`
	Ordered           string     `json:"ordered"`
	Cancelled         string     `json:"cancelled"`
	Shipped           string     `json:"shipped"`
	Delivered         string     `json:"delivered"`
	UOM               string     `json:"uom"`
	Status            string     `json:"status"`
	Confirmation      string     `json:"confirmation"`
	PlacedAt          *time.Time `json:"placed_at"`
	ConfirmedAt       *time.Time `json:"confirmed_at"`
	SourceEventAt     *time.Time `json:"source_event_at"`
	ArrivalDate       string     `json:"arrival_date"`
	ArrivalKind       string     `json:"arrival_kind"`
	ArrivalLabel      string     `json:"arrival_label"`
	OriginalDate      string     `json:"original_date"`
	OriginalKind      string     `json:"original_kind"`
	OriginalLabel     string     `json:"original_label"`
	RemainingSchedule string     `json:"remaining_schedule"`
}

type Requirement struct {
	SourceKey     string `json:"source_key"`
	InternalPO    string `json:"internal_po"`
	Line          string `json:"line"`
	SupplierAlias string `json:"supplier_alias"`
	InternalPart  string `json:"internal_part"`
	Quantity      string `json:"quantity"`
	UOM           string `json:"uom"`
	RequiredDate  string `json:"required_date"`
	BuyerAlias    string `json:"buyer_alias"`
	Origin        string `json:"origin"`
}

type Connector struct {
	Key     string `json:"key"`
	Version string `json:"version"`
}

type Scope struct {
	Statuses         []string `json:"statuses"`
	DateFrom         string   `json:"date_from"`
	DateTo           string   `json:"date_to"`
	Complete         bool     `json:"complete"`
	PagesCollected   int      `json:"pages_collected"`
	ExpectedRecords  *int     `json:"expected_records"`
	RecordsCollected int      `json:"records_collected"`
	Warnings         []string `json:"warnings"`
}

type Batch struct {
	SchemaVersion string    `json:"schema_version"`
	RunID         string    `json:"run_id"`
	ConnectionID  string    `json:"connection_id"`
	WorkspaceID   string    `json:"workspace_id"`
	Generation    int64     `json:"generation"`
	Connector     Connector `json:"connector"`
	Origin        string    `json:"origin"`
	ObservedAt    time.Time `json:"observed_at"`
	Scope         Scope     `json:"scope"`
	Records       []Record  `json:"records"`
}

// Binding comes from trusted server-side connection/run state, never a source
// payload. Scope's statuses and date bounds are the permitted requested scope.
type Binding struct {
	RunID        string
	ConnectionID string
	WorkspaceID  string
	Generation   int64
	Connector    Connector
	Origin       string
	Scope        Scope
}

type Condition struct {
	Rule        string            `json:"rule"`
	Severity    string            `json:"severity"`
	Explanation string            `json:"explanation"`
	DelayDays   int               `json:"delay_days"`
	Active      bool              `json:"active"`
	Evidence    map[string]string `json:"evidence"`
}

type Change struct {
	Field     string `json:"field"`
	Before    string `json:"before"`
	After     string `json:"after"`
	Semantics string `json:"semantics"`
	DeltaDays int    `json:"delta_days"`
	Worsened  bool   `json:"worsened"`
}

type Quantities struct {
	NetOrdered string `json:"net_ordered"`
	Unshipped  string `json:"unshipped"`
	Unreceived string `json:"unreceived"`
}

type Quote struct {
	ID           string `json:"id"`
	Supplier     string `json:"supplier"`
	UnitPrice    string `json:"unit_price"`
	Freight      string `json:"freight"`
	OtherCharges string `json:"other_charges"`
	Approval     string `json:"approval"`
	ArrivalDate  string `json:"arrival_date"`
	UOM          string `json:"uom"`
	Currency     string `json:"currency"`
}

type QuoteComparison struct {
	ID            string   `json:"id"`
	Description   string   `json:"description"`
	Origin        string   `json:"origin"`
	Quantity      string   `json:"quantity"`
	UOM           string   `json:"uom"`
	Currency      string   `json:"currency"`
	RequiredDate  string   `json:"required_date"`
	ExcludedCosts []string `json:"excluded_costs"`
	Quotes        []Quote  `json:"quotes"`
}

type QuoteResult struct {
	Quote
	ModeledTotal string   `json:"modeled_total"`
	Eligible     bool     `json:"eligible"`
	Reasons      []string `json:"reasons"`
	Rank         int      `json:"rank"`
}

type QuoteCalculation struct {
	Results            []QuoteResult `json:"results"`
	WinnerID           string        `json:"winner_id"`
	WinnerSupplier     string        `json:"winner_supplier"`
	SavingsVersusAtlas string        `json:"savings_versus_atlas"`
}
