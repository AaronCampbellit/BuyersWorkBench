package workbench

import (
	"sync"
	"testing"
)

func sourceEventArgs(t *testing.T, f fixture, run Run) CollectionArgs {
	t.Helper()
	args := CollectionArgs{SchemaVersion: 1, RunID: run.ID, ConnectionID: run.ConnectionID, WorkspaceID: f.w, Generation: run.Generation, ActorUserID: f.actor}
	if err := f.admin.QueryRow(f.ctx, "SELECT fence FROM wb_runs WHERE id=$1", run.ID).Scan(&args.Fence); err != nil {
		t.Fatal(err)
	}
	return args
}

func sourceEventCounts(t *testing.T, f fixture) (events, notices int) {
	t.Helper()
	if err := f.admin.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM wb_changes WHERE field='status'),
 (SELECT count(*) FROM wb_notifications WHERE source_event_id IS NOT NULL)`).Scan(&events, &notices); err != nil {
		t.Fatal(err)
	}
	return
}

func TestSourceEventsStatusNoticeAndReplay(t *testing.T) {
	f := testFoundation(t)
	baseline, err := f.service.Seed(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, baseline)
	f.scenario(t, "repeat")
	if events, notices := sourceEventCounts(t, f); events != 0 || notices != 0 {
		t.Fatal("baseline or unchanged collection invented a status event", events, notices)
	}
	partial := f.scenario(t, "partial")
	if events, notices := sourceEventCounts(t, f); events != 1 || notices != 1 {
		t.Fatal("status change did not produce exactly one event and notice", events, notices)
	}
	overview, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil {
		t.Fatal(err)
	}
	var notice Notification
	for _, n := range overview.Notifications {
		if n.SourceEventID != "" {
			notice = n
		} else if n.ExceptionID == "" || n.OrderID == "" {
			t.Fatal("existing condition notification lost its subject", n)
		}
	}
	if notice.SourceEventID == "" || notice.ExceptionID != "" || notice.OrderID == "" || notice.Message != "Order status changed from open to partially shipped." {
		t.Fatal("status notice lacks a distinct source/order subject", notice)
	}
	detail, err := f.service.Order(f.ctx, f.actor, f.w, notice.OrderID)
	if err != nil || detail.Order.Status != "partially_shipped" || detail.Order.SourceKey != "SRC-MERIDIAN-10503:1" {
		t.Fatal("notice points to the wrong source order", detail.Order, err)
	}
	found := false
	for _, event := range detail.History {
		if event.ID == notice.SourceEventID {
			found = true
			if event.EventType != "order.status.changed" || event.SchemaVersion != 1 || event.Before != "open" || event.After != "partially_shipped" || event.RunID != partial.ID || event.ConnectionID != partial.ConnectionID || event.ObservationID == "" || event.DetectedAt.IsZero() {
				t.Fatal("typed event lost change or ingestion lineage", event)
			}
		}
	}
	if !found {
		t.Fatal("source notice does not link to accepted history")
	}
	if err = f.service.MarkNotificationRead(f.ctx, f.actor, f.w, notice.ID, 1); err != nil {
		t.Fatal("status notice could not be marked read", err)
	}

	// Concurrent retries of the already accepted run and a fresh identical run
	// must not produce another event or recipient notice.
	args := sourceEventArgs(t, f, partial)
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for range 2 {
		wg.Go(func() { errs <- f.service.Collect(f.ctx, args) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal("accepted-run replay failed", err)
		}
	}
	f.scenario(t, "repeat")
	if events, notices := sourceEventCounts(t, f); events != 1 || notices != 1 {
		t.Fatal("unchanged/replayed collection duplicated source effects", events, notices)
	}
	f.scenario(t, "baseline")
	f.scenario(t, "partial")
	if events, notices := sourceEventCounts(t, f); events != 3 || notices != 3 {
		t.Fatal("A-to-B-to-A-to-B lost distinct transitions", events, notices)
	}
}

func TestSourceEventsNotificationFailureRollsBackPublication(t *testing.T) {
	f := testFoundation(t)
	baseline, err := f.service.Seed(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, baseline)
	if _, err = f.admin.Exec(f.ctx, `CREATE FUNCTION reject_source_notice() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN IF NEW.source_event_id IS NOT NULL THEN RAISE EXCEPTION 'test source notification rejected'; END IF; RETURN NEW; END $$;
 CREATE TRIGGER reject_source_notice BEFORE INSERT ON wb_notifications FOR EACH ROW EXECUTE FUNCTION reject_source_notice()`); err != nil {
		t.Fatal(err)
	}
	run, err := f.service.Scenario(f.ctx, f.actor, f.w, 1, "partial", "source-notification-rollback")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.Collect(f.ctx, sourceEventArgs(t, f, run)); err == nil {
		t.Fatal("notification insert failure was ignored")
	}
	var events, observations, accepted int
	if err = f.admin.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM wb_changes),
 (SELECT count(*) FROM wb_observations),(SELECT count(*) FROM wb_orders WHERE record->>'status'='partially_shipped')`).Scan(&events, &observations, &accepted); err != nil || events != 0 || observations != 4 || accepted != 0 {
		t.Fatal("failed notice committed partial source state", events, observations, accepted, err)
	}
	if _, err = f.admin.Exec(f.ctx, "DROP TRIGGER reject_source_notice ON wb_notifications"); err != nil {
		t.Fatal(err)
	}
	f.collect(t, run)
	if events, notices := sourceEventCounts(t, f); events != 1 || notices != 1 {
		t.Fatal("retry failed to publish exactly one source event and notice", events, notices)
	}
}

func TestSourceEventsRLSAppendOnlyAndReset(t *testing.T) {
	f := testFoundation(t)
	baseline, err := f.service.Seed(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, baseline)
	run := f.scenario(t, "partial")
	checkVisible := func(actor string, want int) {
		t.Helper()
		tx, err := f.app.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(f.ctx)
		if _, err = tx.Exec(f.ctx, "SELECT set_config('bwb.actor_id',$1,true)", actor); err != nil {
			t.Fatal(err)
		}
		var events, notices int
		if err = tx.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM wb_changes WHERE workspace_id=$1 AND field='status'),
 (SELECT count(*) FROM wb_notifications WHERE workspace_id=$1 AND source_event_id IS NOT NULL)`, f.w).Scan(&events, &notices); err != nil || events != want || notices != want {
			t.Fatal("source event/notice escaped workspace or generation RLS", events, notices, want, err)
		}
	}
	checkVisible(f.actor, 1)
	checkVisible(f.otherActor, 0)
	for _, statement := range []string{"UPDATE wb_changes SET data=data", "DELETE FROM wb_changes"} {
		if _, err = f.app.Exec(f.ctx, statement); err == nil {
			t.Fatal("runtime role can mutate accepted source events", statement)
		}
	}
	if _, err = f.service.Reset(f.ctx, f.actor, f.w, 1, "source-events-reset"); err != nil {
		t.Fatal(err)
	}
	checkVisible(f.actor, 0)
	if events, notices := sourceEventCounts(t, f); events != 1 || notices != 1 {
		t.Fatal("reset destroyed historical source events", events, notices)
	}
	tx, err := f.app.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = tx.Exec(f.ctx, "SELECT set_config('bwb.actor_id',$1,true)", f.actor); err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(f.ctx, `INSERT INTO wb_changes(workspace_id,generation,order_id,run_id,field,data)
 SELECT $1,1,$2,$3,'status','{}'::jsonb`, f.w, sourceStatusOrderID(t, f), run.ID)
	if err == nil {
		t.Fatal("runtime role appended a stale-generation source event")
	}
}

func sourceStatusOrderID(t *testing.T, f fixture) string {
	t.Helper()
	var id string
	if err := f.admin.QueryRow(f.ctx, "SELECT order_id::text FROM wb_changes WHERE field='status'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSourceEventsOnlyNotifyActiveMembers(t *testing.T) {
	f := testFoundation(t)
	baseline, err := f.service.Seed(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, baseline)
	var active string
	for _, state := range []string{"active", "expired", "disabled", "inactive"} {
		var id string
		if err = f.admin.QueryRow(f.ctx, `INSERT INTO users(id,display_name,is_owner,active,expires_at,disabled_at)
 VALUES(gen_random_uuid(),$1,false,$1<>'inactive',CASE WHEN $1='expired' THEN now()-interval '1 second' END,
 CASE WHEN $1 IN('disabled','inactive') THEN now() END) RETURNING id::text`, state).Scan(&id); err != nil {
			t.Fatal(err)
		}
		if _, err = f.admin.Exec(f.ctx, "INSERT INTO memberships(workspace_id,user_id,role) VALUES($1,$2,'reviewer')", f.w, id); err != nil {
			t.Fatal(err)
		}
		if state == "active" {
			active = id
		}
	}
	f.scenario(t, "partial")
	if events, notices := sourceEventCounts(t, f); events != 1 || notices != 2 {
		t.Fatal("status notices did not follow active workspace membership", events, notices)
	}
	tx, err := f.app.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = tx.Exec(f.ctx, "SELECT set_config('bwb.actor_id',$1,true)", active); err != nil {
		t.Fatal(err)
	}
	var recipient string
	if err = tx.QueryRow(f.ctx, "SELECT recipient_id::text FROM wb_notifications WHERE source_event_id IS NOT NULL").Scan(&recipient); err != nil || recipient != active {
		t.Fatal("source notification reads were not recipient scoped", recipient, err)
	}
	var count int
	if err = tx.QueryRow(f.ctx, "SELECT count(*) FROM wb_notifications WHERE source_event_id IS NOT NULL").Scan(&count); err != nil || count != 1 {
		t.Fatal("member read another recipient's source notification", count, err)
	}
	if _, err = tx.Exec(f.ctx, `INSERT INTO wb_notifications(workspace_id,generation,recipient_id,event_key,message)
 VALUES($1,1,$2,'missing-subject','invalid')`, f.w, active); err == nil {
		t.Fatal("notification without exactly one subject was accepted")
	}
}
