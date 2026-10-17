package retention

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"buyersworkbench/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type fixture struct {
	ctx                                     context.Context
	admin, app                              *pgxpool.Pool
	actor, w, otherActor, otherW, c, otherC string
	now                                     time.Time
}

func foundation(t *testing.T) fixture {
	t.Helper()
	adminURL, appURL := os.Getenv("BWB_TEST_DATABASE_URL"), os.Getenv("BWB_TEST_APP_DATABASE_URL")
	if adminURL == "" || appURL == "" {
		t.Skip("explicit disposable database URLs required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	t.Cleanup(cancel)
	controller, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatal("connect disposable test controller")
	}
	var suffix string
	if err = controller.QueryRow(ctx, "SELECT replace(gen_random_uuid()::text,'-','')").Scan(&suffix); err != nil {
		t.Fatal(err)
	}
	schema := "retention_test_" + suffix
	if _, err = controller.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	if _, err = controller.Exec(ctx, "GRANT USAGE ON SCHEMA "+pgx.Identifier{schema}.Sanitize()+" TO bwb_app"); err != nil {
		t.Fatal(err)
	}
	open := func(url string) *pgxpool.Pool {
		cfg, err := pgxpool.ParseConfig(url)
		if err != nil {
			t.Fatal("parse disposable URL")
		}
		cfg.ConnConfig.RuntimeParams["search_path"] = schema
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			t.Fatal("open disposable pool")
		}
		return pool
	}
	f := fixture{ctx: ctx, admin: open(adminURL), app: open(appURL), now: time.Now().UTC().Truncate(time.Second)}
	t.Cleanup(func() {
		f.admin.Close()
		f.app.Close()
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := controller.Exec(cleanup, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error(err)
		}
		controller.Close(cleanup)
	})
	if err = migrations.Apply(ctx, f.admin); err != nil {
		t.Fatal(err)
	}
	for i := range 2 {
		var actor, w, c string
		if err = f.admin.QueryRow(ctx, "INSERT INTO users(id,display_name,is_owner) VALUES(gen_random_uuid(),'Retention fixture',$1) RETURNING id::text", i == 0).Scan(&actor); err != nil {
			t.Fatal(err)
		}
		kind, role := "presentation", "owner"
		if i == 1 {
			kind, role = "reviewer", "reviewer"
		}
		if err = f.admin.QueryRow(ctx, "INSERT INTO workspaces(id,kind,owner_user_id,name,seed_version,business_date) VALUES(gen_random_uuid(),$1,$2,'Retention fixture','seed-1.0','2026-10-19') RETURNING id::text", kind, actor).Scan(&w); err != nil {
			t.Fatal(err)
		}
		if _, err = f.admin.Exec(ctx, "INSERT INTO memberships(workspace_id,user_id,role) VALUES($1,$2,$3)", w, actor, role); err != nil {
			t.Fatal(err)
		}
		if err = f.admin.QueryRow(ctx, "INSERT INTO wb_connections(workspace_id,source_records) VALUES($1,'[]') RETURNING id::text", w).Scan(&c); err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			f.actor, f.w, f.c = actor, w, c
		} else {
			f.otherActor, f.otherW, f.otherC = actor, w, c
		}
	}
	return f
}

func (f fixture) run(t *testing.T, w, c, actor, state string, finished *time.Time, staged bool) string {
	t.Helper()
	var id string
	var payload any
	if staged {
		payload = json.RawMessage(`{"test_payload":"invented-retention-fixture"}`)
	}
	if err := f.admin.QueryRow(f.ctx, `INSERT INTO wb_runs(workspace_id,generation,connection_id,actor_user_id,fence,state,finished_at,staged_batch)
 VALUES($1,1,$2,$3,1,$4,$5,$6) RETURNING id::text`, w, c, actor, state, finished, payload).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f fixture) order(t *testing.T, w, c string, first, last time.Time) string {
	t.Helper()
	var id string
	if err := f.admin.QueryRow(f.ctx, `INSERT INTO wb_orders(workspace_id,generation,connection_id,source_key,record,first_record,first_observed_at,last_observed_at)
 VALUES($1,1,$2,'retention-order','{"status":"processing","description":"A"}','{"status":"processing","description":"A"}',$3,$4) RETURNING id::text`, w, c, first, last).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func (f fixture) observation(t *testing.T, w, order, run, record string, observed time.Time) string {
	t.Helper()
	var id string
	if err := f.admin.QueryRow(f.ctx, "INSERT INTO wb_observations(workspace_id,generation,order_id,run_id,record,observed_at) VALUES($1,1,$2,$3,$4::jsonb,$5) RETURNING id::text", w, order, run, record, observed).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPlanApplyPreservesEvidenceAndOtherWorkspaces(t *testing.T) {
	f := foundation(t)
	old := func(days int) time.Time { return f.now.Add(-time.Duration(days) * 24 * time.Hour) }
	order := f.order(t, f.w, f.c, old(120), old(1))
	days := []int{120, 110, 100, 90, 80, 70, 60, 50, 5, 1}
	ids, runs := []string{}, []string{}
	for i, days := range days {
		observed := old(days)
		finished := observed.Add(time.Minute)
		state := "succeeded"
		if i == 6 {
			state = "running"
		}
		if i == 7 {
			finished = f.now.Add(-time.Hour)
		}
		run := f.run(t, f.w, f.c, f.actor, state, &finished, i == 1 || i == 6 || i == 7)
		record := `{"status":"processing","description":"A"}`
		if i >= 2 {
			record = `{"status":"shipped","description":"A"}`
		}
		if i == 3 || i == 4 {
			record = `{"status":"shipped","description":"B"}`
		}
		ids = append(ids, f.observation(t, f.w, order, run, record, observed))
		runs = append(runs, run)
	}
	if _, err := f.admin.Exec(f.ctx, "UPDATE wb_connections SET active_run_id=$2,lease_expires_at=now()+interval '1 hour' WHERE id=$1", f.c, runs[6]); err != nil {
		t.Fatal(err)
	}
	var event string
	if err := f.admin.QueryRow(f.ctx, `INSERT INTO wb_changes(workspace_id,generation,order_id,run_id,field,data)
 VALUES($1,1,$2,$3,'status','{"field":"status","before":"processing","after":"shipped"}') RETURNING id::text`, f.w, order, runs[2]).Scan(&event); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(f.ctx, `INSERT INTO wb_notifications(workspace_id,generation,source_event_id,recipient_id,event_key,message)
 VALUES($1,1,$2,$3,'retention-fixture-event','An invented source event')`, f.w, event, f.actor); err != nil {
		t.Fatal(err)
	}
	var condition string
	if err := f.admin.QueryRow(f.ctx, `INSERT INTO wb_conditions(workspace_id,generation,order_id,rule,data,condition_active,severity)
 VALUES($1,1,$2,'R01','{}',false,'medium') RETURNING id::text`, f.w, order).Scan(&condition); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(f.ctx, "INSERT INTO wb_notes(workspace_id,generation,condition_id,author_id,body) VALUES($1,1,$2,$3,'Retain this buyer note')", f.w, condition, f.actor); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(f.ctx, `INSERT INTO wb_operations(workspace_id,actor_id,generation,operation,idempotency_key,payload_hash,result)
 VALUES($1,$2,1,'fixture','retained-operation','fixture','{}')`, f.w, f.actor); err != nil {
		t.Fatal(err)
	}
	finished := old(75)
	retired := f.run(t, f.w, f.c, f.actor, "rejected", &finished, true)
	queued := f.run(t, f.w, f.c, f.actor, "queued", nil, true)
	foreignRun := f.run(t, f.otherW, f.otherC, f.otherActor, "rejected", &finished, true)
	foreignOrder := f.order(t, f.otherW, f.otherC, old(120), old(60))
	for _, days := range []int{120, 90, 60} {
		observed := old(days)
		run := f.run(t, f.otherW, f.otherC, f.otherActor, "succeeded", &observed, false)
		f.observation(t, f.otherW, foreignOrder, run, `{"status":"processing","description":"A"}`, observed)
	}
	req := Request{WorkspaceID: f.w, Before: old(30), Limit: 1000}
	var before string
	if err := f.admin.QueryRow(f.ctx, `SELECT jsonb_build_object('runs',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM wb_runs r),'observations',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM wb_observations o),'audits',(SELECT count(*) FROM audit_events))::text`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		plan, err := Plan(f.ctx, f.admin, req)
		if err != nil || plan.Mode != "dry_run" || plan.StagedBatches != 2 || plan.Observations != 2 || plan.MoreEligible {
			t.Fatalf("incorrect plan counts: %+v, %v", plan, err)
		}
	}
	var after string
	if err := f.admin.QueryRow(f.ctx, `SELECT jsonb_build_object('runs',(SELECT jsonb_agg(to_jsonb(r) ORDER BY id) FROM wb_runs r),'observations',(SELECT jsonb_agg(to_jsonb(o) ORDER BY id) FROM wb_observations o),'audits',(SELECT count(*) FROM audit_events))::text`).Scan(&after); err != nil || before != after {
		t.Fatal("dry run changed persisted state", err)
	}
	result, err := Apply(f.ctx, f.admin, req)
	if err != nil || result.StagedBatches != 2 || result.Observations != 2 || result.AuditID == "" {
		t.Fatalf("incorrect apply: %+v, %v", result, err)
	}
	for i, id := range ids {
		var exists bool
		if err = f.admin.QueryRow(f.ctx, "SELECT EXISTS(SELECT 1 FROM wb_observations WHERE id=$1)", id).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists == (i == 1 || i == 4) {
			t.Fatalf("wrong observation retained/deleted at index %d", i)
		}
	}
	for _, run := range []string{runs[6], runs[7], queued, foreignRun} {
		var retained bool
		if err = f.admin.QueryRow(f.ctx, "SELECT staged_batch IS NOT NULL FROM wb_runs WHERE id=$1", run).Scan(&retained); err != nil || !retained {
			t.Fatal("active/recent/foreign payload was cleared", err)
		}
	}
	for _, run := range []string{runs[1], retired} {
		var cleared bool
		if err = f.admin.QueryRow(f.ctx, "SELECT staged_batch IS NULL FROM wb_runs WHERE id=$1", run).Scan(&cleared); err != nil || !cleared {
			t.Fatal("eligible payload was retained", err)
		}
	}
	var protected bool
	if err = f.admin.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM wb_changes)=1 AND (SELECT count(*) FROM wb_notifications)=1
 AND (SELECT count(*) FROM wb_notes)=1 AND (SELECT count(*) FROM wb_conditions)=1 AND (SELECT count(*) FROM wb_operations)=1
 AND (SELECT count(*) FROM wb_observations WHERE workspace_id=$1)=3
 AND (SELECT count(*) FROM wb_runs)=16 AND (SELECT count(*) FROM wb_orders)=2`, f.otherW).Scan(&protected); err != nil || !protected {
		t.Fatal("retention changed protected records", err)
	}
	var auditMatches bool
	if err = f.admin.QueryRow(f.ctx, `SELECT action='retention.apply' AND target_id=$2 AND actor_user_id IS NULL AND outcome='success'
 AND (metadata->>'staged_batches')::int=2 AND (metadata->>'observations')::int=2 AND (metadata->>'before')::timestamptz=$3
 AND metadata->>'database_role'=current_user FROM audit_events WHERE id=$1`, result.AuditID, f.w, req.Before).Scan(&auditMatches); err != nil || !auditMatches {
		t.Fatal("retention audit omitted accounting", err)
	}
	if next, err := Apply(f.ctx, f.admin, req); err != nil || next.StagedBatches != 0 || next.Observations != 0 {
		t.Fatal("repeat apply changed additional protected evidence", err)
	}
}

func TestConcurrentBoundedApplyAndRuntimeDenial(t *testing.T) {
	f := foundation(t)
	old := func(days int) time.Time { return f.now.Add(-time.Duration(days) * 24 * time.Hour) }
	order := f.order(t, f.w, f.c, old(100), old(50))
	for _, days := range []int{100, 90, 80, 70, 50} {
		observed := old(days)
		run := f.run(t, f.w, f.c, f.actor, "succeeded", &observed, false)
		f.observation(t, f.w, order, run, `{"status":"processing","description":"A"}`, observed)
	}
	req := Request{WorkspaceID: f.w, Before: old(30), Limit: 2}
	plan, err := Plan(f.ctx, f.admin, req)
	if err != nil || plan.Observations != 2 || !plan.MoreEligible {
		t.Fatal("batch cap not reported", err)
	}
	lock, err := f.admin.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = lock.Exec(f.ctx, "SELECT id FROM workspaces WHERE id=$1 FOR UPDATE", f.w); err != nil {
		t.Fatal(err)
	}
	blocked, cancel := context.WithTimeout(f.ctx, 100*time.Millisecond)
	_, err = Apply(blocked, f.admin, req)
	cancel()
	if err == nil {
		t.Fatal("apply ignored workspace publication lock")
	}
	if err = lock.Rollback(f.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err = f.admin.Exec(f.ctx, `CREATE FUNCTION retention_audit_failure() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NEW.action='retention.apply' THEN RAISE EXCEPTION 'injected retention audit failure'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER retention_audit_failure BEFORE INSERT ON audit_events FOR EACH ROW EXECUTE FUNCTION retention_audit_failure()`); err != nil {
		t.Fatal(err)
	}
	if _, err = Apply(f.ctx, f.admin, req); err == nil {
		t.Fatal("audit failure permitted an unaudited cleanup")
	}
	var unchanged bool
	if err = f.admin.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM wb_observations)=5 AND (SELECT count(*) FROM audit_events)=0").Scan(&unchanged); err != nil || !unchanged {
		t.Fatal("audit failure did not roll back cleanup", err)
	}
	if _, err = f.admin.Exec(f.ctx, "DROP TRIGGER retention_audit_failure ON audit_events; DROP FUNCTION retention_audit_failure()"); err != nil {
		t.Fatal(err)
	}
	results := make(chan Result, 2)
	failures := make(chan error, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			result, err := Apply(f.ctx, f.admin, req)
			if err != nil {
				failures <- err
			} else {
				results <- result
			}
		})
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	total := 0
	for result := range results {
		if result.Observations > 2 || result.AuditID == "" {
			t.Fatal("unbounded or unaudited apply")
		}
		total += result.Observations
	}
	if total != 3 {
		t.Fatal("concurrent apply repeated or lost candidates")
	}
	var count int
	if err = f.admin.QueryRow(f.ctx, "SELECT count(*) FROM wb_observations").Scan(&count); err != nil || count != 2 {
		t.Fatal("first/latest evidence was lost", err)
	}
	if _, err = Plan(f.ctx, f.app, req); !errors.Is(err, ErrAdmin) {
		t.Fatal("runtime role reached maintenance", err)
	}
	if _, err = Apply(f.ctx, f.app, req); !errors.Is(err, ErrAdmin) {
		t.Fatal("runtime role applied maintenance", err)
	}
	for _, statement := range []string{"UPDATE wb_observations SET record=record WHERE workspace_id=$1", "DELETE FROM wb_observations WHERE workspace_id=$1"} {
		tx, err := f.app.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(f.ctx, "SELECT set_config('bwb.actor_id',$1,true)", f.actor); err != nil {
			t.Fatal(err)
		}
		_, err = tx.Exec(f.ctx, statement, f.w)
		tx.Rollback(f.ctx)
		var pgerr *pgconn.PgError
		if !errors.As(err, &pgerr) || pgerr.Code != "42501" {
			t.Fatal("runtime observation mutation was permitted", err)
		}
	}
	if result, err := Apply(f.ctx, f.admin, req); err != nil || result.Observations != 0 {
		t.Fatal("bounded repeat changed endpoints", err)
	}
	missing := req
	missing.WorkspaceID = strings.Replace(testWorkspace, "2ae65e15", "2ae65e16", 1)
	if _, err = Plan(f.ctx, f.admin, missing); !errors.Is(err, ErrNotFound) {
		t.Fatal("nonexistent workspace accepted", err)
	}
}
