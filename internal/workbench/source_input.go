package workbench

import (
	"context"
	"errors"
	"time"

	"buyersworkbench/internal/domain"
	"github.com/jackc/pgx/v5"
)

func validateSupplierRecordsTx(ctx context.Context, tx pgx.Tx, w, id string, g int64, records []domain.Record) error {
	var supplierID, alias *string
	err := tx.QueryRow(ctx, `SELECT c.supplier_id::text,p.alias FROM wb_connections c LEFT JOIN wb_supplier_profiles p ON p.workspace_id=c.workspace_id AND p.id=c.supplier_id AND p.generation=$3 WHERE c.workspace_id=$1 AND c.id=$2`, w, id, g).Scan(&supplierID, &alias)
	if err != nil {
		return err
	}
	if supplierID == nil {
		return nil
	}
	if alias == nil {
		return ErrInvalid
	}
	for _, r := range records {
		if r.SupplierAlias != *alias {
			return ErrInvalid
		}
	}
	return nil
}

// StageConnectionSource accepts explicitly fictional fixture input and queues a
// common-pipeline collection. Source replacement, requirements, fencing, audit
// and durable job insertion commit together; response retries reuse that run.
func (s *Service) StageConnectionSource(ctx context.Context, actor, w, id string, g, revision int64, key string, records []domain.Record, requirements []domain.Requirement) (Run, error) {
	if !uuidRE.MatchString(id) || g < 1 || revision < 1 || len(records) > 10000 || len(raw(records)) > 4*1024*1024 || len(requirements) > 0 && domain.ValidateRequirements(requirements) != nil {
		return Run{}, ErrInvalid
	}
	if records == nil {
		records = []domain.Record{}
	}
	binding, err := collectionBindingForFeed(CollectionArgs{SchemaVersion: 1, RunID: id, ConnectionID: id, WorkspaceID: w, Generation: g}, "fictional_supplier", "fictional", false)
	if err != nil || domain.ValidateBatch(domain.FictionalBatch(records, binding, time.Now().UTC()), binding) != nil {
		return Run{}, ErrInvalid
	}
	mapping := map[string]domain.Record{}
	for _, r := range records {
		mapping[r.SourceKey] = r
	}
	for _, r := range requirements {
		v, ok := mapping[r.SourceKey]
		if !ok || v.SupplierAlias != r.SupplierAlias || v.UOM != r.UOM {
			return Run{}, ErrInvalid
		}
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
	payload := string(raw(struct {
		ConnectionID string
		Revision     int64
		Records      []domain.Record
		Requirements []domain.Requirement
	}{id, revision, records, requirements}))
	var run Run
	if found, err := operation(ctx, tx, actor, w, "connection.source", key, payload, g, &run); err != nil {
		return run, err
	} else if found {
		return run, tx.Commit(ctx)
	}
	var currentRevision int64
	var allowed bool
	err = tx.QueryRow(ctx, `SELECT revision,method='fictional' AND origin='fictional' AND connector='fictional_supplier' AND NOT paused AND NOT is_demo_default FROM wb_connections WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, w, id).Scan(&currentRevision, &allowed)
	if errors.Is(err, pgx.ErrNoRows) {
		return run, ErrNotFound
	}
	if err != nil {
		return run, err
	}
	if currentRevision != revision {
		return run, ErrStale
	}
	if !allowed {
		return run, ErrInvalid
	}
	if err = validateSupplierRecordsTx(ctx, tx, w, id, g, records); err != nil {
		return run, err
	}
	rows, err := tx.Query(ctx, `SELECT source_key,record->>'source_order_id',record->>'source_line_id' FROM wb_orders WHERE workspace_id=$1 AND generation=$2 AND connection_id=$3`, w, g, id)
	if err != nil {
		return run, err
	}
	count := 0
	identityChanged := false
	for rows.Next() {
		var sourceKey, order, line string
		if err = rows.Scan(&sourceKey, &order, &line); err != nil {
			rows.Close()
			return run, err
		}
		count++
		if r, ok := mapping[sourceKey]; ok && (r.SourceOrderID != order || r.SourceLineID != line) {
			identityChanged = true
		}
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		return run, err
	}
	if identityChanged || count >= 10 && len(records)*2 < count {
		return run, ErrInvalid
	}
	if _, err = tx.Exec(ctx, `UPDATE wb_runs SET state='cancelled',error_code='source_superseded',finished_at=now() WHERE workspace_id=$1 AND generation=$2 AND connection_id=$3 AND state IN('queued','running','validating','publishing')`, w, g, id); err != nil {
		return run, err
	}
	if _, err = tx.Exec(ctx, `UPDATE wb_connections SET source_records=$3,source_failure=false,source_revision=source_revision+1,revision=revision+1,fence=fence+1,active_run_id=NULL,lease_expires_at=NULL WHERE workspace_id=$1 AND id=$2`, w, id, raw(records)); err != nil {
		return run, err
	}
	if _, err = tx.Exec(ctx, `DELETE FROM wb_requirements WHERE workspace_id=$1 AND generation=$2 AND connection_id=$3`, w, g, id); err != nil {
		return run, err
	}
	for _, r := range requirements {
		if _, err = tx.Exec(ctx, `INSERT INTO wb_requirements(workspace_id,generation,connection_id,source_key,data) VALUES($1,$2,$3,$4,$5)`, w, g, id, r.SourceKey, raw(r)); err != nil {
			return run, err
		}
	}
	run, err = s.queueConnectionTx(ctx, tx, actor, w, id, g)
	if err != nil {
		return run, err
	}
	if err = saveOperation(ctx, tx, actor, w, "connection.source", key, payload, g, run); err != nil {
		return run, err
	}
	if err = audit(ctx, tx, actor, w, "connection.source_staged"); err != nil {
		return run, err
	}
	return run, tx.Commit(ctx)
}
