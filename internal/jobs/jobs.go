// Package jobs registers durable foundation and supplier collection workers.
package jobs

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"regexp"
	"time"

	"buyersworkbench/internal/workbench"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
)

type ProbeArgs struct {
	SchemaVersion int    `json:"schema_version"`
	WorkspaceID   string `json:"workspace_id"`
	Generation    int64  `json:"generation"`
	ActorUserID   string `json:"actor_user_id"`
}

func (ProbeArgs) Kind() string                 { return "foundation_probe" }
func (ProbeArgs) InsertOpts() river.InsertOpts { return river.InsertOpts{MaxAttempts: 3} }

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type ProbeWorker struct {
	river.WorkerDefaults[ProbeArgs]
	Pool *pgxpool.Pool
}

func (w *ProbeWorker) Timeout(*river.Job[ProbeArgs]) time.Duration { return 10 * time.Second }

func (w *ProbeWorker) Work(ctx context.Context, job *river.Job[ProbeArgs]) error {
	a := job.Args
	if a.SchemaVersion != 1 || a.Generation < 1 || !uuidPattern.MatchString(a.WorkspaceID) || !uuidPattern.MatchString(a.ActorUserID) {
		return river.JobCancel(errors.New("invalid foundation probe envelope"))
	}
	// Recheck scope in the same statement that writes the durable result. Full
	// reset/ingestion fencing belongs to M2; this job has no purchasing writes.
	var succeeded bool
	err := w.Pool.QueryRow(ctx, `INSERT INTO foundation_checks(id,workspace_id,job_id,result)
		SELECT gen_random_uuid(),w.id,$4,'queue_probe_succeeded'
		FROM workspaces w JOIN memberships m ON m.workspace_id=w.id JOIN users u ON u.id=m.user_id
		WHERE w.id=$1 AND w.kind='presentation' AND w.generation=$2 AND m.user_id=$3
		AND m.role='owner' AND u.active AND u.disabled_at IS NULL AND u.is_owner
		ON CONFLICT(job_id) DO UPDATE SET job_id=EXCLUDED.job_id RETURNING true`, a.WorkspaceID, a.Generation, a.ActorUserID, job.ID).Scan(&succeeded)
	if errors.Is(err, pgx.ErrNoRows) {
		return river.JobCancel(errors.New("foundation probe scope is no longer authorized"))
	}
	if err != nil {
		return errors.New("foundation probe result write failed")
	}
	return nil
}

func NewClient(pool *pgxpool.Pool, runWorkers bool, logger *slog.Logger) (*river.Client[pgx.Tx], error) {
	workers := river.NewWorkers()
	river.AddWorker(workers, &ProbeWorker{Pool: pool})
	river.AddWorker(workers, &CollectionWorker{Service: workbench.New(pool)})
	config := &river.Config{Workers: workers, Logger: logger}
	if runWorkers {
		config.Queues = map[string]river.QueueConfig{river.QueueDefault: {MaxWorkers: 2}}
	}
	return river.NewClient(riverpgxv5.New(pool), config)
}

// EnqueueProbe is a local operator command, not an HTTP purchasing workflow.
// It can target only the bootstrap owner's presentation workspace.
func EnqueueProbe(ctx context.Context, pool *pgxpool.Pool, workspaceID string) (int64, error) {
	if !uuidPattern.MatchString(workspaceID) {
		return 0, errors.New("probe requires a workspace UUID")
	}
	args := ProbeArgs{SchemaVersion: 1, WorkspaceID: workspaceID}
	err := pool.QueryRow(ctx, `SELECT w.generation,w.owner_user_id::text FROM workspaces w
		JOIN users u ON u.id=w.owner_user_id JOIN memberships m ON m.workspace_id=w.id AND m.user_id=u.id
		WHERE w.id=$1 AND w.kind='presentation' AND u.is_owner AND u.active AND u.disabled_at IS NULL AND m.role='owner'`, workspaceID).
		Scan(&args.Generation, &args.ActorUserID)
	if err != nil {
		return 0, errors.New("probe requires an authorized owner presentation workspace")
	}
	client, err := NewClient(pool, false, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		return 0, errors.New("queue initialization failed")
	}
	result, err := client.Insert(ctx, args, nil)
	if err != nil {
		return 0, errors.New("queue probe enqueue failed")
	}
	return result.Job.ID, nil
}
