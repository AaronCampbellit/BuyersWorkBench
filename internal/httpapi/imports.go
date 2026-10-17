package httpapi

import (
	"encoding/json"
	"mime"
	"net/http"

	"buyersworkbench/internal/domain"
	"buyersworkbench/internal/imports"
	"buyersworkbench/internal/workbench"
)

func (s *Server) registerImports(mux *http.ServeMux) {
	s.route(mux, "GET /api/v1/workspaces/{w}/imports/internal-orders/template.csv", s.importTemplate)
	s.route(mux, "POST /api/v1/workspaces/{w}/imports/internal-orders/preview", s.importPreview)
	s.route(mux, "POST /api/v1/workspaces/{w}/imports/internal-orders/{id}/publish", s.importPublish)
}
func (s *Server) importTemplate(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, false)
	if !ok {
		return
	}
	overview, err := svc.Overview(r.Context(), session.user.ID, r.PathValue("w"))
	if err != nil {
		s.domainError(w, err)
		return
	}
	if overview.Origin != "fictional" {
		s.domainError(w, workbench.ErrPrivate)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="fictional-internal-requirements.csv"`)
	_ = imports.Template(w, domain.SeedRequirements())
}
func (s *Server) importPreview(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, true)
	if !ok {
		return
	}
	if session.user.Role != "owner" {
		s.domainError(w, workbench.ErrNotFound)
		return
	}
	// CSV never contains workspace IDs or origin claims. The authenticated path
	// and current generation supply scope, independently of the uploaded rows.
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "text/csv" {
		fail(w, 415, "Content-Type must be text/csv.")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, imports.MaxBytes)
	requirements, err := imports.ParseCSV(r.Body)
	if err != nil {
		respond(w, 422, map[string]string{"error": err.Error(), "code": "invalid_csv"})
		return
	}
	overview, err := svc.Overview(r.Context(), session.user.ID, r.PathValue("w"))
	if err != nil {
		s.domainError(w, err)
		return
	}
	if overview.Origin != "fictional" {
		s.domainError(w, workbench.ErrPrivate)
		return
	}
	if err = domain.ValidateRequirements(requirements); err != nil {
		s.domainError(w, workbench.ErrInvalid)
		return
	}
	connectionID := r.URL.Query().Get("connection_id")
	if connectionID == "" {
		for _, connection := range overview.Connections {
			if connection.IsDemoDefault {
				connectionID = connection.ID
				break
			}
		}
	}
	var allowedConnection bool
	for _, connection := range overview.Connections {
		if connection.ID == connectionID && connection.Origin == "fictional" {
			allowedConnection = true
			break
		}
	}
	if !allowedConnection {
		s.domainError(w, workbench.ErrNotFound)
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
	keys := make([]string, 0, len(requirements))
	for _, requirement := range requirements {
		keys = append(keys, requirement.SourceKey)
	}
	rows, err := tx.Query(r.Context(), `SELECT source_key,record FROM wb_orders WHERE workspace_id=$1 AND generation=$2 AND connection_id=$3 AND source_key=ANY($4::text[])`, r.PathValue("w"), overview.Generation, connectionID, keys)
	if err != nil {
		s.domainError(w, err)
		return
	}
	known := map[string]domain.Record{}
	for rows.Next() {
		var key string
		var data []byte
		if err = rows.Scan(&key, &data); err != nil {
			rows.Close()
			s.domainError(w, err)
			return
		}
		var record domain.Record
		if err = json.Unmarshal(data, &record); err != nil {
			rows.Close()
			s.domainError(w, err)
			return
		}
		known[key] = record
	}
	rows.Close()
	if err = rows.Err(); err != nil {
		s.domainError(w, err)
		return
	}
	for _, requirement := range requirements {
		record, exists := known[requirement.SourceKey]
		if !exists || record.Origin != "fictional" || record.SupplierAlias != requirement.SupplierAlias || record.UOM != requirement.UOM {
			respond(w, 422, map[string]string{"error": "Every mapping must match a current fictional source line, supplier, and unit.", "code": "invalid_mapping"})
			return
		}
	}
	data, _ := json.Marshal(requirements)
	var id string
	err = tx.QueryRow(r.Context(), `INSERT INTO wb_import_previews(workspace_id,generation,actor_user_id,requirements,connection_id) VALUES($1,$2,$3,$4,$5) RETURNING id::text`, r.PathValue("w"), overview.Generation, session.user.ID, data, connectionID).Scan(&id)
	if err != nil {
		s.domainError(w, err)
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, 201, map[string]any{"id": id, "generation": overview.Generation, "origin": "fictional", "connection_id": connectionID, "requirements": requirements, "row_count": len(requirements), "expires_in_seconds": 3600})
}
func (s *Server) importPublish(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, true)
	if !ok {
		return
	}
	var in struct {
		Generation int64 `json:"generation"`
		Confirm    bool  `json:"confirm"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if !in.Confirm || in.Generation < 1 || !uuidPattern.MatchString(r.PathValue("id")) {
		s.domainError(w, workbench.ErrInvalid)
		return
	}
	if session.user.Role != "owner" {
		s.domainError(w, workbench.ErrNotFound)
		return
	}
	run, err := svc.PublishImport(r.Context(), session.user.ID, r.PathValue("w"), r.PathValue("id"), in.Generation)
	if err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, 202, run)
}
