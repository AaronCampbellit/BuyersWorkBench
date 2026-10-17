package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"buyersworkbench/internal/domain"
	"github.com/jackc/pgx/v5"
)

func exceptionRow(row interface{ Scan(...any) error }) (Exception, error) {
	var e Exception
	var data []byte
	var resolutionID, resolutionActor *string
	var resolutionAt *time.Time
	var resolutionData []byte
	err := row.Scan(&e.ID, &e.OrderID, &e.SourceKey, &e.SupplierAlias, &e.Description, &e.InternalPO, &e.RequiredDate, &data, &e.Revision, &e.MaterialRevision, &e.Acknowledged, &e.AssigneeID, &e.FollowUpDate, &e.Status, &e.FirstSeenAt, &e.LastSeenAt,
		&e.ResolvedAt, &resolutionID, &resolutionData, &resolutionActor, &resolutionAt)
	if err == nil {
		err = json.Unmarshal(data, &e.Condition)
	}
	if err == nil && resolutionID != nil && resolutionActor != nil && resolutionAt != nil {
		var details issueEventDetails
		err = json.Unmarshal(resolutionData, &details)
		if err == nil {
			e.Resolution = resolutionFromDetails(*resolutionID, *resolutionActor, *resolutionAt, details)
		}
	}
	return e, err
}

// Materialize each authorized scope once. RLS predicates can otherwise make
// underestimated nested loops repeatedly rescan the same current evidence.
const exceptionQuery = `WITH scoped_orders AS MATERIALIZED (
 SELECT * FROM wb_orders WHERE workspace_id=$1 AND generation=$2
), scoped_requirements AS MATERIALIZED (
 SELECT * FROM wb_requirements WHERE workspace_id=$1 AND generation=$2
), scoped_conditions AS MATERIALIZED (
 SELECT * FROM wb_conditions WHERE workspace_id=$1 AND generation=$2
)
SELECT c.id::text,c.order_id::text,o.source_key,o.record->>'supplier_alias',o.record->>'description',COALESCE(r.data->>'internal_po',''),COALESCE(r.data->>'required_date',''),c.data,c.revision,c.material_revision,c.acknowledged,c.assignee_id::text,to_char(c.follow_up_date,'YYYY-MM-DD'),c.status,c.first_seen_at,c.last_seen_at,
 c.resolved_at,latest_resolution.id::text,latest_resolution.details,latest_resolution.submitted_by,latest_resolution.created_at
 FROM scoped_conditions c JOIN scoped_orders o ON o.workspace_id=c.workspace_id AND o.generation=c.generation AND o.id=c.order_id
 LEFT JOIN scoped_requirements r ON r.workspace_id=o.workspace_id AND r.generation=o.generation AND r.source_key=o.source_key AND r.connection_id=o.connection_id
 LEFT JOIN LATERAL (SELECT ev.id,ev.details,ev.created_at,COALESCE(author.display_name,'Source verification') AS submitted_by
   FROM wb_issue_events ev LEFT JOIN users author ON author.id=ev.actor_id
   WHERE ev.workspace_id=c.workspace_id AND ev.generation=c.generation AND ev.condition_id=c.id AND ev.action='resolution_submitted'
   ORDER BY ev.created_at DESC,ev.id DESC LIMIT 1) latest_resolution ON true
 WHERE c.workspace_id=$1 AND c.generation=$2`
const exceptionSort = ` ORDER BY c.condition_active DESC,CASE c.severity WHEN 'high' THEN 0 WHEN 'medium' THEN 1 ELSE 2 END, NULLIF(r.data->>'required_date','') NULLS LAST,COALESCE((c.data->>'delay_days')::integer,0) DESC,o.source_key,o.connection_id,c.rule,c.id LIMIT 500`

func orderRow(row interface{ Scan(...any) error }) (Order, error) {
	var o Order
	var record, requirement []byte
	err := row.Scan(&o.ID, &o.ConnectionID, &record, &requirement, &o.FirstObservedAt, &o.LastObservedAt)
	if err == nil {
		err = json.Unmarshal(record, &o.Record)
		if err == nil {
			o.Lifecycle = domain.DeriveObservedOrderLifecycle(o.Record)
		}
	}
	if err == nil && len(requirement) > 0 {
		var r domain.Requirement
		err = json.Unmarshal(requirement, &r)
		o.Requirement = &r
	}
	return o, err
}

const orderQuery = `WITH scoped_orders AS MATERIALIZED (
 SELECT * FROM wb_orders WHERE workspace_id=$1 AND generation=$2
), scoped_requirements AS MATERIALIZED (
 SELECT * FROM wb_requirements WHERE workspace_id=$1 AND generation=$2
)
SELECT o.id::text,o.connection_id::text,o.record,r.data,o.first_observed_at,o.last_observed_at FROM scoped_orders o LEFT JOIN scoped_requirements r ON r.workspace_id=o.workspace_id AND r.generation=o.generation AND r.source_key=o.source_key AND r.connection_id=o.connection_id WHERE o.workspace_id=$1 AND o.generation=$2`

func quotesTx(ctx context.Context, tx pgx.Tx, w string, g int64) (Quotes, error) {
	var q Quotes
	var data []byte
	err := tx.QueryRow(ctx, "SELECT data,revision FROM wb_quote_comparisons WHERE workspace_id=$1 AND generation=$2", w, g).Scan(&data, &q.Revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return q, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(data, &q.Comparison)
	}
	if err == nil {
		q.Calculation = domain.CalculateQuotes(q.Comparison)
	}
	return q, err
}
func (s *Service) Overview(ctx context.Context, actor, w string) (Overview, error) {
	result := Overview{WorkspaceID: w, Orders: []Order{}, Priorities: []Exception{}, Connections: []Connection{}, Runs: []Run{}, Notifications: []Notification{}, Suppliers: []Supplier{}}
	tx, sc, err := s.begin(ctx, actor, w, 0, false)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	result.Generation = sc.generation
	result.SeedVersion = sc.seedVersion
	result.BusinessDate = sc.businessDate.Format("2006-01-02")
	result.Origin = "fictional"
	result.State = "unseeded"
	result.Suppliers, err = suppliersTx(ctx, tx, w, sc.generation)
	if err != nil {
		return result, err
	}
	result.PurchaseOrders, err = purchaseOrdersTx(ctx, tx, w, sc.generation)
	if err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, "SELECT "+connectionColumns+" FROM wb_connections WHERE workspace_id=$1 ORDER BY is_demo_default DESC,name,id", w)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		c, err := connectionRow(rows)
		if err != nil {
			rows.Close()
			return result, err
		}
		result.Connections = append(result.Connections, c)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return result, err
	}
	if sc.kind == "private" {
		result.Origin = "no_supplier_data"
		result.State = "not_configured"
		return result, tx.Commit(ctx)
	}
	rows, err = tx.Query(ctx, orderQuery+" ORDER BY o.source_key,o.connection_id,o.id LIMIT 500", w, sc.generation)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		o, err := orderRow(rows)
		if err != nil {
			rows.Close()
			return result, err
		}
		result.Orders = append(result.Orders, o)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return result, err
	}
	rows, err = tx.Query(ctx, exceptionQuery+exceptionSort, w, sc.generation)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		e, err := exceptionRow(rows)
		if err != nil {
			rows.Close()
			return result, err
		}
		result.Priorities = append(result.Priorities, e)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return result, err
	}
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM wb_connections WHERE workspace_id=$1 AND is_demo_default),EXISTS(SELECT 1 FROM wb_connections WHERE workspace_id=$1 AND is_demo_default AND initialized_generation=$2)`, w, sc.generation).Scan(&result.Seeded, &result.Ready); err != nil {
		return result, err
	}
	if result.Seeded {
		result.State = "initializing"
		if result.Ready {
			result.State = "ready"
		}
	}
	rows, err = tx.Query(ctx, "SELECT "+runColumns+" FROM wb_runs WHERE workspace_id=$1 AND generation=$2 ORDER BY enqueued_at DESC,id LIMIT 20", w, sc.generation)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		r, err := runRow(rows)
		if err != nil {
			rows.Close()
			return result, err
		}
		result.Runs = append(result.Runs, r)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return result, err
	}
	rows, err = tx.Query(ctx, `WITH notices AS MATERIALIZED (
 SELECT * FROM wb_notifications WHERE workspace_id=$1 AND generation=$2 AND recipient_id=$3
 ORDER BY created_at DESC,id LIMIT 100
), subjects AS MATERIALIZED (
 SELECT workspace_id,generation,id,order_id FROM wb_conditions WHERE workspace_id=$1 AND generation=$2
 AND id IN (SELECT condition_id FROM notices)
), events AS MATERIALIZED (
 SELECT workspace_id,generation,id,order_id FROM wb_changes WHERE workspace_id=$1 AND generation=$2
 AND id IN (SELECT source_event_id FROM notices)
)
SELECT n.id::text,COALESCE(n.condition_id::text,''),COALESCE(n.source_event_id::text,''),
 COALESCE(c.order_id::text,e.order_id::text,''),n.message,n.created_at,n.read_at
 FROM notices n
 LEFT JOIN subjects c ON c.workspace_id=n.workspace_id AND c.generation=n.generation AND c.id=n.condition_id
 LEFT JOIN events e ON e.workspace_id=n.workspace_id AND e.generation=n.generation AND e.id=n.source_event_id
 WHERE n.workspace_id=$1 AND n.generation=$2 AND n.recipient_id=$3 ORDER BY n.created_at DESC,n.id LIMIT 100`, w, sc.generation, actor)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var n Notification
		if err = rows.Scan(&n.ID, &n.ExceptionID, &n.SourceEventID, &n.OrderID, &n.Message, &n.CreatedAt, &n.ReadAt); err != nil {
			rows.Close()
			return result, err
		}
		result.Notifications = append(result.Notifications, n)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return result, err
	}
	q, err := quotesTx(ctx, tx, w, sc.generation)
	if err == nil {
		result.Quotes = &q
	} else if !errors.Is(err, ErrNotFound) {
		return result, err
	}
	return result, tx.Commit(ctx)
}
func (s *Service) Order(ctx context.Context, actor, w, id string) (OrderDetail, error) {
	result := OrderDetail{History: []History{}, Conditions: []Exception{}, Notes: []Note{}}
	if !uuidRE.MatchString(id) {
		return result, ErrNotFound
	}
	tx, sc, err := s.begin(ctx, actor, w, 0, false)
	if err != nil {
		return result, err
	}
	defer tx.Rollback(ctx)
	result.Order, err = orderRow(tx.QueryRow(ctx, orderQuery+" AND o.id=$3", w, sc.generation, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return result, ErrNotFound
	}
	if err != nil {
		return result, err
	}
	rows, err := tx.Query(ctx, `SELECT e.id::text,e.event_type,e.schema_version,e.run_id::text,r.connection_id::text,
 COALESCE(o.id::text,''),e.data,e.detected_at FROM wb_changes e
 JOIN wb_runs r ON r.workspace_id=e.workspace_id AND r.generation=e.generation AND r.id=e.run_id
 LEFT JOIN wb_observations o ON o.workspace_id=e.workspace_id AND o.generation=e.generation AND o.run_id=e.run_id AND o.order_id=e.order_id
 WHERE e.workspace_id=$1 AND e.generation=$2 AND e.order_id=$3 ORDER BY e.detected_at DESC,e.id LIMIT 200`, w, sc.generation, id)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var h History
		var data []byte
		if err = rows.Scan(&h.ID, &h.EventType, &h.SchemaVersion, &h.RunID, &h.ConnectionID, &h.ObservationID, &data, &h.DetectedAt); err != nil {
			rows.Close()
			return result, err
		}
		if err = json.Unmarshal(data, &h.Change); err != nil {
			rows.Close()
			return result, err
		}
		result.History = append(result.History, h)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return result, err
	}
	rows, err = tx.Query(ctx, exceptionQuery+" AND c.order_id=$3"+exceptionSort, w, sc.generation, id)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		e, err := exceptionRow(rows)
		if err != nil {
			rows.Close()
			return result, err
		}
		result.Conditions = append(result.Conditions, e)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return result, err
	}
	rows, err = tx.Query(ctx, `SELECT n.id::text,n.condition_id::text,u.display_name,n.body,n.created_at FROM wb_notes n JOIN users u ON u.id=n.author_id JOIN wb_conditions c ON c.id=n.condition_id AND c.workspace_id=n.workspace_id AND c.generation=n.generation WHERE n.workspace_id=$1 AND n.generation=$2 AND c.order_id=$3 ORDER BY n.created_at,n.id LIMIT 200`, w, sc.generation, id)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var n Note
		if err = rows.Scan(&n.ID, &n.ExceptionID, &n.AuthorName, &n.Body, &n.CreatedAt); err != nil {
			rows.Close()
			return result, err
		}
		result.Notes = append(result.Notes, n)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return result, err
	}
	result.IssueEvents, err = issueEventsTx(ctx, tx, w, sc.generation, id)
	if err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
func (s *Service) Run(ctx context.Context, actor, w, id string) (Run, error) {
	if !uuidRE.MatchString(id) {
		return Run{}, ErrNotFound
	}
	tx, sc, err := s.begin(ctx, actor, w, 0, false)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback(ctx)
	r, err := runRow(tx.QueryRow(ctx, "SELECT "+runColumns+" FROM wb_runs WHERE workspace_id=$1 AND generation=$2 AND id=$3", w, sc.generation, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, err
	}
	return r, tx.Commit(ctx)
}
