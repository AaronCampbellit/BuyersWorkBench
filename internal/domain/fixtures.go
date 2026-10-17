package domain

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// These are owned, immutable runtime copies of the fictional planning examples,
// not reads from the imported specification. See fixtures/README.md.
//
//go:embed fixtures/*.json
var fixtures embed.FS

type wireDate struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
	Label string `json:"source_label"`
}

type wireRecord struct {
	SourceKey     string     `json:"record_key"`
	SourceOrderID string     `json:"source_order_id"`
	SourceLineID  string     `json:"source_line_id"`
	SupplierAlias string     `json:"supplier_alias"`
	SupplierPart  string     `json:"supplier_part"`
	Description   string     `json:"item_description"`
	PlacedAt      *time.Time `json:"order_placed_at"`
	Confirmation  struct {
		State string     `json:"state"`
		At    *time.Time `json:"confirmed_at"`
	} `json:"confirmation"`
	Status     string `json:"status"`
	Quantities struct {
		Ordered   string  `json:"ordered"`
		Cancelled *string `json:"cancelled"`
		Shipped   *string `json:"shipped"`
		Delivered *string `json:"delivered"`
		UOM       string  `json:"uom"`
	} `json:"quantities"`
	LatestDate        *wireDate  `json:"latest_date"`
	OriginalDate      *wireDate  `json:"documented_original_date"`
	RemainingSchedule string     `json:"remaining_schedule"`
	SourceEventAt     *time.Time `json:"source_event_at"`
}

func strictJSON(data []byte, target any) error {
	if len(data) > 4*1024*1024 {
		return errors.New("normalized source input exceeds four MiB")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return errors.New("invalid normalized source JSON")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("normalized source JSON must contain one object")
	}
	return nil
}

func optionalString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func requireWireFields(data []byte, fields ...string) (map[string]json.RawMessage, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil || object == nil {
		return nil, errors.New("expected normalized source object")
	}
	for _, field := range fields {
		if _, ok := object[field]; !ok {
			return nil, errors.New("normalized source object is missing required fields")
		}
	}
	return object, nil
}

// Optional values must be explicitly null in the wire contract. Distinguishing
// omitted fields catches connector regressions rather than silently reclassifying
// missing quantities or dates as a supported unknown value.
func validateWireShape(data []byte) error {
	root, err := requireWireFields(data, "schema_version", "run_id", "connection_id", "workspace_id", "generation", "connector", "origin", "observed_at", "scope", "records")
	if err != nil {
		return err
	}
	if _, err = requireWireFields(root["connector"], "key", "version"); err != nil {
		return err
	}
	scope, err := requireWireFields(root["scope"], "statuses", "date_from", "date_to", "complete", "pages_collected", "expected_records", "records_collected", "warnings")
	if err != nil {
		return err
	}
	for _, key := range []string{"statuses", "warnings"} {
		if bytes.Equal(bytes.TrimSpace(scope[key]), []byte("null")) {
			return errors.New("scope arrays cannot be null")
		}
	}
	var rows []json.RawMessage
	if err = json.Unmarshal(root["records"], &rows); err != nil || bytes.Equal(bytes.TrimSpace(root["records"]), []byte("null")) {
		return errors.New("expected normalized records array")
	}
	for _, row := range rows {
		r, err := requireWireFields(row, "record_key", "source_order_id", "source_line_id", "supplier_alias", "supplier_part", "item_description", "order_placed_at", "confirmation", "status", "quantities", "latest_date", "documented_original_date", "remaining_schedule", "source_event_at")
		if err != nil {
			return err
		}
		if _, err = requireWireFields(r["confirmation"], "state", "confirmed_at"); err != nil {
			return err
		}
		if _, err = requireWireFields(r["quantities"], "ordered", "cancelled", "shipped", "delivered", "uom"); err != nil {
			return err
		}
		for _, key := range []string{"latest_date", "documented_original_date"} {
			if !bytes.Equal(bytes.TrimSpace(r[key]), []byte("null")) {
				if _, err = requireWireFields(r[key], "kind", "value", "source_label"); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// DecodeObservationBatch decodes the narrow original connector wire contract.
// The caller must ValidateBatch against trusted server-side run binding before
// staging/publication; successfully decoding JSON is not completeness evidence.
func DecodeObservationBatch(data []byte) (Batch, error) {
	if len(data) > 4*1024*1024 {
		return Batch{}, errors.New("normalized source input exceeds four MiB")
	}
	if err := validateWireShape(data); err != nil {
		return Batch{}, err
	}
	var w struct {
		SchemaVersion string    `json:"schema_version"`
		RunID         string    `json:"run_id"`
		ConnectionID  string    `json:"connection_id"`
		WorkspaceID   string    `json:"workspace_id"`
		Generation    int64     `json:"generation"`
		Connector     Connector `json:"connector"`
		Origin        string    `json:"origin"`
		ObservedAt    time.Time `json:"observed_at"`
		Scope         struct {
			Statuses         []string `json:"statuses"`
			DateFrom         *string  `json:"date_from"`
			DateTo           *string  `json:"date_to"`
			Complete         bool     `json:"complete"`
			PagesCollected   int      `json:"pages_collected"`
			ExpectedRecords  *int     `json:"expected_records"`
			RecordsCollected int      `json:"records_collected"`
			Warnings         []string `json:"warnings"`
		} `json:"scope"`
		Records []wireRecord `json:"records"`
	}
	if err := strictJSON(data, &w); err != nil {
		return Batch{}, err
	}
	b := Batch{SchemaVersion: w.SchemaVersion, RunID: w.RunID, ConnectionID: w.ConnectionID, WorkspaceID: w.WorkspaceID, Generation: w.Generation, Connector: w.Connector, Origin: w.Origin, ObservedAt: w.ObservedAt,
		Scope: Scope{Statuses: w.Scope.Statuses, DateFrom: optionalString(w.Scope.DateFrom), DateTo: optionalString(w.Scope.DateTo), Complete: w.Scope.Complete, PagesCollected: w.Scope.PagesCollected, ExpectedRecords: w.Scope.ExpectedRecords, RecordsCollected: w.Scope.RecordsCollected, Warnings: w.Scope.Warnings}, Records: make([]Record, 0, len(w.Records))}
	for _, v := range w.Records {
		r := Record{SourceKey: v.SourceKey, SourceOrderID: v.SourceOrderID, SourceLineID: v.SourceLineID, SupplierAlias: v.SupplierAlias, SupplierPart: v.SupplierPart, Description: v.Description, Origin: w.Origin,
			Ordered: v.Quantities.Ordered, Cancelled: optionalString(v.Quantities.Cancelled), Shipped: optionalString(v.Quantities.Shipped), Delivered: optionalString(v.Quantities.Delivered), UOM: v.Quantities.UOM,
			Status: v.Status, Confirmation: v.Confirmation.State, PlacedAt: v.PlacedAt, ConfirmedAt: v.Confirmation.At, SourceEventAt: v.SourceEventAt, RemainingSchedule: v.RemainingSchedule}
		if v.LatestDate != nil {
			r.ArrivalDate, r.ArrivalKind, r.ArrivalLabel = v.LatestDate.Value, v.LatestDate.Kind, v.LatestDate.Label
		}
		if v.OriginalDate != nil {
			r.OriginalDate, r.OriginalKind, r.OriginalLabel = v.OriginalDate.Value, v.OriginalDate.Kind, v.OriginalDate.Label
		}
		b.Records = append(b.Records, r)
	}
	return b, nil
}

func fixture(name string) []byte {
	data, err := fixtures.ReadFile("fixtures/" + name)
	if err != nil {
		panic("missing embedded fictional fixture")
	}
	return data
}

func SeedRecords() []Record {
	b, err := DecodeObservationBatch(fixture("fictional-baseline.json"))
	if err != nil {
		panic("invalid embedded fictional records")
	}
	return b.Records
}

func SeedRequirements() []Requirement {
	var w struct {
		SchemaVersion    string `json:"schema_version"`
		Origin           string `json:"origin"`
		DatasetVersion   string `json:"dataset_version"`
		BusinessDate     string `json:"business_date"`
		BusinessTimezone string `json:"business_timezone"`
		Requirements     []struct {
			InternalPO    string `json:"internal_po"`
			Line          string `json:"line"`
			SupplierAlias string `json:"supplier_alias"`
			InternalPart  string `json:"internal_part"`
			SourceKey     string `json:"source_record_key"`
			Quantity      string `json:"quantity"`
			UOM           string `json:"uom"`
			RequiredDate  string `json:"required_arrival_date"`
			BuyerAlias    string `json:"buyer_alias"`
		} `json:"requirements"`
	}
	if err := strictJSON(fixture("internal-requirements.json"), &w); err != nil {
		panic("invalid embedded fictional requirements")
	}
	requirements := make([]Requirement, 0, len(w.Requirements))
	for _, v := range w.Requirements {
		requirements = append(requirements, Requirement{SourceKey: v.SourceKey, InternalPO: v.InternalPO, Line: v.Line, SupplierAlias: v.SupplierAlias, InternalPart: v.InternalPart, Quantity: v.Quantity, UOM: v.UOM, RequiredDate: v.RequiredDate, BuyerAlias: v.BuyerAlias, Origin: w.Origin})
	}
	return requirements
}

func SeedQuotes() QuoteComparison {
	var w struct {
		SchemaVersion string   `json:"schema_version"`
		Origin        string   `json:"origin"`
		ID            string   `json:"comparison_id"`
		Description   string   `json:"description"`
		Quantity      string   `json:"quantity"`
		UOM           string   `json:"uom"`
		Currency      string   `json:"currency"`
		RequiredDate  string   `json:"required_arrival_date"`
		ExcludedCosts []string `json:"excluded_costs"`
		Quotes        []struct {
			Supplier         string `json:"supplier"`
			UnitPrice        string `json:"unit_price"`
			Freight          string `json:"freight"`
			OtherCharges     string `json:"other_charges"`
			Approval         string `json:"approval"`
			ArrivalDate      string `json:"arrival_date"`
			ExpectedTotal    string `json:"expected_modeled_total"`
			ExpectedEligible bool   `json:"expected_eligible"`
		} `json:"quotes"`
		ExpectedWinner  string `json:"expected_winner"`
		ExpectedSavings string `json:"expected_savings_vs_atlas"`
	}
	if err := strictJSON(fixture("quotes.json"), &w); err != nil {
		panic("invalid embedded fictional quotes")
	}
	c := QuoteComparison{ID: w.ID, Description: w.Description, Origin: w.Origin, Quantity: w.Quantity, UOM: w.UOM, Currency: w.Currency, RequiredDate: w.RequiredDate, ExcludedCosts: w.ExcludedCosts, Quotes: []Quote{}}
	for _, v := range w.Quotes {
		c.Quotes = append(c.Quotes, Quote{ID: strings.ToLower(strings.TrimPrefix(v.Supplier, "Fictional ")), Supplier: v.Supplier, UnitPrice: v.UnitPrice, Freight: v.Freight, OtherCharges: v.OtherCharges, Approval: v.Approval, ArrivalDate: v.ArrivalDate, UOM: w.UOM, Currency: w.Currency})
	}
	return c
}

func FictionalScope() Scope {
	return Scope{Statuses: []string{"open", "partially_shipped"}, DateFrom: "2026-10-01", DateTo: "2026-10-31", Complete: true, PagesCollected: 1, Warnings: []string{}}
}

// FictionalBatch stamps actual collection time and server binding on fictional
// source records. It never reuses the historical example observation timestamp.
func FictionalBatch(records []Record, binding Binding, observedAt time.Time) Batch {
	scope := binding.Scope
	if len(scope.Statuses) == 0 {
		scope = FictionalScope()
	}
	scope.Statuses = append([]string(nil), scope.Statuses...)
	scope.Complete = true
	scope.PagesCollected = 1
	scope.RecordsCollected = len(records)
	n := len(records)
	scope.ExpectedRecords = &n
	scope.Warnings = []string{}
	return Batch{SchemaVersion: SchemaVersion, RunID: binding.RunID, ConnectionID: binding.ConnectionID, WorkspaceID: binding.WorkspaceID, Generation: binding.Generation, Connector: Connector{Key: "fictional_supplier", Version: SeedVersion}, Origin: "fictional", ObservedAt: observedAt.UTC(), Scope: scope, Records: CloneRecords(records)}
}

func CloneRecords(records []Record) []Record {
	out := append([]Record(nil), records...)
	for i := range out {
		if out[i].PlacedAt != nil {
			value := *out[i].PlacedAt
			out[i].PlacedAt = &value
		}
		if out[i].ConfirmedAt != nil {
			value := *out[i].ConfirmedAt
			out[i].ConfirmedAt = &value
		}
		if out[i].SourceEventAt != nil {
			value := *out[i].SourceEventAt
			out[i].SourceEventAt = &value
		}
	}
	return out
}

var ErrIncompleteSource = errors.New("fictional source collection is incomplete")

type Scenario struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

func Scenarios() []Scenario {
	return []Scenario{
		{Key: "baseline", Title: "Baseline", Description: "Restore the versioned fictional source and collect it."},
		{Key: "delay", Title: "Delivery delay", Description: "Move the fictional Atlas arrival from October 21 to October 28."},
		{Key: "partial", Title: "Partial shipment", Description: "Ship 40 of 100 fictional Meridian units; remaining schedule is unknown."},
		{Key: "confirm", Title: "Confirmation restored", Description: "Confirm the fictional Harbor order."},
		{Key: "failure", Title: "Incomplete source", Description: "Simulate incomplete collection; preserve the last valid observations."},
		{Key: "repeat", Title: "Repeat unchanged", Description: "Collect identical source values without duplicate changes."},
		{Key: "recovery", Title: "Arrival recovery", Description: "Restore the fictional Atlas arrival to October 21."},
	}
}

// ApplyScenario modifies source facts only. Conditions, notifications, history,
// and buyer workflow are produced later by the shared accepted-batch pipeline.
func ApplyScenario(records []Record, key string) ([]Record, error) {
	for _, record := range records {
		if record.Origin != "fictional" {
			return nil, errors.New("scenario only applies to fictional source facts")
		}
	}
	key = strings.ToLower(strings.TrimSpace(key))
	aliases := map[string]string{"sc01": "baseline", "sc02": "delay", "sc03": "partial", "partial-shipment": "partial", "sc04": "confirm", "confirmation": "confirm", "sc05": "failure", "incomplete": "failure", "sc06": "quotes", "sc07": "repeat", "unchanged": "repeat", "sc08": "recovery", "recover": "recovery"}
	if alias, ok := aliases[key]; ok {
		key = alias
	}
	if key == "baseline" {
		return SeedRecords(), nil
	}
	if key == "failure" {
		return nil, ErrIncompleteSource
	}
	out := CloneRecords(records)
	if key == "repeat" || key == "quotes" {
		return out, nil
	}
	target := ""
	switch key {
	case "delay", "recovery":
		target = "SRC-ATLAS-10482:3"
	case "partial":
		target = "SRC-MERIDIAN-10503:1"
	case "confirm":
		target = "SRC-HARBOR-10520:1"
	default:
		return nil, errors.New("unknown fictional scenario")
	}
	for i := range out {
		if out[i].SourceKey != target {
			continue
		}
		switch key {
		case "delay", "recovery":
			out[i].ArrivalDate = "2026-10-28"
			if key == "recovery" {
				out[i].ArrivalDate = "2026-10-21"
			}
			out[i].ArrivalKind = "committed_arrival"
			out[i].ArrivalLabel = "Fictional committed delivery"
		case "partial":
			out[i].Shipped, out[i].Delivered, out[i].Status = "40", "0", "partially_shipped"
			out[i].ArrivalDate, out[i].ArrivalKind, out[i].ArrivalLabel = "", "", ""
			out[i].RemainingSchedule = "unknown"
		case "confirm":
			out[i].Confirmation = "confirmed"
			// This is a fictional business event, not the actual collection time.
			at := time.Date(2026, 10, 19, 13, 0, 0, 0, time.UTC)
			out[i].ConfirmedAt = &at
		}
		return out, nil
	}
	return nil, fmt.Errorf("fictional scenario target is absent from source")
}
