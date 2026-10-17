package workbench

import (
	"buyersworkbench/internal/domain"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"slices"
	"strings"
	"time"
)

func normalizeIssueResolution(in IssueResolutionInput, active bool) (IssueResolutionInput, error) {
	in.ResolutionType = strings.TrimSpace(in.ResolutionType)
	in.ActionTaken = strings.TrimSpace(in.ActionTaken)
	in.Result = strings.TrimSpace(in.Result)
	in.Reference = strings.TrimSpace(in.Reference)
	in.SupplierContact = strings.TrimSpace(in.SupplierContact)
	in.RevisedDate = strings.TrimSpace(in.RevisedDate)
	in.ConfirmedQuantity = strings.TrimSpace(in.ConfirmedQuantity)
	in.FollowUpDate = strings.TrimSpace(in.FollowUpDate)
	if !slices.Contains([]string{"confirmation_received", "delivery_updated", "shipment_updated", "data_corrected", "other"}, in.ResolutionType) ||
		in.ActionTaken == "" || !supplierText(in.ActionTaken, 4000, true) || in.Result == "" || !supplierText(in.Result, 4000, true) ||
		in.Reference == "" || !supplierText(in.Reference, 500, false) || !supplierText(in.SupplierContact, 200, false) {
		return in, ErrInvalid
	}
	if (in.RevisedDate != "" && !domain.ValidDate(in.RevisedDate)) ||
		(in.FollowUpDate != "" && !domain.ValidDate(in.FollowUpDate)) ||
		(in.ConfirmedQuantity != "" && !domain.ValidDecimal(in.ConfirmedQuantity)) || (active && in.FollowUpDate == "") {
		return in, ErrInvalid
	}
	return in, nil
}

// Rule absence is not sufficient evidence of recovery when the source has
// become unknown. Confirmation proof remains independent from arrival quality.
func verifiedIssueClear(rule string, record domain.Record, requirement *domain.Requirement, conditions []domain.Condition) bool {
	if domain.ValidateRecord(record) != nil {
		return false
	}
	balances, err := domain.Balance(record)
	if err != nil {
		return false
	}
	zero := func(value string) bool { return value != "" && strings.Trim(value, "0.") == "" }
	terminal := zero(balances.NetOrdered) || zero(balances.Unreceived)
	if rule == "R03" {
		return record.Confirmation == "confirmed" || terminal
	}
	arrival := record.ArrivalDate != "" && (record.ArrivalKind == "committed_arrival" || record.ArrivalKind == "estimated_arrival")
	if rule == "R01" {
		return terminal || (arrival && requirement != nil && domain.ValidateRequirement(*requirement) == nil &&
			requirement.SourceKey == record.SourceKey && requirement.SupplierAlias == record.SupplierAlias && requirement.UOM == record.UOM && record.ArrivalDate <= requirement.RequiredDate)
	}
	for _, condition := range conditions {
		if condition.Rule == rule || condition.Rule == "R05" {
			return false
		}
	}
	switch rule {
	case "R02":
		return terminal || arrival
	case "R04":
		return terminal || balances.Unshipped != ""
	default:
		return true
	}
}

func issueClearVerifiedTx(ctx context.Context, tx pgx.Tx, w string, g int64, id, rule string, at time.Time) (bool, error) {
	var recordJSON, requirementJSON []byte
	var dataAttention bool
	err := tx.QueryRow(ctx, `SELECT o.record,r.data,EXISTS(SELECT 1 FROM wb_conditions attention
		WHERE attention.workspace_id=c.workspace_id AND attention.generation=c.generation AND attention.order_id=c.order_id AND attention.rule='R05' AND attention.condition_active)
		FROM wb_conditions c JOIN wb_orders o ON o.workspace_id=c.workspace_id AND o.generation=c.generation AND o.id=c.order_id
		LEFT JOIN wb_requirements r ON r.workspace_id=c.workspace_id AND r.generation=c.generation AND r.source_key=o.source_key AND r.connection_id=o.connection_id
		WHERE c.workspace_id=$1 AND c.generation=$2 AND c.id=$3`, w, g, id).Scan(&recordJSON, &requirementJSON, &dataAttention)
	if err != nil {
		return false, err
	}
	var record domain.Record
	if err = json.Unmarshal(recordJSON, &record); err != nil {
		return false, err
	}
	var requirement *domain.Requirement
	if len(requirementJSON) > 0 {
		var req domain.Requirement
		if err = json.Unmarshal(requirementJSON, &req); err != nil {
			return false, err
		}
		requirement = &req
	}
	conditions := domain.Evaluate(record, requirement, at)
	if dataAttention && rule != "R03" {
		conditions = append(conditions, domain.Condition{Rule: "R05"})
	}
	return verifiedIssueClear(rule, record, requirement, conditions), nil
}

func workflowOf(issue Exception) WorkflowInput {
	return WorkflowInput{Acknowledged: issue.Acknowledged, AssigneeID: issue.AssigneeID, FollowUpDate: issue.FollowUpDate, Status: issue.Status}
}

func issueSnapshot(issue Exception, unit string) issueEventDetails {
	return issueEventDetails{MaterialRevision: issue.MaterialRevision, ConditionActive: issue.Active,
		Rule: issue.Rule, Evidence: issue.Evidence, QuantityUOM: unit}
}

func issueQuantityUOM(ctx context.Context, tx pgx.Tx, w string, g int64, id string) (string, error) {
	var unit string
	err := tx.QueryRow(ctx, `SELECT COALESCE(o.record->>'uom','') FROM wb_conditions c
		JOIN wb_orders o ON o.workspace_id=c.workspace_id AND o.generation=c.generation AND o.id=c.order_id
		WHERE c.workspace_id=$1 AND c.generation=$2 AND c.id=$3`, w, g, id).Scan(&unit)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return unit, err
}

func appendIssueEvent(ctx context.Context, tx pgx.Tx, actor, w, id string, g int64, action, from, to string, details issueEventDetails) error {
	var actorID any
	if actor != "" {
		actorID = actor
	}
	_, err := tx.Exec(ctx, `INSERT INTO wb_issue_events(workspace_id,generation,condition_id,actor_id,action,from_status,to_status,details)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, w, g, id, actorID, action, from, to, raw(details))
	return err
}

func appendSystemIssueTransition(ctx context.Context, tx pgx.Tx, w, id string, g int64, action, from, to, reason string) error {
	current, err := exceptionRow(tx.QueryRow(ctx, exceptionQuery+" AND c.id=$3", w, g, id))
	if err != nil {
		return err
	}
	unit, err := issueQuantityUOM(ctx, tx, w, g, id)
	if err != nil {
		return err
	}
	details := issueSnapshot(current, unit)
	// System verification records the accepted source facts that justified this
	// transition. Submitted buyer evidence remains unchanged in its own event.
	var recordJSON, requirementJSON []byte
	var observedAt time.Time
	err = tx.QueryRow(ctx, `SELECT o.record,r.data,o.last_observed_at FROM wb_conditions c
		JOIN wb_orders o ON o.workspace_id=c.workspace_id AND o.generation=c.generation AND o.id=c.order_id
		LEFT JOIN wb_requirements r ON r.workspace_id=c.workspace_id AND r.generation=c.generation AND r.source_key=o.source_key AND r.connection_id=o.connection_id
		WHERE c.workspace_id=$1 AND c.generation=$2 AND c.id=$3`, w, g, id).Scan(&recordJSON, &requirementJSON, &observedAt)
	if err != nil {
		return err
	}
	var record domain.Record
	if err = json.Unmarshal(recordJSON, &record); err != nil {
		return err
	}
	var requirement domain.Requirement
	if len(requirementJSON) > 0 {
		if err = json.Unmarshal(requirementJSON, &requirement); err != nil {
			return err
		}
	}
	proof := make(map[string]string, len(details.Evidence)+13)
	for key, value := range details.Evidence {
		proof[key] = value
	}
	for key, value := range map[string]string{"source_key": record.SourceKey, "confirmation": record.Confirmation,
		"arrival_date": record.ArrivalDate, "arrival_kind": record.ArrivalKind, "ordered": record.Ordered, "cancelled": record.Cancelled,
		"shipped": record.Shipped, "delivered": record.Delivered, "uom": record.UOM, "required_date": requirement.RequiredDate,
		"source_label": record.ArrivalLabel, "observed_at": observedAt.UTC().Format(time.RFC3339Nano)} {
		proof[key] = value
	}
	if balances, err := domain.Balance(record); err == nil {
		proof["net_ordered"], proof["unshipped"], proof["unreceived"] = balances.NetOrdered, balances.Unshipped, balances.Unreceived
	}
	details.Evidence = proof
	details.Reason = reason
	before, after := workflowOf(current), workflowOf(current)
	before.Status = from
	after.Status = to
	details.BeforeWorkflow, details.AfterWorkflow = &before, &after
	return appendIssueEvent(ctx, tx, "", w, id, g, action, from, to, details)
}

func resolutionFromDetails(id, actor string, at time.Time, details issueEventDetails) *IssueResolution {
	if details.Resolution == nil {
		return nil
	}
	return &IssueResolution{ID: id, IssueResolutionInput: *details.Resolution, SubmittedBy: actor, SubmittedAt: at,
		MaterialRevision: details.MaterialRevision, ConditionActive: details.ConditionActive, Rule: details.Rule,
		Evidence: details.Evidence, QuantityUOM: details.QuantityUOM}
}

func issueEventsTx(ctx context.Context, tx pgx.Tx, w string, g int64, orderID string) ([]IssueEvent, error) {
	events := []IssueEvent{}
	rows, err := tx.Query(ctx, `SELECT e.id::text,e.condition_id::text,e.action,e.from_status,e.to_status,
		COALESCE(u.display_name,'Source verification'),e.created_at,e.details FROM wb_issue_events e
		JOIN wb_conditions c ON c.workspace_id=e.workspace_id AND c.generation=e.generation AND c.id=e.condition_id
		LEFT JOIN users u ON u.id=e.actor_id WHERE e.workspace_id=$1 AND e.generation=$2 AND c.order_id=$3
		ORDER BY e.created_at DESC,e.id DESC LIMIT 200`, w, g, orderID)
	if err != nil {
		return events, err
	}
	defer rows.Close()
	for rows.Next() {
		var event IssueEvent
		var data []byte
		if err = rows.Scan(&event.ID, &event.ExceptionID, &event.Action, &event.FromStatus, &event.ToStatus, &event.ActorName, &event.CreatedAt, &data); err != nil {
			return events, err
		}
		var details issueEventDetails
		if err = json.Unmarshal(data, &details); err != nil {
			return events, err
		}
		event.Reason, event.MaterialRevision, event.ConditionActive, event.Rule, event.Evidence, event.QuantityUOM = details.Reason, details.MaterialRevision, details.ConditionActive, details.Rule, details.Evidence, details.QuantityUOM
		event.BeforeWorkflow, event.AfterWorkflow = details.BeforeWorkflow, details.AfterWorkflow
		event.Resolution = resolutionFromDetails(event.ID, event.ActorName, event.CreatedAt, details)
		events = append(events, event)
	}
	return events, rows.Err()
}

func (s *Service) ResolveIssue(ctx context.Context, actor, w, id string, g, revision int64, key string, input IssueResolutionInput) (Exception, error) {
	if !uuidRE.MatchString(id) || g < 1 || revision < 1 || !validPOKey(key) {
		return Exception{}, ErrInvalid
	}
	tx, sc, err := s.begin(ctx, actor, w, g, true)
	if err != nil {
		return Exception{}, err
	}
	defer tx.Rollback(ctx)
	if sc.kind == "private" {
		return Exception{}, ErrPrivate
	}
	payload := string(raw(struct {
		Revision int64
		Input    IssueResolutionInput
	}{revision, input}))
	kind := "issue.resolve:" + id
	var result Exception
	if found, err := operation(ctx, tx, actor, w, kind, key, payload, g, &result); err != nil {
		return result, err
	} else if found {
		return result, tx.Commit(ctx)
	}
	current, err := exceptionRow(tx.QueryRow(ctx, exceptionQuery+" AND c.id=$3", w, g, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = ErrNotFound
		}
		return current, err
	}
	if current.Revision != revision {
		return current, ErrStale
	}
	if !current.Active {
		verified, err := issueClearVerifiedTx(ctx, tx, w, g, id, current.Rule, sc.businessDate)
		if err != nil {
			return current, err
		}
		if !verified {
			return current, ErrVerificationRequired
		}
	}
	input, err = normalizeIssueResolution(input, current.Active)
	if err != nil {
		return current, err
	}
	unit, err := issueQuantityUOM(ctx, tx, w, g, id)
	if err != nil {
		return current, err
	}
	status := "resolved"
	if current.Active {
		status = "awaiting_verification"
	}
	changed, err := tx.Exec(ctx, `UPDATE wb_conditions SET status=$5,follow_up_date=NULLIF($6,'')::date,
		resolved_at=CASE WHEN $5='resolved' THEN now() ELSE NULL END,revision=revision+1
		WHERE workspace_id=$1 AND generation=$2 AND id=$3 AND revision=$4`, w, g, id, revision, status, input.FollowUpDate)
	if err != nil {
		return current, err
	}
	if changed.RowsAffected() != 1 {
		return current, ErrStale
	}
	details := issueSnapshot(current, unit)
	details.Resolution = &input
	before, after := workflowOf(current), workflowOf(current)
	after.Status = status
	if input.FollowUpDate == "" {
		after.FollowUpDate = nil
	} else {
		after.FollowUpDate = &input.FollowUpDate
	}
	details.BeforeWorkflow, details.AfterWorkflow = &before, &after
	if err = appendIssueEvent(ctx, tx, actor, w, id, g, "resolution_submitted", current.Status, status, details); err != nil {
		return current, err
	}
	result, err = exceptionRow(tx.QueryRow(ctx, exceptionQuery+" AND c.id=$3", w, g, id))
	if err != nil {
		return result, err
	}
	if err = saveOperation(ctx, tx, actor, w, kind, key, payload, g, result); err != nil {
		return result, err
	}
	if err = audit(ctx, tx, actor, w, "issue.resolution_submitted"); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}

func (s *Service) ReopenIssue(ctx context.Context, actor, w, id string, g, revision int64, key, reason string) (Exception, error) {
	if !uuidRE.MatchString(id) || g < 1 || revision < 1 || !validPOKey(key) {
		return Exception{}, ErrInvalid
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || !supplierText(reason, 4000, true) {
		return Exception{}, ErrInvalid
	}
	tx, sc, err := s.begin(ctx, actor, w, g, true)
	if err != nil {
		return Exception{}, err
	}
	defer tx.Rollback(ctx)
	if sc.kind == "private" {
		return Exception{}, ErrPrivate
	}
	payload := string(raw(struct {
		Revision int64
		Reason   string
	}{revision, reason}))
	kind := "issue.reopen:" + id
	var result Exception
	if found, err := operation(ctx, tx, actor, w, kind, key, payload, g, &result); err != nil {
		return result, err
	} else if found {
		return result, tx.Commit(ctx)
	}
	current, err := exceptionRow(tx.QueryRow(ctx, exceptionQuery+" AND c.id=$3", w, g, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = ErrNotFound
		}
		return current, err
	}
	if current.Revision != revision {
		return current, ErrStale
	}
	if !slices.Contains([]string{"completed", "awaiting_verification", "resolved"}, current.Status) {
		return current, ErrInvalid
	}
	unit, err := issueQuantityUOM(ctx, tx, w, g, id)
	if err != nil {
		return current, err
	}
	changed, err := tx.Exec(ctx, "UPDATE wb_conditions SET status='open',resolved_at=NULL,revision=revision+1 WHERE workspace_id=$1 AND generation=$2 AND id=$3 AND revision=$4", w, g, id, revision)
	if err != nil {
		return current, err
	}
	if changed.RowsAffected() != 1 {
		return current, ErrStale
	}
	details := issueSnapshot(current, unit)
	details.Reason = reason
	before, after := workflowOf(current), workflowOf(current)
	after.Status = "open"
	details.BeforeWorkflow, details.AfterWorkflow = &before, &after
	if err = appendIssueEvent(ctx, tx, actor, w, id, g, "reopened", current.Status, "open", details); err != nil {
		return current, err
	}
	result, err = exceptionRow(tx.QueryRow(ctx, exceptionQuery+" AND c.id=$3", w, g, id))
	if err != nil {
		return result, err
	}
	if err = saveOperation(ctx, tx, actor, w, kind, key, payload, g, result); err != nil {
		return result, err
	}
	if err = audit(ctx, tx, actor, w, "issue.reopened"); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
