package domain

import (
	"encoding/json"
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"
)

func dateDays(before, after string) int {
	a, errA := time.Parse(time.DateOnly, before)
	b, errB := time.Parse(time.DateOnly, after)
	if errA != nil || errB != nil {
		return 0
	}
	return int(b.Sub(a) / (24 * time.Hour))
}

func arrivalKind(kind string) bool {
	return kind == "committed_arrival" || kind == "estimated_arrival"
}

func positive(s string) bool {
	v, err := decimal(s)
	return err == nil && v.Sign() > 0
}

func hasNeed(r Record, balances Quantities) bool {
	if balances.Unreceived != "" {
		return positive(balances.Unreceived)
	}
	// An explicit open state can establish an open need, but never a fabricated
	// received balance. Unknown or closed states require additional evidence.
	return positive(r.Ordered) && slices.Contains([]string{"open", "partially_shipped", "shipped"}, r.Status)
}

// Evaluate returns active machine conditions only. Persistence resolves absent
// prior conditions after a complete accepted observation, while preserving all
// human workflow and history. now must be the fictional business clock for a
// fictional record and actual current time for a private real record.
func Evaluate(r Record, requirement *Requirement, now time.Time) []Condition {
	return EvaluateWithPrevious(r, requirement, nil, now)
}

// EvaluateWithPrevious adds R02 comparisons without duplicating an already late
// subject's R01 issue. It only uses accepted observations supplied by the caller.
func EvaluateWithPrevious(r Record, requirement *Requirement, previous *Record, now time.Time) []Condition {
	conditions := []Condition{}
	attention := []string{}
	evidence := map[string]string{"source_key": r.SourceKey, "origin": r.Origin}
	if err := ValidateRecord(r); err != nil {
		return []Condition{{Rule: "R05", Severity: "medium", Explanation: "Source facts cannot be evaluated reliably: " + err.Error(), Active: true, Evidence: evidence}}
	}
	balances, _ := Balance(r)
	evidence["unshipped"], evidence["unreceived"] = balances.Unshipped, balances.Unreceived
	need := hasNeed(r, balances)
	if r.Status == "unknown" {
		attention = append(attention, "source order status is unknown")
	}
	if r.Cancelled == "" {
		attention = append(attention, "net ordered quantity is unknown because cancellation quantity is unavailable")
	}
	if requirement == nil {
		attention = append(attention, "no explicit internal requirement mapping")
	} else if err := ValidateRequirement(*requirement); err != nil {
		attention = append(attention, "internal requirement is invalid")
		requirement = nil
	} else if requirement.SourceKey != r.SourceKey || requirement.SupplierAlias != r.SupplierAlias || requirement.UOM != r.UOM {
		attention = append(attention, "internal mapping or quantity unit does not match source")
		requirement = nil
	}

	knownArrival := r.ArrivalDate != "" && arrivalKind(r.ArrivalKind)
	lateDays := 0
	if need && requirement != nil {
		evidence["required_date"] = requirement.RequiredDate
		if !knownArrival {
			if r.ArrivalDate == "" {
				attention = append(attention, "arrival date is unknown; delivery risk cannot be classified as safe")
			} else {
				attention = append(attention, "source provides a ship date; no arrival or transit assumption is available")
			}
		} else {
			lateDays = dateDays(requirement.RequiredDate, r.ArrivalDate)
			if lateDays > 0 {
				label := "Committed arrival"
				if r.ArrivalKind == "estimated_arrival" {
					label = "Estimated arrival"
				}
				conditions = append(conditions, Condition{Rule: "R01", Severity: "high", Explanation: fmt.Sprintf("%s %s is %d calendar days after required arrival %s.", label, r.ArrivalDate, lateDays, requirement.RequiredDate), DelayDays: lateDays, Active: true, Evidence: map[string]string{"arrival_date": r.ArrivalDate, "arrival_kind": r.ArrivalKind, "source_label": r.ArrivalLabel, "required_date": requirement.RequiredDate, "unreceived": balances.Unreceived}})
			}
		}
	}
	if previous != nil && previous.SourceKey == r.SourceKey {
		for _, change := range Changes(*previous, r) {
			if change.Field == "arrival_kind" && need {
				attention = append(attention, "source date semantics changed; inspect the observed history")
			}
			if change.Field != "arrival_date" || !change.Worsened {
				continue
			}
			if lateDays > 0 {
				for i := range conditions {
					if conditions[i].Rule == "R01" {
						conditions[i].Evidence["previous_date"] = change.Before
						conditions[i].Evidence["date_change_days"] = strconv.Itoa(change.DeltaDays)
					}
				}
			} else if need {
				conditions = append(conditions, Condition{Rule: "R02", Severity: "medium", Explanation: fmt.Sprintf("Observed %s moved %d calendar days later, from %s to %s.", strings.ReplaceAll(change.Semantics, "_", " "), change.DeltaDays, change.Before, change.After), Active: true, Evidence: map[string]string{"previous_date": change.Before, "arrival_date": change.After, "arrival_kind": change.Semantics, "date_change_days": strconv.Itoa(change.DeltaDays)}})
			}
		}
	}
	if need && r.Confirmation == "unconfirmed" {
		if r.PlacedAt == nil || now.IsZero() || now.Before(*r.PlacedAt) {
			attention = append(attention, "missing or inconsistent placement time prevents elapsed confirmation check")
		} else if now.Sub(*r.PlacedAt) >= 48*time.Hour {
			conditions = append(conditions, Condition{Rule: "R03", Severity: "medium", Explanation: "Source explicitly reports unconfirmed at least 48 elapsed hours after placement.", Active: true, Evidence: map[string]string{"confirmation": r.Confirmation, "placed_at": r.PlacedAt.UTC().Format(time.RFC3339), "evaluated_at": now.UTC().Format(time.RFC3339), "threshold_hours": "48"}})
		}
	}
	if positive(r.Shipped) && positive(balances.Unshipped) && (r.RemainingSchedule != "known" || !knownArrival || lateDays > 0) {
		severity, explanation := "medium", "Remaining shipment needs a schedule follow-up; a partial shipment does not establish a shortage."
		if lateDays > 0 {
			severity = "high"
			explanation = "Remaining shipment's arrival is later than the internal requirement; shipment is not receipt."
		}
		conditions = append(conditions, Condition{Rule: "R04", Severity: severity, Explanation: explanation, DelayDays: max(lateDays, 0), Active: true, Evidence: map[string]string{"ordered": r.Ordered, "shipped": r.Shipped, "delivered": r.Delivered, "unshipped": balances.Unshipped, "unreceived": balances.Unreceived, "remaining_schedule": r.RemainingSchedule}})
	}
	if len(attention) > 0 {
		conditions = append(conditions, Condition{Rule: "R05", Severity: "medium", Explanation: "Data attention: " + strings.Join(attention, "; ") + ".", Active: true, Evidence: evidence})
	}
	return conditions
}

func canonicalQuantity(s string) string {
	v, err := decimal(s)
	if err != nil {
		return s
	}
	return formatDecimal(v, 0)
}

// Changes compares business values, not collection timestamps, JSON ordering,
// or source-event metadata. No hash remembers previously seen values: each
// accepted adjacent transition in A→B→A→B remains independently observable.
func Changes(before, after Record) []Change {
	if before.SourceKey != after.SourceKey {
		return nil
	}
	changes := []Change{}
	add := func(field, a, b, semantics string) {
		if a != b {
			changes = append(changes, Change{Field: field, Before: a, After: b, Semantics: semantics})
		}
	}
	if before.ArrivalDate != after.ArrivalDate {
		change := Change{Field: "arrival_date", Before: before.ArrivalDate, After: after.ArrivalDate, Semantics: after.ArrivalKind}
		if before.ArrivalKind == after.ArrivalKind && before.ArrivalDate != "" && after.ArrivalDate != "" {
			change.DeltaDays = dateDays(before.ArrivalDate, after.ArrivalDate)
			change.Worsened = change.DeltaDays > 0
		}
		changes = append(changes, change)
	}
	add("arrival_kind", before.ArrivalKind, after.ArrivalKind, "source_date_semantics")
	add("status", before.Status, after.Status, "source_status")
	add("confirmation", before.Confirmation, after.Confirmation, "source_confirmation")
	add("remaining_schedule", before.RemainingSchedule, after.RemainingSchedule, "source_remaining_schedule")
	add("uom", before.UOM, after.UOM, "source_unit")
	add("supplier_alias", before.SupplierAlias, after.SupplierAlias, "source_supplier")
	add("supplier_part", before.SupplierPart, after.SupplierPart, "source_part")
	add("description", before.Description, after.Description, "source_description")
	instant := func(value *time.Time) string {
		if value == nil {
			return ""
		}
		return value.UTC().Format(time.RFC3339Nano)
	}
	add("order_placed_at", instant(before.PlacedAt), instant(after.PlacedAt), "source_order_placement")
	add("confirmed_at", instant(before.ConfirmedAt), instant(after.ConfirmedAt), "source_confirmation_time")
	for _, field := range []struct{ name, old, new string }{{"ordered", before.Ordered, after.Ordered}, {"cancelled", before.Cancelled, after.Cancelled}, {"shipped", before.Shipped, after.Shipped}, {"delivered", before.Delivered, after.Delivered}} {
		add(field.name, canonicalQuantity(field.old), canonicalQuantity(field.new), "source_quantity_"+after.UOM)
	}
	add("documented_original_date", before.OriginalDate, after.OriginalDate, after.OriginalKind)
	add("documented_original_kind", before.OriginalKind, after.OriginalKind, "source_date_semantics")
	return changes
}

// ConditionFingerprint is stable across identical refreshes and ignores the
// operational time in R03 evidence. Database recurrence/revision IDs must still
// distinguish a resolved condition that becomes active again.
func ConditionFingerprint(c Condition) string {
	material := map[string]string{}
	for key, value := range c.Evidence {
		// Evaluation time and R01's one-run change annotation are context,
		// not changes to the current purchasing condition.
		if key == "evaluated_at" || key == "source_label" || (c.Rule == "R01" && (key == "previous_date" || key == "date_change_days")) {
			continue
		}
		if slices.Contains([]string{"ordered", "cancelled", "shipped", "delivered", "unshipped", "unreceived", "net_ordered"}, key) {
			value = canonicalQuantity(value)
		}
		material[key] = value
	}
	data, _ := json.Marshal(struct {
		Rule, Severity, Explanation string
		DelayDays                   int
		Evidence                    map[string]string
	}{c.Rule, c.Severity, c.Explanation, c.DelayDays, material})
	return string(data)
}

// ConditionWorsened compares material machine facts for notification policy.
// Creation and recurrence are tracked by persistence, separately from this
// comparison. A newly later R02 date and a larger unscheduled remainder are
// material even when severity or the absolute required-date gap stays equal.
func ConditionWorsened(before, after Condition) bool {
	severity := func(s string) int {
		switch s {
		case "high":
			return 3
		case "medium":
			return 2
		default:
			return 1
		}
	}
	if severity(after.Severity) > severity(before.Severity) || after.DelayDays > before.DelayDays {
		return true
	}
	if after.Rule == "R02" && before.Rule == "R02" {
		oldDate, newDate := before.Evidence["arrival_date"], after.Evidence["arrival_date"]
		return ValidDate(oldDate) && ValidDate(newDate) && before.Evidence["arrival_kind"] == after.Evidence["arrival_kind"] && newDate > oldDate
	}
	if after.Rule == "R04" && before.Rule == "R04" {
		oldQuantity, newQuantity := before.Evidence["unshipped"], after.Evidence["unshipped"]
		return ValidDecimal(oldQuantity) && ValidDecimal(newQuantity) && CompareDecimals(newQuantity, oldQuantity) > 0
	}
	return false
}

// CompareDecimals compares valid exact decimal strings for sorting. Invalid
// values are never valid totals; callers must validate inputs first.
func CompareDecimals(a, b string) int {
	x, okX := new(big.Rat).SetString(a)
	y, okY := new(big.Rat).SetString(b)
	if !okX || !okY {
		return strings.Compare(a, b)
	}
	return x.Cmp(y)
}
