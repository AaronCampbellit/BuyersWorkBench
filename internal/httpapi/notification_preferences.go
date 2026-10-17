package httpapi

import (
	"net/http"

	"buyersworkbench/internal/workbench"
)

func (s *Server) registerNotificationRoutes(mux *http.ServeMux) {
	s.route(mux, "GET /api/v1/workspaces/{w}/notification-preferences", s.getNotificationPreferences)
	s.route(mux, "PATCH /api/v1/workspaces/{w}/notification-preferences", s.setNotificationPreferences)
	s.route(mux, "GET /api/v1/workspaces/{w}/members", s.workspaceMembers)
}

func (s *Server) getNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, false)
	if !ok {
		return
	}
	preferences, err := svc.GetNotificationPreferences(r.Context(), session.user.ID, r.PathValue("w"))
	if err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, http.StatusOK, preferences)
}

func (s *Server) setNotificationPreferences(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, true)
	if !ok {
		return
	}
	var in struct {
		Generation         int64  `json:"generation"`
		Revision           *int64 `json:"revision"`
		StatusChanges      *bool  `json:"status_changes"`
		SupplierConditions *bool  `json:"supplier_conditions"`
	}
	if !decodeJSONLimit(w, r, &in, 8*1024) {
		return
	}
	if in.Generation < 1 || in.Revision == nil || *in.Revision < 0 || in.StatusChanges == nil || in.SupplierConditions == nil {
		s.domainError(w, workbench.ErrInvalid)
		return
	}
	preferences, err := svc.SetNotificationPreferences(r.Context(), session.user.ID, r.PathValue("w"), in.Generation, *in.Revision,
		workbench.NotificationPreferencesInput{StatusChanges: *in.StatusChanges, SupplierConditions: *in.SupplierConditions})
	if err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, http.StatusOK, preferences)
}

func (s *Server) workspaceMembers(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, false)
	if !ok {
		return
	}
	members, err := svc.WorkspaceMembers(r.Context(), session.user.ID, r.PathValue("w"))
	if err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"members": members})
}
