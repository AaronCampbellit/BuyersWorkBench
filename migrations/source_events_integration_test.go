package migrations

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSourceEventsUpgradePreservesPopulatedVersionEight(t *testing.T) { testCollectionUpgrade(t, 8) }
func TestConnectionsUpgradePreservesPopulatedVersionNine(t *testing.T)   { testCollectionUpgrade(t, 9) }

func testCollectionUpgrade(t *testing.T, fromVersion int64) {
	adminURL, appURL := os.Getenv("BWB_TEST_DATABASE_URL"), os.Getenv("BWB_TEST_APP_DATABASE_URL")
	if adminURL == "" || appURL == "" {
		t.Skip("explicit disposable database URLs required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	controller, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatal("connect disposable migration database", err)
	}
	defer controller.Close(context.Background())
	var suffix string
	if err = controller.QueryRow(ctx, "SELECT replace(gen_random_uuid()::text,'-','')").Scan(&suffix); err != nil {
		t.Fatal(err)
	}
	schema := "source_event_upgrade_" + suffix
	quotedSchema := pgx.Identifier{schema}.Sanitize()
	if _, err = controller.Exec(ctx, "CREATE SCHEMA "+quotedSchema); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := controller.Exec(cleanup, "DROP SCHEMA "+quotedSchema+" CASCADE"); err != nil {
			t.Error(err)
		}
	}()
	if _, err = controller.Exec(ctx, "GRANT USAGE ON SCHEMA "+quotedSchema+" TO bwb_app"); err != nil {
		t.Fatal(err)
	}
	open := func(url string) *pgxpool.Pool {
		t.Helper()
		cfg, err := pgxpool.ParseConfig(url)
		if err != nil {
			t.Fatal("parse disposable database URL")
		}
		cfg.ConnConfig.RuntimeParams["search_path"] = schema
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		return pool
	}
	admin, app := open(adminURL), open(appURL)
	defer admin.Close()
	defer app.Close()
	if _, err = admin.Exec(ctx, `CREATE TABLE bwb_schema_migrations (
 version bigint PRIMARY KEY,name text NOT NULL,checksum text NOT NULL,applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		t.Fatal(err)
	}
	wanted, err := embedded()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range wanted {
		if m.version > fromVersion {
			break
		}
		tx, err := admin.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = tx.Exec(ctx, m.sql); err == nil {
			_, err = tx.Exec(ctx, "INSERT INTO bwb_schema_migrations(version,name,checksum) VALUES($1,$2,$3)", m.version, m.name, m.checksum)
		}
		if err != nil {
			tx.Rollback(ctx)
			t.Fatal("construct version-eight schema", m.name, err)
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
	const actor = "10000000-0000-4000-8000-000000000001"
	if _, err = admin.Exec(ctx, `
 INSERT INTO users(id,display_name,is_owner) VALUES('10000000-0000-4000-8000-000000000001','Migration fixture',true);
 INSERT INTO workspaces(id,owner_user_id,kind,name,seed_version,business_date)
 VALUES('10000000-0000-4000-8000-000000000002','10000000-0000-4000-8000-000000000001','reviewer','Migration fixture','seed-1.0','2026-10-19');
 INSERT INTO memberships(workspace_id,user_id,role)
 VALUES('10000000-0000-4000-8000-000000000002','10000000-0000-4000-8000-000000000001','owner');
 INSERT INTO wb_connections(id,workspace_id,source_records)
 VALUES('10000000-0000-4000-8000-000000000003','10000000-0000-4000-8000-000000000002','[]');
 INSERT INTO wb_runs(id,workspace_id,generation,connection_id,actor_user_id,fence,state)
 VALUES('10000000-0000-4000-8000-000000000004','10000000-0000-4000-8000-000000000002',1,'10000000-0000-4000-8000-000000000003','10000000-0000-4000-8000-000000000001',1,'succeeded');
 INSERT INTO wb_orders(id,workspace_id,generation,source_key,record,first_record,first_observed_at,last_observed_at)
 VALUES('10000000-0000-4000-8000-000000000005','10000000-0000-4000-8000-000000000002',1,'migration-line','{"status":"partially_shipped"}','{"status":"open"}','2026-10-16T12:00:00Z','2026-10-16T12:30:00Z');
 INSERT INTO wb_observations(workspace_id,generation,order_id,run_id,record,observed_at)
 VALUES('10000000-0000-4000-8000-000000000002',1,'10000000-0000-4000-8000-000000000005','10000000-0000-4000-8000-000000000004','{"status":"partially_shipped"}','2026-10-16T12:30:00Z');
 INSERT INTO wb_changes(id,workspace_id,generation,order_id,run_id,field,data,detected_at)
 VALUES('10000000-0000-4000-8000-000000000006','10000000-0000-4000-8000-000000000002',1,'10000000-0000-4000-8000-000000000005','10000000-0000-4000-8000-000000000004','status','{"field":"status","before":"open","after":"partially_shipped","semantics":"source_status"}','2026-10-16T12:30:01Z');
 INSERT INTO wb_conditions(id,workspace_id,generation,order_id,rule,data,condition_active,severity)
 VALUES('10000000-0000-4000-8000-000000000007','10000000-0000-4000-8000-000000000002',1,'10000000-0000-4000-8000-000000000005','R04','{"rule":"R04"}',true,'medium');
 INSERT INTO wb_notifications(id,workspace_id,generation,condition_id,recipient_id,event_key,message,created_at,read_at)
 VALUES('10000000-0000-4000-8000-000000000008','10000000-0000-4000-8000-000000000002',1,'10000000-0000-4000-8000-000000000007','10000000-0000-4000-8000-000000000001','condition:legacy:1','Existing buyer notice','2026-10-16T12:30:02Z','2026-10-16T12:35:00Z');
 `); err != nil {
		t.Fatal("populate legacy source history", err)
	}
	if _, err = admin.Exec(ctx, `INSERT INTO wb_requirements(workspace_id,generation,source_key,data) VALUES('10000000-0000-4000-8000-000000000002',1,'migration-line','{"internal_po":"Existing requirement"}')`); err != nil {
		t.Fatal(err)
	}
	const snapshotSQL = `SELECT jsonb_build_object(
 'change',(SELECT to_jsonb(e)-'event_type'-'schema_version' FROM wb_changes e),
 'notice',(SELECT to_jsonb(n)-'source_event_id' FROM wb_notifications n),
 'order',(SELECT to_jsonb(o)-'connection_id' FROM wb_orders o),
 'observation',(SELECT to_jsonb(o) FROM wb_observations o),
 'requirement',(SELECT to_jsonb(r)-'connection_id' FROM wb_requirements r),
 'connection',(SELECT to_jsonb(c)-'name'-'supplier_id'-'method'-'revision'-'paused'-'minimum_cadence_seconds'-'is_demo_default'-'buyer_user_id' FROM wb_connections c))::text`
	var before, after string
	if err = admin.QueryRow(ctx, snapshotSQL).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err = Apply(ctx, admin); err != nil {
		t.Fatal("upgrade populated schema", err)
	}
	if err = Apply(ctx, admin); err != nil {
		t.Fatal("repeat upgraded migration", err)
	}
	if err = Check(ctx, admin); err != nil {
		t.Fatal("upgraded migration ledger", err)
	}
	if err = admin.QueryRow(ctx, snapshotSQL).Scan(&after); err != nil || before != after {
		t.Fatal("upgrade altered existing IDs, facts, history or buyer notices", err)
	}
	var mapped bool
	if err = admin.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM wb_orders o JOIN wb_requirements r USING(workspace_id,generation,source_key,connection_id) JOIN wb_connections c ON c.id=o.connection_id AND c.workspace_id=o.workspace_id WHERE c.is_demo_default AND c.method='fictional' AND c.minimum_cadence_seconds=30 AND c.revision=1)`).Scan(&mapped); err != nil || !mapped {
		t.Fatal("legacy connection mapping or requirements lost", err)
	}
	var validated bool
	if err = admin.QueryRow(ctx, `SELECT count(*)=2 AND bool_and(convalidated) FROM pg_constraint
 WHERE conrelid='wb_notifications'::regclass AND conname IN('wb_notifications_source_event','wb_notifications_one_subject')`).Scan(&validated); err != nil || !validated {
		t.Fatal("new notification constraints were not validated", err)
	}
	tx, err := app.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT set_config('bwb.actor_id',$1,true)", actor); err != nil {
		t.Fatal(err)
	}
	var eventType, beforeStatus, afterStatus string
	var version, notices, sourceNotices int
	if err = tx.QueryRow(ctx, "SELECT event_type,schema_version,data->>'before',data->>'after' FROM wb_changes").Scan(&eventType, &version, &beforeStatus, &afterStatus); err != nil || eventType != "order.status.changed" || version != 1 || beforeStatus != "open" || afterStatus != "partially_shipped" {
		t.Fatal("runtime could not read typed legacy history", eventType, version, beforeStatus, afterStatus, err)
	}
	if err = tx.QueryRow(ctx, "SELECT count(*),count(source_event_id) FROM wb_notifications").Scan(&notices, &sourceNotices); err != nil || notices != 1 || sourceNotices != 0 {
		t.Fatal("upgrade created retroactive source notifications", notices, sourceNotices, err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{"UPDATE wb_changes SET data=data", "DELETE FROM wb_changes"} {
		_, err = app.Exec(ctx, statement)
		var denied *pgconn.PgError
		if !errors.As(err, &denied) || denied.Code != "42501" {
			t.Fatal("upgrade did not revoke runtime event mutations", statement, err)
		}
	}
}
