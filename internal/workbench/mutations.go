package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"buyersworkbench/internal/domain"
	"github.com/jackc/pgx/v5"
)

func (s *Service) Scenario(ctx context.Context, actor, w string, g int64, key, idempotency string) (Run, error) {
	tx, sc, err := s.begin(ctx, actor, w, 0, true)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback(ctx)
	if sc.kind == "private" {
		return Run{}, ErrPrivate
	}
	payload := fmt.Sprintf("%d:%s", g, key)
	var prior Run
	if found, err := operation(ctx, tx, actor, w, "scenario", idempotency, payload, g, &prior); err != nil {
		return Run{}, err
	} else if found {
		return prior, tx.Commit(ctx)
	}
	if sc.generation != g {
		return Run{}, ErrStale
	}
	var source []byte
	if err = tx.QueryRow(ctx, "SELECT source_records FROM wb_connections WHERE workspace_id=$1 AND is_demo_default", w).Scan(&source); err != nil {
		return Run{}, ErrNotFound
	}
	var records []domain.Record
	if err = json.Unmarshal(source, &records); err != nil {
		return Run{}, err
	}
	next, scenarioErr := domain.ApplyScenario(records, key)
	failure := errors.Is(scenarioErr, domain.ErrIncompleteSource)
	if scenarioErr != nil && !failure {
		return Run{}, ErrInvalid
	}
	if failure {
		next = records
	}
	// A scenario always supersedes the prior lease. An already collecting worker
	// may finish its source read but cannot publish after this fence advances.
	if _, err = tx.Exec(ctx, `UPDATE wb_runs SET state='cancelled',finished_at=now(),error_code='scenario_superseded' WHERE workspace_id=$1 AND generation=$2 AND connection_id=(SELECT id FROM wb_connections WHERE workspace_id=$1 AND is_demo_default) AND state IN('queued','running','validating','publishing')`, w, g); err != nil {
		return Run{}, err
	}
	if _, err = tx.Exec(ctx, `UPDATE wb_connections SET source_records=$2,source_failure=$3,source_revision=source_revision+1,fence=fence+1,active_run_id=NULL,lease_expires_at=NULL WHERE workspace_id=$1 AND is_demo_default`, w, raw(next), failure); err != nil {
		return Run{}, err
	}
	if strings.EqualFold(strings.TrimSpace(key), "SC06") || strings.EqualFold(strings.TrimSpace(key), "quotes") {
		if _, err = tx.Exec(ctx, "UPDATE wb_quote_comparisons SET data=$3,revision=revision+1 WHERE workspace_id=$1 AND generation=$2", w, g, raw(domain.SeedQuotes())); err != nil {
			return Run{}, err
		}
	}
	run, err := s.queueTx(ctx, tx, actor, w, g)
	if err != nil {
		return Run{}, err
	}
	if err = saveOperation(ctx, tx, actor, w, "scenario", idempotency, payload, g, run); err != nil {
		return Run{}, err
	}
	if err = audit(ctx, tx, actor, w, "fictional.scenario"); err != nil {
		return Run{}, err
	}
	return run, tx.Commit(ctx)
}
func (s *Service) Reset(ctx context.Context, actor, w string, g int64, idempotency string) (ResetResult, error) {
	tx, sc, err := s.begin(ctx, actor, w, 0, true)
	if err != nil {
		return ResetResult{}, err
	}
	defer tx.Rollback(ctx)
	if sc.kind == "private" {
		return ResetResult{}, ErrPrivate
	}
	payload := fmt.Sprint(g)
	var result ResetResult
	if found, err := operation(ctx, tx, actor, w, "reset", idempotency, payload, g, &result); err != nil {
		return result, err
	} else if found {
		return result, tx.Commit(ctx)
	}
	if sc.generation != g {
		return result, ErrStale
	}
	if _, err = tx.Exec(ctx, `UPDATE wb_runs SET state='stale_generation',finished_at=now(),error_code='workspace_reset' WHERE workspace_id=$1 AND generation=$2 AND state IN('queued','running','validating','publishing')`, w, g); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `UPDATE workspaces SET generation=generation+1 WHERE id=$1`, w); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `UPDATE wb_connections SET source_records=CASE WHEN is_demo_default THEN $2::jsonb ELSE '[]'::jsonb END,source_failure=false,source_revision=1,revision=revision+1,fence=fence+1,active_run_id=NULL,lease_expires_at=NULL,status='ready',last_attempt_at=NULL,last_success_at=NULL,last_failure_code=NULL,initialized_generation=NULL,paused=false,schedule_enabled=false,next_due_at=NULL,supplier_id=NULL WHERE workspace_id=$1 AND method='fictional'`, w, raw(domain.SeedRecords())); err != nil {
		return result, err
	}
	// Previous generations remain immutable history. Reads and writes all require
	// current generation; no global identity/audit/private deletion is possible.
	if err = s.seedTx(ctx, tx, actor, w, g+1); err != nil {
		return result, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO wb_snapshot_copies(workspace_id,generation,snapshot_id,snapshot) SELECT workspace_id,$3,snapshot_id,snapshot FROM wb_snapshot_copies WHERE workspace_id=$1 AND generation=$2 ON CONFLICT DO NOTHING`, w, g, g+1); err != nil {
		return result, err
	}
	run, err := s.queueTx(ctx, tx, actor, w, g+1)
	if err != nil {
		return result, err
	}
	result = ResetResult{g + 1, SeedVersion, run}
	if err = saveOperation(ctx, tx, actor, w, "reset", idempotency, payload, g, result); err != nil {
		return result, err
	}
	if err = audit(ctx, tx, actor, w, "fictional.reset"); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
func (s *Service) UpdateWorkflow(ctx context.Context, actor, w, id string, g, revision int64, input WorkflowInput) (Exception, error) {
	if !uuidRE.MatchString(id) || g < 1 || revision < 1 {
		return Exception{}, ErrInvalid
	}
	tx, sc, err := s.begin(ctx, actor, w, g, true)
	if err != nil {
		return Exception{}, err
	}
	defer tx.Rollback(ctx)
	if sc.kind == "private" {
		return Exception{}, ErrPrivate
	}
	current, err := exceptionRow(tx.QueryRow(ctx, exceptionQuery+" AND c.id=$3", w, sc.generation, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Exception{}, ErrNotFound
	}
	if err != nil {
		return Exception{}, err
	}
	if current.Revision != revision {
		return current, ErrStale
	}
	closed := slices.Contains([]string{"completed", "awaiting_verification", "resolved"}, current.Status)
	if (closed && input.Status != current.Status) || (!closed && slices.Contains([]string{"completed", "awaiting_verification", "resolved"}, input.Status)) {
		return current, ErrResolutionRequired
	}
	if input.Status != "open" && input.Status != "in_progress" && !(closed && input.Status == current.Status) {
		return Exception{}, ErrInvalid
	}
	if input.FollowUpDate != nil && *input.FollowUpDate != "" && !domain.ValidDate(*input.FollowUpDate) {
		return Exception{}, ErrInvalid
	}
	if input.AssigneeID != nil && *input.AssigneeID == "" {
		input.AssigneeID = nil
	}
	if input.FollowUpDate != nil && *input.FollowUpDate == "" {
		input.FollowUpDate = nil
	}
	if current.Active && current.Status == "awaiting_verification" && input.FollowUpDate == nil {
		return current, ErrInvalid
	}
	if input.AssigneeID != nil {
		if !uuidRE.MatchString(*input.AssigneeID) {
			return Exception{}, ErrInvalid
		}
		var allowed bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM memberships m JOIN users u ON u.id=m.user_id WHERE m.workspace_id=$1 AND m.user_id=$2 AND u.active AND u.disabled_at IS NULL AND (u.expires_at IS NULL OR u.expires_at>now()))`, w, *input.AssigneeID).Scan(&allowed); err != nil {
			return Exception{}, err
		}
		if !allowed {
			return Exception{}, ErrInvalid
		}
	}
	changed, err := tx.Exec(ctx, `UPDATE wb_conditions SET acknowledged=$5,assignee_id=$6,follow_up_date=$7::date,status=$8,revision=revision+1 WHERE workspace_id=$1 AND generation=$2 AND id=$3 AND revision=$4`, w, g, id, revision, input.Acknowledged, input.AssigneeID, input.FollowUpDate, input.Status)
	if err != nil {
		return Exception{}, err
	}
	if changed.RowsAffected() != 1 {
		return Exception{}, ErrStale
	}
	result, err := exceptionRow(tx.QueryRow(ctx, exceptionQuery+" AND c.id=$3", w, g, id))
	if err != nil {
		return Exception{}, err
	}
	unit, err := issueQuantityUOM(ctx, tx, w, sc.generation, id)
	if err != nil {
		return result, err
	}
	details := issueSnapshot(result, unit)
	before, after := workflowOf(current), workflowOf(result)
	details.BeforeWorkflow, details.AfterWorkflow = &before, &after
	if err = appendIssueEvent(ctx, tx, actor, w, id, sc.generation, "workflow_updated", current.Status, result.Status, details); err != nil {
		return result, err
	}
	if err = audit(ctx, tx, actor, w, "condition.workflow"); err != nil {
		return result, err
	}
	return result, tx.Commit(ctx)
}
func (s *Service) AddNote(ctx context.Context, actor, w, id string, g, revision int64, body string) (Note, error) {
	body = strings.TrimSpace(body)
	if !uuidRE.MatchString(id) || len(body) < 1 || len([]rune(body)) > 4000 || revision < 1 || strings.ContainsRune(body, 0) {
		return Note{}, ErrInvalid
	}
	tx, _, err := s.begin(ctx, actor, w, g, true)
	if err != nil {
		return Note{}, err
	}
	defer tx.Rollback(ctx)
	changed, err := tx.Exec(ctx, "UPDATE wb_conditions SET revision=revision+1 WHERE workspace_id=$1 AND generation=$2 AND id=$3 AND revision=$4", w, g, id, revision)
	if err != nil {
		return Note{}, err
	}
	if changed.RowsAffected() != 1 {
		return Note{}, ErrStale
	}
	var note Note
	err = tx.QueryRow(ctx, `WITH inserted AS(INSERT INTO wb_notes(workspace_id,generation,condition_id,author_id,body) VALUES($1,$2,$3,$4,$5) RETURNING id,condition_id,body,created_at) SELECT n.id::text,n.condition_id::text,u.display_name,n.body,n.created_at FROM inserted n JOIN users u ON u.id=$4`, w, g, id, actor, body).Scan(&note.ID, &note.ExceptionID, &note.AuthorName, &note.Body, &note.CreatedAt)
	if err != nil {
		return note, err
	}
	return note, tx.Commit(ctx)
}
func (s *Service) UpdateQuote(ctx context.Context, actor, w, id string, g, revision int64, input domain.Quote) (Quotes, error) {
	tx, sc, err := s.begin(ctx, actor, w, g, true)
	if err != nil {
		return Quotes{}, err
	}
	defer tx.Rollback(ctx)
	if sc.kind == "private" {
		return Quotes{}, ErrPrivate
	}
	quotes, err := quotesTx(ctx, tx, w, g)
	if err != nil {
		return quotes, err
	}
	if quotes.Revision != revision {
		return quotes, ErrStale
	}
	found := false
	for i, q := range quotes.Comparison.Quotes {
		if q.ID == id {
			input.ID = q.ID
			input.Supplier = q.Supplier
			quotes.Comparison.Quotes[i] = input
			found = true
			break
		}
	}
	if !found {
		return quotes, ErrNotFound
	}
	if err = domain.ValidateQuote(input); err != nil {
		return quotes, ErrInvalid
	}
	quotes.Calculation = domain.CalculateQuotes(quotes.Comparison)
	quotes.Revision++
	if _, err = tx.Exec(ctx, "UPDATE wb_quote_comparisons SET data=$3,revision=$4 WHERE workspace_id=$1 AND generation=$2", w, g, raw(quotes.Comparison), quotes.Revision); err != nil {
		return quotes, err
	}
	return quotes, tx.Commit(ctx)
}
func (s *Service) MarkNotificationRead(ctx context.Context, actor, w, id string, g int64) error {
	if !uuidRE.MatchString(id) {
		return ErrNotFound
	}
	tx, _, err := s.begin(ctx, actor, w, g, true)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	changed, err := tx.Exec(ctx, "UPDATE wb_notifications SET read_at=COALESCE(read_at,now()) WHERE workspace_id=$1 AND generation=$2 AND id=$3 AND recipient_id=$4", w, g, id, actor)
	if err != nil {
		return err
	}
	if changed.RowsAffected() != 1 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}
func (s *Service) ReplaceRequirements(ctx context.Context, actor, w string, g int64, requirements []domain.Requirement) (Run, error) {
	if domain.ValidateRequirements(requirements) != nil {
		return Run{}, ErrInvalid
	}
	tx, sc, err := s.begin(ctx, actor, w, g, true)
	if err != nil {
		return Run{}, err
	}
	defer tx.Rollback(ctx)
	if !sc.isOwner {
		return Run{}, ErrNotFound
	}
	if sc.kind == "private" {
		return Run{}, ErrPrivate
	}
	var source []byte
	if err = tx.QueryRow(ctx, "SELECT source_records FROM wb_connections WHERE workspace_id=$1 AND is_demo_default", w).Scan(&source); err != nil {
		return Run{}, ErrNotFound
	}
	var records []domain.Record
	if err = json.Unmarshal(source, &records); err != nil {
		return Run{}, err
	}
	mapping := map[string]domain.Record{}
	for _, r := range records {
		mapping[r.SourceKey] = r
	}
	for _, req := range requirements {
		r, ok := mapping[req.SourceKey]
		if !ok || r.SupplierAlias != req.SupplierAlias || r.UOM != req.UOM {
			return Run{}, ErrInvalid
		}
	}
	if _, err = tx.Exec(ctx, "DELETE FROM wb_requirements WHERE workspace_id=$1 AND generation=$2 AND connection_id=(SELECT id FROM wb_connections WHERE workspace_id=$1 AND is_demo_default)", w, g); err != nil {
		return Run{}, err
	}
	for _, r := range requirements {
		if _, err = tx.Exec(ctx, "INSERT INTO wb_requirements(workspace_id,generation,source_key,data,connection_id) SELECT $1,$2,$3,$4,id FROM wb_connections WHERE workspace_id=$1 AND is_demo_default", w, g, r.SourceKey, raw(r)); err != nil {
			return Run{}, err
		}
	}
	run, err := s.queueTx(ctx, tx, actor, w, g)
	if err != nil {
		return Run{}, err
	}
	if err = audit(ctx, tx, actor, w, "requirements.import"); err != nil {
		return Run{}, err
	}
	return run, tx.Commit(ctx)
}
