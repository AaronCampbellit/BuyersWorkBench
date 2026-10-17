package domain

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func businessNow() time.Time { return time.Date(2026, 10, 19, 13, 0, 0, 0, time.UTC) }

func testBinding() Binding {
	return Binding{RunID: "30000000-0000-4000-8000-000000000001", ConnectionID: "20000000-0000-4000-8000-000000000001", WorkspaceID: "10000000-0000-4000-8000-000000000001", Generation: 1, Connector: Connector{Key: "fictional_supplier", Version: SeedVersion}, Origin: "fictional", Scope: FictionalScope()}
}

func findCondition(conditions []Condition, rule string) *Condition {
	for _, c := range conditions {
		if c.Rule == rule {
			return &c
		}
	}
	return nil
}

func TestFictionalFixturePipeline(t *testing.T) {
	baseline := SeedRecords()
	requirements := SeedRequirements()
	if len(baseline) != 4 || len(requirements) != 4 {
		t.Fatal("seed coverage changed")
	}
	if err := ValidateRequirements(requirements); err != nil {
		t.Fatal(err)
	}
	batch := FictionalBatch(baseline, testBinding(), time.Now())
	if err := ValidateBatch(batch, testBinding()); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for i, r := range baseline {
		if r.OriginalDate != "" {
			t.Fatal("first observed value was promoted to original")
		}
		for _, c := range Evaluate(r, &requirements[i], businessNow()) {
			counts[c.Rule]++
		}
	}
	if counts["R01"] != 0 || counts["R03"] != 1 {
		t.Fatalf("unexpected baseline: %v", counts)
	}
	if counts["R05"] != 1 {
		t.Fatalf("unknown arrival must remain visible: %v", counts)
	}
	delayed, err := ApplyScenario(baseline, "SC02")
	if err != nil {
		t.Fatal(err)
	}
	changes := Changes(baseline[0], delayed[0])
	if len(changes) != 1 || changes[0].DeltaDays != 7 || !changes[0].Worsened {
		t.Fatalf("expected 7-day observed change: %+v", changes)
	}
	conditions := EvaluateWithPrevious(delayed[0], &requirements[0], &baseline[0], businessNow())
	risk := findCondition(conditions, "R01")
	if risk == nil || risk.DelayDays != 3 || risk.Severity != "high" || risk.Evidence["date_change_days"] != "7" {
		t.Fatalf("wrong delivery risk: %+v", conditions)
	}
	if findCondition(conditions, "R02") != nil {
		t.Fatal("date change duplicated the late subject")
	}
	if baseline[0].ArrivalDate != "2026-10-21" {
		t.Fatal("scenario mutated its input source")
	}
	if len(Changes(delayed[0], delayed[0])) != 0 {
		t.Fatal("unchanged collection made business changes")
	}
	recovered, err := ApplyScenario(delayed, "SC08")
	if err != nil {
		t.Fatal(err)
	}
	if findCondition(EvaluateWithPrevious(recovered[0], &requirements[0], &delayed[0], businessNow()), "R01") != nil {
		t.Fatal("risk did not resolve")
	}
	again, _ := ApplyScenario(recovered, "delay")
	if len(Changes(baseline[0], delayed[0]))+len(Changes(delayed[0], recovered[0]))+len(Changes(recovered[0], again[0])) != 3 {
		t.Fatal("recurring values lost a transition")
	}
}

func TestArrivalSemanticsAndMapping(t *testing.T) {
	r := SeedRecords()[0]
	req := SeedRequirements()[0]
	r.ArrivalDate = "2026-10-28"
	r.ArrivalKind = "estimated_arrival"
	r.ArrivalLabel = "Estimated delivery"
	conditions := Evaluate(r, &req, businessNow())
	if risk := findCondition(conditions, "R01"); risk == nil || !strings.Contains(risk.Explanation, "Estimated") {
		t.Fatal("estimate was described as commitment")
	}
	for _, kind := range []string{"estimated_ship", "committed_ship"} {
		r.ArrivalKind = kind
		conditions = Evaluate(r, &req, businessNow())
		if findCondition(conditions, "R01") != nil || findCondition(conditions, "R05") == nil {
			t.Fatalf("ship-only date became arrival: %+v", conditions)
		}
	}
	r.ArrivalDate, r.ArrivalKind, r.ArrivalLabel = "", "", ""
	if findCondition(Evaluate(r, &req, businessNow()), "R05") == nil {
		t.Fatal("unknown delivery ranked safe")
	}
	if findCondition(Evaluate(r, nil, businessNow()), "R05") == nil {
		t.Fatal("missing mapping hidden")
	}
	r = SeedRecords()[0]
	req.SupplierAlias = "foreign_supplier"
	conditions = Evaluate(r, &req, businessNow())
	if findCondition(conditions, "R05") == nil || findCondition(conditions, "R01") != nil {
		t.Fatal("foreign mapping evaluated")
	}
}

func TestConfirmationElapsedHours(t *testing.T) {
	r := SeedRecords()[2]
	req := SeedRequirements()[2]
	placed := *r.PlacedAt
	for _, tc := range []struct {
		elapsed time.Duration
		trigger bool
	}{{48*time.Hour - time.Nanosecond, false}, {48 * time.Hour, true}, {49 * time.Hour, true}} {
		got := findCondition(Evaluate(r, &req, placed.Add(tc.elapsed)), "R03") != nil
		if got != tc.trigger {
			t.Fatalf("elapsed %s triggered %v", tc.elapsed, got)
		}
	}
	r.Confirmation = "unavailable"
	if findCondition(Evaluate(r, &req, businessNow()), "R03") != nil {
		t.Fatal("unsupported confirmation became unconfirmed")
	}
	r.Confirmation = "unconfirmed"
	r.PlacedAt = nil
	conditions := Evaluate(r, &req, businessNow())
	if findCondition(conditions, "R03") != nil || findCondition(conditions, "R05") == nil {
		t.Fatal("unknown order age invented")
	}
	r = SeedRecords()[2]
	first := findCondition(Evaluate(r, &req, businessNow()), "R03")
	later := findCondition(Evaluate(r, &req, businessNow().Add(time.Hour)), "R03")
	if ConditionFingerprint(*first) != ConditionFingerprint(*later) {
		t.Fatal("time passing duplicated an unchanged confirmation condition")
	}
}

func TestPartialShipmentIsNotReceiptOrShortage(t *testing.T) {
	records, err := ApplyScenario(SeedRecords(), "partial")
	if err != nil {
		t.Fatal(err)
	}
	r := records[1]
	req := SeedRequirements()[1]
	balances, err := Balance(r)
	if err != nil || balances.Unshipped != "60" || balances.Unreceived != "100" {
		t.Fatalf("wrong balances: %+v %v", balances, err)
	}
	conditions := Evaluate(r, &req, businessNow())
	if followup := findCondition(conditions, "R04"); followup == nil || followup.Severity != "medium" {
		t.Fatalf("missing shipment follow-up: %+v", conditions)
	}
	if findCondition(conditions, "R01") != nil {
		t.Fatal("unknown remainder date became a precise delay")
	}
	r.ArrivalDate, r.ArrivalKind, r.ArrivalLabel, r.RemainingSchedule = "2026-10-24", "committed_arrival", "Fictional commitment", "known"
	if findCondition(Evaluate(r, &req, businessNow()), "R04") != nil {
		t.Fatal("on-time scheduled partial shipment created follow-up")
	}
	r.Delivered = ""
	balances, err = Balance(r)
	if err != nil || balances.Unreceived != "" || balances.Unshipped != "60" {
		t.Fatal("unknown receipt became zero")
	}
	r.Cancelled = ""
	balances, err = Balance(r)
	if err != nil || balances.NetOrdered != "" || balances.Unshipped != "" {
		t.Fatal("unknown cancellation became zero")
	}
}

func TestQuantityValidation(t *testing.T) {
	for _, tc := range []struct{ field, value string }{{"ordered", "-1"}, {"ordered", "NaN"}, {"ordered", "1e2"}, {"ordered", "01"}, {"cancelled", "25"}, {"shipped", "25"}, {"delivered", "1"}, {"ordered", "1.00001"}} {
		r := SeedRecords()[0]
		switch tc.field {
		case "ordered":
			r.Ordered = tc.value
		case "cancelled":
			r.Cancelled = tc.value
		case "shipped":
			r.Shipped = tc.value
		case "delivered":
			r.Delivered = tc.value
		}
		if err := ValidateRecord(r); err == nil {
			t.Fatalf("accepted invalid %s=%s", tc.field, tc.value)
		}
	}
	r := SeedRecords()[1]
	r.Ordered, r.Shipped, r.Delivered = "0.3", "0.1", "0"
	b, err := Balance(r)
	if err != nil || b.Unshipped != "0.2" {
		t.Fatalf("inexact decimal balance: %+v %v", b, err)
	}
	r = SeedRecords()[1]
	r.Cancelled, r.Shipped, r.Delivered = "", "40", "60"
	if _, err := Balance(r); err == nil {
		t.Fatal("unknown cancellation hid a delivered/shipped contradiction")
	}
}

func TestChangesIgnoreFormattingAndOperationalMetadata(t *testing.T) {
	a := SeedRecords()[0]
	b := CloneRecords([]Record{a})[0]
	b.Ordered, b.Cancelled, b.Shipped, b.Delivered = "24.0000", "0.00", "0.0", "0.000"
	b.ArrivalLabel = "Equivalent display label"
	now := time.Now()
	b.SourceEventAt = &now
	if changes := Changes(a, b); len(changes) != 0 {
		t.Fatalf("format-only changes: %+v", changes)
	}
	b.ArrivalKind, b.ArrivalDate = "estimated_arrival", "2026-10-28"
	for _, change := range Changes(a, b) {
		if change.Worsened || change.DeltaDays != 0 {
			t.Fatal("incomparable semantics produced commitment slippage")
		}
	}
	if findCondition(EvaluateWithPrevious(b, &SeedRequirements()[0], &a, businessNow()), "R05") == nil {
		t.Fatal("semantics change needs inspection")
	}
}

func TestConditionMaterialRevisionAndWorsening(t *testing.T) {
	baseline := SeedRecords()
	req := SeedRequirements()
	delayed, _ := ApplyScenario(baseline, "delay")
	initial := findCondition(EvaluateWithPrevious(delayed[0], &req[0], &baseline[0], businessNow()), "R01")
	repeat := findCondition(EvaluateWithPrevious(delayed[0], &req[0], &delayed[0], businessNow()), "R01")
	if ConditionFingerprint(*initial) != ConditionFingerprint(*repeat) {
		t.Fatal("one-run R01 change annotation caused a material revision")
	}
	partial, _ := ApplyScenario(baseline, "partial")
	old := findCondition(Evaluate(partial[1], &req[1], businessNow()), "R04")
	partial[1].Shipped = "20"
	worse := findCondition(Evaluate(partial[1], &req[1], businessNow()), "R04")
	if ConditionFingerprint(*old) == ConditionFingerprint(*worse) || !ConditionWorsened(*old, *worse) {
		t.Fatal("larger unscheduled remainder did not worsen condition")
	}
	worse.Evidence["shipped"] = "20.0000"
	formatted := *worse
	formatted.Evidence = map[string]string{}
	for k, v := range worse.Evidence {
		formatted.Evidence[k] = v
	}
	formatted.Evidence["shipped"] = "20"
	if ConditionFingerprint(*worse) != ConditionFingerprint(formatted) {
		t.Fatal("decimal formatting revised a condition")
	}
	a := baseline[0]
	b := a
	b.ArrivalDate = "2026-10-22"
	c := b
	c.ArrivalDate = "2026-10-23"
	r02a := findCondition(EvaluateWithPrevious(b, &req[0], &a, businessNow()), "R02")
	r02b := findCondition(EvaluateWithPrevious(c, &req[0], &b, businessNow()), "R02")
	if r02a == nil || r02b == nil || !ConditionWorsened(*r02a, *r02b) {
		t.Fatal("successive one-day slippages did not notify as worsening")
	}
}

func TestBatchBindingAndCompleteness(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*Batch)
	}{
		{"workspace", func(b *Batch) { b.WorkspaceID = "10000000-0000-4000-8000-000000000002" }},
		{"run", func(b *Batch) { b.RunID = "30000000-0000-4000-8000-000000000002" }},
		{"connection", func(b *Batch) { b.ConnectionID = "20000000-0000-4000-8000-000000000002" }},
		{"generation", func(b *Batch) { b.Generation++ }},
		{"origin", func(b *Batch) { b.Origin = "live_private" }},
		{"version", func(b *Batch) { b.Connector.Version = "unknown" }},
		{"partial", func(b *Batch) { b.Scope.Complete = false }},
		{"count", func(b *Batch) { b.Scope.RecordsCollected = 1 }},
		{"expected", func(b *Batch) { n := 5; b.Scope.ExpectedRecords = &n }},
		{"scope", func(b *Batch) { b.Scope.DateTo = "2026-10-30" }},
		{"statuses", func(b *Batch) { b.Scope.Statuses = []string{"open"} }},
		{"origin row", func(b *Batch) { b.Records[0].Origin = "live_private" }},
		{"duplicate", func(b *Batch) { b.Records[1] = b.Records[0] }},
		{"no pages", func(b *Batch) { b.Scope.PagesCollected = 0 }},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			b := FictionalBatch(SeedRecords(), testBinding(), time.Now())
			tc.mutate(&b)
			if ValidateBatch(b, testBinding()) == nil {
				t.Fatal("bad batch accepted")
			}
		})
	}
	for _, file := range []string{"fictional-baseline.json", "fictional-delayed.json", "fictional-partial-shipment.json", "fictional-incomplete.json"} {
		b, err := DecodeObservationBatch(fixture(file))
		if err != nil {
			t.Fatal(err)
		}
		binding := testBinding()
		binding.RunID = b.RunID
		err = ValidateBatch(b, binding)
		if (err != nil) != (file == "fictional-incomplete.json") {
			t.Fatalf("fixture %s: %v", file, err)
		}
	}
	// A complete empty scope is valid evidence of zero collected records. It
	// carries no instruction to delete previously observed missing orders.
	empty := FictionalBatch(nil, testBinding(), time.Now())
	if err := ValidateBatch(empty, testBinding()); err != nil {
		t.Fatal(err)
	}
}

func TestQuoteFixtureAndExactArithmetic(t *testing.T) {
	c := SeedQuotes()
	if err := ValidateQuoteComparison(c); err != nil {
		t.Fatal(err)
	}
	result := CalculateQuotes(c)
	if result.WinnerID != "meridian" || result.WinnerSupplier != "Fictional Meridian" || result.SavingsVersusAtlas != "205.00" {
		t.Fatalf("wrong winner: %+v", result)
	}
	for i, want := range []string{"1330.00", "1125.00", "990.00"} {
		if result.Results[i].ModeledTotal != want {
			t.Fatalf("wrong total: %+v", result.Results[i])
		}
	}
	if result.Results[2].Eligible || result.Results[2].Rank != 0 || len(result.Results[2].Reasons) != 2 {
		t.Fatal("unapproved late quote was eligible")
	}
	c.Quantity = "0.1"
	for i := range c.Quotes {
		c.Quotes[i].UnitPrice = "0.2"
		c.Quotes[i].Freight = "0.1"
		c.Quotes[i].OtherCharges = "0"
	}
	result = CalculateQuotes(c)
	if result.Results[0].ModeledTotal != "0.12" {
		t.Fatalf("inexact money: %s", result.Results[0].ModeledTotal)
	}
	c.Quantity = "0.0001"
	c.Quotes[0].UnitPrice = "0.0001"
	c.Quotes[0].Freight = "0"
	if got := CalculateQuotes(c).Results[0].ModeledTotal; got != "0.00000001" {
		t.Fatalf("fractional cent was silently rounded: %s", got)
	}
}

func TestQuoteUnknownOrIncompatibleInputsAreIneligible(t *testing.T) {
	mutations := []struct {
		name   string
		mutate func(*QuoteComparison)
	}{
		{"arrival", func(c *QuoteComparison) { c.Quotes[0].ArrivalDate = "" }},
		{"date", func(c *QuoteComparison) { c.Quotes[0].ArrivalDate = "2026-02-30" }},
		{"required", func(c *QuoteComparison) { c.RequiredDate = "" }},
		{"approval", func(c *QuoteComparison) { c.Quotes[0].Approval = "unknown" }},
		{"currency", func(c *QuoteComparison) { c.Quotes[0].Currency = "EUR" }},
		{"unit", func(c *QuoteComparison) { c.Quotes[0].UOM = "KG" }},
		{"freight unknown", func(c *QuoteComparison) { c.Quotes[0].Freight = "" }},
		{"price negative", func(c *QuoteComparison) { c.Quotes[0].UnitPrice = "-1" }},
		{"quantity zero", func(c *QuoteComparison) { c.Quantity = "0" }},
		{"quantity unknown", func(c *QuoteComparison) { c.Quantity = "" }},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			c := SeedQuotes()
			tc.mutate(&c)
			r := CalculateQuotes(c).Results[0]
			if r.Eligible || r.Rank != 0 || len(r.Reasons) == 0 {
				t.Fatal("unreliable quote eligible")
			}
		})
	}
}

func TestScenarioIsolationAndFailure(t *testing.T) {
	a, b := SeedRecords(), SeedRecords()
	*a[0].PlacedAt = time.Now()
	if a[0].PlacedAt.Equal(*b[0].PlacedAt) {
		t.Fatal("seeds share mutable timestamp")
	}
	clone := CloneRecords(b)
	*clone[0].PlacedAt = time.Now()
	if clone[0].PlacedAt.Equal(*b[0].PlacedAt) {
		t.Fatal("clone shares mutable timestamp")
	}
	before, _ := json.Marshal(b)
	if _, err := ApplyScenario(b, "failure"); !errors.Is(err, ErrIncompleteSource) {
		t.Fatal("failure became success")
	}
	after, _ := json.Marshal(b)
	if string(before) != string(after) {
		t.Fatal("failed scenario mutated source")
	}
	if _, err := ApplyScenario(b, "arbitrary"); err == nil {
		t.Fatal("unknown scenario accepted")
	}
	b[0].Origin = "live_private"
	for _, key := range []string{"baseline", "repeat", "delay", "failure", "partial"} {
		if _, err := ApplyScenario(b, key); err == nil {
			t.Fatal("scenario accepted private source")
		}
	}
	confirmed, err := ApplyScenario(SeedRecords(), "confirm")
	if err != nil {
		t.Fatal(err)
	}
	if findCondition(Evaluate(confirmed[2], &SeedRequirements()[2], businessNow()), "R03") != nil {
		t.Fatal("confirmation did not clear condition")
	}
}

func TestDecodeRejectsUnknownAndMultipleObjects(t *testing.T) {
	data := fixture("fictional-baseline.json")
	unknown := strings.Replace(string(data), `"schema_version": "1.0"`, `"schema_version": "1.0", "arbitrary_url": "https://example.invalid"`, 1)
	if _, err := DecodeObservationBatch([]byte(unknown)); err == nil {
		t.Fatal("unknown source field accepted")
	}
	if _, err := DecodeObservationBatch(append(data, []byte("{}")...)); err == nil {
		t.Fatal("multiple JSON objects accepted")
	}
	for _, key := range []string{"cancelled", "complete", "documented_original_date", "confirmed_at"} {
		var document map[string]any
		if err := json.Unmarshal(data, &document); err != nil {
			t.Fatal(err)
		}
		row := document["records"].([]any)[0].(map[string]any)
		switch key {
		case "cancelled":
			delete(row["quantities"].(map[string]any), key)
		case "complete":
			delete(document["scope"].(map[string]any), key)
		case "documented_original_date":
			delete(row, key)
		case "confirmed_at":
			delete(row["confirmation"].(map[string]any), key)
		}
		missing, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeObservationBatch(missing); err == nil {
			t.Fatalf("omitted wire field accepted: %s", key)
		}
	}
}
