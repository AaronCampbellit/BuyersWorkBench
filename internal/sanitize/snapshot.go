// Package sanitize provides an explicit allowlist projection. It deliberately
// cannot serialize a normalized private Record or its lineage.
package sanitize

import (
	"errors"
	"regexp"
	"time"

	"buyersworkbench/internal/domain"
)

const Version = "allowlist-v1"

type Approval struct {
	Description string `json:"safe_description"`
	Quantities  bool   `json:"include_quantities"`
	Status      bool   `json:"include_status"`
	Arrival     bool   `json:"include_arrival"`
}
type Snapshot struct {
	ID               string    `json:"id"`
	Origin           string    `json:"origin"`
	SanitizerVersion string    `json:"sanitizer_version"`
	Description      string    `json:"description"`
	SourceLabel      string    `json:"source_label"`
	CollectedAt      time.Time `json:"collected_at"`
	ExportedAt       time.Time `json:"exported_at"`
	Ordered          *string   `json:"ordered,omitempty"`
	Cancelled        *string   `json:"cancelled,omitempty"`
	Shipped          *string   `json:"shipped,omitempty"`
	Delivered        *string   `json:"delivered,omitempty"`
	UOM              string    `json:"uom,omitempty"`
	Status           string    `json:"status,omitempty"`
	ArrivalDate      string    `json:"arrival_date,omitempty"`
	ArrivalKind      string    `json:"arrival_kind,omitempty"`
}

var description = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9 .-]{0,79}$`)

func Project(id string, record domain.Record, collectedAt, exportedAt time.Time, approval Approval) (Snapshot, error) {
	if domain.ValidateRecord(record) != nil || (approval.Quantities && record.UOM != "EA") || record.Origin != "live_private" || !description.MatchString(approval.Description) || id == "" || collectedAt.IsZero() || exportedAt.Before(collectedAt) {
		return Snapshot{}, errors.New("invalid snapshot approval")
	}
	result := Snapshot{ID: id, Origin: "sanitized_snapshot", SanitizerVersion: Version, Description: approval.Description, SourceLabel: "Owner-approved supplier snapshot", CollectedAt: collectedAt, ExportedAt: exportedAt}
	optional := func(value string) *string {
		if value == "" {
			return nil
		}
		return &value
	}
	if approval.Quantities {
		result.Ordered = optional(record.Ordered)
		result.Cancelled = optional(record.Cancelled)
		result.Shipped = optional(record.Shipped)
		result.Delivered = optional(record.Delivered)
		result.UOM = record.UOM
	}
	if approval.Status {
		result.Status = record.Status
	}
	if approval.Arrival {
		result.ArrivalDate = record.ArrivalDate
		result.ArrivalKind = record.ArrivalKind
	}
	return result, nil
}
