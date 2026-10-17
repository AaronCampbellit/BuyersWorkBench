package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"buyersworkbench/internal/domain"
	"buyersworkbench/internal/sanitize"
	"buyersworkbench/internal/workbench"
	"github.com/jackc/pgx/v5"
)

func (s *Server) registerSnapshots(mux *http.ServeMux) {
	s.route(mux, "POST /api/v1/admin/sanitized-exports/preview", s.snapshotPreview)
	s.route(mux, "POST /api/v1/admin/sanitized-exports/{id}/publish", s.snapshotPublish)
	s.route(mux, "GET /api/v1/workspaces/{w}/snapshots", s.snapshots)
}
func (s *Server) snapshotPreview(w http.ResponseWriter, r *http.Request) {
	session, ok := s.mutationSession(w, r)
	if !ok {
		return
	}
	if session.user.Role != "owner" {
		s.domainError(w, workbench.ErrNotFound)
		return
	}
	if !s.recentAuthentication(w, r, session) {
		return
	}
	var in struct {
		WorkspaceID    string `json:"workspace_id"`
		SourceRecordID string `json:"source_record_id"`
		Generation     int64  `json:"generation"`
		sanitize.Approval
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !uuidPattern.MatchString(in.WorkspaceID) || !uuidPattern.MatchString(in.SourceRecordID) || in.Generation < 1 {
		s.domainError(w, workbench.ErrInvalid)
		return
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		s.domainError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT set_config('bwb.actor_id',$1,true)`, session.user.ID); err != nil {
		s.domainError(w, err)
		return
	}
	var recordJSON []byte
	var collected time.Time
	err = tx.QueryRow(r.Context(), `SELECT o.record,o.last_observed_at FROM wb_orders o JOIN workspaces w ON w.id=o.workspace_id AND w.generation=o.generation WHERE o.workspace_id=$1 AND o.id=$2 AND o.generation=$3 AND w.kind='private' AND w.owner_user_id=$4`, in.WorkspaceID, in.SourceRecordID, in.Generation, session.user.ID).Scan(&recordJSON, &collected)
	if errors.Is(err, pgx.ErrNoRows) {
		s.domainError(w, workbench.ErrNotFound)
		return
	}
	if err != nil {
		s.domainError(w, err)
		return
	}
	var record domain.Record
	if json.Unmarshal(recordJSON, &record) != nil {
		s.domainError(w, errors.New("invalid private record"))
		return
	}
	var snapshotID string
	if err = tx.QueryRow(r.Context(), `SELECT gen_random_uuid()::text`).Scan(&snapshotID); err != nil {
		s.domainError(w, err)
		return
	}
	snapshot, err := sanitize.Project(snapshotID, record, collected, time.Now().UTC(), in.Approval)
	if err != nil {
		s.domainError(w, workbench.ErrInvalid)
		return
	}
	projection, _ := json.Marshal(snapshot)
	var exportID string
	err = tx.QueryRow(r.Context(), `INSERT INTO wb_sanitized_exports(workspace_id,generation,source_record_id,snapshot_id,snapshot,actor_user_id) VALUES($1,$2,$3,$4,$5,$6) RETURNING id::text`, in.WorkspaceID, in.Generation, in.SourceRecordID, snapshotID, projection, session.user.ID).Scan(&exportID)
	if err != nil {
		s.domainError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, 201, map[string]any{"export_id": exportID, "snapshot": snapshot, "approval_required": true})
}
func (s *Server) snapshotPublish(w http.ResponseWriter, r *http.Request) {
	session, ok := s.mutationSession(w, r)
	if !ok {
		return
	}
	if session.user.Role != "owner" {
		s.domainError(w, workbench.ErrNotFound)
		return
	}
	if !s.recentAuthentication(w, r, session) {
		return
	}
	var in struct {
		WorkspaceID string `json:"target_workspace_id"`
		Generation  int64  `json:"generation"`
		Approve     bool   `json:"approve"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !in.Approve || !uuidPattern.MatchString(in.WorkspaceID) || !uuidPattern.MatchString(r.PathValue("id")) || in.Generation < 1 {
		s.domainError(w, workbench.ErrInvalid)
		return
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		s.domainError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT set_config('bwb.actor_id',$1,true)`, session.user.ID); err != nil {
		s.domainError(w, err)
		return
	}
	var currentGeneration int64
	var kind string
	err = tx.QueryRow(r.Context(), `SELECT generation,kind FROM workspaces WHERE id=$1 FOR UPDATE`, in.WorkspaceID).Scan(&currentGeneration, &kind)
	if errors.Is(err, pgx.ErrNoRows) {
		s.domainError(w, workbench.ErrNotFound)
		return
	}
	if err != nil {
		s.domainError(w, err)
		return
	}
	if kind == "private" {
		s.domainError(w, workbench.ErrPrivate)
		return
	}
	if currentGeneration != in.Generation {
		s.domainError(w, workbench.ErrStale)
		return
	}
	var snapshotID string
	var data []byte
	err = tx.QueryRow(r.Context(), `SELECT snapshot_id::text,snapshot FROM wb_sanitized_exports WHERE id=$1 FOR UPDATE`, r.PathValue("id")).Scan(&snapshotID, &data)
	if errors.Is(err, pgx.ErrNoRows) {
		s.domainError(w, workbench.ErrNotFound)
		return
	}
	if err != nil {
		s.domainError(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO wb_snapshot_copies(workspace_id,generation,snapshot_id,snapshot) VALUES($1,$2,$3,$4) ON CONFLICT DO NOTHING`, in.WorkspaceID, in.Generation, snapshotID, data); err != nil {
		s.domainError(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE wb_sanitized_exports SET approved_at=COALESCE(approved_at,now()) WHERE id=$1`, r.PathValue("id")); err != nil {
		s.domainError(w, err)
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO audit_events(id,actor_user_id,action,target_type,target_id,outcome,metadata) VALUES(gen_random_uuid(),$1,'snapshot.publish','workspace',$2,'success',jsonb_build_object('snapshot_id',$3::text))`, session.user.ID, in.WorkspaceID, snapshotID); err != nil {
		s.domainError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, 200, map[string]any{"published": true, "snapshot_id": snapshotID, "generation": in.Generation})
}
func (s *Server) snapshots(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, false)
	if !ok {
		return
	}
	overview, err := svc.Overview(r.Context(), session.user.ID, r.PathValue("w"))
	if err != nil {
		s.domainError(w, err)
		return
	}
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		s.domainError(w, err)
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `SELECT set_config('bwb.actor_id',$1,true)`, session.user.ID); err != nil {
		s.domainError(w, err)
		return
	}
	rows, err := tx.Query(r.Context(), `SELECT snapshot,seeded_at FROM wb_snapshot_copies WHERE workspace_id=$1 AND generation=$2 ORDER BY seeded_at,id LIMIT 200`, r.PathValue("w"), overview.Generation)
	if err != nil {
		s.domainError(w, err)
		return
	}
	defer rows.Close()
	type copy struct {
		Snapshot sanitize.Snapshot `json:"snapshot"`
		SeededAt time.Time         `json:"seeded_at"`
	}
	result := []copy{}
	for rows.Next() {
		var item copy
		var data []byte
		if err = rows.Scan(&data, &item.SeededAt); err != nil {
			break
		}
		if err = json.Unmarshal(data, &item.Snapshot); err != nil {
			break
		}
		result = append(result, item)
	}
	rows.Close()
	if err != nil || rows.Err() != nil {
		s.domainError(w, errors.New("snapshot read failed"))
		return
	}
	respond(w, 200, map[string]any{"snapshots": result, "generation": overview.Generation})
}
