package workbench

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"buyersworkbench/internal/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func issueFixture(t *testing.T) (fixture, Exception) {
	t.Helper()
	f := testFoundation(t)
	run, err := f.service.Seed(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, run)
	f.scenario(t, "delay")
	view, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range view.Priorities {
		if issue.Rule == "R01" && issue.Active {
			return f, issue
		}
	}
	t.Fatal("fixture requires an active late-arrival issue")
	return f, Exception{}
}

func issueByID(t *testing.T, f fixture, id string) Exception {
	t.Helper()
	view, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range view.Priorities {
		if issue.ID == id {
			return issue
		}
	}
	t.Fatal("issue is missing from the authorized current generation")
	return Exception{}
}

func validIssueResolution() IssueResolutionInput {
	return IssueResolutionInput{ResolutionType: "delivery_updated", ActionTaken: "Called supplier and checked the revised commitment.", Result: "Supplier agreed an expedited arrival; waiting for observed confirmation.", Reference: "SUPPLIER-CALL-10482", SupplierContact: "Supplier scheduling desk", RevisedDate: "2026-10-24", ConfirmedQuantity: "24", FollowUpDate: "2026-10-20"}
}

func issueEventCount(t *testing.T, f fixture, id, action string) int {
	t.Helper()
	var count int
	if err := f.admin.QueryRow(f.ctx, "SELECT count(*) FROM wb_issue_events WHERE workspace_id=$1 AND condition_id=$2 AND ($3='' OR action=$3)", f.w, id, action).Scan(&count); err != nil {
		t.Fatal(err)
	}
	return count
}

func issueJSON(t *testing.T, issue Exception) map[string]any {
	t.Helper()
	data, err := json.Marshal(issue)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func resolutionJSON(t *testing.T, issue Exception) map[string]any {
	t.Helper()
	result, ok := issueJSON(t, issue)["resolution"].(map[string]any)
	if !ok {
		t.Fatal("accepted issue response omits immutable resolution")
	}
	return result
}

func issueSourceFacts(t *testing.T, f fixture) string {
	t.Helper()
	var facts string
	if err := f.admin.QueryRow(f.ctx, `SELECT jsonb_agg(jsonb_build_object('id',id,'record',record,'first_record',first_record) ORDER BY source_key)::text FROM wb_orders WHERE workspace_id=$1`, f.w).Scan(&facts); err != nil {
		t.Fatal(err)
	}
	return facts
}

func TestIssueResolutionValidatesAllFieldsWithoutPartialWrites(t *testing.T) {
	f, issue := issueFixture(t)
	valid := validIssueResolution()
	cases := []struct {
		name string
		edit func(*IssueResolutionInput)
	}{
		{"empty type", func(v *IssueResolutionInput) { v.ResolutionType = "" }},
		{"unknown type", func(v *IssueResolutionInput) { v.ResolutionType = "ignore_risk" }},
		{"missing action", func(v *IssueResolutionInput) { v.ActionTaken = " \n " }},
		{"long action", func(v *IssueResolutionInput) { v.ActionTaken = strings.Repeat("a", 4001) }},
		{"control action", func(v *IssueResolutionInput) { v.ActionTaken = "bad\x00text" }},
		{"invalid utf8", func(v *IssueResolutionInput) { v.ActionTaken = string([]byte{0xff}) }},
		{"missing result", func(v *IssueResolutionInput) { v.Result = " " }},
		{"long result", func(v *IssueResolutionInput) { v.Result = strings.Repeat("a", 4001) }},
		{"control result", func(v *IssueResolutionInput) { v.Result = "bad\x1btext" }},
		{"missing reference", func(v *IssueResolutionInput) { v.Reference = " " }},
		{"long reference", func(v *IssueResolutionInput) { v.Reference = strings.Repeat("a", 501) }},
		{"multiline reference", func(v *IssueResolutionInput) { v.Reference = "REF\nforged" }},
		{"long contact", func(v *IssueResolutionInput) { v.SupplierContact = strings.Repeat("a", 201) }},
		{"multiline contact", func(v *IssueResolutionInput) { v.SupplierContact = "Desk\nsecond" }},
		{"bad revised date", func(v *IssueResolutionInput) { v.RevisedDate = "2026-02-30" }},
		{"negative confirmed quantity", func(v *IssueResolutionInput) { v.ConfirmedQuantity = "-1" }},
		{"exponent confirmed quantity", func(v *IssueResolutionInput) { v.ConfirmedQuantity = "1e2" }},
		{"missing active followup", func(v *IssueResolutionInput) { v.FollowUpDate = "" }},
		{"bad followup date", func(v *IssueResolutionInput) { v.FollowUpDate = "10/20/2026" }},
	}
	before := issueSourceFacts(t, f)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			input := valid
			tc.edit(&input)
			_, err := f.service.ResolveIssue(f.ctx, f.actor, f.w, issue.ID, 1, issue.Revision, "invalid-resolution-"+tc.name, input)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid closeout accepted: %v", err)
			}
		})
	}
	current := issueByID(t, f, issue.ID)
	if current.Revision != issue.Revision || current.Status != "open" || issueEventCount(t, f, issue.ID, "") != 0 || issueSourceFacts(t, f) != before {
		t.Fatal("validation failure partially changed status, event history, or supplier facts")
	}
	if _, err := f.service.UpdateWorkflow(f.ctx, f.actor, f.w, issue.ID, 1, issue.Revision, WorkflowInput{Status: "completed"}); !errors.Is(err, ErrResolutionRequired) {
		t.Fatal("legacy completed action bypassed the structured closeout")
	}
	if issueEventCount(t, f, issue.ID, "") != 0 {
		t.Fatal("rejected legacy shortcut created an event")
	}
	if _, err := f.admin.Exec(f.ctx, "UPDATE wb_conditions SET status='completed' WHERE id=$1", issue.ID); err != nil {
		t.Fatal(err)
	}
	legacy := issueByID(t, f, issue.ID)
	if legacy.Status != "completed" || issueJSON(t, legacy)["resolution"] != nil {
		t.Fatal("legacy completion was rewritten or assigned fabricated evidence")
	}
	converted, err := f.service.ResolveIssue(f.ctx, f.actor, f.w, legacy.ID, 1, legacy.Revision, "document-legacy-completion", valid)
	if err != nil || converted.Status != "awaiting_verification" || !converted.Active || issueSourceFacts(t, f) != before {
		t.Fatal("legacy completed issue could not receive an accountable closeout", err)
	}
}

func TestIssueResolutionEventFailureRollsBackStatusAndOperation(t *testing.T) {
	f, issue := issueFixture(t)
	before := issueSourceFacts(t, f)
	// This fault exists only inside the freshly isolated test schema. Force the
	// append to fail after the service's status update and require full rollback.
	if _, err := f.admin.Exec(f.ctx, "ALTER TABLE wb_issue_events ADD CONSTRAINT test_reject_resolution_event CHECK(action<>'resolution_submitted')"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ResolveIssue(f.ctx, f.actor, f.w, issue.ID, 1, issue.Revision, "retry-after-event-failure", validIssueResolution()); err == nil {
		t.Fatal("simulated event-storage failure was ignored")
	}
	current := issueByID(t, f, issue.ID)
	if current.Revision != issue.Revision || current.Status != issue.Status || issueEventCount(t, f, issue.ID, "") != 0 || issueSourceFacts(t, f) != before {
		t.Fatal("event failure committed a status change, evidence, or supplier facts")
	}
	var operations int
	if err := f.admin.QueryRow(f.ctx, "SELECT count(*) FROM wb_operations WHERE idempotency_key='retry-after-event-failure'").Scan(&operations); err != nil || operations != 0 {
		t.Fatal("failed transaction left a successful idempotency result", err)
	}
	if _, err := f.admin.Exec(f.ctx, "ALTER TABLE wb_issue_events DROP CONSTRAINT test_reject_resolution_event"); err != nil {
		t.Fatal(err)
	}
	accepted, err := f.service.ResolveIssue(f.ctx, f.actor, f.w, issue.ID, 1, issue.Revision, "retry-after-event-failure", validIssueResolution())
	if err != nil || accepted.Status != "awaiting_verification" || issueEventCount(t, f, issue.ID, "resolution_submitted") != 1 {
		t.Fatal("retry after rolled-back failure did not commit exactly once", err)
	}
}

func TestIssueResolutionAtomicEvidenceIdempotencyAndSourceVerification(t *testing.T) {
	f, issue := issueFixture(t)
	note, err := f.service.AddNote(f.ctx, f.actor, f.w, issue.ID, 1, issue.Revision, "Keep the buyer's original follow-up note.")
	if err != nil {
		t.Fatal(err)
	}
	issue = issueByID(t, f, issue.ID)
	followup := "2026-10-20"
	issue, err = f.service.UpdateWorkflow(f.ctx, f.actor, f.w, issue.ID, 1, issue.Revision, WorkflowInput{Status: "in_progress", Acknowledged: true, AssigneeID: &f.actor, FollowUpDate: &followup})
	if err != nil || issueEventCount(t, f, issue.ID, "workflow_updated") != 1 {
		t.Fatal("workflow state and immutable event were not committed together", err)
	}
	before := issueSourceFacts(t, f)
	input := validIssueResolution()
	input.ActionTaken = "  Called supplier.\n\tReviewed schedule.  "
	input.Reference = "  SUPPLIER-CALL-10482  "
	oldRevision := issue.Revision
	closed, err := f.service.ResolveIssue(f.ctx, f.actor, f.w, issue.ID, 1, oldRevision, "record-resolution-once", input)
	if err != nil || !closed.Active || closed.Status != "awaiting_verification" || closed.Revision != oldRevision+1 || issueJSON(t, closed)["resolved_at"] != nil {
		t.Fatalf("active source issue was falsely verified: %+v %v", closed, err)
	}
	resolution := resolutionJSON(t, closed)
	if resolution["action_taken"] != strings.TrimSpace(input.ActionTaken) || resolution["reference"] != strings.TrimSpace(input.Reference) || resolution["quantity_uom"] != "EA" || resolution["condition_active"] != true || resolution["rule"] != "R01" || resolution["submitted_by"] != "Test actor" || resolution["material_revision"] != float64(issue.MaterialRevision) {
		t.Fatalf("resolution lost immutable buyer/source evidence: %+v", resolution)
	}
	var actor, from, to string
	if err = f.admin.QueryRow(f.ctx, "SELECT actor_id::text,from_status,to_status FROM wb_issue_events WHERE condition_id=$1 AND action='resolution_submitted'", issue.ID).Scan(&actor, &from, &to); err != nil || actor != f.actor || from != "in_progress" || to != "awaiting_verification" {
		t.Fatal("resolution event did not retain accountable transition", err)
	}
	if issueSourceFacts(t, f) != before {
		t.Fatal("buyer resolution rewrote accepted source observations")
	}
	f.scenario(t, "repeat")
	unchanged := issueByID(t, f, issue.ID)
	if unchanged.Status != "awaiting_verification" || unchanged.Revision != closed.Revision || issueEventCount(t, f, issue.ID, "source_verified") != 0 {
		t.Fatal("identical source refresh verified or reopened the buyer closeout")
	}
	retry, err := f.service.ResolveIssue(f.ctx, f.actor, f.w, issue.ID, 1, oldRevision, "record-resolution-once", input)
	if err != nil || retry.Revision != closed.Revision || resolutionJSON(t, retry)["id"] != resolution["id"] || issueEventCount(t, f, issue.ID, "resolution_submitted") != 1 {
		t.Fatal("lost-response retry duplicated the resolution", err)
	}
	changedInput := input
	changedInput.Result = "Different outcome under the same operation key"
	if _, err = f.service.ResolveIssue(f.ctx, f.actor, f.w, issue.ID, 1, oldRevision, "record-resolution-once", changedInput); !errors.Is(err, ErrStale) {
		t.Fatal("idempotency key accepted a different immutable closeout")
	}
	f.scenario(t, "recovery")
	verified := issueByID(t, f, issue.ID)
	if verified.Active || verified.Status != "resolved" || issueJSON(t, verified)["resolved_at"] == nil || issueEventCount(t, f, issue.ID, "source_verified") != 1 || resolutionJSON(t, verified)["id"] != resolution["id"] {
		t.Fatal("accepted clearing source did not verify while preserving buyer evidence")
	}
	var sourceActor *string
	if err = f.admin.QueryRow(f.ctx, "SELECT actor_id::text FROM wb_issue_events WHERE condition_id=$1 AND action='source_verified'", issue.ID).Scan(&sourceActor); err != nil || sourceActor != nil {
		t.Fatal("automatic source verification was attributed to a buyer", err)
	}
	detail, err := f.service.Order(f.ctx, f.actor, f.w, issue.OrderID)
	if err != nil || len(detail.Notes) != 1 || detail.Notes[0].ID != note.ID || detail.Notes[0].Body != note.Body {
		t.Fatal("verification discarded original notes", err)
	}
	f.scenario(t, "repeat")
	if issueEventCount(t, f, issue.ID, "source_verified") != 1 {
		t.Fatal("unchanged cleared source duplicated verification")
	}
	f.scenario(t, "delay")
	recurred := issueByID(t, f, issue.ID)
	if !recurred.Active || recurred.Status != "open" || issueJSON(t, recurred)["resolved_at"] != nil || issueEventCount(t, f, issue.ID, "reopened") != 1 || resolutionJSON(t, recurred)["id"] != resolution["id"] {
		t.Fatal("recurring source risk did not reopen without losing the prior resolution")
	}
}

func TestIssueResolutionManualReopenWorseningAndClearedSubmission(t *testing.T) {
	f, issue := issueFixture(t)
	closed, err := f.service.ResolveIssue(f.ctx, f.actor, f.w, issue.ID, 1, issue.Revision, "resolve-before-reopen", validIssueResolution())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.service.ReopenIssue(f.ctx, f.actor, f.w, issue.ID, 1, closed.Revision, "blank-reopen-reason", " \n "); !errors.Is(err, ErrInvalid) {
		t.Fatal("manual reopen accepted without a reason")
	}
	reopened, err := f.service.ReopenIssue(f.ctx, f.actor, f.w, issue.ID, 1, closed.Revision, "reopen-buyer-followup", "Supplier could not support its earlier commitment.")
	if err != nil || reopened.Status != "open" || resolutionJSON(t, reopened)["id"] != resolutionJSON(t, closed)["id"] {
		t.Fatal("manual reopen lost prior resolution or failed", err)
	}
	var actor string
	if err = f.admin.QueryRow(f.ctx, "SELECT actor_id::text FROM wb_issue_events WHERE condition_id=$1 AND action='reopened'", issue.ID).Scan(&actor); err != nil || actor != f.actor {
		t.Fatal("manual reopen lost buyer accountability", err)
	}
	retry, err := f.service.ReopenIssue(f.ctx, f.actor, f.w, issue.ID, 1, closed.Revision, "reopen-buyer-followup", "Supplier could not support its earlier commitment.")
	if err != nil || retry.Revision != reopened.Revision || issueEventCount(t, f, issue.ID, "reopened") != 1 {
		t.Fatal("manual reopen retry duplicated the event")
	}
	closed, err = f.service.ResolveIssue(f.ctx, f.actor, f.w, issue.ID, 1, reopened.Revision, "resolve-before-worsening", validIssueResolution())
	if err != nil {
		t.Fatal(err)
	}
	var records []domain.Record
	var data []byte
	if err = f.admin.QueryRow(f.ctx, "SELECT source_records FROM wb_connections WHERE workspace_id=$1", f.w).Scan(&data); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &records); err != nil {
		t.Fatal(err)
	}
	for i := range records {
		if records[i].SourceKey == issue.SourceKey {
			records[i].ArrivalDate = "2026-10-29"
		}
	}
	if _, err = f.admin.Exec(f.ctx, "UPDATE wb_connections SET source_records=$2,source_revision=source_revision+1 WHERE workspace_id=$1", f.w, raw(records)); err != nil {
		t.Fatal(err)
	}
	run, err := f.service.Refresh(f.ctx, f.actor, f.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, run)
	worsened := issueByID(t, f, issue.ID)
	if worsened.Status != "open" || worsened.DelayDays != 4 || worsened.MaterialRevision <= closed.MaterialRevision || issueEventCount(t, f, issue.ID, "reopened") != 2 {
		t.Fatal("material worsening retained the buyer's completed followup")
	}
	f.scenario(t, "recovery")
	cleared := issueByID(t, f, issue.ID)
	input := validIssueResolution()
	input.ResolutionType = "data_corrected"
	input.FollowUpDate = ""
	input.ConfirmedQuantity = "0"
	resolved, err := f.service.ResolveIssue(f.ctx, f.actor, f.w, issue.ID, 1, cleared.Revision, "document-cleared-issue", input)
	if err != nil || resolved.Active || resolved.Status != "resolved" || issueJSON(t, resolved)["resolved_at"] == nil || resolutionJSON(t, resolved)["condition_active"] != false {
		t.Fatal("cleared source closeout required a fabricated followup or lacked resolution", err)
	}
}

func TestIssueResolutionUnknownSourceFactsCannotVerify(t *testing.T) {
	f, late := issueFixture(t)
	view, err := f.service.Overview(f.ctx, f.actor, f.w)
	if err != nil {
		t.Fatal(err)
	}
	var unconfirmed Exception
	for _, issue := range view.Priorities {
		if issue.Rule == "R03" && issue.Active {
			unconfirmed = issue
		}
	}
	if unconfirmed.ID == "" {
		t.Fatal("fixture requires an explicitly unconfirmed order")
	}
	resolutionIDs := map[string]any{}
	for _, issue := range []Exception{late, unconfirmed} {
		resolved, err := f.service.ResolveIssue(f.ctx, f.actor, f.w, issue.ID, 1, issue.Revision, "resolve-unknown-proof-"+issue.Rule, validIssueResolution())
		if err != nil || resolved.Status != "awaiting_verification" {
			t.Fatal("prepare buyer closeout awaiting source proof", err)
		}
		resolutionIDs[issue.ID] = resolutionJSON(t, resolved)["id"]
	}
	updateSource := func(change func(*domain.Record)) {
		t.Helper()
		var data []byte
		if err := f.admin.QueryRow(f.ctx, "SELECT source_records FROM wb_connections WHERE workspace_id=$1", f.w).Scan(&data); err != nil {
			t.Fatal(err)
		}
		var records []domain.Record
		if err := json.Unmarshal(data, &records); err != nil {
			t.Fatal(err)
		}
		for i := range records {
			change(&records[i])
		}
		if _, err := f.admin.Exec(f.ctx, "UPDATE wb_connections SET source_records=$2,source_revision=source_revision+1 WHERE workspace_id=$1", f.w, raw(records)); err != nil {
			t.Fatal(err)
		}
		run, err := f.service.Refresh(f.ctx, f.actor, f.w, 1)
		if err != nil {
			t.Fatal(err)
		}
		f.collect(t, run)
		accepted, err := f.service.Run(f.ctx, f.actor, f.w, run.ID)
		if err != nil || accepted.State != "succeeded" {
			t.Fatal("regression must exercise an accepted source observation", err)
		}
	}
	updateSource(func(record *domain.Record) {
		switch record.SourceKey {
		case late.SourceKey:
			record.ArrivalDate, record.ArrivalKind, record.ArrivalLabel = "", "", ""
		case unconfirmed.SourceKey:
			record.Confirmation, record.ConfirmedAt = "unavailable", nil
		}
	})
	for _, issue := range []Exception{late, unconfirmed} {
		current := issueByID(t, f, issue.ID)
		if current.Status != "awaiting_verification" || issueJSON(t, current)["resolved_at"] != nil || issueEventCount(t, f, issue.ID, "source_verified") != 0 || resolutionJSON(t, current)["id"] != resolutionIDs[issue.ID] {
			t.Fatalf("missing source facts falsely verified %s", issue.Rule)
		}
	}
	detail, err := f.service.Order(f.ctx, f.actor, f.w, unconfirmed.OrderID)
	if err != nil || detail.Order.Confirmation != "unavailable" {
		t.Fatal("new unknown source facts were not accepted independently of workflow", err)
	}
	updateSource(func(record *domain.Record) {
		switch record.SourceKey {
		case late.SourceKey:
			record.ArrivalDate, record.ArrivalKind, record.ArrivalLabel = "2026-10-21", "committed_arrival", "Fictional committed delivery"
		case unconfirmed.SourceKey:
			at := time.Date(2026, 10, 19, 13, 0, 0, 0, time.UTC)
			record.Confirmation, record.ConfirmedAt = "confirmed", &at
			record.ArrivalDate, record.ArrivalKind, record.ArrivalLabel = "2026-10-23", "committed_arrival", "Fictional committed delivery"
			record.RemainingSchedule = "known"
		}
	})
	for _, issue := range []Exception{late, unconfirmed} {
		current := issueByID(t, f, issue.ID)
		if current.Active || current.Status != "resolved" || issueJSON(t, current)["resolved_at"] == nil || issueEventCount(t, f, issue.ID, "source_verified") != 1 || resolutionJSON(t, current)["id"] != resolutionIDs[issue.ID] {
			t.Fatalf("explicit safe source facts did not verify %s exactly once", issue.Rule)
		}
	}
}

func TestIssueResolutionConcurrentCommandsAndGenerationIsolation(t *testing.T) {
	f, issue := issueFixture(t)
	var wg sync.WaitGroup
	errorsByCaller := make([]error, 2)
	results := make([]Exception, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errorsByCaller[i] = f.service.ResolveIssue(f.ctx, f.actor, f.w, issue.ID, 1, issue.Revision, "concurrent-same-resolution", validIssueResolution())
		}(i)
	}
	wg.Wait()
	if errorsByCaller[0] != nil || errorsByCaller[1] != nil || results[0].Revision != results[1].Revision || issueEventCount(t, f, issue.ID, "resolution_submitted") != 1 {
		t.Fatalf("same concurrent operation was not replayed once: %v", errorsByCaller)
	}
	if _, err := f.service.ResolveIssue(f.ctx, f.actor, f.w, issue.ID, 1, issue.Revision, "stale-independent-resolution", validIssueResolution()); !errors.Is(err, ErrStale) {
		t.Fatal("stale second closeout replaced the accepted resolution")
	}
	current := issueByID(t, f, issue.ID)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			keys := []string{"concurrent-reopen-first", "concurrent-reopen-second"}
			_, errorsByCaller[i] = f.service.ReopenIssue(f.ctx, f.actor, f.w, issue.ID, 1, current.Revision, keys[i], "Supplier commitment needs another review.")
		}(i)
	}
	wg.Wait()
	accepted, stale := 0, 0
	for _, err := range errorsByCaller {
		if err == nil {
			accepted++
		} else if errors.Is(err, ErrStale) {
			stale++
		} else {
			t.Fatal(err)
		}
	}
	if accepted != 1 || stale != 1 || issueEventCount(t, f, issue.ID, "reopened") != 1 {
		t.Fatal("concurrent distinct commands both won an optimistic revision")
	}
	reset, err := f.service.Reset(f.ctx, f.actor, f.w, 1, "reset-issue-resolution")
	if err != nil {
		t.Fatal(err)
	}
	f.collect(t, reset.Run)
	if _, err = f.service.ResolveIssue(f.ctx, f.actor, f.w, issue.ID, 1, current.Revision, "concurrent-same-resolution", validIssueResolution()); !errors.Is(err, ErrStale) {
		t.Fatal("prior-generation operation was replayed into the reset workspace")
	}
	if _, err = f.service.ResolveIssue(f.ctx, f.actor, f.w, issue.ID, 2, current.Revision, "old-issue-new-generation", validIssueResolution()); !errors.Is(err, ErrNotFound) {
		t.Fatal("old condition UUID remained writable in a new generation")
	}
	tx, err := f.app.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = tx.Exec(f.ctx, "SELECT set_config('bwb.actor_id',$1,true)", f.actor); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = tx.QueryRow(f.ctx, "SELECT count(*) FROM wb_issue_events WHERE workspace_id=$1 AND generation=1", f.w).Scan(&count); err != nil || count != 0 {
		t.Fatal("runtime role exposed old-generation closeout evidence", err)
	}
}

func TestIssueResolutionRuntimeScopeAppendOnlyAndReviewerAccess(t *testing.T) {
	f, issue := issueFixture(t)
	if _, err := f.service.ResolveIssue(f.ctx, f.otherActor, f.otherW, issue.ID, 1, issue.Revision, "foreign-resolution-attempt", validIssueResolution()); !errors.Is(err, ErrNotFound) {
		t.Fatal("foreign issue UUID was accepted in an authorized other workspace")
	}
	resolved, err := f.service.ResolveIssue(f.ctx, f.actor, f.w, issue.ID, 1, issue.Revision, "scope-resolution-evidence", validIssueResolution())
	if err != nil {
		t.Fatal(err)
	}
	var eventID string
	if err = f.admin.QueryRow(f.ctx, "SELECT id::text FROM wb_issue_events WHERE condition_id=$1 AND action='resolution_submitted'", issue.ID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	var canUpdate, canDelete bool
	if err = f.app.QueryRow(f.ctx, "SELECT has_table_privilege(current_user,'wb_issue_events','UPDATE'),has_table_privilege(current_user,'wb_issue_events','DELETE')").Scan(&canUpdate, &canDelete); err != nil || canUpdate || canDelete {
		t.Fatal("runtime role may rewrite submitted resolution events", err)
	}
	withRuntime := func(actor string, fn func(pgx.Tx)) {
		t.Helper()
		tx, err := f.app.Begin(f.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(f.ctx)
		if _, err = tx.Exec(f.ctx, "SELECT set_config('bwb.actor_id',$1,true)", actor); err != nil {
			t.Fatal(err)
		}
		fn(tx)
	}
	withRuntime(f.otherActor, func(tx pgx.Tx) {
		var count int
		if err := tx.QueryRow(f.ctx, "SELECT count(*) FROM wb_issue_events WHERE id=$1", eventID).Scan(&count); err != nil || count != 0 {
			t.Fatal("actual runtime role leaked foreign events", err)
		}
		_, err := tx.Exec(f.ctx, `INSERT INTO wb_issue_events(workspace_id,generation,condition_id,actor_id,action,from_status,to_status,details) VALUES($1,1,$2,$3,'resolution_submitted','open','awaiting_verification','{}')`, f.w, issue.ID, f.otherActor)
		var denied *pgconn.PgError
		if !errors.As(err, &denied) || denied.Code != "42501" {
			t.Fatal("runtime RLS accepted a foreign issue event", err)
		}
	})
	withRuntime(f.actor, func(tx pgx.Tx) {
		_, err := tx.Exec(f.ctx, "UPDATE wb_issue_events SET details='{}' WHERE id=$1", eventID)
		var denied *pgconn.PgError
		if !errors.As(err, &denied) || denied.Code != "42501" {
			t.Fatal("runtime role rewrote an immutable event", err)
		}
	})
	withRuntime(f.actor, func(tx pgx.Tx) {
		_, err := tx.Exec(f.ctx, `INSERT INTO wb_issue_events(workspace_id,generation,condition_id,actor_id,action,from_status,to_status,details) VALUES($1,1,$2,$3,'resolution_submitted','open','awaiting_verification','{}')`, f.w, issue.ID, f.otherActor)
		var denied *pgconn.PgError
		if !errors.As(err, &denied) || denied.Code != "42501" {
			t.Fatal("runtime role forged another buyer's authorship", err)
		}
	})
	if resolutionJSON(t, issueByID(t, f, issue.ID))["id"] != resolutionJSON(t, resolved)["id"] {
		t.Fatal("denied SQL attempts altered the accepted evidence")
	}
	// An authorized reviewer has the same issue workflow capabilities in their
	// own disposable workspace, independently from owner-only supplier/PO edits.
	other := f
	other.actor, other.w = f.otherActor, f.otherW
	run, err := other.service.Seed(other.ctx, other.actor, other.w, 1)
	if err != nil {
		t.Fatal(err)
	}
	other.collect(t, run)
	other.scenario(t, "delay")
	view, err := other.service.Overview(other.ctx, other.actor, other.w)
	if err != nil {
		t.Fatal(err)
	}
	var reviewerIssue Exception
	for _, candidate := range view.Priorities {
		if candidate.Rule == "R01" && candidate.Active {
			reviewerIssue = candidate
		}
	}
	if reviewerIssue.ID == "" {
		t.Fatal("reviewer issue fixture missing")
	}
	reviewerResult, err := other.service.ResolveIssue(other.ctx, other.actor, other.w, reviewerIssue.ID, 1, reviewerIssue.Revision, "reviewer-local-resolution", validIssueResolution())
	if err != nil || reviewerResult.Status != "awaiting_verification" {
		t.Fatal("authorized reviewer could not record their resolution", err)
	}
	reviewerEventID, ok := resolutionJSON(t, reviewerResult)["id"].(string)
	if !ok {
		t.Fatal("reviewer closeout event identity missing")
	}
	withRuntime(other.actor, func(tx pgx.Tx) {
		var count int
		if err := tx.QueryRow(f.ctx, "SELECT count(*) FROM wb_issue_events WHERE id=$1", reviewerEventID).Scan(&count); err != nil || count != 1 {
			t.Fatal("actual reviewer runtime role cannot read its authorized event", err)
		}
		_, err := tx.Exec(f.ctx, "UPDATE wb_issue_events SET details='{}' WHERE id=$1", reviewerEventID)
		var denied *pgconn.PgError
		if !errors.As(err, &denied) || denied.Code != "42501" {
			t.Fatal("actual reviewer runtime role rewrote its own immutable event", err)
		}
	})
	withRuntime(other.actor, func(tx pgx.Tx) {
		_, err := tx.Exec(f.ctx, "DELETE FROM wb_issue_events WHERE id=$1", reviewerEventID)
		var denied *pgconn.PgError
		if !errors.As(err, &denied) || denied.Code != "42501" {
			t.Fatal("actual reviewer runtime role deleted its own immutable event", err)
		}
	})
	if _, err = f.service.ResolveIssue(f.ctx, f.actor, f.w, reviewerIssue.ID, 1, reviewerIssue.Revision, "owner-foreign-condition", validIssueResolution()); !errors.Is(err, ErrNotFound) {
		t.Fatal("owner bypassed condition workspace scope")
	}
}
