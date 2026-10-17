// Package workbench owns transactional, workspace-scoped purchasing workflows.
package workbench

import (
	"buyersworkbench/internal/domain"
	"errors"
	"github.com/riverqueue/river"
	"time"
)

var ErrNotFound = errors.New("not_found")
var ErrStale = errors.New("stale_generation_or_revision")
var ErrInvalid = errors.New("invalid_input")
var ErrPrivate = errors.New("fictional_workspace_required")

type CollectionArgs struct {
	SchemaVersion int    `json:"schema_version"`
	RunID         string `json:"run_id"`
	ConnectionID  string `json:"connection_id"`
	WorkspaceID   string `json:"workspace_id"`
	Generation    int64  `json:"generation"`
	ActorUserID   string `json:"actor_user_id"`
	Fence         int64  `json:"fence"`
}

func (CollectionArgs) Kind() string                 { return "collect_fictional_connection" }
func (CollectionArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: 3} }

type Run struct {
	ID           string     `json:"id"`
	ConnectionID string     `json:"connection_id"`
	Generation   int64      `json:"generation"`
	State        string     `json:"state"`
	Attempt      int        `json:"attempt"`
	EnqueuedAt   time.Time  `json:"enqueued_at"`
	StartedAt    *time.Time `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
	RecordCount  int        `json:"record_count"`
	ErrorCode    *string    `json:"error_code"`
}
type WorkflowInput struct {
	Acknowledged bool    `json:"acknowledged"`
	AssigneeID   *string `json:"assignee_id"`
	FollowUpDate *string `json:"follow_up_date"`
	Status       string  `json:"status"`
}
type Exception struct {
	ID            string `json:"id"`
	OrderID       string `json:"order_id"`
	SourceKey     string `json:"source_key"`
	SupplierAlias string `json:"supplier_alias"`
	Description   string `json:"description"`
	InternalPO    string `json:"internal_po"`
	RequiredDate  string `json:"required_date"`
	domain.Condition
	Revision         int64            `json:"revision"`
	MaterialRevision int64            `json:"material_revision"`
	Acknowledged     bool             `json:"acknowledged"`
	AssigneeID       *string          `json:"assignee_id"`
	FollowUpDate     *string          `json:"follow_up_date"`
	Status           string           `json:"status"`
	FirstSeenAt      time.Time        `json:"first_seen_at"`
	LastSeenAt       time.Time        `json:"last_seen_at"`
	Resolution       *IssueResolution `json:"resolution"`
	ResolvedAt       *time.Time       `json:"resolved_at"`
}
type Note struct {
	ID          string    `json:"id"`
	ExceptionID string    `json:"exception_id"`
	AuthorName  string    `json:"author_name"`
	Body        string    `json:"body"`
	CreatedAt   time.Time `json:"created_at"`
}
type Order struct {
	ID           string `json:"id"`
	ConnectionID string `json:"connection_id"`
	domain.Record
	Lifecycle       domain.ObservedOrderLifecycle `json:"lifecycle"`
	Requirement     *domain.Requirement           `json:"requirement"`
	FirstObservedAt time.Time                     `json:"first_observed_at"`
	LastObservedAt  time.Time                     `json:"last_observed_at"`
}
type History struct {
	ID            string `json:"id"`
	EventType     string `json:"event_type"`
	SchemaVersion int    `json:"schema_version"`
	RunID         string `json:"run_id"`
	ConnectionID  string `json:"connection_id"`
	ObservationID string `json:"observation_id"`
	domain.Change
	DetectedAt time.Time `json:"detected_at"`
}
type OrderDetail struct {
	Order       Order        `json:"order"`
	History     []History    `json:"history"`
	Conditions  []Exception  `json:"conditions"`
	Notes       []Note       `json:"notes"`
	IssueEvents []IssueEvent `json:"issue_events"`
}
type Notification struct {
	ID            string     `json:"id"`
	ExceptionID   string     `json:"exception_id"`
	SourceEventID string     `json:"source_event_id"`
	OrderID       string     `json:"order_id"`
	Message       string     `json:"message"`
	CreatedAt     time.Time  `json:"created_at"`
	ReadAt        *time.Time `json:"read_at"`
}
type Quotes struct {
	Comparison  domain.QuoteComparison  `json:"comparison"`
	Calculation domain.QuoteCalculation `json:"calculation"`
	Revision    int64                   `json:"revision"`
}
type Supplier struct {
	ID    string `json:"id"`
	Alias string `json:"alias"`
	SupplierInput
	Revision         int64  `json:"revision"`
	Origin           string `json:"origin"`
	OrderCount       int    `json:"order_count"`
	ActiveConditions int    `json:"active_conditions"`
	CoverageLabel    string `json:"coverage_label"`
}
type SupplierInput struct {
	Name        string `json:"name"`
	ContactName string `json:"contact_name"`
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	Website     string `json:"website"`
	Notes       string `json:"notes"`
	Status      string `json:"status"`
}
type Overview struct {
	WorkspaceID    string          `json:"workspace_id"`
	Generation     int64           `json:"generation"`
	Origin         string          `json:"origin"`
	BusinessDate   string          `json:"business_date"`
	SeedVersion    string          `json:"seed_version"`
	Seeded         bool            `json:"seeded"`
	Ready          bool            `json:"ready"`
	State          string          `json:"state"`
	Orders         []Order         `json:"orders"`
	Priorities     []Exception     `json:"priorities"`
	Connections    []Connection    `json:"connections"`
	Runs           []Run           `json:"runs"`
	Notifications  []Notification  `json:"notifications"`
	Quotes         *Quotes         `json:"quotes"`
	Suppliers      []Supplier      `json:"suppliers"`
	PurchaseOrders []PurchaseOrder `json:"purchase_orders"`
}
type ResetResult struct {
	Generation  int64  `json:"generation"`
	SeedVersion string `json:"seed_version"`
	Run         Run    `json:"run"`
}
