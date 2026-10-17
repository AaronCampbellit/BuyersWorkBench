package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"buyersworkbench/internal/domain"
	"github.com/jackc/pgx/v5"
)

// PublishImport consumes a reviewed immutable preview in the same transaction
// as replacement requirements, audit, and queue insertion. An interrupted HTTP
// response can be retried without duplicating the publication.
func (s *Service) PublishImport(ctx context.Context, actor, w, previewID string, g int64) (Run, error) {
	if !uuidRE.MatchString(previewID) || g < 1 {
		return Run{}, ErrInvalid
	}
	tx, sc, err := s.begin(ctx, actor, w, g, true)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback(ctx)
	if !sc.isOwner {
		return Run{}, ErrNotFound
	}
	if sc.kind == "private" {
		return Run{}, ErrPrivate
	}
	var data []byte
	var generation int64
	var created time.Time
	var published *time.Time
	var runID *string
	var connectionID *string
	err = tx.QueryRow(ctx, `SELECT requirements,generation,created_at,published_at,run_id::text,connection_id::text FROM wb_import_previews WHERE id=$1 AND workspace_id=$2 AND actor_user_id=$3 FOR UPDATE`, previewID, w, actor).Scan(&data, &generation, &created, &published, &runID, &connectionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, err
	}
	if generation != g {
		return Run{}, ErrStale
	}
	if published != nil && runID != nil {
		run, err := runRow(tx.QueryRow(ctx, "SELECT "+runColumns+" FROM wb_runs WHERE workspace_id=$1 AND generation=$2 AND id=$3", w, g, *runID))
		if err != nil {
			return Run{}, err
		}
		return run, tx.Commit(ctx)
	}
	if time.Since(created) > time.Hour {
		return Run{}, ErrInvalid
	}
	if connectionID == nil {
		id, err := defaultConnectionID(ctx, tx, w)
		if err != nil {
			return Run{}, err
		}
		connectionID = &id
	}
	var runnable bool
	if err = tx.QueryRow(ctx, `SELECT method='fictional' AND NOT paused FROM wb_connections WHERE workspace_id=$1 AND id=$2`, w, *connectionID).Scan(&runnable); err != nil {
		return Run{}, ErrNotFound
	}
	if !runnable {
		return Run{}, ErrInvalid
	}
	var requirements []domain.Requirement
	if err = json.Unmarshal(data, &requirements); err != nil {
		return Run{}, err
	}
	if domain.ValidateRequirements(requirements) != nil {
		return Run{}, ErrInvalid
	}
	for _, r := range requirements {
		var valid bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM wb_orders WHERE workspace_id=$1 AND generation=$2 AND source_key=$3 AND record->>'supplier_alias'=$4 AND record->>'uom'=$5 AND record->>'origin'='fictional' AND connection_id=$6)`, w, g, r.SourceKey, r.SupplierAlias, r.UOM, *connectionID).Scan(&valid); err != nil {
			return Run{}, err
		}
		if !valid {
			return Run{}, ErrInvalid
		}
	}
	if _, err = tx.Exec(ctx, `DELETE FROM wb_requirements WHERE workspace_id=$1 AND generation=$2 AND connection_id=$3`, w, g, *connectionID); err != nil {
		return Run{}, err
	}
	for _, r := range requirements {
		if _, err = tx.Exec(ctx, `INSERT INTO wb_requirements(workspace_id,generation,source_key,data,connection_id) VALUES($1,$2,$3,$4,$5)`, w, g, r.SourceKey, raw(r), *connectionID); err != nil {
			return Run{}, err
		}
	}
	run, err := s.queueConnectionTx(ctx, tx, actor, w, *connectionID, g)
	if err != nil {
		return Run{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE wb_import_previews SET published_at=now(),run_id=$2 WHERE id=$1`, previewID, run.ID); err != nil {
		return Run{}, err
	}
	if err = audit(ctx, tx, actor, w, "requirements.import"); err != nil {
		return Run{}, err
	}
	return run, tx.Commit(ctx)
}
