package workbench

import (
	"errors"
	"fmt"
	"sync"
	"testing"

	"buyersworkbench/internal/domain"
)

func newFixtureConnection(t *testing.T, f fixture, key string) Connection {
	t.Helper()
	c, err := f.service.CreateConnection(f.ctx, f.actor, f.w, 1, key, ConnectionInput{Name: "Fixture feed " + key, Method: "fictional", MinimumCadenceSeconds: 60, CadenceSeconds: 120})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func connectionArgs(t *testing.T, f fixture, run Run) CollectionArgs {
	t.Helper()
	a := CollectionArgs{SchemaVersion: 1, RunID: run.ID, ConnectionID: run.ConnectionID, WorkspaceID: f.w, Generation: run.Generation, ActorUserID: f.actor}
	if err := f.admin.QueryRow(f.ctx, `SELECT fence FROM wb_runs WHERE id=$1`, run.ID).Scan(&a.Fence); err != nil {
		t.Fatal(err)
	}
	return a
}
func TestConnectionsSourceCollisionRequirementsAndReset(t *testing.T) {
	f := testFoundation(t)
	// Creating an empty feed before setup must not replace the fixed demo feed.
	extra := newFixtureConnection(t, f, "extra")
	baseline, err := f.service.Seed(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, baseline)
	o, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil || len(o.Connections) != 2 || len(o.Orders) != 4 || !o.Ready {
		t.Fatal("default seed missing", len(o.Connections), len(o.Orders), err)
	}
	legacy := map[string]string{}
	for _, order := range o.Orders {
		legacy[order.SourceKey] = order.ID
	}
	empty, err := f.service.RefreshConnection(f.ctx, f.actor, f.w, extra.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, empty)
	var count int
	if err = f.admin.QueryRow(f.ctx, `SELECT record_count FROM wb_runs WHERE id=$1`, empty.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("empty feed fabricated activity", count, err)
	}
	records := domain.SeedRecords()[:1]
	records[0].SourceOrderID = "SECOND-ACCOUNT-ORDER"
	requirements := domain.SeedRequirements()[:1]
	requirements[0].InternalPO = "SECOND-ACCOUNT-INTERNAL"
	run, err := f.service.ReplaceConnectionSource(f.ctx, f.actor, f.w, extra.ID, 1, records, requirements)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, run)
	o, err = f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil || len(o.Orders) != 5 {
		t.Fatal("colliding source key merged across accounts", len(o.Orders), err)
	}
	var extraOrder Order
	for _, order := range o.Orders {
		if order.ConnectionID == extra.ID {
			extraOrder = order
			if order.Requirement == nil || order.Requirement.InternalPO != "SECOND-ACCOUNT-INTERNAL" {
				t.Fatal("extra requirement mapping crossed feeds", order)
			}
		} else if legacy[order.SourceKey] != order.ID || order.Requirement != nil && order.Requirement.InternalPO == "SECOND-ACCOUNT-INTERNAL" {
			t.Fatal("legacy IDs or mappings changed", order)
		}
	}
	if extraOrder.ID == "" || extraOrder.SourceKey != records[0].SourceKey {
		t.Fatal("source key rewritten or fixture missing")
	}
	detail, err := f.service.Order(f.ctx, f.actor, f.w, extraOrder.ID)
	if err != nil || detail.Order.Requirement.InternalPO != "SECOND-ACCOUNT-INTERNAL" {
		t.Fatal(err)
	}
	for _, condition := range detail.Conditions {
		if condition.InternalPO != "SECOND-ACCOUNT-INTERNAL" {
			t.Fatal("condition joined another feed's requirement")
		}
	}
	// Existing workspace import behavior changes only the fixed default feed.
	if _, err = f.service.ReplaceRequirements(f.ctx, f.actor, f.w, 1, domain.SeedRequirements()); err != nil {
		t.Fatal(err)
	}
	var requirementPO string
	if err = f.admin.QueryRow(f.ctx, `SELECT data->>'internal_po' FROM wb_requirements WHERE workspace_id=$1 AND generation=1 AND connection_id=$2`, f.w, extra.ID).Scan(&requirementPO); err != nil || requirementPO != "SECOND-ACCOUNT-INTERNAL" {
		t.Fatal("legacy import replaced extra mappings", err)
	}
	// The run's exact connection cannot be substituted at publication.
	queued, err := f.service.RefreshConnection(f.ctx, f.actor, f.w, extra.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	forged := connectionArgs(t, f, queued)
	forged.ConnectionID = baseline.ConnectionID
	binding, _ := collectionBinding(forged, "fictional_supplier", "fictional")
	if err = f.service.PublishBatch(f.ctx, forged, domain.FictionalBatch(records, binding, o.Orders[0].LastObservedAt)); !errors.Is(err, ErrStale) {
		t.Fatal("cross-connection run published", err)
	}
	originalArgs := connectionArgs(t, f, queued)
	reset, err := f.service.Reset(f.ctx, f.actor, f.w, 1, "multi-feed-reset")
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, reset.Run)
	if err = f.service.Collect(f.ctx, originalArgs); !errors.Is(err, ErrStale) {
		t.Fatal("pre-reset extra run published", err)
	}
	o, err = f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil || len(o.Orders) != 4 || len(o.Connections) != 2 || !o.Ready {
		t.Fatal("reset copied baseline into additional feed", len(o.Orders), err)
	}
	for _, c := range o.Connections {
		if c.ID == extra.ID && (c.LastSuccessAt != nil || c.ScheduleEnabled) {
			t.Fatal("extra feed retained old-generation success or schedule")
		}
	}
	if err = f.admin.QueryRow(f.ctx, `SELECT jsonb_array_length(source_records) FROM wb_connections WHERE id=$1`, extra.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("extra fixture did not reset to empty", count, err)
	}
}

func TestConnectionPauseRevisionAndConcurrentSchedules(t *testing.T) {
	f := testFoundation(t)
	run, err := f.service.Seed(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, run)
	c := newFixtureConnection(t, f, "parallel")
	repeated, err := f.service.CreateConnection(f.ctx, f.actor, f.w, 1, "parallel", ConnectionInput{Name: "Fixture feed parallel", Method: "fictional", MinimumCadenceSeconds: 60, CadenceSeconds: 120})
	if err != nil || repeated.ID != c.ID {
		t.Fatal("create retry duplicated connection", err)
	}
	if _, err = f.service.UpdateConnection(f.ctx, f.actor, f.w, c.ID, 1, c.Revision, ConnectionInput{Name: "Changed", Method: "api_polling"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("method changed across existing identity", err)
	}
	var wg sync.WaitGroup
	ids := make(chan string, 8)
	failures := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := f.service.RefreshConnection(f.ctx, f.actor, f.w, c.ID, 1)
			ids <- r.ID
			failures <- e
		}()
	}
	wg.Wait()
	close(ids)
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	var pending Run
	for id := range ids {
		if pending.ID != "" && pending.ID != id {
			t.Fatal("same-feed refresh did not coalesce")
		}
		pending.ID = id
	}
	pending.ConnectionID = c.ID
	pending.Generation = 1
	args := connectionArgs(t, f, pending)
	scheduled, err := f.service.SetConnectionSchedule(f.ctx, f.actor, f.w, c.ID, 1, c.Revision, true, 60)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.SetConnectionSchedule(f.ctx, f.actor, f.w, c.ID, 1, scheduled.Revision, true, 30); !errors.Is(err, ErrInvalid) {
		t.Fatal("schedule bypassed approved minimum", err)
	}
	paused, err := f.service.PauseConnection(f.ctx, f.actor, f.w, c.ID, 1, scheduled.Revision, true)
	if err != nil || !paused.Paused || paused.Runnable || paused.ScheduleEnabled {
		t.Fatal("pause did not fence schedule", paused, err)
	}
	if err = f.service.Collect(f.ctx, args); !errors.Is(err, ErrStale) {
		t.Fatal("late worker published after pause", err)
	}
	if _, err = f.service.RefreshConnection(f.ctx, f.actor, f.w, c.ID, 1); !errors.Is(err, ErrInvalid) {
		t.Fatal("paused feed refreshed", err)
	}
	if _, err = f.service.PauseConnection(f.ctx, f.actor, f.w, c.ID, 1, scheduled.Revision, false); !errors.Is(err, ErrStale) {
		t.Fatal("stale revision resumed connection", err)
	}
	resumed, err := f.service.PauseConnection(f.ctx, f.actor, f.w, c.ID, 1, paused.Revision, false)
	if err != nil || !resumed.Runnable || resumed.ScheduleEnabled {
		t.Fatal("resume executed or enabled source", resumed, err)
	}
	c2 := newFixtureConnection(t, f, "second-parallel")
	if _, err = f.service.SetConnectionSchedule(f.ctx, f.actor, f.w, c.ID, 1, resumed.Revision, true, 60); err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.SetConnectionSchedule(f.ctx, f.actor, f.w, c2.ID, 1, c2.Revision, true, 60); err != nil {
		t.Fatal(err)
	}
	if _, err = f.admin.Exec(f.ctx, `UPDATE wb_connections SET next_due_at=now()-interval '1 day' WHERE workspace_id=$1 AND NOT is_demo_default`, f.w); err != nil {
		t.Fatal(err)
	}
	failures = make(chan error, 4)
	for range 4 {
		wg.Add(1)
		go func() { defer wg.Done(); failures <- f.service.ReconcileSchedules(f.ctx) }()
	}
	wg.Wait()
	close(failures)
	for e := range failures {
		if e != nil {
			t.Fatal(e)
		}
	}
	var count, feeds int
	if err = f.admin.QueryRow(f.ctx, `SELECT count(*),count(DISTINCT connection_id) FROM wb_runs WHERE workspace_id=$1 AND state='queued'`, f.w).Scan(&count, &feeds); err != nil || count != 2 || feeds != 2 {
		t.Fatal("due feeds not independently coalesced", count, feeds, err)
	}
	for _, id := range []string{c.ID, c2.ID} {
		r, err := f.service.RefreshConnection(f.ctx, f.actor, f.w, id, 1)
		if err != nil {
			t.Fatal(err)
		}
		f.collect(t, r)
	}
	o, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil || len(o.Orders) != 4 {
		t.Fatal("empty scheduled feed fabricated records", len(o.Orders), err)
	}
}

func TestPrivateConnectionPlaceholdersAndReviewerDenial(t *testing.T) {
	f := testFoundation(t)
	if _, err := f.service.CreateConnection(f.ctx, f.otherActor, f.otherW, 1, "denied", ConnectionInput{Name: "Feed", Method: "fictional"}); !errors.Is(err, ErrNotFound) {
		t.Fatal("reviewer managed connections", err)
	}
	if _, err := f.service.CreateConnection(f.ctx, f.actor, f.w, 1, "wrong-origin", ConnectionInput{Name: "Portal", Method: "browser"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("private placeholder entered fictional workspace", err)
	}
	var private string
	if err := f.admin.QueryRow(f.ctx, `INSERT INTO workspaces(id,owner_user_id,kind,name) VALUES(gen_random_uuid(),$1,'private','Private fixtures') RETURNING id::text`, f.actor).Scan(&private); err != nil {
		t.Fatal(err)
	}
	if _, err := f.admin.Exec(f.ctx, `INSERT INTO memberships(workspace_id,user_id,role) VALUES($1,$2,'owner')`, private, f.actor); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreateConnection(f.ctx, f.actor, private, 1, "no-fiction", ConnectionInput{Name: "Feed", Method: "fictional"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("fictional source entered private workspace", err)
	}
	for i, method := range []string{"api_polling", "webhook", "browser", "public_scrape", "email"} {
		c, err := f.service.CreateConnection(f.ctx, f.actor, private, 1, fmt.Sprintf("placeholder-%d", i), ConnectionInput{Name: "Pending " + method, Method: method})
		if err != nil || c.Runnable || c.Status != "not_configured" || c.Origin != "live_private" || c.LastSuccessAt != nil {
			t.Fatal("placeholder claims source evidence", c, err)
		}
		if _, err = f.service.RefreshConnection(f.ctx, f.actor, private, c.ID, 1); !errors.Is(err, ErrInvalid) {
			t.Fatal("unconfigured source executed", err)
		}
		if _, err = f.service.SetConnectionSchedule(f.ctx, f.actor, private, c.ID, 1, c.Revision, true, 300); !errors.Is(err, ErrInvalid) {
			t.Fatal("unconfigured source scheduled", err)
		}
		paused, err := f.service.PauseConnection(f.ctx, f.actor, private, c.ID, 1, c.Revision, true)
		if err != nil {
			t.Fatal(err)
		}
		c, err = f.service.PauseConnection(f.ctx, f.actor, private, c.ID, 1, paused.Revision, false)
		if err != nil || c.Runnable || c.Status != "not_configured" {
			t.Fatal("pending source resume claims readiness", err)
		}
	}
	o, err := f.service.Overview(f.ctx, f.actor, private)
	if err != nil || len(o.Connections) != 5 || len(o.Orders) != 0 || len(o.Runs) != 0 || o.Ready || o.Seeded || o.Origin != "no_supplier_data" {
		t.Fatal("private placeholders altered purchasing activity", o, err)
	}
	if _, err = f.service.Overview(f.ctx, f.otherActor, private); !errors.Is(err, ErrNotFound) {
		t.Fatal("private placeholder visible to reviewer", err)
	}
	badBuyer := f.otherActor
	if _, err = f.service.CreateConnection(f.ctx, f.actor, private, 1, "bad-buyer", ConnectionInput{Name: "Bad buyer", Method: "email", BuyerUserID: &badBuyer}); !errors.Is(err, ErrInvalid) {
		t.Fatal("buyer from another workspace accepted", err)
	}
	badSupplier := f.otherW
	if _, err = f.service.CreateConnection(f.ctx, f.actor, private, 1, "bad-supplier", ConnectionInput{Name: "Bad supplier", Method: "email", SupplierID: &badSupplier}); !errors.Is(err, ErrInvalid) {
		t.Fatal("supplier from another workspace accepted", err)
	}
}

func TestStageConnectionSourceAtomicValidationRetryAndTransitions(t *testing.T) {
	f := testFoundation(t)
	baseline, err := f.service.Seed(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, baseline)
	c := newFixtureConnection(t, f, "staging")
	record := domain.SeedRecords()[0]
	run, err := f.service.StageConnectionSource(f.ctx, f.actor, f.w, c.ID, 1, c.Revision, "first-source", []domain.Record{record}, nil)
	if err != nil {
		t.Fatal(err)
	}
	retry, err := f.service.StageConnectionSource(f.ctx, f.actor, f.w, c.ID, 1, c.Revision, "first-source", []domain.Record{record}, nil)
	if err != nil || retry.ID != run.ID {
		t.Fatal("source retry duplicated queue effects", err)
	}
	if _, err = f.service.StageConnectionSource(f.ctx, f.actor, f.w, c.ID, 1, c.Revision, "another-source", []domain.Record{record}, nil); !errors.Is(err, ErrStale) {
		t.Fatal("stale source editor overwrote staged input", err)
	}
	f.collect(t, run)
	o, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil {
		t.Fatal(err)
	}
	for _, conn := range o.Connections {
		if conn.ID == c.ID {
			c = conn
		}
	}
	var sourceOrderID string
	for _, order := range o.Orders {
		if order.ConnectionID == c.ID {
			sourceOrderID = order.ID
			if order.Requirement != nil {
				t.Fatal("optional empty requirement imported default mapping")
			}
		}
	}
	before := len(o.Runs)
	invalid := []struct {
		name         string
		records      []domain.Record
		requirements []domain.Requirement
	}{
		{"duplicate key", []domain.Record{record, record}, nil},
		{"mismatched requirement", []domain.Record{record}, domain.SeedRequirements()[1:2]},
	}
	live := record
	live.Origin = "live_private"
	invalid = append(invalid, struct {
		name         string
		records      []domain.Record
		requirements []domain.Requirement
	}{"live origin", []domain.Record{live}, nil})
	changedIdentity := record
	changedIdentity.SourceOrderID = "IMPERSONATING-ORDER"
	invalid = append(invalid, struct {
		name         string
		records      []domain.Record
		requirements []domain.Requirement
	}{"changed identity", []domain.Record{changedIdentity}, nil})
	duplicateIdentity := record
	duplicateIdentity.SourceKey = "OTHER-KEY"
	invalid = append(invalid, struct {
		name         string
		records      []domain.Record
		requirements []domain.Requirement
	}{"duplicate identity", []domain.Record{record, duplicateIdentity}, nil})
	for i, tc := range invalid {
		if _, err = f.service.StageConnectionSource(f.ctx, f.actor, f.w, c.ID, 1, c.Revision, fmt.Sprintf("invalid-%d", i), tc.records, tc.requirements); !errors.Is(err, ErrInvalid) {
			t.Fatal(tc.name, err)
		}
	}
	o, err = f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil || len(o.Runs) != before {
		t.Fatal("invalid staging partially inserted runs", err)
	}
	for _, conn := range o.Connections {
		if conn.ID == c.ID && conn.Revision != c.Revision {
			t.Fatal("invalid staging changed revision")
		}
	}
	if _, err = f.service.StageConnectionSource(f.ctx, f.actor, f.w, baseline.ConnectionID, 1, 1, "fixed-default", []domain.Record{record}, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("fixture upload changed pinned scenario feed", err)
	}
	// A completed fictional shipment uses the common event engine with the exact
	// extra-feed order ID; the default same-key record remains untouched.
	shipped := record
	shipped.Status = "shipped"
	shipped.Shipped = shipped.Ordered
	run, err = f.service.StageConnectionSource(f.ctx, f.actor, f.w, c.ID, 1, c.Revision, "shipment-source", []domain.Record{shipped}, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, run)
	detail, err := f.service.Order(f.ctx, f.actor, f.w, sourceOrderID)
	if err != nil || detail.Order.Status != "shipped" {
		t.Fatal("full normalized fixture status did not publish", err)
	}
	found := false
	for _, event := range detail.History {
		if event.Field == "status" {
			found = true
			if event.ConnectionID != c.ID || event.RunID != run.ID || event.Before != record.Status || event.After != "shipped" {
				t.Fatal("transition lineage crossed source connection", event)
			}
		}
	}
	if !found {
		t.Fatal("fixture status change emitted no event")
	}
	for _, order := range o.Orders {
		if order.ConnectionID == baseline.ConnectionID && order.SourceKey == record.SourceKey {
			original, err := f.service.Order(f.ctx, f.actor, f.w, order.ID)
			if err != nil || original.Order.Status != record.Status {
				t.Fatal("fixture transition modified default order", err)
			}
		}
	}
	// Empty source data remains a valid explicit fixture input for a new feed.
	empty := newFixtureConnection(t, f, "empty-stage")
	run, err = f.service.StageConnectionSource(f.ctx, f.actor, f.w, empty.ID, 1, empty.Revision, "empty-input", nil, nil)
	if err != nil {
		t.Fatal("empty fictional source could not be staged", err)
	}
	f.collect(t, run)
	var count int
	if err = f.admin.QueryRow(f.ctx, `SELECT record_count FROM wb_runs WHERE id=$1`, run.ID).Scan(&count); err != nil || count != 0 {
		t.Fatal("empty staging invented records", err)
	}
}

func TestConnectionCountGuardAndSelectedSupplier(t *testing.T) {
	f := testFoundation(t)
	run, err := f.service.Seed(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, run)
	selected := domain.SeedRecords()[0].SupplierAlias
	var supplierID string
	if err = f.admin.QueryRow(f.ctx, `SELECT id::text FROM wb_supplier_profiles WHERE workspace_id=$1 AND alias=$2`, f.w, selected).Scan(&supplierID); err != nil {
		t.Fatal(err)
	}
	c, err := f.service.CreateConnection(f.ctx, f.actor, f.w, 1, "selected-supplier", ConnectionInput{Name: "Selected supplier", Method: "fictional", SupplierID: &supplierID})
	if err != nil {
		t.Fatal(err)
	}
	records := domain.SeedRecords()
	wrong := records[0]
	wrong.SupplierAlias = "Different supplier"
	if _, err = f.service.StageConnectionSource(f.ctx, f.actor, f.w, c.ID, 1, c.Revision, "mismatched-supplier", []domain.Record{wrong}, nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("selected supplier did not bind source aliases", err)
	}
	// Ten accepted lines in another feed must never quarantine this feed's single
	// observation; a drop within that same feed must still be rejected.
	large := newFixtureConnection(t, f, "large-feed")
	many := []domain.Record{}
	for i := range 12 {
		r := records[0]
		r.SourceKey = fmt.Sprintf("LARGE-%d", i)
		r.SourceOrderID = fmt.Sprintf("LARGE-ORDER-%d", i)
		many = append(many, r)
	}
	run, err = f.service.ReplaceConnectionSource(f.ctx, f.actor, f.w, large.ID, 1, many, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, run)
	run, err = f.service.StageConnectionSource(f.ctx, f.actor, f.w, c.ID, 1, c.Revision, "single-source", records[:1], nil)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, run)
	var state string
	if err = f.admin.QueryRow(f.ctx, `SELECT state FROM wb_runs WHERE id=$1`, run.ID).Scan(&state); err != nil || state != "succeeded" {
		t.Fatal("record count drop compared another connection", state, err)
	}
	o, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil {
		t.Fatal(err)
	}
	for _, conn := range o.Connections {
		if conn.ID == large.ID {
			large = conn
		}
	}
	if _, err = f.service.StageConnectionSource(f.ctx, f.actor, f.w, large.ID, 1, large.Revision, "large-drop", many[:1], nil); !errors.Is(err, ErrInvalid) {
		t.Fatal("same-connection count drop staged", err)
	}
	// A late source-contract batch is also checked even if staging is bypassed by
	// local administrative fixture setup.
	if _, err = f.admin.Exec(f.ctx, `UPDATE wb_connections SET source_records=$2 WHERE id=$1`, large.ID, raw(many[:1])); err != nil {
		t.Fatal(err)
	}
	run, err = f.service.RefreshConnection(f.ctx, f.actor, f.w, large.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.Collect(f.ctx, connectionArgs(t, f, run)); err != nil {
		t.Fatal(err)
	}
	var code *string
	if err = f.admin.QueryRow(f.ctx, `SELECT state,error_code FROM wb_runs WHERE id=$1`, run.ID).Scan(&state, &code); err != nil || state != "rejected" || code == nil || *code != "unexpected_record_count_drop" {
		t.Fatal("publication bypassed same-connection count guard", state, code, err)
	}
	var count int
	if err = f.admin.QueryRow(f.ctx, `SELECT count(*) FROM wb_orders WHERE workspace_id=$1 AND connection_id=$2`, f.w, large.ID).Scan(&count); err != nil || count != 12 {
		t.Fatal("rejected drop changed accepted records", count, err)
	}
}

func TestConnectionLimitReservesFixedDemoFeed(t *testing.T) {
	f := testFoundation(t)
	// Reserve one slot for explicit seeding when the owner adds feeds first.
	if _, err := f.admin.Exec(f.ctx, `INSERT INTO wb_connections(workspace_id,name,source_records,is_demo_default) SELECT $1,'Empty fixture '||i,'[]',false FROM generate_series(1,49) i`, f.w); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.CreateConnection(f.ctx, f.actor, f.w, 1, "over-limit", ConnectionInput{Name: "Excess feed", Method: "fictional"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("unseeded workspace consumed fixed-feed slot", err)
	}
	run, err := f.service.Seed(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal("default seed slot unavailable", err)
	}
	f.collect(t, run)
	o, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil || len(o.Connections) != 50 || len(o.Orders) != 4 {
		t.Fatal("connection limit or seed count incorrect", len(o.Connections), len(o.Orders), err)
	}
	if _, err = f.service.CreateConnection(f.ctx, f.actor, f.w, 1, "over-limit-again", ConnectionInput{Name: "Excess feed", Method: "fictional"}); !errors.Is(err, ErrInvalid) {
		t.Fatal("workspace exceeded fifty connections", err)
	}
}

func TestStageConnectionSourceQueueFailureRollsBack(t *testing.T) {
	f := testFoundation(t)
	c := newFixtureConnection(t, f, "queue-rollback")
	broken := &Service{Pool: f.app}
	records := domain.SeedRecords()[:1]
	requirements := domain.SeedRequirements()[:1]
	if _, err := broken.StageConnectionSource(f.ctx, f.actor, f.w, c.ID, 1, c.Revision, "retry-after-queue-failure", records, requirements); err == nil {
		t.Fatal("staging succeeded with no durable queue")
	}
	var unchanged bool
	if err := f.admin.QueryRow(f.ctx, `SELECT c.revision=1 AND c.source_revision=1 AND c.source_records='[]'::jsonb AND c.active_run_id IS NULL AND NOT EXISTS(SELECT 1 FROM wb_runs r WHERE r.connection_id=c.id) AND NOT EXISTS(SELECT 1 FROM wb_requirements req WHERE req.connection_id=c.id) AND NOT EXISTS(SELECT 1 FROM wb_operations op WHERE op.workspace_id=c.workspace_id AND op.operation='connection.source') FROM wb_connections c WHERE c.id=$1`, c.ID).Scan(&unchanged); err != nil || !unchanged {
		t.Fatal("failed queue insert retained staged facts or partial publication", err)
	}
	run, err := f.service.StageConnectionSource(f.ctx, f.actor, f.w, c.ID, 1, c.Revision, "retry-after-queue-failure", records, requirements)
	if err != nil {
		t.Fatal("failed staging reserved retry key", err)
	}
	f.collect(t, run)
}
