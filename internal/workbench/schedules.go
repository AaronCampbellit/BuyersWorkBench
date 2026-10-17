package workbench

import (
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"time"
)

// SetSchedule preserves the legacy workspace endpoint as a default-feed operation.
func (s *Service) SetSchedule(ctx context.Context, actor, w string, g int64, enabled bool, cadenceSeconds int) (Connection, error) {
	tx, sc, err := s.begin(ctx, actor, w, g, true)
	if err != nil {
		return Connection{}, err
	}
	defer tx.Rollback(ctx)
	if !sc.isOwner {
		return Connection{}, ErrNotFound
	}
	if sc.kind == "private" {
		return Connection{}, ErrPrivate
	}
	id, err := defaultConnectionID(ctx, tx, w)
	if err != nil {
		return Connection{}, err
	}
	c, err := s.setConnectionScheduleTx(ctx, tx, actor, w, id, 0, enabled, cadenceSeconds)
	if err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}
func (s *Service) SetConnectionSchedule(ctx context.Context, actor, w, id string, g, revision int64, enabled bool, cadenceSeconds int) (Connection, error) {
	if !uuidRE.MatchString(id) || g < 1 || revision < 1 {
		return Connection{}, ErrInvalid
	}
	tx, sc, err := s.begin(ctx, actor, w, g, true)
	if err != nil {
		return Connection{}, err
	}
	defer tx.Rollback(ctx)
	if !sc.isOwner {
		return Connection{}, ErrNotFound
	}
	if sc.kind == "private" {
		return Connection{}, ErrInvalid
	}
	c, err := s.setConnectionScheduleTx(ctx, tx, actor, w, id, revision, enabled, cadenceSeconds)
	if err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}
func (s *Service) setConnectionScheduleTx(ctx context.Context, tx pgx.Tx, actor, w, id string, revision int64, enabled bool, cadenceSeconds int) (Connection, error) {
	c, err := connectionRow(tx.QueryRow(ctx, `SELECT `+connectionColumns+` FROM wb_connections WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, w, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	if revision > 0 && c.Revision != revision {
		return c, ErrStale
	}
	if c.Method != "fictional" || c.Paused && enabled || cadenceSeconds < c.MinimumCadenceSeconds || cadenceSeconds > 86400 {
		return c, ErrInvalid
	}
	c, err = connectionRow(tx.QueryRow(ctx, `UPDATE wb_connections SET schedule_enabled=$3,cadence_seconds=$4,scheduled_by_user_id=$5,revision=revision+1,next_due_at=CASE WHEN $3 AND NOT paused THEN now()+($4::integer*interval '1 second') ELSE NULL END WHERE workspace_id=$1 AND id=$2 RETURNING `+connectionColumns, w, id, enabled, cadenceSeconds, actor))
	if err != nil {
		return c, err
	}
	if err = audit(ctx, tx, actor, w, "connection.schedule"); err != nil {
		return c, err
	}
	return c, nil
}

// Missed ticks are coalesced into one durable run per due connection. Both the
// generation and exact connection are reauthorized under the workspace lock.
func (s *Service) ReconcileSchedules(ctx context.Context) error {
	if _, err := s.Pool.Exec(ctx, "SELECT bwb_reconcile_expired_collections()"); err != nil {
		return err
	}
	rows, err := s.Pool.Query(ctx, "SELECT workspace_id::text,actor_id::text,connection_id::text FROM bwb_due_schedules()")
	if err != nil {
		return err
	}
	type due struct{ w, a, c string }
	var items []due
	for rows.Next() {
		var d due
		if err = rows.Scan(&d.w, &d.a, &d.c); err != nil {
			rows.Close()
			return err
		}
		items = append(items, d)
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return err
	}
	for _, d := range items {
		tx, sc, err := s.begin(ctx, d.a, d.w, 0, true)
		if err != nil {
			continue
		}
		var dueNow bool
		var cadence int
		err = tx.QueryRow(ctx, `SELECT schedule_enabled AND NOT paused AND method='fictional' AND cadence_seconds>=minimum_cadence_seconds AND next_due_at<=now(),cadence_seconds FROM wb_connections WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, d.w, d.c).Scan(&dueNow, &cadence)
		if err != nil || !dueNow || sc.kind == "private" || !sc.isOwner {
			tx.Rollback(ctx)
			continue
		}
		if _, err = s.queueConnectionTx(ctx, tx, d.a, d.w, d.c, sc.generation); err != nil {
			tx.Rollback(ctx)
			return err
		}
		if _, err = tx.Exec(ctx, `UPDATE wb_connections SET next_due_at=now()+($3::integer*interval '1 second') WHERE workspace_id=$1 AND id=$2`, d.w, d.c, cadence); err != nil {
			tx.Rollback(ctx)
			return err
		}
		if err = tx.Commit(ctx); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) RunScheduler(ctx context.Context, onError func(error)) {
	reconcile := func() {
		check, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		if err := s.ReconcileSchedules(check); err != nil && ctx.Err() == nil && onError != nil {
			onError(err)
		}
	}
	reconcile()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcile()
		}
	}
}
