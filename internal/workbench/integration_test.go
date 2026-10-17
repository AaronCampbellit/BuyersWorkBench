package workbench

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"buyersworkbench/internal/domain"
	"buyersworkbench/internal/store"
	"buyersworkbench/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixture struct {
	ctx                          context.Context
	admin, app                   *pgxpool.Pool
	service                      *Service
	actor, w, otherActor, otherW string
}

func testFoundation(t *testing.T) fixture {
	t.Helper()
	adminURL, appURL := os.Getenv("BWB_TEST_DATABASE_URL"), os.Getenv("BWB_TEST_APP_DATABASE_URL")
	if adminURL == "" || appURL == "" {
		t.Skip("explicit disposable database URLs required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	t.Cleanup(cancel)
	controller, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatal("connect test controller")
	}
	var suffix string
	if err = controller.QueryRow(ctx, "SELECT replace(gen_random_uuid()::text,'-','')").Scan(&suffix); err != nil {
		t.Fatal(err)
	}
	schema := "wb_test_" + suffix
	if _, err = controller.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	if _, err = controller.Exec(ctx, "GRANT USAGE ON SCHEMA "+pgx.Identifier{schema}.Sanitize()+" TO bwb_app"); err != nil {
		t.Fatal(err)
	}
	open := func(url string) *pgxpool.Pool {
		cfg, err := pgxpool.ParseConfig(url)
		if err != nil {
			t.Fatal("parse test URL")
		}
		cfg.ConnConfig.RuntimeParams["search_path"] = schema
		p, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	admin, app := open(adminURL), open(appURL)
	t.Cleanup(func() {
		admin.Close()
		app.Close()
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := controller.Exec(cleanup, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error(err)
		}
		controller.Close(cleanup)
	})
	if err = migrations.Apply(ctx, admin); err != nil {
		t.Fatal(err)
	}
	if err = store.MigrateQueue(ctx, admin); err != nil {
		t.Fatal(err)
	}
	f := fixture{ctx: ctx, admin: admin, app: app, service: New(app)}
	for i := range 2 {
		var actor, w string
		if err = admin.QueryRow(ctx, "INSERT INTO users(id,display_name,is_owner) VALUES(gen_random_uuid(),'Test actor',$1) RETURNING id::text", i == 0).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		if err = admin.QueryRow(ctx, "INSERT INTO workspaces(id,owner_user_id,kind,name,seed_version,business_date) VALUES(gen_random_uuid(),$1,'reviewer','Isolated test','seed-1.0','2026-10-19') RETURNING id::text", actor).Scan(&w); err != nil {
			t.Fatal(err)
		}
		role := "reviewer"
		if i == 0 {
			role = "owner"
		}
		if _, err = admin.Exec(ctx, "INSERT INTO memberships(workspace_id,user_id,role) VALUES($1,$2,$3)", w, actor, role); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			f.actor, f.w = actor, w
		} else {
			f.otherActor, f.otherW = actor, w
		}
	}
	return f
}
func (f fixture) collect(t *testing.T, run Run) {
	t.Helper()
	var args CollectionArgs
	args.SchemaVersion = 1
	args.RunID = run.ID
	args.ConnectionID = run.ConnectionID
	args.WorkspaceID = f.w
	args.Generation = run.Generation
	args.ActorUserID = f.actor
	if err := f.admin.QueryRow(f.ctx, "SELECT fence FROM wb_runs WHERE id=$1", run.ID).Scan(&args.Fence); err != nil {
		t.Fatal(err)
	}
	if err := f.service.Collect(f.ctx, args); err != nil {
		t.Fatal(err)
	}
}
func (f fixture) scenario(t *testing.T, key string) Run {
	t.Helper()
	var id string
	if err := f.admin.QueryRow(f.ctx, "SELECT gen_random_uuid()::text").Scan(&id); err != nil {
		t.Fatal(err)
	}
	r, err := f.service.Scenario(f.ctx, f.actor, f.w, 1, key, id)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, r)
	return r
}
func TestFictionalPipelineWorkflowAndRepeat(t *testing.T) {
	f := testFoundation(t)
	baseline, err := f.service.Seed(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	initializing, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil || initializing.Ready || initializing.State != "initializing" {
		t.Fatal("unpublished seed falsely appears ready", err)
	}
	f.collect(t, baseline)
	overview, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Orders) != 4 || overview.Quotes == nil || overview.Quotes.Calculation.WinnerID != "meridian" {
		t.Fatalf("bad seeded overview: %+v", overview)
	}
	if !overview.Ready || overview.State != "ready" || overview.SeedVersion != domain.SeedVersion {
		t.Fatal("accepted seed readiness metadata missing")
	}
	if overview.Connections[0].LastSuccessAt == nil {
		t.Fatal("real freshness timestamp missing")
	}
	f.scenario(t, "delay")
	delayed, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil {
		t.Fatal(err)
	}
	var risk Exception
	for _, c := range delayed.Priorities {
		if c.Rule == "R01" && c.Active {
			risk = c
		}
	}
	if risk.ID == "" || risk.DelayDays != 3 {
		t.Fatalf("delay did not calculate +3 days: %+v", delayed.Priorities)
	}
	note, err := f.service.AddNote(f.ctx, f.actor, f.w, risk.ID, 1, risk.Revision, "Follow up with fictional supplier")
	if err != nil || note.Body == "" {
		t.Fatal("note persistence:", err)
	}
	if _, err = f.service.UpdateWorkflow(f.ctx, f.actor, f.w, risk.ID, 1, risk.Revision, WorkflowInput{Status: "open"}); !errors.Is(err, ErrStale) {
		t.Fatal("stale workflow revision accepted")
	}
	var changesBefore, notificationsBefore int
	if err = f.admin.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM wb_changes),(SELECT count(*) FROM wb_notifications)").Scan(&changesBefore, &notificationsBefore); err != nil {
		t.Fatal(err)
	}
	f.scenario(t, "repeat")
	var changesAfter, notificationsAfter int
	if err = f.admin.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM wb_changes),(SELECT count(*) FROM wb_notifications)").Scan(&changesAfter, &notificationsAfter); err != nil {
		t.Fatal(err)
	}
	if changesAfter != changesBefore || notificationsAfter != notificationsBefore {
		t.Fatalf("repeat duplicated effects: changes %d/%d notifications %d/%d", changesBefore, changesAfter, notificationsBefore, notificationsAfter)
	}
	detail, err := f.service.Order(f.ctx, f.actor, f.w, risk.OrderID)
	if err != nil || len(detail.Notes) != 1 || detail.Notes[0].Body != note.Body {
		t.Fatal("note did not survive collection:", err)
	}
	f.scenario(t, "recovery")
	f.scenario(t, "delay")
	var transitions int
	if err = f.admin.QueryRow(f.ctx, "SELECT count(*) FROM wb_changes WHERE field='arrival_date'").Scan(&transitions); err != nil || transitions != 3 {
		t.Fatalf("A-B-A-B transitions lost: %d %v", transitions, err)
	}
	beforeFailure, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil {
		t.Fatal(err)
	}
	failed := f.scenario(t, "failure")
	failure, err := f.service.Run(f.ctx, f.actor, f.w, failed.ID)
	if err != nil || failure.State != "rejected" {
		t.Fatalf("failure not visible: %+v %v", failure, err)
	}
	afterFailure, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil || len(afterFailure.Orders) != 4 || !afterFailure.Orders[0].LastObservedAt.Equal(beforeFailure.Orders[0].LastObservedAt) {
		t.Fatal("failure overwrote accepted state")
	}
}
func TestGenerationFencesResetAndRuntimeRLS(t *testing.T) {
	f := testFoundation(t)
	baseline, err := f.service.Seed(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, baseline)
	if _, err = f.service.Overview(f.ctx, f.otherActor, f.w); !errors.Is(err, ErrNotFound) {
		t.Fatal("cross-workspace service access accepted")
	}
	tx, err := f.app.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(f.ctx, "SELECT set_config('bwb.actor_id',$1,true)", f.otherActor); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = tx.QueryRow(f.ctx, "SELECT count(*) FROM wb_orders WHERE workspace_id=$1", f.w).Scan(&count); err != nil || count != 0 {
		t.Fatalf("runtime RLS leaked rows: %d %v", count, err)
	}
	tx.Rollback(f.ctx)
	old, err := f.service.Refresh(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	var fence int64
	if err = f.admin.QueryRow(f.ctx, "SELECT fence FROM wb_runs WHERE id=$1", old.ID).Scan(&fence); err != nil {
		t.Fatal(err)
	}
	reset, err := f.service.Reset(f.ctx, f.actor, f.w, 1, "reset-test-key")
	if err != nil || reset.Generation != 2 {
		t.Fatalf("reset: %+v %v", reset, err)
	}
	initializing, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil || initializing.Ready || initializing.State != "initializing" {
		t.Fatal("reset falsely appears ready before baseline publication")
	}
	repeat, err := f.service.Reset(f.ctx, f.actor, f.w, 1, "reset-test-key")
	if err != nil || repeat.Generation != 2 || repeat.Run.ID != reset.Run.ID {
		t.Fatal("duplicate reset advanced generation")
	}
	if err = f.service.Collect(f.ctx, CollectionArgs{1, old.ID, old.ConnectionID, f.w, 1, f.actor, fence}); !errors.Is(err, ErrStale) {
		t.Fatal("old-generation collector published after reset")
	}
	f.collect(t, reset.Run)
	if _, err = f.service.Refresh(f.ctx, f.actor, f.w, 1); !errors.Is(err, ErrStale) {
		t.Fatal("stale browser mutation accepted")
	}
	overview, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil || overview.Generation != 2 || len(overview.Orders) != 4 {
		t.Fatalf("reset not coherent: %+v %v", overview, err)
	}
	if _, err = f.service.SetSchedule(f.ctx, f.actor, f.w, 2, true, 30); err != nil {
		t.Fatal(err)
	}
	if _, err = f.admin.Exec(f.ctx, "UPDATE wb_connections SET next_due_at=now()-interval '1 day' WHERE workspace_id=$1", f.w); err != nil {
		t.Fatal(err)
	}
	if err = f.service.ReconcileSchedules(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err = f.service.ReconcileSchedules(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err = f.admin.QueryRow(f.ctx, "SELECT count(*) FROM wb_runs WHERE workspace_id=$1 AND generation=2 AND state='queued'", f.w).Scan(&count); err != nil || count != 1 {
		t.Fatalf("overdue schedule replayed ticks: %d %v", count, err)
	}
}
func TestQuotesAndRequirementRecalculation(t *testing.T) {
	f := testFoundation(t)
	run, err := f.service.Seed(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, run)
	overview, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil {
		t.Fatal(err)
	}
	quotes := overview.Quotes
	quote := quotes.Comparison.Quotes[0]
	quote.Freight = "0"
	updated, err := f.service.UpdateQuote(f.ctx, f.actor, f.w, quote.ID, 1, quotes.Revision, quote)
	if err != nil || updated.Calculation.WinnerID != "atlas" {
		t.Fatalf("quote edits not recalculated: %+v %v", updated, err)
	}
	if _, err = f.service.UpdateQuote(f.ctx, f.actor, f.w, quote.ID, 1, quotes.Revision, quote); !errors.Is(err, ErrStale) {
		t.Fatal("concurrent quote edit silently overwrote")
	}
	requirements := domain.SeedRequirements()
	requirements[0].RequiredDate = "2026-10-20"
	run, err = f.service.ReplaceRequirements(f.ctx, f.actor, f.w, 1, requirements)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, run)
	overview, err = f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range overview.Priorities {
		if c.Rule == "R01" && c.Active && strings.Contains(c.Explanation, "1 calendar days") {
			found = true
		}
	}
	if !found {
		t.Fatal("imported requirement did not pass through common rules")
	}
}

func TestLeaseBindingQuarantineAndConcurrentReset(t *testing.T) {
	f := testFoundation(t)
	baseline, err := f.service.Seed(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, baseline)
	f.collect(t, baseline)
	var observations int
	if err = f.admin.QueryRow(f.ctx, "SELECT count(*) FROM wb_observations").Scan(&observations); err != nil || observations != 4 {
		t.Fatal("retry after commit duplicated observations")
	}
	old, err := f.service.Refresh(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	var oldFence int64
	if err = f.admin.QueryRow(f.ctx, "SELECT fence FROM wb_runs WHERE id=$1", old.ID).Scan(&oldFence); err != nil {
		t.Fatal(err)
	}
	latest, err := f.service.Scenario(f.ctx, f.actor, f.w, 1, "delay", "superseding-scenario")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.Collect(f.ctx, CollectionArgs{1, old.ID, old.ConnectionID, f.w, 1, f.actor, oldFence}); !errors.Is(err, ErrStale) {
		t.Fatal("old same-generation lease published")
	}
	f.collect(t, latest)
	pending, err := f.service.Refresh(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	var fence int64
	if err = f.admin.QueryRow(f.ctx, "SELECT fence FROM wb_runs WHERE id=$1", pending.ID).Scan(&fence); err != nil {
		t.Fatal(err)
	}
	args := CollectionArgs{1, pending.ID, pending.ConnectionID, f.w, 1, f.actor, fence}
	binding := domain.Binding{RunID: pending.ID, ConnectionID: pending.ConnectionID, WorkspaceID: f.w, Generation: 1, Connector: domain.Connector{Key: "fictional_supplier", Version: domain.SeedVersion}, Origin: "fictional", Scope: domain.FictionalScope()}
	invalid := domain.FictionalBatch(domain.SeedRecords(), binding, time.Now())
	invalid.WorkspaceID = f.otherW
	if err = f.service.PublishBatch(f.ctx, args, invalid); !errors.Is(err, ErrInvalid) {
		t.Fatal("mismatched binding was accepted")
	}
	var rejected, staged bool
	if err = f.admin.QueryRow(f.ctx, "SELECT state='rejected',staged_batch IS NOT NULL FROM wb_runs WHERE id=$1", pending.ID).Scan(&rejected, &staged); err != nil || !rejected || !staged {
		t.Fatal("invalid batch was not quarantined")
	}
	overview, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil {
		t.Fatal(err)
	}
	var oldCondition string
	for _, c := range overview.Priorities {
		if c.Rule == "R01" {
			oldCondition = c.ID
		}
	}
	if oldCondition == "" {
		t.Fatal("quarantine replaced prior accepted risk")
	}
	var wg sync.WaitGroup
	results := make(chan ResetResult, 2)
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			r, err := f.service.Reset(f.ctx, f.actor, f.w, 1, "concurrent-reset")
			results <- r
			errs <- err
		})
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var runID string
	for r := range results {
		if r.Generation != 2 {
			t.Fatal("concurrent reset advanced twice")
		}
		if runID != "" && r.Run.ID != runID {
			t.Fatal("concurrent resets queued different baseline runs")
		}
		runID = r.Run.ID
	}
	tx, err := f.app.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(f.ctx, "SELECT set_config('bwb.actor_id',$1,true)", f.actor); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(f.ctx, `INSERT INTO wb_notifications(workspace_id,generation,condition_id,recipient_id,event_key,message) VALUES($1,1,$2,$3,'stale-async','must never be delivered')`, f.w, oldCondition, f.actor)
	if err == nil {
		tx.Rollback(f.ctx)
		t.Fatal("old-generation notification escaped database RLS")
	}
	tx.Rollback(f.ctx)
}

func TestExpiredManualLeaseReconcilesWithoutPublishing(t *testing.T) {
	f := testFoundation(t)
	baseline, err := f.service.Seed(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, baseline)
	initial, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil {
		t.Fatal(err)
	}
	freshness := initial.Connections[0].LastSuccessAt
	pending, err := f.service.Refresh(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	var oldFence int64
	if err = f.admin.QueryRow(f.ctx, "SELECT fence FROM wb_runs WHERE id=$1", pending.ID).Scan(&oldFence); err != nil {
		t.Fatal(err)
	}
	if _, err = f.admin.Exec(f.ctx, "UPDATE wb_connections SET lease_expires_at=now()-interval '1 minute',schedule_enabled=false WHERE workspace_id=$1", f.w); err != nil {
		t.Fatal(err)
	}
	// The normal persisted scheduler tick also repairs manual work, even with
	// every application collection schedule disabled.
	if err = f.service.ReconcileSchedules(f.ctx); err != nil {
		t.Fatal(err)
	}
	failed, err := f.service.Run(f.ctx, f.actor, f.w, pending.ID)
	if err != nil || failed.State != "terminal_failed" || failed.ErrorCode == nil || *failed.ErrorCode != "collection_lease_expired" {
		t.Fatalf("expired run remained active: %+v %v", failed, err)
	}
	after, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil || len(after.Orders) != 4 || after.Connections[0].Status != "attention" || after.Connections[0].LastSuccessAt == nil || !after.Connections[0].LastSuccessAt.Equal(*freshness) {
		t.Fatal("lease repair altered accepted data/freshness")
	}
	var observations, notifications, audits int
	if err = f.admin.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM wb_observations),(SELECT count(*) FROM wb_notifications),(SELECT count(*) FROM audit_events WHERE action='collection.lease_expired')`).Scan(&observations, &notifications, &audits); err != nil || observations != 4 || notifications != len(initial.Notifications) || audits != 1 {
		t.Fatal("lease repair published data/notifications or omitted safe audit")
	}
	if err = f.service.ReconcileSchedules(f.ctx); err != nil {
		t.Fatal(err)
	}
	if err = f.admin.QueryRow(f.ctx, "SELECT count(*) FROM audit_events WHERE action='collection.lease_expired'").Scan(&audits); err != nil || audits != 1 {
		t.Fatal("lease repair repeated its durable effect")
	}
	if err = f.service.Collect(f.ctx, CollectionArgs{1, pending.ID, pending.ConnectionID, f.w, 1, f.actor, oldFence}); !errors.Is(err, ErrStale) {
		t.Fatal("expired worker resurrected its finished run")
	}
	next, err := f.service.Refresh(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal("manual collection could not recover:", err)
	}
	f.collect(t, next)
}

func TestPrivilegedMembershipLookupCannotUseTemporaryShadows(t *testing.T) {
	f := testFoundation(t)
	// Production denies TEMP to the runtime role. Build the stronger adversarial
	// setup as the administrator, then evaluate the helper as the runtime role.
	tx, err := f.admin.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = tx.Exec(f.ctx, "SELECT set_config('bwb.actor_id',$1,true)", f.otherActor); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(f.ctx, `CREATE TEMP TABLE memberships(workspace_id uuid,user_id uuid);
 CREATE TEMP TABLE users(id uuid,active boolean,disabled_at timestamptz,expires_at timestamptz,is_owner boolean);
 CREATE TEMP TABLE workspaces(id uuid,kind text,owner_user_id uuid)`); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(f.ctx, "INSERT INTO memberships VALUES($1,$2)", f.w, f.otherActor); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(f.ctx, "INSERT INTO users VALUES($1,true,NULL,NULL,false)", f.otherActor); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(f.ctx, "INSERT INTO workspaces VALUES($1,'reviewer',$2)", f.w, f.otherActor); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(f.ctx, "SET LOCAL ROLE bwb_app"); err != nil {
		t.Fatal(err)
	}
	var allowed bool
	if err = tx.QueryRow(f.ctx, "SELECT bwb_can_access($1)", f.w).Scan(&allowed); err != nil || allowed {
		t.Fatal("temporary table shadow bypassed privileged membership lookup", err)
	}
}

type acceptedSnapshot struct {
	Orders, Conditions                   string
	Observations, Changes, Notifications int
}

func snapshotAcceptedState(t *testing.T, f fixture) acceptedSnapshot {
	t.Helper()
	var state acceptedSnapshot
	err := f.admin.QueryRow(f.ctx, `SELECT
 COALESCE((SELECT jsonb_agg(o ORDER BY o.source_key)::text FROM wb_orders o),'[]'),
 COALESCE((SELECT jsonb_agg(c ORDER BY c.id)::text FROM wb_conditions c),'[]'),
 (SELECT count(*) FROM wb_observations),(SELECT count(*) FROM wb_changes),(SELECT count(*) FROM wb_notifications)`).Scan(&state.Orders, &state.Conditions, &state.Observations, &state.Changes, &state.Notifications)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

// T011: a complete-looking scope with a severe unexplained count drop must be
// quarantined as a whole, before even the surviving lines can change state.
func TestUnexplainedCountDropQuarantinesEntireBatch(t *testing.T) {
	f := testFoundation(t)
	baseline, err := f.service.Seed(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	template := domain.SeedRecords()[0]
	records := make([]domain.Record, 120)
	for i := range records {
		records[i] = template
		records[i].SourceKey = fmt.Sprintf("TEST-COUNT-DROP-%03d:1", i)
		records[i].SourceOrderID = fmt.Sprintf("TEST-COUNT-DROP-%03d", i)
		records[i].SourceLineID = "1"
	}
	if _, err = f.admin.Exec(f.ctx, "UPDATE wb_connections SET source_records=$2,source_revision=source_revision+1 WHERE workspace_id=$1", f.w, raw(records)); err != nil {
		t.Fatal(err)
	}
	f.collect(t, baseline)
	before := snapshotAcceptedState(t, f)
	if before.Observations != 120 {
		t.Fatal("large complete baseline was not accepted")
	}
	overview, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil || len(overview.Orders) != 120 || overview.Connections[0].LastSuccessAt == nil {
		t.Fatal("baseline coverage/freshness missing", err)
	}
	lastSuccess := *overview.Connections[0].LastSuccessAt
	reduced := domain.CloneRecords(records[:12])
	reduced[0].ArrivalDate = "2026-10-28"
	if _, err = f.admin.Exec(f.ctx, "UPDATE wb_connections SET source_records=$2,source_revision=source_revision+1 WHERE workspace_id=$1", f.w, raw(reduced)); err != nil {
		t.Fatal(err)
	}
	pending, err := f.service.Refresh(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, pending)
	rejected, err := f.service.Run(f.ctx, f.actor, f.w, pending.ID)
	if err != nil || rejected.State != "rejected" || rejected.ErrorCode == nil || *rejected.ErrorCode != "unexpected_record_count_drop" || rejected.RecordCount != 0 {
		t.Fatalf("120-to-12 drop was not rejected: %+v %v", rejected, err)
	}
	var complete bool
	var stagedCount int
	if err = f.admin.QueryRow(f.ctx, `SELECT (staged_batch->'scope'->>'complete')::boolean,(staged_batch->'scope'->>'records_collected')::integer FROM wb_runs WHERE id=$1`, pending.ID).Scan(&complete, &stagedCount); err != nil || !complete || stagedCount != 12 {
		t.Fatal("count-drop evidence was not quarantined", err)
	}
	after := snapshotAcceptedState(t, f)
	if before != after {
		t.Fatal("quarantined count drop changed accepted facts, conditions, history, or notifications")
	}
	overview, err = f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil || len(overview.Orders) != 120 || overview.Connections[0].Status != "attention" || overview.Connections[0].LastSuccessAt == nil || !overview.Connections[0].LastSuccessAt.Equal(lastSuccess) {
		t.Fatal("rejected scope overwrote current coverage/freshness", err)
	}
}

// T012: a valid complete empty scope is a collection result, not evidence that
// previously observed orders were delivered/cancelled or their risks resolved.
func TestCompleteEmptyScopeDoesNotInferOrderClosure(t *testing.T) {
	f := testFoundation(t)
	baseline, err := f.service.Seed(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, baseline)
	before := snapshotAcceptedState(t, f)
	if before.Observations != 4 {
		t.Fatal("four-record baseline missing")
	}
	overview, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil || overview.Connections[0].LastSuccessAt == nil {
		t.Fatal(err)
	}
	lastSuccess := *overview.Connections[0].LastSuccessAt
	if _, err = f.admin.Exec(f.ctx, "UPDATE wb_connections SET source_records=$2,source_revision=source_revision+1 WHERE workspace_id=$1", f.w, raw([]domain.Record{})); err != nil {
		t.Fatal(err)
	}
	empty, err := f.service.Refresh(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, empty)
	accepted, err := f.service.Run(f.ctx, f.actor, f.w, empty.ID)
	if err != nil || accepted.State != "succeeded" || accepted.RecordCount != 0 || accepted.ErrorCode != nil {
		t.Fatalf("complete empty scope was not accepted: %+v %v", accepted, err)
	}
	var complete bool
	var collected, expected int
	if err = f.admin.QueryRow(f.ctx, `SELECT (staged_batch->'scope'->>'complete')::boolean,(staged_batch->'scope'->>'records_collected')::integer,(staged_batch->'scope'->>'expected_records')::integer FROM wb_runs WHERE id=$1`, empty.ID).Scan(&complete, &collected, &expected); err != nil || !complete || collected != 0 || expected != 0 {
		t.Fatal("complete empty scope evidence missing", err)
	}
	after := snapshotAcceptedState(t, f)
	if before != after {
		t.Fatal("source absence inferred order closure, refreshed missing facts, or cleared conditions")
	}
	overview, err = f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil || len(overview.Orders) != 4 || !overview.Ready || overview.Connections[0].Status != "ready" || overview.Connections[0].LastSuccessAt == nil || !overview.Connections[0].LastSuccessAt.After(lastSuccess) {
		t.Fatal("successful empty collection lost current facts or operational freshness", err)
	}
}
