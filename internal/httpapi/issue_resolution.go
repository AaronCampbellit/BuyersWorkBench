package httpapi

import (
	"net/http"

	"buyersworkbench/internal/workbench"
)

func (s *Server) registerIssueResolution(mux *http.ServeMux) {
	s.route(mux, "POST /api/v1/workspaces/{w}/exceptions/{id}/resolve", s.issueResolve)
	s.route(mux, "POST /api/v1/workspaces/{w}/exceptions/{id}/reopen", s.issueReopen)
}

func (s *Server) issueResolve(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, true)
	if !ok {
		return
	}
	var in struct {
		Generation     int64  `json:"generation"`
		Revision       int64  `json:"revision"`
		IdempotencyKey string `json:"idempotency_key"`
		workbench.IssueResolutionInput
	}
	if !decodeJSONLimit(w, r, &in, 64*1024) {
		return
	}
	if in.Generation < 1 || in.Revision < 1 {
		s.domainError(w, workbench.ErrInvalid)
		return
	}
	if in.IdempotencyKey == "" {
		in.IdempotencyKey = r.Header.Get("Idempotency-Key")
	}
	result, err := svc.ResolveIssue(r.Context(), session.user.ID, r.PathValue("w"), r.PathValue("id"), in.Generation, in.Revision, in.IdempotencyKey, in.IssueResolutionInput)
	if err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, http.StatusOK, result)
}

func (s *Server) issueReopen(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, true)
	if !ok {
		return
	}
	var in struct {
		Generation     int64  `json:"generation"`
		Revision       int64  `json:"revision"`
		IdempotencyKey string `json:"idempotency_key"`
		Reason         string `json:"reason"`
	}
	if !decodeJSONLimit(w, r, &in, 32*1024) {
		return
	}
	if in.Generation < 1 || in.Revision < 1 {
		s.domainError(w, workbench.ErrInvalid)
		return
	}
	if in.IdempotencyKey == "" {
		in.IdempotencyKey = r.Header.Get("Idempotency-Key")
	}
	result, err := svc.ReopenIssue(r.Context(), session.user.ID, r.PathValue("w"), r.PathValue("id"), in.Generation, in.Revision, in.IdempotencyKey, in.Reason)
	if err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, http.StatusOK, result)
}
