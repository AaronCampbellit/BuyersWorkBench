package jobs

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"buyersworkbench/internal/store"
	"buyersworkbench/internal/workbench"
	"buyersworkbench/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

func TestProbeRejectsInvalidEnvelopeBeforeDatabase(t *testing.T) {
	w := &ProbeWorker{}
	for _, args := range []ProbeArgs{
		{},
		{SchemaVersion: 1, WorkspaceID: "arbitrary", Generation: 1, ActorUserID: "arbitrary"},
		{SchemaVersion: 2, WorkspaceID: "00000000-0000-0000-0000-000000000001", Generation: 1, ActorUserID: "00000000-0000-0000-0000-000000000002"},
	} {
		if w.Work(context.Background(), &river.Job[ProbeArgs]{Args: args}) == nil {
			t.Fatal("invalid job envelope accepted")
		}
	}
}

func TestDurableRiverProbeIntegration(t *testing.T) {
	adminURL, appURL := os.Getenv("BWB_TEST_DATABASE_URL"), os.Getenv("BWB_TEST_APP_DATABASE_URL")
	if adminURL == "" || appURL == "" {
		t.Skip("set both database test URLs to run a real River worker with the runtime role")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	admin, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatal("connect test administrator")
	}
	var id string
	if err = admin.QueryRow(ctx, `SELECT gen_random_uuid()::text`).Scan(&id); err != nil {
		t.Fatal("generate schema ID")
	}
	schema := "bwb_jobs_test_" + strings.ReplaceAll(id, "-", "")
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatal("create isolated job schema")
	}
	t.Cleanup(func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if _, err := admin.Exec(cleanup, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Error("drop job schema failed")
		}
		_ = admin.Close(cleanup)
	})
	newPool := func(url string) *pgxpool.Pool {
		cfg, err := pgxpool.ParseConfig(url)
		if err != nil {
			t.Fatal("parse test pool")
		}
		cfg.ConnConfig.RuntimeParams["search_path"] = schema
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			t.Fatal("open isolated test pool")
		}
		t.Cleanup(pool.Close)
		return pool
	}
	adminPool := newPool(adminURL)
	if err = migrations.Apply(ctx, adminPool); err != nil {
		t.Fatal("apply isolated foundation migrations")
	}
	if err = store.MigrateQueue(ctx, adminPool); err != nil {
		t.Fatal("apply isolated queue migrations")
	}
	if _, err = admin.Exec(ctx, "GRANT USAGE ON SCHEMA "+quotedSchema+" TO bwb_app"); err != nil {
		t.Fatal("grant isolated runtime schema usage")
	}
	owner, err := store.BootstrapOwner(ctx, adminPool, store.OwnerInput{Email: "probe@example.test", DisplayName: "Probe test owner", Password: "Isolated probe test-only long password"})
	if err != nil {
		t.Fatal("bootstrap isolated probe owner")
	}
	appPool := newPool(appURL)
	client, err := NewClient(appPool, true, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal("create River client")
	}
	events, unsubscribe := client.Subscribe(river.EventKindJobCompleted, river.EventKindJobCancelled, river.EventKindJobFailed)
	t.Cleanup(unsubscribe)
	if err = client.Start(ctx); err != nil {
		t.Fatal("start real River worker")
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if client.Stop(stopCtx) != nil {
			t.Error("stop real River worker")
		}
	})
	jobID, err := EnqueueProbe(ctx, appPool, owner.PresentationWorkspaceID)
	if err != nil {
		t.Fatal("enqueue durable probe")
	}
	select {
	case event := <-events:
		if event.Kind != river.EventKindJobCompleted || event.Job.ID != jobID {
			t.Fatalf("probe job did not complete: %s", event.Kind)
		}
	case <-ctx.Done():
		t.Fatal("real River probe timed out")
	}
	var result string
	var count int
	if err = adminPool.QueryRow(ctx, `SELECT result FROM foundation_checks WHERE workspace_id=$1 AND job_id=$2`, owner.PresentationWorkspaceID, jobID).Scan(&result); err != nil || result != "queue_probe_succeeded" {
		t.Fatal("real worker did not persist its result")
	}
	worker := &ProbeWorker{Pool: appPool}
	if err = worker.Work(ctx, &river.Job[ProbeArgs]{JobRow: &rivertype.JobRow{ID: jobID}, Args: ProbeArgs{SchemaVersion: 1, WorkspaceID: owner.PresentationWorkspaceID, Generation: 1, ActorUserID: owner.OwnerID}}); err != nil {
		t.Fatal("duplicate probe execution failed")
	}
	if err = adminPool.QueryRow(ctx, `SELECT count(*) FROM foundation_checks WHERE job_id=$1`, jobID).Scan(&count); err != nil || count != 1 {
		t.Fatal("duplicate probe created extra durable results")
	}
	if _, err = EnqueueProbe(ctx, appPool, owner.PrivateWorkspaceID); err == nil {
		t.Fatal("probe accepted a private workspace")
	}
	stale, err := client.Insert(ctx, ProbeArgs{SchemaVersion: 1, WorkspaceID: owner.PresentationWorkspaceID, Generation: 999, ActorUserID: owner.OwnerID}, nil)
	if err != nil {
		t.Fatal("enqueue stale probe")
	}
	select {
	case event := <-events:
		if event.Kind != river.EventKindJobCancelled || event.Job.ID != stale.Job.ID {
			t.Fatalf("stale probe was not cancelled: %s", event.Kind)
		}
	case <-ctx.Done():
		t.Fatal("stale River probe timed out")
	}
	if err = adminPool.QueryRow(ctx, `SELECT count(*) FROM foundation_checks WHERE job_id=$1`, stale.Job.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("stale probe wrote a durable result")
	}
	// Inject a publication failure with a working database. Earlier queue
	// attempts remain retryable; the final one must finish the domain run too.
	stopCtx, stopCancel := context.WithTimeout(ctx, 5*time.Second)
	if err = client.Stop(stopCtx); err != nil {
		stopCancel()
		t.Fatal("stop background workers before injected failure")
	}
	stopCancel()
	svc := workbench.New(appPool)
	baseline, err := svc.Seed(ctx, owner.OwnerID, owner.PresentationWorkspaceID, 1)
	if err != nil {
		t.Fatal(err)
	}
	args := workbench.CollectionArgs{SchemaVersion: 1, RunID: baseline.ID, ConnectionID: baseline.ConnectionID, WorkspaceID: owner.PresentationWorkspaceID, Generation: 1, ActorUserID: owner.OwnerID}
	if err = adminPool.QueryRow(ctx, "SELECT fence FROM wb_runs WHERE id=$1", baseline.ID).Scan(&args.Fence); err != nil {
		t.Fatal(err)
	}
	if err = svc.Collect(ctx, args); err != nil {
		t.Fatal(err)
	}
	pending, err := svc.Refresh(ctx, owner.OwnerID, owner.PresentationWorkspaceID, 1)
	if err != nil {
		t.Fatal(err)
	}
	args.RunID = pending.ID
	if err = adminPool.QueryRow(ctx, "SELECT fence FROM wb_runs WHERE id=$1", pending.ID).Scan(&args.Fence); err != nil {
		t.Fatal(err)
	}
	if _, err = adminPool.Exec(ctx, `CREATE FUNCTION test_publication_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected transient publication failure' USING ERRCODE='40001'; END $$;
 CREATE TRIGGER test_publication_failure BEFORE INSERT ON wb_observations FOR EACH ROW EXECUTE FUNCTION test_publication_failure()`); err != nil {
		t.Fatal(err)
	}
	collector := &CollectionWorker{Service: svc}
	if err = collector.Work(ctx, &river.Job[workbench.CollectionArgs]{JobRow: &rivertype.JobRow{Attempt: 2, MaxAttempts: 3}, Args: args}); err == nil {
		t.Fatal("injected publication failure was swallowed")
	}
	var state string
	if err = adminPool.QueryRow(ctx, "SELECT state FROM wb_runs WHERE id=$1", pending.ID).Scan(&state); err != nil || state != "running" {
		t.Fatal("retryable collection was prematurely terminal")
	}
	if err = collector.Work(ctx, &river.Job[workbench.CollectionArgs]{JobRow: &rivertype.JobRow{Attempt: 3, MaxAttempts: 3}, Args: args}); err == nil {
		t.Fatal("last failure was swallowed")
	}
	var code string
	var active bool
	if err = adminPool.QueryRow(ctx, `SELECT r.state,r.error_code,c.active_run_id IS NOT NULL FROM wb_runs r JOIN wb_connections c ON c.id=r.connection_id WHERE r.id=$1`, pending.ID).Scan(&state, &code, &active); err != nil || state != "terminal_failed" || code != "collection_retries_exhausted" || active {
		t.Fatalf("last queue attempt did not finish domain failure: %s %s active=%v err=%v", state, code, active, err)
	}
	if err = adminPool.QueryRow(ctx, "SELECT count(*) FROM wb_observations").Scan(&count); err != nil || count != 4 {
		t.Fatal("failed publication overwrote accepted observations")
	}
}
