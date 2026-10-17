package workbench

import (
	"errors"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestNotificationPreferencesDefaultsScopeRevisionAndReset(t *testing.T) {
	f := testFoundation(t)
	p, err := f.service.GetNotificationPreferences(f.ctx, f.actor, f.w)
	if err != nil || p.Revision != 0 || !p.StatusChanges || !p.SupplierConditions {
		t.Fatal("new members must receive default future notices", p, err)
	}
	input := NotificationPreferencesInput{StatusChanges: false, SupplierConditions: true}
	p, err = f.service.SetNotificationPreferences(f.ctx, f.actor, f.w, 1, 0, input)
	if err != nil || p.Revision != 1 || p.NotificationPreferencesInput != input {
		t.Fatal("preference edit was not persisted", p, err)
	}
	replayed, err := f.service.SetNotificationPreferences(f.ctx, f.actor, f.w, 1, 0, input)
	if err != nil || replayed != p {
		t.Fatal("lost-response retry did not return its accepted revision", replayed, err)
	}
	if _, err = f.service.SetNotificationPreferences(f.ctx, f.actor, f.w, 1, 0, NotificationPreferencesInput{true, false}); !errors.Is(err, ErrStale) {
		t.Fatal("stale tab overwrote preferences", err)
	}
	if _, err = f.service.SetNotificationPreferences(f.ctx, f.actor, f.w, 2, p.Revision, input); !errors.Is(err, ErrStale) {
		t.Fatal("stale generation was accepted", err)
	}
	if _, err = f.service.GetNotificationPreferences(f.ctx, f.otherActor, f.w); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign member read workspace preferences", err)
	}
	for _, actor := range []string{f.actor, f.otherActor} {
		workspace := f.w
		if actor != f.actor {
			workspace = f.otherW
		}
		tx, _, err := f.service.begin(f.ctx, actor, workspace, 1, true)
		if err != nil {
			t.Fatal(err)
		}
		var count int
		err = tx.QueryRow(f.ctx, "SELECT count(*) FROM wb_notification_preferences WHERE workspace_id=$1", f.w).Scan(&count)
		want := 0
		if actor == f.actor {
			want = 1
		}
		if err != nil || count != want {
			t.Fatal("preference rows escaped actor/workspace RLS", count, want, err)
		}
		if actor == f.actor {
			_, err = tx.Exec(f.ctx, `INSERT INTO wb_notification_preferences(workspace_id,user_id) VALUES($1,$2)`, f.otherW, f.otherActor)
			if err == nil {
				t.Fatal("runtime actor inserted foreign preferences")
			}
		}
		tx.Rollback(f.ctx)
	}
	run, err := f.service.Seed(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, run)
	if _, err = f.service.Reset(f.ctx, f.actor, f.w, 1, "preferences-survive-reset"); err != nil {
		t.Fatal(err)
	}
	after, err := f.service.GetNotificationPreferences(f.ctx, f.actor, f.w)
	if err != nil || after != p {
		t.Fatal("demo reset discarded member preferences", after, p, err)
	}
}

func TestNotificationPreferencesSuppressFutureNoticesWithoutHistoryReplay(t *testing.T) {
	f := testFoundation(t)
	p, err := f.service.SetNotificationPreferences(f.ctx, f.actor, f.w, 1, 0, NotificationPreferencesInput{false, false})
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := f.service.Seed(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, baseline)
	f.scenario(t, "partial")
	var notices, conditions int
	if err = f.admin.QueryRow(f.ctx, "SELECT (SELECT count(*) FROM wb_notifications),(SELECT count(*) FROM wb_conditions)").Scan(&notices, &conditions); err != nil || notices != 0 || conditions == 0 {
		t.Fatal("opt-out changed conditions or inserted notices", notices, conditions, err)
	}
	if events, sourceNotices := sourceEventCounts(t, f); events != 1 || sourceNotices != 0 {
		t.Fatal("status opt-out discarded accepted history", events, sourceNotices)
	}
	if _, err = f.service.SetNotificationPreferences(f.ctx, f.actor, f.w, 1, p.Revision, NotificationPreferencesInput{true, true}); err != nil {
		t.Fatal(err)
	}
	f.scenario(t, "repeat")
	if events, sourceNotices := sourceEventCounts(t, f); events != 1 || sourceNotices != 0 {
		t.Fatal("opting in replayed an old source transition", events, sourceNotices)
	}
	f.scenario(t, "baseline")
	if events, sourceNotices := sourceEventCounts(t, f); events != 2 || sourceNotices != 1 {
		t.Fatal("new transition did not honor opt-in", events, sourceNotices)
	}
	p, err = f.service.GetNotificationPreferences(f.ctx, f.actor, f.w)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.SetNotificationPreferences(f.ctx, f.actor, f.w, 1, p.Revision, NotificationPreferencesInput{false, false}); err != nil {
		t.Fatal(err)
	}
	f.scenario(t, "partial")
	if events, sourceNotices := sourceEventCounts(t, f); events != 3 || sourceNotices != 1 {
		t.Fatal("opt-out deleted existing notices or generated new ones", events, sourceNotices)
	}
}

func notificationMember(t *testing.T, f fixture, name string) string {
	t.Helper()
	var id string
	if err := f.admin.QueryRow(f.ctx, "INSERT INTO users(id,display_name) VALUES(gen_random_uuid(),$1) RETURNING id::text", name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(f.ctx, "INSERT INTO memberships(workspace_id,user_id,role) VALUES($1,$2,'reviewer')", f.w, id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestNotificationRoutingAssignmentBuyerFallbackAndEligibility(t *testing.T) {
	f := testFoundation(t)
	run, err := f.service.Seed(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, run)
	assigned := notificationMember(t, f, "Assigned buyer")
	buyer := notificationMember(t, f, "Connection buyer")
	if _, err = f.admin.Exec(f.ctx, "UPDATE wb_connections SET buyer_user_id=$2 WHERE id=$1", run.ConnectionID, buyer); err != nil {
		t.Fatal(err)
	}
	f.scenario(t, "partial")
	var recipient string
	if err = f.admin.QueryRow(f.ctx, "SELECT recipient_id::text FROM wb_notifications WHERE source_event_id IS NOT NULL").Scan(&recipient); err != nil || recipient != buyer {
		t.Fatal("status event did not select the configured buyer", recipient, err)
	}
	var conditionID string
	if err = f.admin.QueryRow(f.ctx, "SELECT id::text FROM wb_conditions LIMIT 1").Scan(&conditionID); err != nil {
		t.Fatal(err)
	}
	if _, err = f.admin.Exec(f.ctx, "UPDATE wb_conditions SET assignee_id=$2 WHERE id=$1", conditionID, assigned); err != nil {
		t.Fatal(err)
	}
	notify := func(key string, want []string) {
		t.Helper()
		tx, _, err := f.service.begin(f.ctx, f.actor, f.w, 1, true)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(f.ctx)
		if err = notifyConditionTx(f.ctx, tx, CollectionArgs{WorkspaceID: f.w, ConnectionID: run.ConnectionID, Generation: 1}, conditionID, key, "Condition requires attention"); err != nil {
			t.Fatal(err)
		}
		// Repeated publication of one event is deduplicated for every recipient.
		if err = notifyConditionTx(f.ctx, tx, CollectionArgs{WorkspaceID: f.w, ConnectionID: run.ConnectionID, Generation: 1}, conditionID, key, "Condition requires attention"); err != nil {
			t.Fatal(err)
		}
		if err = tx.Commit(f.ctx); err != nil {
			t.Fatal(err)
		}
		rows, err := f.admin.Query(f.ctx, "SELECT recipient_id::text FROM wb_notifications WHERE event_key=$1 ORDER BY recipient_id", key)
		if err != nil {
			t.Fatal(err)
		}
		got, err := pgx.CollectRows(rows, pgx.RowTo[string])
		slices.Sort(want)
		if err != nil || !slices.Equal(got, want) {
			t.Fatal("condition recipient routing mismatch", key, got, want, err)
		}
	}
	notify("route-assigned", []string{assigned})
	if _, err = f.service.SetNotificationPreferences(f.ctx, assigned, f.w, 1, 0, NotificationPreferencesInput{true, false}); err != nil {
		t.Fatal(err)
	}
	tx, _, err := f.service.begin(f.ctx, f.actor, f.w, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	var visible, foreignRecipients int
	if err = tx.QueryRow(f.ctx, `SELECT (SELECT count(*) FROM wb_notification_preferences WHERE user_id=$1),
 (SELECT count(*) FROM bwb_notification_recipients($2,NULL,NULL,'status_changes'))`, assigned, f.otherW).Scan(&visible, &foreignRecipients); err != nil || visible != 0 || foreignRecipients != 0 {
		t.Fatal("runtime read another member's preferences or foreign recipients", visible, foreignRecipients, err)
	}
	changed, err := tx.Exec(f.ctx, "UPDATE wb_notification_preferences SET supplier_conditions=true WHERE user_id=$1", assigned)
	if err != nil || changed.RowsAffected() != 0 {
		t.Fatal("runtime updated another member's preferences", err)
	}
	tx.Rollback(f.ctx)
	notify("route-opted-out-assigned", nil)
	if _, err = f.admin.Exec(f.ctx, "UPDATE users SET expires_at=now()-interval '1 second' WHERE id=$1", assigned); err != nil {
		t.Fatal(err)
	}
	notify("route-expired-assigned", []string{buyer})
	if _, err = f.admin.Exec(f.ctx, "UPDATE users SET disabled_at=now() WHERE id=$1", buyer); err != nil {
		t.Fatal(err)
	}
	notify("route-no-active-buyer", []string{f.actor})
	f.scenario(t, "baseline")
	if err = f.admin.QueryRow(f.ctx, `SELECT recipient_id::text FROM wb_notifications WHERE source_event_id IS NOT NULL
 ORDER BY created_at DESC LIMIT 1`).Scan(&recipient); err != nil || recipient != f.actor {
		t.Fatal("expired buyer status notice did not fall back to active members", recipient, err)
	}
	members, err := f.service.WorkspaceMembers(f.ctx, f.actor, f.w)
	if err != nil || len(members) != 1 || members[0].ID != f.actor || members[0].DisplayName == "" {
		t.Fatal("member listing included disabled, expired or foreign identities", members, err)
	}
}
