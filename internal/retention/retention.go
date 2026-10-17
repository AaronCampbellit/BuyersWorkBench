// Package retention provides explicit local-administration maintenance. It
// preserves source facts, event-bearing evidence and all run/idempotency ledgers.
// Nothing invokes it automatically and runtime roles receive no deletion grant.
package retention

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const MaxLimit = 1000

var (
	ErrInvalid     = errors.New("retention requires a workspace UUID, a cutoff at least one day old, and a limit between 1 and 1000")
	ErrDays        = errors.New("retention days must be between 1 and 3650")
	ErrAdmin       = errors.New("retention requires trusted local administration credentials with row-security bypass")
	ErrNotFound    = errors.New("retention workspace not found")
	ErrUnavailable = errors.New("retention could not complete; no committed result was confirmed")
	uuidRE         = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
)

type Request struct {
	WorkspaceID string
	Before      time.Time
	Limit       int
}

type Result struct {
	Mode          string    `json:"mode"`
	WorkspaceID   string    `json:"workspace_id"`
	Generation    int64     `json:"generation"`
	Before        time.Time `json:"before"`
	Limit         int       `json:"limit"`
	StagedBatches int       `json:"staged_batches"`
	Observations  int       `json:"observations"`
	MoreEligible  bool      `json:"more_eligible"`
	GeneratedAt   time.Time `json:"generated_at"`
	AuditID       string    `json:"audit_id,omitempty"`
}

func ForDays(workspaceID string, days, limit int, now time.Time) (Request, error) {
	if days < 1 || days > 3650 {
		return Request{}, ErrDays
	}
	req := Request{WorkspaceID: workspaceID, Before: now.UTC().Add(-time.Duration(days) * 24 * time.Hour), Limit: limit}
	if err := req.Validate(now); err != nil {
		return Request{}, err
	}
	return req, nil
}

func (r Request) Validate(now time.Time) error {
	if !uuidRE.MatchString(r.WorkspaceID) || r.WorkspaceID == "00000000-0000-0000-0000-000000000000" || r.Before.IsZero() || r.Before.After(now.UTC().Add(-24*time.Hour)) || r.Limit < 1 || r.Limit > MaxLimit {
		return ErrInvalid
	}
	return nil
}

type candidate struct{ kind, id string }

// The complete window includes recent observations, so retention never mistakes
// an old slice's endpoint for an order's actual first/latest snapshot. Only a
// consecutive identical JSONB record is redundant. Untyped field transitions
// remain evidence even when they did not produce a source event.
const candidatesSQL = `WITH payloads AS (
 SELECT 0 AS priority,'staged_batch'::text AS kind,r.id,r.finished_at AS sort_at
 FROM wb_runs r WHERE r.workspace_id=$1 AND r.staged_batch IS NOT NULL
 AND r.finished_at<$2 AND r.state IN ('succeeded','rejected','transient_failed','terminal_failed','cancelled','stale_generation')
 AND NOT EXISTS(SELECT 1 FROM wb_connections c WHERE c.active_run_id=r.id)
 ORDER BY r.finished_at,r.id LIMIT $3
), ranked AS MATERIALIZED (
 SELECT o.*,row_number() OVER(PARTITION BY o.generation,o.order_id ORDER BY o.observed_at,o.id) AS first_position,
 row_number() OVER(PARTITION BY o.generation,o.order_id ORDER BY o.observed_at DESC,o.id DESC) AS last_position,
 lag(o.record) OVER(PARTITION BY o.generation,o.order_id ORDER BY o.observed_at,o.id) AS previous_record
 FROM wb_observations o WHERE o.workspace_id=$1
), observations AS (
 SELECT 1 AS priority,'observation'::text AS kind,o.id,o.observed_at AS sort_at
 FROM ranked o
 JOIN wb_runs r ON r.workspace_id=$1 AND r.generation=o.generation AND r.id=o.run_id
 JOIN wb_orders ord ON ord.workspace_id=$1 AND ord.generation=o.generation AND ord.id=o.order_id
 WHERE o.observed_at<$2 AND r.finished_at<$2 AND r.state='succeeded'
 AND o.first_position>1 AND o.last_position>1 AND o.record=o.previous_record
 AND o.observed_at<>ord.first_observed_at AND o.observed_at<>ord.last_observed_at
 AND NOT EXISTS(SELECT 1 FROM wb_connections c WHERE c.active_run_id=o.run_id)
 AND NOT EXISTS(SELECT 1 FROM wb_changes e WHERE e.workspace_id=$1 AND e.generation=o.generation AND e.order_id=o.order_id AND e.run_id=o.run_id)
 ORDER BY o.observed_at,o.id LIMIT $3
)
SELECT kind,id::text FROM(SELECT * FROM payloads UNION ALL SELECT * FROM observations) candidates
ORDER BY priority,sort_at,id LIMIT $3`

func prepare(ctx context.Context, pool *pgxpool.Pool, req Request) (pgx.Tx, Result, []candidate, error) {
	if err := req.Validate(time.Now()); err != nil {
		return nil, Result{}, nil, err
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, Result{}, nil, ErrUnavailable
	}
	fail := func(err error) (pgx.Tx, Result, []candidate, error) {
		tx.Rollback(context.Background())
		return nil, Result{}, nil, err
	}
	var role string
	var privileged bool
	if err = tx.QueryRow(ctx, `SELECT current_user,(rolsuper OR rolbypassrls) FROM pg_roles WHERE rolname=current_user`).Scan(&role, &privileged); err != nil {
		return fail(ErrUnavailable)
	}
	if role == "bwb_app" || !privileged {
		return fail(ErrAdmin)
	}
	// This is the same lock order as publication, reset and connection changes.
	// Even a dry run holds the workspace row until its snapshot is complete.
	if _, err = tx.Exec(ctx, "SET LOCAL lock_timeout='5s'"); err != nil {
		return fail(ErrUnavailable)
	}
	result := Result{WorkspaceID: strings.ToLower(req.WorkspaceID), Before: req.Before.UTC(), Limit: req.Limit, GeneratedAt: time.Now().UTC()}
	if err = tx.QueryRow(ctx, "SELECT generation FROM workspaces WHERE id=$1 FOR UPDATE", req.WorkspaceID).Scan(&result.Generation); errors.Is(err, pgx.ErrNoRows) {
		return fail(ErrNotFound)
	} else if err != nil {
		return fail(ErrUnavailable)
	}
	result.GeneratedAt = time.Now().UTC()
	rows, err := tx.Query(ctx, candidatesSQL, req.WorkspaceID, req.Before, req.Limit+1)
	if err != nil {
		return fail(ErrUnavailable)
	}
	selected := make([]candidate, 0, req.Limit)
	for rows.Next() {
		var item candidate
		if err = rows.Scan(&item.kind, &item.id); err != nil {
			rows.Close()
			return fail(ErrUnavailable)
		}
		if len(selected) == req.Limit {
			result.MoreEligible = true
			continue
		}
		selected = append(selected, item)
		if item.kind == "staged_batch" {
			result.StagedBatches++
		} else {
			result.Observations++
		}
	}
	rows.Close()
	if rows.Err() != nil {
		return fail(ErrUnavailable)
	}
	return tx, result, selected, nil
}

// Plan acquires the workspace publication lock but makes no persisted changes,
// including no audit insert. Its cutoff can be supplied verbatim to Apply.
func Plan(ctx context.Context, pool *pgxpool.Pool, req Request) (Result, error) {
	tx, result, _, err := prepare(ctx, pool, req)
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback(context.Background())
	result.Mode = "dry_run"
	return result, nil
}

// Apply re-evaluates a fixed cutoff under the workspace lock. It is one bounded
// transaction, not an instruction to drain all candidates. Run rows and all
// event/notice/workflow/audit/idempotency records remain intact.
func Apply(ctx context.Context, pool *pgxpool.Pool, req Request) (Result, error) {
	tx, result, selected, err := prepare(ctx, pool, req)
	if err != nil {
		return Result{}, err
	}
	defer tx.Rollback(context.Background())
	runs, observations := []string{}, []string{}
	for _, item := range selected {
		if item.kind == "staged_batch" {
			runs = append(runs, item.id)
		} else {
			observations = append(observations, item.id)
		}
	}
	if len(runs) > 0 {
		tag, err := tx.Exec(ctx, "UPDATE wb_runs SET staged_batch=NULL WHERE workspace_id=$1 AND id=ANY($2::uuid[]) AND staged_batch IS NOT NULL", req.WorkspaceID, runs)
		if err != nil || tag.RowsAffected() != int64(len(runs)) {
			return Result{}, ErrUnavailable
		}
	}
	if len(observations) > 0 {
		tag, err := tx.Exec(ctx, "DELETE FROM wb_observations WHERE workspace_id=$1 AND id=ANY($2::uuid[])", req.WorkspaceID, observations)
		if err != nil || tag.RowsAffected() != int64(len(observations)) {
			return Result{}, ErrUnavailable
		}
	}
	result.Mode = "applied"
	metadata, err := json.Marshal(result)
	if err != nil {
		return Result{}, ErrUnavailable
	}
	if err = tx.QueryRow(ctx, `INSERT INTO audit_events(id,action,target_type,target_id,outcome,metadata)
 VALUES(gen_random_uuid(),'retention.apply','workspace',$1,'success',$2::jsonb||jsonb_build_object('database_role',current_user)) RETURNING id::text`, req.WorkspaceID, metadata).Scan(&result.AuditID); err != nil {
		return Result{}, ErrUnavailable
	}
	if err = tx.Commit(ctx); err != nil {
		return Result{}, ErrUnavailable
	}
	return result, nil
}
