package workbench

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"regexp"
	"time"

	"buyersworkbench/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

const SeedVersion = "seed-1.0"

type Service struct {
	Pool  *pgxpool.Pool
	queue *river.Client[pgx.Tx]
}

func New(pool *pgxpool.Pool) *Service {
	q, _ := river.NewClient(riverpgxv5.New(pool), &river.Config{Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	return &Service{Pool: pool, queue: q}
}

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type scope struct {
	kind         string
	generation   int64
	businessDate time.Time
	isOwner      bool
	seedVersion  string
}

func (s *Service) begin(ctx context.Context, actor, w string, g int64, write bool) (pgx.Tx, scope, error) {
	var sc scope
	if !uuidRE.MatchString(actor) || !uuidRE.MatchString(w) {
		return nil, sc, ErrNotFound
	}
	opts := pgx.TxOptions{}
	if !write {
		opts.IsoLevel = pgx.RepeatableRead
		opts.AccessMode = pgx.ReadOnly
	}
	tx, err := s.Pool.BeginTx(ctx, opts)
	if err != nil {
		return nil, sc, err
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('bwb.actor_id',$1,true)", actor); err != nil {
		tx.Rollback(ctx)
		return nil, sc, err
	}
	query := `SELECT w.kind,w.generation,COALESCE(w.business_date,current_date),u.is_owner,COALESCE(w.seed_version,'') FROM workspaces w JOIN users u ON u.id=$2 WHERE w.id=$1 AND bwb_can_access(w.id)`
	if write {
		query += " FOR UPDATE OF w"
	}
	err = tx.QueryRow(ctx, query, w, actor).Scan(&sc.kind, &sc.generation, &sc.businessDate, &sc.isOwner, &sc.seedVersion)
	if err != nil {
		tx.Rollback(ctx)
		if errors.Is(err, pgx.ErrNoRows) {
			err = ErrNotFound
		}
		return nil, sc, err
	}
	if g > 0 && g != sc.generation {
		tx.Rollback(ctx)
		return nil, sc, ErrStale
	}
	return tx, sc, nil
}
func raw(v any) []byte { b, _ := json.Marshal(v); return b }
func audit(ctx context.Context, tx pgx.Tx, actor, w, action string) error {
	_, err := tx.Exec(ctx, `INSERT INTO audit_events(id,actor_user_id,action,target_type,target_id,outcome) VALUES(gen_random_uuid(),$1,$3,'workspace',$2,'success')`, actor, w, action)
	return err
}
func runRow(row interface{ Scan(...any) error }) (Run, error) {
	var r Run
	err := row.Scan(&r.ID, &r.ConnectionID, &r.Generation, &r.State, &r.Attempt, &r.EnqueuedAt, &r.StartedAt, &r.FinishedAt, &r.RecordCount, &r.ErrorCode)
	return r, err
}

const runColumns = `id::text,connection_id::text,generation,state,attempt,enqueued_at,started_at,finished_at,record_count,error_code`

func connectionRow(row interface{ Scan(...any) error }) (Connection, error) {
	var c Connection
	err := row.Scan(&c.ID, &c.Connector, &c.Origin, &c.Status, &c.SourceRevision, &c.LastAttemptAt, &c.LastSuccessAt, &c.LastFailureCode, &c.ScheduleEnabled, &c.CadenceSeconds, &c.NextDueAt, &c.Name, &c.SupplierID, &c.Method, &c.Revision, &c.Paused, &c.MinimumCadenceSeconds, &c.IsDemoDefault, &c.BuyerUserID)
	c.Runnable = c.Method == "fictional" && c.Connector == "fictional_supplier" && c.Origin == "fictional" && !c.Paused
	return c, err
}

const connectionColumns = `id::text,connector,origin,status,source_revision,last_attempt_at,last_success_at,last_failure_code,schedule_enabled,cadence_seconds,next_due_at,name,supplier_id::text,method,revision,paused,minimum_cadence_seconds,is_demo_default,buyer_user_id::text`

func (s *Service) Seed(ctx context.Context, actor, w string, g int64) (Run, error) {
	tx, sc, err := s.begin(ctx, actor, w, g, true)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback(ctx)
	if sc.kind == "private" {
		return Run{}, ErrPrivate
	}
	if err = s.seedTx(ctx, tx, actor, w, sc.generation); err != nil {
		return Run{}, err
	}
	run, err := s.queueTx(ctx, tx, actor, w, sc.generation)
	if err != nil {
		return Run{}, err
	}
	return run, tx.Commit(ctx)
}

// EnsureSeed is explicit setup, never invoked from a read endpoint.
func (s *Service) EnsureSeed(ctx context.Context, actor, w string) error {
	_, err := s.Seed(ctx, actor, w, 0)
	return err
}
func (s *Service) seedTx(ctx context.Context, tx pgx.Tx, actor, w string, g int64) error {
	// Name-only profiles are derived from the pinned fictional seed. Existing
	// contacts and edited display names are never overwritten by reseeding.
	for _, record := range domain.SeedRecords() {
		if err := ensureSupplierTx(ctx, tx, w, g, record.SupplierAlias); err != nil {
			return err
		}
	}
	var exists bool
	if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM wb_connections WHERE workspace_id=$1 AND is_demo_default)", w).Scan(&exists); err != nil {
		return err
	}
	if exists {
		var seeded bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM wb_quote_comparisons WHERE workspace_id=$1 AND generation=$2)", w, g).Scan(&seeded); err != nil {
			return err
		}
		if seeded {
			return nil
		}
	}
	if !exists {
		if _, err := tx.Exec(ctx, `INSERT INTO wb_connections(workspace_id,source_records,is_demo_default) VALUES($1,$2,true)`, w, raw(domain.SeedRecords())); err != nil {
			return err
		}
	}
	for _, r := range domain.SeedRequirements() {
		if _, err := tx.Exec(ctx, `INSERT INTO wb_requirements(workspace_id,generation,source_key,data,connection_id) SELECT $1,$2,$3,$4,id FROM wb_connections WHERE workspace_id=$1 AND is_demo_default ON CONFLICT DO NOTHING`, w, g, r.SourceKey, raw(r)); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO wb_quote_comparisons(workspace_id,generation,data) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, w, g, raw(domain.SeedQuotes())); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE workspaces SET seed_version=$2,business_date='2026-10-19' WHERE id=$1`, w, SeedVersion)
	if err != nil {
		return err
	}
	return audit(ctx, tx, actor, w, "fictional.seed")
}
func (s *Service) queueTx(ctx context.Context, tx pgx.Tx, actor, w string, g int64) (Run, error) {
	connectionID, err := defaultConnectionID(ctx, tx, w)
	if err != nil {
		return Run{}, err
	}
	return s.queueConnectionTx(ctx, tx, actor, w, connectionID, g)
}
func (s *Service) queueConnectionTx(ctx context.Context, tx pgx.Tx, actor, w, connectionID string, g int64) (Run, error) {
	var fence int64
	var active *string
	var live, runnable bool
	err := tx.QueryRow(ctx, `SELECT fence,active_run_id::text,COALESCE(lease_expires_at>now(),false),method='fictional' AND origin='fictional' AND connector='fictional_supplier' AND NOT paused FROM wb_connections WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, w, connectionID).Scan(&fence, &active, &live, &runnable)
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	if err != nil {
		return Run{}, err
	}
	if !runnable {
		return Run{}, ErrInvalid
	}
	if active != nil && live {
		run, err := runRow(tx.QueryRow(ctx, "SELECT "+runColumns+" FROM wb_runs WHERE id=$1 AND workspace_id=$2 AND generation=$3 AND connection_id=$4 AND fence=$5", *active, w, g, connectionID, fence))
		if err == nil && (run.State == "queued" || run.State == "running" || run.State == "validating" || run.State == "publishing") {
			return run, nil
		}
	}
	if active != nil {
		if _, err := tx.Exec(ctx, `UPDATE wb_runs SET state='cancelled',finished_at=now(),error_code='superseded_lease' WHERE id=$1 AND state IN('queued','running','validating','publishing')`, *active); err != nil {
			return Run{}, err
		}
	}
	var id string
	if err = tx.QueryRow(ctx, "SELECT gen_random_uuid()::text").Scan(&id); err != nil {
		return Run{}, err
	}
	fence++
	if _, err = tx.Exec(ctx, `UPDATE wb_connections SET fence=$2,active_run_id=$3,lease_expires_at=now()+interval '2 minutes',last_attempt_at=now(),status='queued' WHERE id=$1`, connectionID, fence, id); err != nil {
		return Run{}, err
	}
	run, err := runRow(tx.QueryRow(ctx, `INSERT INTO wb_runs(id,workspace_id,generation,connection_id,actor_user_id,fence) VALUES($1,$2,$3,$4,$5,$6) RETURNING `+runColumns, id, w, g, connectionID, actor, fence))
	if err != nil {
		return Run{}, err
	}
	if s.queue == nil {
		return Run{}, errors.New("queue initialization failed")
	}
	job, err := s.queue.InsertTx(ctx, tx, CollectionArgs{1, id, connectionID, w, g, actor, fence}, nil)
	if err != nil {
		return Run{}, err
	}
	if _, err = tx.Exec(ctx, "UPDATE wb_runs SET job_id=$2 WHERE id=$1", id, job.Job.ID); err != nil {
		return Run{}, err
	}
	return run, nil
}
func (s *Service) Refresh(ctx context.Context, actor, w string, g int64) (Run, error) {
	tx, sc, err := s.begin(ctx, actor, w, g, true)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback(ctx)
	if sc.kind == "private" {
		return Run{}, ErrPrivate
	}
	run, err := s.queueTx(ctx, tx, actor, w, sc.generation)
	if err != nil {
		return Run{}, err
	}
	if err = audit(ctx, tx, actor, w, "fictional.refresh"); err != nil {
		return Run{}, err
	}
	return run, tx.Commit(ctx)
}
func operation(ctx context.Context, tx pgx.Tx, actor, w, kind, key, payload string, g int64, result any) (bool, error) {
	if key == "" || len(key) > 128 {
		return false, ErrInvalid
	}
	sum := sha256.Sum256([]byte(payload))
	hash := hex.EncodeToString(sum[:])
	var oldHash string
	var old []byte
	err := tx.QueryRow(ctx, "SELECT payload_hash,result FROM wb_operations WHERE workspace_id=$1 AND actor_id=$2 AND operation=$3 AND idempotency_key=$4 AND generation=$5", w, actor, kind, key, g).Scan(&oldHash, &old)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if oldHash != hash {
		return false, ErrStale
	}
	return true, json.Unmarshal(old, result)
}
func saveOperation(ctx context.Context, tx pgx.Tx, actor, w, kind, key, payload string, g int64, result any) error {
	sum := sha256.Sum256([]byte(payload))
	_, err := tx.Exec(ctx, `INSERT INTO wb_operations(workspace_id,actor_id,operation,idempotency_key,payload_hash,result,generation) VALUES($1,$2,$3,$4,$5,$6,$7)`, w, actor, kind, key, hex.EncodeToString(sum[:]), raw(result), g)
	return err
}
