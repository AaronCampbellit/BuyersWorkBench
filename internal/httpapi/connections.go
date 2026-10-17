package httpapi

import (
	"net/http"
	"strings"

	"buyersworkbench/internal/domain"
	"buyersworkbench/internal/workbench"
)

func (s *Server) registerConnections(mux *http.ServeMux) {
	s.route(mux, "POST /api/v1/workspaces/{w}/connections", s.createConnection)
	s.route(mux, "PATCH /api/v1/workspaces/{w}/connections/{id}", s.updateConnection)
	s.route(mux, "POST /api/v1/workspaces/{w}/connections/{id}/refresh", s.connectionOperation)
	s.route(mux, "PATCH /api/v1/workspaces/{w}/connections/{id}/schedule", s.connectionOperation)
	s.route(mux, "PATCH /api/v1/workspaces/{w}/connections/{id}/pause", s.connectionOperation)
	s.route(mux, "POST /api/v1/workspaces/{w}/connections/{id}/source", s.stageConnectionSource)
}

func (s *Server) stageConnectionSource(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, true)
	if !ok {
		return
	}
	var in struct {
		Generation   int64                `json:"generation"`
		Revision     int64                `json:"revision"`
		Key          string               `json:"idempotency_key"`
		Records      *[]domain.Record     `json:"records"`
		Requirements []domain.Requirement `json:"requirements"`
	}
	if !decodeJSONLimit(w, r, &in, 4*1024*1024) {
		return
	}
	if in.Key == "" {
		in.Key = r.Header.Get("Idempotency-Key")
	}
	if in.Generation < 1 || in.Revision < 1 || in.Records == nil || len(in.Key) < 8 || len(in.Key) > 128 {
		s.domainError(w, workbench.ErrInvalid)
		return
	}
	result, err := svc.StageConnectionSource(r.Context(), session.user.ID, r.PathValue("w"), r.PathValue("id"), in.Generation, in.Revision, in.Key, *in.Records, in.Requirements)
	if err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, http.StatusAccepted, result)
}

func (s *Server) createConnection(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, true)
	if !ok {
		return
	}
	var in struct {
		Generation int64  `json:"generation"`
		Key        string `json:"idempotency_key"`
		workbench.ConnectionInput
	}
	if !decodeJSONLimit(w, r, &in, 8192) {
		return
	}
	if in.Key == "" {
		in.Key = r.Header.Get("Idempotency-Key")
	}
	if in.Generation < 1 || len(in.Key) < 8 || len(in.Key) > 128 {
		s.domainError(w, workbench.ErrInvalid)
		return
	}
	result, err := svc.CreateConnection(r.Context(), session.user.ID, r.PathValue("w"), in.Generation, in.Key, in.ConnectionInput)
	if err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, http.StatusCreated, result)
}

func (s *Server) updateConnection(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, true)
	if !ok {
		return
	}
	var in struct {
		Generation int64 `json:"generation"`
		Revision   int64 `json:"revision"`
		workbench.ConnectionInput
	}
	if !decodeJSONLimit(w, r, &in, 8192) {
		return
	}
	if in.Generation < 1 || in.Revision < 1 {
		s.domainError(w, workbench.ErrInvalid)
		return
	}
	result, err := svc.UpdateConnection(r.Context(), session.user.ID, r.PathValue("w"), r.PathValue("id"), in.Generation, in.Revision, in.ConnectionInput)
	if err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, http.StatusOK, result)
}

func (s *Server) connectionOperation(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, true)
	if !ok {
		return
	}
	var in struct {
		Generation int64 `json:"generation"`
		Revision   int64 `json:"revision"`
		Enabled    *bool `json:"enabled"`
		Paused     *bool `json:"paused"`
		Cadence    int   `json:"cadence_seconds"`
	}
	if !decodeJSONLimit(w, r, &in, 4096) {
		return
	}
	if in.Generation < 1 || (!strings.HasSuffix(r.URL.Path, "/refresh") && in.Revision < 1) {
		s.domainError(w, workbench.ErrInvalid)
		return
	}
	actor, workspace, id := session.user.ID, r.PathValue("w"), r.PathValue("id")
	var result any
	var err error
	status := http.StatusOK
	switch {
	case strings.HasSuffix(r.URL.Path, "/refresh"):
		status = http.StatusAccepted
		result, err = svc.RefreshConnection(r.Context(), actor, workspace, id, in.Generation)
	case strings.HasSuffix(r.URL.Path, "/schedule"):
		if in.Enabled == nil {
			s.domainError(w, workbench.ErrInvalid)
			return
		}
		result, err = svc.SetConnectionSchedule(r.Context(), actor, workspace, id, in.Generation, in.Revision, *in.Enabled, in.Cadence)
	case strings.HasSuffix(r.URL.Path, "/pause"):
		if in.Paused == nil {
			s.domainError(w, workbench.ErrInvalid)
			return
		}
		result, err = svc.PauseConnection(r.Context(), actor, workspace, id, in.Generation, in.Revision, *in.Paused)
	}
	if err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, status, result)
}
