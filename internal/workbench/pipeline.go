package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"buyersworkbench/internal/collection"
	"buyersworkbench/internal/domain"
	"github.com/jackc/pgx/v5"
)

// Collect dispatches a server-owned connection through its registered collector,
// validates the complete batch,
// then rechecks authorization, workspace generation and lease fence at publication.
func (s *Service) Collect(ctx context.Context, args CollectionArgs) error {
	if args.SchemaVersion != 1 || args.Generation < 1 || args.Fence < 1 || !uuidRE.MatchString(args.RunID) || !uuidRE.MatchString(args.ConnectionID) {
		return ErrInvalid
	}
	tx, sc, err := s.begin(ctx, args.ActorUserID, args.WorkspaceID, args.Generation, true)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if sc.kind == "private" {
		return ErrPrivate
	}
	var source []byte
	var failure bool
	var revision int64
	var state string
	var connectorKey, origin string
	var isDefault bool
	err = tx.QueryRow(ctx, `SELECT c.source_records,c.source_failure,c.source_revision,r.state,c.connector,c.origin,c.is_demo_default FROM wb_connections c JOIN wb_runs r ON r.id=$2 AND r.workspace_id=c.workspace_id AND r.connection_id=c.id AND r.fence=$4 WHERE c.id=$1 AND c.workspace_id=$3 AND c.fence=$4 AND c.active_run_id=$2 AND c.lease_expires_at>now() AND NOT c.paused AND c.method='fictional' AND r.generation=$5`, args.ConnectionID, args.RunID, args.WorkspaceID, args.Fence, args.Generation).Scan(&source, &failure, &revision, &state, &connectorKey, &origin, &isDefault)
	if errors.Is(err, pgx.ErrNoRows) {
		var completed bool
		check := tx.QueryRow(ctx, "SELECT state='succeeded' FROM wb_runs WHERE id=$1 AND workspace_id=$2 AND generation=$3 AND connection_id=$4 AND fence=$5", args.RunID, args.WorkspaceID, args.Generation, args.ConnectionID, args.Fence).Scan(&completed)
		if check == nil && completed {
			return tx.Commit(ctx)
		}
		return ErrStale
	}
	if err != nil {
		return err
	}
	if state == "succeeded" {
		return tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, `UPDATE wb_runs SET state='running',attempt=attempt+1,started_at=COALESCE(started_at,now()),source_revision=$2 WHERE id=$1`, args.RunID, revision); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE wb_connections SET status='running',lease_expires_at=now()+interval '90 seconds' WHERE id=$1`, args.ConnectionID); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	binding, err := collectionBindingForFeed(args, connectorKey, origin, isDefault)
	if err != nil {
		return s.failRun(ctx, args, "rejected", "connector_not_configured")
	}
	var records []domain.Record
	if err = json.Unmarshal(source, &records); err != nil {
		return s.failRun(ctx, args, "rejected", "source_schema_invalid")
	}
	batch, err := collection.DefaultRegistry().Collect(ctx, connectorKey, collection.Request{Binding: binding, Records: records, Incomplete: failure, ObservedAt: time.Now().UTC()})
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if collection.Retryable(err) {
			return err
		}
		code := string(collection.FailureCode(err))
		if collection.FailureCode(err) == collection.CodeIncomplete {
			code = "incomplete_fictional_scope"
		}
		return s.failRun(ctx, args, "rejected", code)
	}
	if err = domain.ValidateBatch(batch, binding); err != nil {
		return s.failRun(ctx, args, "rejected", "batch_validation_failed")
	}
	err = s.PublishBatch(ctx, args, batch)
	if errors.Is(err, ErrInvalid) {
		_ = s.failRun(ctx, args, "rejected", "record_identity_changed")
		return nil
	}
	return err
}

// PublishBatch permits testable source-contract validation without granting a
// connector authority to select its own scope or origin. Callers supply a valid
// server-issued run/fence; every field is rechecked against that durable binding.
func (s *Service) PublishBatch(ctx context.Context, args CollectionArgs, batch domain.Batch) error {
	tx, sc, err := s.begin(ctx, args.ActorUserID, args.WorkspaceID, args.Generation, true)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var active bool
	var state string
	var connectorKey, origin string
	var isDefault bool
	err = tx.QueryRow(ctx, `SELECT c.fence=$4 AND c.active_run_id=$2 AND c.lease_expires_at>now() AND NOT c.paused AND c.method='fictional',r.state,c.connector,c.origin,c.is_demo_default FROM wb_connections c JOIN wb_runs r ON r.id=$2 AND r.workspace_id=c.workspace_id AND r.connection_id=c.id AND r.fence=$4 WHERE c.id=$1 AND c.workspace_id=$3 AND r.generation=$5`, args.ConnectionID, args.RunID, args.WorkspaceID, args.Fence, args.Generation).Scan(&active, &state, &connectorKey, &origin, &isDefault)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrStale
	}
	if err != nil {
		return err
	}
	if state == "succeeded" {
		return tx.Commit(ctx)
	}
	if !active || sc.kind == "private" {
		return ErrStale
	}
	binding, err := collectionBindingForFeed(args, connectorKey, origin, isDefault)
	if err != nil {
		return rejectBatch(ctx, tx, args, "connector_not_configured")
	}
	staged := raw(batch)
	if len(staged) > 4*1024*1024 || len(batch.Records) > 10000 {
		tx.Rollback(ctx)
		_ = s.failRun(ctx, args, "rejected", "batch_size_limit")
		return ErrInvalid
	}
	if _, err = tx.Exec(ctx, "UPDATE wb_runs SET state='validating',staged_batch=$2 WHERE id=$1", args.RunID, staged); err != nil {
		return err
	}
	if err = domain.ValidateBatch(batch, binding); err != nil {
		return rejectBatch(ctx, tx, args, "batch_validation_failed")
	}
	if err = validateSupplierRecordsTx(ctx, tx, args.WorkspaceID, args.ConnectionID, args.Generation, batch.Records); err != nil {
		if errors.Is(err, ErrInvalid) {
			return rejectBatch(ctx, tx, args, "supplier_binding_mismatch")
		}
		return err
	}
	var previousCount int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM wb_orders WHERE workspace_id=$1 AND generation=$2 AND connection_id=$3", args.WorkspaceID, args.Generation, args.ConnectionID).Scan(&previousCount); err != nil {
		return err
	}
	if previousCount >= 10 && len(batch.Records)*2 < previousCount {
		return rejectBatch(ctx, tx, args, "unexpected_record_count_drop")
	}
	// Stable external keys cannot be reused for a different order/line identity.
	rows, err := tx.Query(ctx, "SELECT source_key,record->>'source_order_id',record->>'source_line_id' FROM wb_orders WHERE workspace_id=$1 AND generation=$2 AND connection_id=$3", args.WorkspaceID, args.Generation, args.ConnectionID)
	if err != nil {
		return err
	}
	identities := map[string][2]string{}
	for rows.Next() {
		var key, order, line string
		if err = rows.Scan(&key, &order, &line); err != nil {
			rows.Close()
			return err
		}
		identities[key] = [2]string{order, line}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, r := range batch.Records {
		if prior, found := identities[r.SourceKey]; found && (prior[0] != r.SourceOrderID || prior[1] != r.SourceLineID) {
			return rejectBatch(ctx, tx, args, "record_identity_changed")
		}
	}
	if _, err = tx.Exec(ctx, "UPDATE wb_runs SET state='publishing' WHERE id=$1", args.RunID); err != nil {
		return err
	}
	for _, record := range batch.Records {
		if err = s.publishRecord(ctx, tx, args, record, batch.ObservedAt, sc.businessDate); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(ctx, `UPDATE wb_runs SET state='succeeded',record_count=$2,finished_at=now(),error_code=NULL WHERE id=$1`, args.RunID, len(batch.Records)); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE wb_connections SET status='ready',last_success_at=$2,last_failure_code=NULL,active_run_id=NULL,lease_expires_at=NULL,initialized_generation=$4 WHERE id=$1 AND fence=$3`, args.ConnectionID, batch.ObservedAt, args.Fence, args.Generation); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func rejectBatch(ctx context.Context, tx pgx.Tx, args CollectionArgs, code string) error {
	if _, err := tx.Exec(ctx, "UPDATE wb_runs SET state='rejected',error_code=$2,finished_at=now() WHERE id=$1", args.RunID, code); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, "UPDATE wb_connections SET status='attention',last_failure_code=$2,active_run_id=NULL,lease_expires_at=NULL WHERE id=$1 AND fence=$3", args.ConnectionID, code, args.Fence); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return ErrInvalid
}
func (s *Service) failRun(ctx context.Context, args CollectionArgs, state, code string) error {
	tx, _, err := s.begin(ctx, args.ActorUserID, args.WorkspaceID, args.Generation, true)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	result, err := tx.Exec(ctx, `UPDATE wb_connections SET status='attention',last_failure_code=$4,active_run_id=NULL,lease_expires_at=NULL WHERE id=$1 AND workspace_id=$2 AND fence=$3 AND active_run_id=$5`, args.ConnectionID, args.WorkspaceID, args.Fence, code, args.RunID)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return ErrStale
	}
	if _, err = tx.Exec(ctx, "UPDATE wb_runs SET state=$2,error_code=$3,finished_at=now() WHERE id=$1", args.RunID, state, code); err != nil {
		return err
	}
	if err = audit(ctx, tx, args.ActorUserID, args.WorkspaceID, "collection."+state); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// FailCollection records a terminal queue failure when retries are exhausted.
// It verifies the original lease/generation, so a late failure cannot overwrite
// a successful replacement run or a reset workspace.
func (s *Service) FailCollection(ctx context.Context, args CollectionArgs) error {
	return s.failRun(ctx, args, "terminal_failed", "collection_retries_exhausted")
}
func (s *Service) publishRecord(ctx context.Context, tx pgx.Tx, args CollectionArgs, r domain.Record, observedAt, businessDate time.Time) error {
	if err := ensureSupplierTx(ctx, tx, args.WorkspaceID, args.Generation, r.SupplierAlias); err != nil {
		return err
	}
	var id string
	var oldJSON []byte
	var previous *domain.Record
	err := tx.QueryRow(ctx, "SELECT id::text,record FROM wb_orders WHERE workspace_id=$1 AND generation=$2 AND source_key=$3 AND connection_id=$4", args.WorkspaceID, args.Generation, r.SourceKey, args.ConnectionID).Scan(&id, &oldJSON)
	if err == nil {
		var old domain.Record
		if err = json.Unmarshal(oldJSON, &old); err != nil {
			return err
		}
		previous = &old
		if old.SourceOrderID != r.SourceOrderID || old.SourceLineID != r.SourceLineID {
			return ErrInvalid
		}
		_, err = tx.Exec(ctx, "UPDATE wb_orders SET record=$2,last_observed_at=$3 WHERE id=$1", id, raw(r), observedAt)
	} else if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO wb_orders(workspace_id,generation,source_key,record,first_record,first_observed_at,last_observed_at,connection_id) VALUES($1,$2,$3,$4,$4,$5,$5,$6) RETURNING id::text`, args.WorkspaceID, args.Generation, r.SourceKey, raw(r), observedAt, args.ConnectionID).Scan(&id)
	}
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO wb_observations(workspace_id,generation,order_id,run_id,record,observed_at) VALUES($1,$2,$3,$4,$5,$6) ON CONFLICT DO NOTHING`, args.WorkspaceID, args.Generation, id, args.RunID, raw(r), observedAt); err != nil {
		return err
	}
	if previous != nil {
		for _, c := range domain.Changes(*previous, r) {
			if err = publishSourceChange(ctx, tx, args, id, c); err != nil {
				return err
			}
		}
	}
	var requirement *domain.Requirement
	var reqJSON []byte
	err = tx.QueryRow(ctx, "SELECT data FROM wb_requirements WHERE workspace_id=$1 AND generation=$2 AND source_key=$3 AND connection_id=$4", args.WorkspaceID, args.Generation, r.SourceKey, args.ConnectionID).Scan(&reqJSON)
	if err == nil {
		var req domain.Requirement
		if err = json.Unmarshal(reqJSON, &req); err != nil {
			return err
		}
		requirement = &req
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	conditions := domain.EvaluateWithPrevious(r, requirement, previous, businessDate)
	seen := map[string]bool{}
	for _, condition := range conditions {
		seen[condition.Rule] = true
		if err = s.publishCondition(ctx, tx, args, id, condition); err != nil {
			return err
		}
	}
	rows, err := tx.Query(ctx, "SELECT id::text,rule,data,status FROM wb_conditions WHERE workspace_id=$1 AND generation=$2 AND order_id=$3 AND condition_active", args.WorkspaceID, args.Generation, id)
	if err != nil {
		return err
	}
	type pending struct {
		id     string
		c      domain.Condition
		status string
	}
	var resolved []pending
	for rows.Next() {
		var p pending
		var rule string
		var data []byte
		if err = rows.Scan(&p.id, &rule, &data, &p.status); err != nil {
			rows.Close()
			return err
		}
		if !seen[rule] {
			if !verifiedIssueClear(rule, r, requirement, conditions) {
				continue
			}
			if rule == "R02" && !seen["R01"] && previous != nil && previous.ArrivalDate == r.ArrivalDate && previous.ArrivalKind == r.ArrivalKind {
				continue
			}
			if err = json.Unmarshal(data, &p.c); err != nil {
				rows.Close()
				return err
			}
			p.c.Active = false
			resolved = append(resolved, p)
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, p := range resolved {
		if _, err = tx.Exec(ctx, `UPDATE wb_conditions SET data=$2,condition_active=false,
			status=CASE WHEN status='awaiting_verification' THEN 'resolved' ELSE status END,
			resolved_at=CASE WHEN status='awaiting_verification' THEN now() ELSE resolved_at END,
			revision=revision+1,last_seen_at=now() WHERE id=$1`, p.id, raw(p.c)); err != nil {
			return err
		}
		if p.status == "awaiting_verification" {
			if err = appendSystemIssueTransition(ctx, tx, args.WorkspaceID, p.id, args.Generation, "source_verified", p.status, "resolved", "An accepted supplier observation no longer triggers this condition."); err != nil {
				return err
			}
		}
	}
	return nil
}
func (s *Service) publishCondition(ctx context.Context, tx pgx.Tx, args CollectionArgs, orderID string, c domain.Condition) error {
	var id string
	var data []byte
	var active bool
	var revision int64
	var status string
	notify := false
	err := tx.QueryRow(ctx, "SELECT id::text,data,condition_active,material_revision,status FROM wb_conditions WHERE workspace_id=$1 AND generation=$2 AND order_id=$3 AND rule=$4", args.WorkspaceID, args.Generation, orderID, c.Rule).Scan(&id, &data, &active, &revision, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		err = tx.QueryRow(ctx, `INSERT INTO wb_conditions(workspace_id,generation,order_id,rule,data,condition_active,severity) VALUES($1,$2,$3,$4,$5,true,$6) RETURNING id::text,material_revision`, args.WorkspaceID, args.Generation, orderID, c.Rule, raw(c), c.Severity).Scan(&id, &revision)
		notify = true
	} else if err == nil {
		var old domain.Condition
		if err = json.Unmarshal(data, &old); err != nil {
			return err
		}
		changed := !active || domain.ConditionFingerprint(old) != domain.ConditionFingerprint(c)
		worsened := domain.ConditionWorsened(old, c)
		notify = !active || worsened
		reopen := !active || ((status == "awaiting_verification" || status == "resolved" || status == "completed") && worsened)
		if changed {
			revision++
			_, err = tx.Exec(ctx, `UPDATE wb_conditions SET data=$2,condition_active=true,severity=$3,material_revision=$4,
				status=CASE WHEN $5 THEN 'open' ELSE status END,resolved_at=CASE WHEN $5 THEN NULL ELSE resolved_at END,
				revision=revision+1,last_seen_at=now() WHERE id=$1`, id, raw(c), c.Severity, revision, reopen)
		} else {
			_, err = tx.Exec(ctx, "UPDATE wb_conditions SET last_seen_at=now() WHERE id=$1", id)
		}
		if err == nil && reopen {
			reason := "The condition recurred in an accepted supplier observation."
			if active {
				reason = "The condition materially worsened after buyer closeout was recorded."
			}
			err = appendSystemIssueTransition(ctx, tx, args.WorkspaceID, id, args.Generation, "reopened", status, "open", reason)
		}
	}
	if err != nil {
		return err
	}
	if !notify {
		return nil
	}
	event := fmt.Sprintf("condition:%s:%d", id, revision)
	return notifyConditionTx(ctx, tx, args, id, event, c.Explanation)
}
func severity(s string) int {
	switch strings.ToLower(s) {
	case "high":
		return 3
	case "medium":
		return 2
	default:
		return 1
	}
}
