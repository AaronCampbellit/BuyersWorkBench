package workbench

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"buyersworkbench/internal/domain"
	"github.com/jackc/pgx/v5"
)

func defaultConnectionID(ctx context.Context, tx pgx.Tx, w string) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT id::text FROM wb_connections WHERE workspace_id=$1 AND is_demo_default`, w).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		err = ErrNotFound
	}
	return id, err
}

func normalizeConnection(input ConnectionInput) (ConnectionInput, error) {
	input.Name = strings.TrimSpace(input.Name)
	if !utf8.ValidString(input.Name) || len([]rune(input.Name)) < 1 || len([]rune(input.Name)) > 100 || strings.IndexFunc(input.Name, unicode.IsControl) >= 0 {
		return input, ErrInvalid
	}
	switch input.Method {
	case "fictional", "api_polling", "webhook", "browser", "public_scrape", "email":
	default:
		return input, ErrInvalid
	}
	if input.MinimumCadenceSeconds == 0 {
		input.MinimumCadenceSeconds = 30
	}
	if input.CadenceSeconds == 0 {
		input.CadenceSeconds = 300
		if input.CadenceSeconds < input.MinimumCadenceSeconds {
			input.CadenceSeconds = input.MinimumCadenceSeconds
		}
	}
	if input.MinimumCadenceSeconds < 30 || input.MinimumCadenceSeconds > 86400 || input.CadenceSeconds < input.MinimumCadenceSeconds || input.CadenceSeconds > 86400 {
		return input, ErrInvalid
	}
	if input.SupplierID != nil && *input.SupplierID == "" {
		input.SupplierID = nil
	}
	if input.BuyerUserID != nil && *input.BuyerUserID == "" {
		input.BuyerUserID = nil
	}
	if input.SupplierID != nil && !uuidRE.MatchString(*input.SupplierID) || input.BuyerUserID != nil && !uuidRE.MatchString(*input.BuyerUserID) {
		return input, ErrInvalid
	}
	return input, nil
}
func validateConnectionReferences(ctx context.Context, tx pgx.Tx, w string, g int64, input ConnectionInput) error {
	if input.SupplierID != nil {
		var valid bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM wb_supplier_profiles WHERE workspace_id=$1 AND generation=$2 AND id=$3 AND status='active')`, w, g, *input.SupplierID).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return ErrInvalid
		}
	}
	if input.BuyerUserID != nil {
		var valid bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.workspace_id=$1 AND m.user_id=$2 AND u.active AND u.disabled_at IS NULL AND (u.expires_at IS NULL OR u.expires_at>now()))`, w, *input.BuyerUserID).Scan(&valid); err != nil {
			return err
		}
		if !valid {
			return ErrInvalid
		}
	}
	return nil
}
func (s *Service) CreateConnection(ctx context.Context, actor, w string, g int64, key string, input ConnectionInput) (Connection, error) {
	if g < 1 {
		return Connection{}, ErrInvalid
	}
	input, err := normalizeConnection(input)
	if err != nil {
		return Connection{}, err
	}
	tx, sc, err := s.begin(ctx, actor, w, g, true)
	if err != nil {
		return Connection{}, err
	}
	defer tx.Rollback(ctx)
	if !sc.isOwner {
		return Connection{}, ErrNotFound
	}
	if (sc.kind == "private") == (input.Method == "fictional") {
		return Connection{}, ErrInvalid
	}
	payload := string(raw(input))
	var c Connection
	if found, err := operation(ctx, tx, actor, w, "connection.create", key, payload, sc.generation, &c); err != nil {
		return c, err
	} else if found {
		return c, tx.Commit(ctx)
	}
	var count int
	var hasDefault bool
	if err = tx.QueryRow(ctx, `SELECT count(*),COALESCE(bool_or(is_demo_default),false) FROM wb_connections WHERE workspace_id=$1`, w).Scan(&count, &hasDefault); err != nil {
		return c, err
	}
	if count >= 50 || sc.kind != "private" && !hasDefault && count >= 49 {
		return c, ErrInvalid
	}
	if err = validateConnectionReferences(ctx, tx, w, sc.generation, input); err != nil {
		return c, err
	}
	connector, origin, status := "fictional_supplier", "fictional", "ready"
	if input.Method != "fictional" {
		connector, origin, status = "unconfigured", "live_private", "not_configured"
	}
	c, err = connectionRow(tx.QueryRow(ctx, `INSERT INTO wb_connections(workspace_id,name,supplier_id,buyer_user_id,method,minimum_cadence_seconds,cadence_seconds,connector,origin,status,is_demo_default,source_records,paused) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,false,'[]',false) RETURNING `+connectionColumns, w, input.Name, input.SupplierID, input.BuyerUserID, input.Method, input.MinimumCadenceSeconds, input.CadenceSeconds, connector, origin, status))
	if err != nil {
		return c, err
	}
	if err = saveOperation(ctx, tx, actor, w, "connection.create", key, payload, sc.generation, c); err != nil {
		return c, err
	}
	if err = audit(ctx, tx, actor, w, "connection.create"); err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}
func (s *Service) UpdateConnection(ctx context.Context, actor, w, id string, g, revision int64, input ConnectionInput) (Connection, error) {
	if !uuidRE.MatchString(id) || g < 1 || revision < 1 {
		return Connection{}, ErrInvalid
	}
	input, err := normalizeConnection(input)
	if err != nil {
		return Connection{}, err
	}
	tx, sc, err := s.begin(ctx, actor, w, g, true)
	if err != nil {
		return Connection{}, err
	}
	defer tx.Rollback(ctx)
	if !sc.isOwner {
		return Connection{}, ErrNotFound
	}
	c, err := connectionRow(tx.QueryRow(ctx, `SELECT `+connectionColumns+` FROM wb_connections WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, w, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	if c.Revision != revision {
		return c, ErrStale
	}
	// Method identifies the feed. Replacing it requires a new connection, so
	// accepted observations can never be reinterpreted as another supplier source.
	if c.Method != input.Method || c.IsDemoDefault && input.SupplierID != nil {
		return c, ErrInvalid
	}
	if err = validateConnectionReferences(ctx, tx, w, sc.generation, input); err != nil {
		return c, err
	}
	if input.SupplierID != nil {
		var mismatch bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM wb_orders o JOIN wb_supplier_profiles p ON p.workspace_id=o.workspace_id AND p.generation=o.generation AND p.id=$4 WHERE o.workspace_id=$1 AND o.generation=$2 AND o.connection_id=$3 AND o.record->>'supplier_alias'<>p.alias)`, w, sc.generation, id, *input.SupplierID).Scan(&mismatch); err != nil {
			return c, err
		}
		if mismatch {
			return c, ErrInvalid
		}
	}
	c, err = connectionRow(tx.QueryRow(ctx, `UPDATE wb_connections SET name=$3,supplier_id=$4,buyer_user_id=$5,minimum_cadence_seconds=$6,cadence_seconds=$7,revision=revision+1,next_due_at=CASE WHEN schedule_enabled AND NOT paused THEN now()+($7::integer*interval '1 second') ELSE NULL END WHERE workspace_id=$1 AND id=$2 RETURNING `+connectionColumns, w, id, input.Name, input.SupplierID, input.BuyerUserID, input.MinimumCadenceSeconds, input.CadenceSeconds))
	if err != nil {
		return c, err
	}
	if err = audit(ctx, tx, actor, w, "connection.update"); err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}
func (s *Service) PauseConnection(ctx context.Context, actor, w, id string, g, revision int64, paused bool) (Connection, error) {
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
	c, err := connectionRow(tx.QueryRow(ctx, `SELECT `+connectionColumns+` FROM wb_connections WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, w, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	if c.Revision != revision {
		return c, ErrStale
	}
	if c.Paused == paused {
		return c, tx.Commit(ctx)
	}
	if paused {
		if _, err = tx.Exec(ctx, `UPDATE wb_runs SET state='cancelled',error_code='connection_paused',finished_at=now() WHERE workspace_id=$1 AND generation=$2 AND connection_id=$3 AND state IN('queued','running','validating','publishing')`, w, sc.generation, id); err != nil {
			return c, err
		}
	}
	c, err = connectionRow(tx.QueryRow(ctx, `UPDATE wb_connections SET paused=$3,schedule_enabled=CASE WHEN $3 THEN false ELSE schedule_enabled END,revision=revision+1,fence=fence+1,active_run_id=NULL,lease_expires_at=NULL,next_due_at=CASE WHEN NOT $3 AND schedule_enabled THEN now()+(cadence_seconds*interval '1 second') ELSE NULL END,status=CASE WHEN method<>'fictional' THEN 'not_configured' WHEN $3 THEN 'paused' WHEN last_failure_code IS NOT NULL THEN 'attention' ELSE 'ready' END WHERE workspace_id=$1 AND id=$2 RETURNING `+connectionColumns, w, id, paused))
	if err != nil {
		return c, err
	}
	if err = audit(ctx, tx, actor, w, "connection.pause"); err != nil {
		return c, err
	}
	return c, tx.Commit(ctx)
}
func (s *Service) RefreshConnection(ctx context.Context, actor, w, id string, g int64) (Run, error) {
	if g < 1 {
		return Run{}, ErrInvalid
	}
	if !uuidRE.MatchString(id) {
		return Run{}, ErrNotFound
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
		return Run{}, ErrInvalid
	}
	run, err := s.queueConnectionTx(ctx, tx, actor, w, id, sc.generation)
	if err != nil {
		return run, err
	}
	if err = audit(ctx, tx, actor, w, "connection.refresh"); err != nil {
		return run, err
	}
	return run, tx.Commit(ctx)
}

// ReplaceConnectionSource is a local convenience for fixture tests; HTTP uses
// StageConnectionSource with an explicit revision and stable retry key.
func (s *Service) ReplaceConnectionSource(ctx context.Context, actor, w, id string, g int64, records []domain.Record, requirements []domain.Requirement) (Run, error) {
	o, err := s.Overview(ctx, actor, w)
	if err != nil {
		return Run{}, err
	}
	var revision int64
	for _, c := range o.Connections {
		if c.ID == id {
			revision = c.Revision
		}
	}
	if revision == 0 {
		return Run{}, ErrNotFound
	}
	var key string
	if err = s.Pool.QueryRow(ctx, "SELECT gen_random_uuid()::text").Scan(&key); err != nil {
		return Run{}, err
	}
	return s.StageConnectionSource(ctx, actor, w, id, g, revision, key, records, requirements)
}
