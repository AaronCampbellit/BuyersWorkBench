package domain

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func validText(s string, maximum int) bool {
	return len(strings.TrimSpace(s)) > 0 && len(s) <= maximum && !strings.ContainsFunc(s, unicode.IsControl)
}

func ValidDate(s string) bool {
	t, err := time.Parse(time.DateOnly, s)
	return err == nil && t.Format(time.DateOnly) == s
}

func validDateFact(value, kind, label string) bool {
	if value == "" {
		return kind == "" && label == ""
	}
	return ValidDate(value) && slices.Contains([]string{"committed_arrival", "estimated_arrival", "committed_ship", "estimated_ship"}, kind) && validText(label, 240)
}

func ValidateRecord(r Record) error {
	for _, v := range []string{r.SourceKey, r.SourceOrderID, r.SourceLineID, r.SupplierAlias, r.SupplierPart, r.Description} {
		if !validText(v, 240) {
			return errors.New("missing or invalid source identity/description")
		}
	}
	if !slices.Contains([]string{"fictional", "live_private", "sanitized_snapshot"}, r.Origin) {
		return errors.New("invalid record origin")
	}
	if !validText(r.UOM, 24) {
		return errors.New("invalid unit")
	}
	if !slices.Contains([]string{"open", "partially_shipped", "shipped", "delivered", "cancelled", "unknown"}, r.Status) {
		return errors.New("invalid source status")
	}
	if !slices.Contains([]string{"confirmed", "unconfirmed", "unavailable"}, r.Confirmation) {
		return errors.New("invalid confirmation state")
	}
	if r.Confirmation != "confirmed" && r.ConfirmedAt != nil {
		return errors.New("confirmation timestamp contradicts confirmation state")
	}
	if r.PlacedAt != nil && r.ConfirmedAt != nil && r.ConfirmedAt.Before(*r.PlacedAt) {
		return errors.New("confirmation predates order placement")
	}
	if !validDateFact(r.ArrivalDate, r.ArrivalKind, r.ArrivalLabel) || !validDateFact(r.OriginalDate, r.OriginalKind, r.OriginalLabel) {
		return errors.New("invalid source date or date semantics")
	}
	if !slices.Contains([]string{"known", "unknown", "not_applicable"}, r.RemainingSchedule) {
		return errors.New("invalid remaining schedule state")
	}
	_, err := Balance(r)
	return err
}

// ValidateRequirement accepts only explicitly mapped fictional internal data.
// Supplier facts never supply or override the internal purchasing requirement.
func ValidateRequirement(r Requirement) error {
	for _, v := range []string{r.SourceKey, r.InternalPO, r.Line, r.SupplierAlias, r.InternalPart, r.BuyerAlias} {
		if !validText(v, 240) {
			return errors.New("missing or invalid internal requirement field")
		}
	}
	if r.Origin != "fictional" || !ValidDecimal(r.Quantity) || !validText(r.UOM, 24) || !ValidDate(r.RequiredDate) {
		return errors.New("invalid requirement origin, quantity, unit, or required date")
	}
	q, _ := decimal(r.Quantity)
	if q.Sign() == 0 {
		return errors.New("required quantity must be greater than zero")
	}
	return nil
}

// ValidateRequirements also rejects duplicate source mappings and PO lines.
func ValidateRequirements(requirements []Requirement) error {
	if len(requirements) == 0 || len(requirements) > 10000 {
		return errors.New("expected between one and 10000 internal requirements")
	}
	seenSource, seenLine := map[string]bool{}, map[string]bool{}
	for i, r := range requirements {
		if err := ValidateRequirement(r); err != nil {
			return fmt.Errorf("requirement %d: %w", i+1, err)
		}
		line := r.SupplierAlias + "\x00" + r.InternalPO + "\x00" + r.Line
		if seenSource[r.SourceKey] || seenLine[line] {
			return errors.New("duplicate internal requirement mapping")
		}
		seenSource[r.SourceKey], seenLine[line] = true, true
	}
	return nil
}

func sameSet(a, b []string) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}

// ValidateBatch enforces trusted run binding and complete scope before any
// publication. The database must separately fence generation/lease, authorize
// the actor, and guard unexpected drops; schema validity is not authorization.
func ValidateBatch(b Batch, expected Binding) error {
	if b.SchemaVersion != SchemaVersion || b.Generation < 1 || !uuidPattern.MatchString(b.RunID) || !uuidPattern.MatchString(b.ConnectionID) || !uuidPattern.MatchString(b.WorkspaceID) {
		return errors.New("invalid observation envelope")
	}
	if b.RunID != expected.RunID || b.ConnectionID != expected.ConnectionID || b.WorkspaceID != expected.WorkspaceID || b.Generation != expected.Generation || b.Connector != expected.Connector || b.Origin != expected.Origin {
		return errors.New("observation binding mismatch")
	}
	if !validText(b.Connector.Key, 240) || !validText(b.Connector.Version, 240) || b.ObservedAt.IsZero() || !slices.Contains([]string{"fictional", "live_private"}, b.Origin) {
		return errors.New("invalid connector, origin, or observation timestamp")
	}
	s := b.Scope
	if !sameSet(s.Statuses, expected.Scope.Statuses) || s.DateFrom != expected.Scope.DateFrom || s.DateTo != expected.Scope.DateTo {
		return errors.New("observation scope mismatch")
	}
	if !s.Complete {
		return errors.New("incomplete observation scope")
	}
	if len(s.Statuses) == 0 || len(s.Statuses) > 20 || s.PagesCollected < 0 || s.RecordsCollected < 0 || len(b.Records) > 10000 || s.RecordsCollected != len(b.Records) {
		return errors.New("invalid scope counts")
	}
	if len(b.Records) > 0 && s.PagesCollected == 0 {
		return errors.New("nonempty observation has no collection evidence")
	}
	if s.ExpectedRecords != nil && (*s.ExpectedRecords < 0 || *s.ExpectedRecords != len(b.Records)) {
		return errors.New("scope count does not match expected records")
	}
	if (s.DateFrom != "" && !ValidDate(s.DateFrom)) || (s.DateTo != "" && !ValidDate(s.DateTo)) || (s.DateFrom != "" && s.DateTo != "" && s.DateFrom > s.DateTo) {
		return errors.New("invalid scope dates")
	}
	statuses := map[string]bool{}
	for _, status := range s.Statuses {
		if !slices.Contains([]string{"open", "partially_shipped", "shipped", "delivered", "cancelled", "unknown"}, status) || statuses[status] {
			return errors.New("invalid or repeated scope status")
		}
		statuses[status] = true
	}
	for _, warning := range s.Warnings {
		if !validText(warning, 240) {
			return errors.New("invalid scope warning")
		}
	}
	keys, identities := map[string]bool{}, map[string]bool{}
	for i, r := range b.Records {
		if err := ValidateRecord(r); err != nil {
			return fmt.Errorf("record %d: %w", i+1, err)
		}
		if r.Origin != b.Origin || !statuses[r.Status] {
			return errors.New("record origin or status exceeds requested source scope")
		}
		identity := r.SourceOrderID + "\x00" + r.SourceLineID
		if keys[r.SourceKey] || identities[identity] {
			return errors.New("duplicate source record identity")
		}
		keys[r.SourceKey], identities[identity] = true, true
	}
	return nil
}
