package httpapi

import (
	"errors"
	"net/http"
	"strings"

	"buyersworkbench/internal/domain"
	"buyersworkbench/internal/workbench"
)

func (s *Server) registerWorkbench(mux *http.ServeMux) {
	s.registerSuppliers(mux)
	s.registerPurchaseOrders(mux)
	s.registerIssueResolution(mux)
	s.registerConnections(mux)
	s.registerNotificationRoutes(mux)
	s.route(mux, "GET /api/v1/workspaces/{w}/overview", s.workbenchOverview)
	s.route(mux, "GET /api/v1/workspaces/{w}/orders/{id}", s.workbenchOrder)
	s.route(mux, "GET /api/v1/workspaces/{w}/orders/{id}/history", s.workbenchOrder)
	for _, view := range []string{"priorities", "orders", "suppliers", "connections", "notifications", "quotes", "runs"} {
		s.route(mux, "GET /api/v1/workspaces/{w}/"+view, s.workbenchList)
	}
	s.route(mux, "GET /api/v1/workspaces/{w}/runs/{id}", s.workbenchRun)
	for _, operation := range []string{"seed", "refresh", "demo/reset", "demo/scenarios/{key}", "schedule", "notifications/{id}/read", "exceptions/{id}/notes"} {
		s.route(mux, "POST /api/v1/workspaces/{w}/"+operation, s.workbenchPost)
	}
	s.route(mux, "PATCH /api/v1/workspaces/{w}/exceptions/{id}/workflow", s.workbenchWorkflow)
	s.route(mux, "PATCH /api/v1/workspaces/{w}/quotes/{id}", s.workbenchQuote)
	s.route(mux, "PATCH /api/v1/workspaces/{w}/schedule", s.workbenchPost)
}

func (s *Server) workbenchSession(w http.ResponseWriter, r *http.Request, mutate bool) (*authenticatedSession, *workbench.Service, bool) {
	var session *authenticatedSession
	var ok bool
	if mutate {
		session, ok = s.mutationSession(w, r)
	} else {
		session, ok = s.authenticate(w, r)
	}
	if !ok {
		return nil, nil, false
	}
	if !uuidPattern.MatchString(r.PathValue("w")) {
		s.domainError(w, workbench.ErrNotFound)
		return nil, nil, false
	}
	return session, workbench.New(s.pool), true
}
func (s *Server) domainError(w http.ResponseWriter, err error) {
	code, status, message := "service_unavailable", 503, "Workbench is temporarily unavailable."
	switch {
	case errors.Is(err, workbench.ErrNotFound):
		code, status, message = "not_found", 404, "Record not found."
	case errors.Is(err, workbench.ErrStale):
		code, status, message = "stale_generation_or_revision", 409, "The workspace changed. Reload and try again."
	case errors.Is(err, workbench.ErrPrivate):
		code, status, message = "fictional_workspace_required", 422, "This operation requires a fictional workspace."
	case errors.Is(err, workbench.ErrResolutionRequired):
		code, status, message = "resolution_required", 422, "Use Resolve issue to record the outcome and supporting reference. Use Reopen issue to resume work."
	case errors.Is(err, workbench.ErrVerificationRequired):
		code, status, message = "verification_required", 422, "Current source evidence is incomplete. Refresh or correct the source before resolving this issue."
	case errors.Is(err, workbench.ErrInvalid):
		code, status, message = "invalid_input", 422, "Check the supplied values and try again."
	default:
		s.logger.Error("workbench operation failed", "code", code)
	}
	correlation, _ := randomToken()
	respond(w, status, map[string]any{"error": message, "code": code, "correlation_id": correlation, "retryable": status == 503})
}
func (s *Server) workbenchOverview(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, false)
	if !ok {
		return
	}
	result, err := svc.Overview(r.Context(), session.user.ID, r.PathValue("w"))
	if err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, 200, result)
}
func (s *Server) workbenchOrder(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, false)
	if !ok {
		return
	}
	if !uuidPattern.MatchString(r.PathValue("id")) {
		s.domainError(w, workbench.ErrNotFound)
		return
	}
	result, err := svc.Order(r.Context(), session.user.ID, r.PathValue("w"), r.PathValue("id"))
	if err != nil {
		s.domainError(w, err)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/history") {
		respond(w, 200, map[string]any{"history": result.History})
		return
	}
	respond(w, 200, result)
}
func (s *Server) workbenchList(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, false)
	if !ok {
		return
	}
	result, err := svc.Overview(r.Context(), session.user.ID, r.PathValue("w"))
	if err != nil {
		s.domainError(w, err)
		return
	}
	view := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	if view == "quotes" {
		respond(w, 200, result.Quotes)
		return
	}
	s.pagedWorkbenchList(w, r, session, result, view)
}
func (s *Server) workbenchRun(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, false)
	if !ok {
		return
	}
	result, err := svc.Run(r.Context(), session.user.ID, r.PathValue("w"), r.PathValue("id"))
	if err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, 200, result)
}
func (s *Server) workbenchPost(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, true)
	if !ok {
		return
	}
	var in struct {
		Generation     int64  `json:"generation"`
		Revision       int64  `json:"revision"`
		IdempotencyKey string `json:"idempotency_key"`
		Confirm        bool   `json:"confirm"`
		Body           string `json:"body"`
		Enabled        bool   `json:"enabled"`
		Cadence        int    `json:"cadence_seconds"`
	}
	requestLimit := int64(4096)
	if strings.HasSuffix(r.URL.Path, "/notes") {
		requestLimit = 32 * 1024
	}
	if !decodeJSONLimit(w, r, &in, requestLimit) {
		return
	}
	if in.Generation < 1 {
		s.domainError(w, workbench.ErrInvalid)
		return
	}
	if in.IdempotencyKey == "" {
		in.IdempotencyKey = r.Header.Get("Idempotency-Key")
	}
	actor, workspace := session.user.ID, r.PathValue("w")
	var result any
	var err error
	status := 202
	switch {
	case strings.HasSuffix(r.URL.Path, "/seed"):
		result, err = svc.Seed(r.Context(), actor, workspace, in.Generation)
	case strings.HasSuffix(r.URL.Path, "/refresh"):
		result, err = svc.Refresh(r.Context(), actor, workspace, in.Generation)
	case strings.HasSuffix(r.URL.Path, "/demo/reset"):
		if !in.Confirm || len(in.IdempotencyKey) < 8 || len(in.IdempotencyKey) > 128 {
			s.domainError(w, workbench.ErrInvalid)
			return
		}
		result, err = svc.Reset(r.Context(), actor, workspace, in.Generation, in.IdempotencyKey)
	case r.PathValue("key") != "":
		if len(in.IdempotencyKey) < 8 || len(in.IdempotencyKey) > 128 {
			s.domainError(w, workbench.ErrInvalid)
			return
		}
		result, err = svc.Scenario(r.Context(), actor, workspace, in.Generation, r.PathValue("key"), in.IdempotencyKey)
	case strings.HasSuffix(r.URL.Path, "/schedule"):
		status = 200
		result, err = svc.SetSchedule(r.Context(), actor, workspace, in.Generation, in.Enabled, in.Cadence)
	case strings.HasSuffix(r.URL.Path, "/read"):
		status = 204
		err = svc.MarkNotificationRead(r.Context(), actor, workspace, r.PathValue("id"), in.Generation)
	case strings.HasSuffix(r.URL.Path, "/notes"):
		status = 201
		result, err = svc.AddNote(r.Context(), actor, workspace, r.PathValue("id"), in.Generation, in.Revision, in.Body)
	}
	if err != nil {
		s.domainError(w, err)
		return
	}
	if status == 204 {
		w.WriteHeader(status)
		return
	}
	respond(w, status, result)
}
func (s *Server) workbenchWorkflow(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, true)
	if !ok {
		return
	}
	var in struct {
		Generation int64 `json:"generation"`
		Revision   int64 `json:"revision"`
		workbench.WorkflowInput
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	result, err := svc.UpdateWorkflow(r.Context(), session.user.ID, r.PathValue("w"), r.PathValue("id"), in.Generation, in.Revision, in.WorkflowInput)
	if err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, 200, result)
}
func (s *Server) workbenchQuote(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, true)
	if !ok {
		return
	}
	var in struct {
		Generation int64        `json:"generation"`
		Revision   int64        `json:"revision"`
		Quote      domain.Quote `json:"quote"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Quote.ID != r.PathValue("id") {
		s.domainError(w, workbench.ErrInvalid)
		return
	}
	result, err := svc.UpdateQuote(r.Context(), session.user.ID, r.PathValue("w"), r.PathValue("id"), in.Generation, in.Revision, in.Quote)
	if err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, 200, result)
}
