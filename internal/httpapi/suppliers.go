package httpapi

import (
	"net/http"

	"buyersworkbench/internal/workbench"
)

func (s *Server) registerSuppliers(mux *http.ServeMux) {
	s.route(mux, "POST /api/v1/workspaces/{w}/suppliers", s.createSupplier)
	s.route(mux, "PATCH /api/v1/workspaces/{w}/suppliers/{id}", s.updateSupplier)
}

func (s *Server) createSupplier(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, true)
	if !ok {
		return
	}
	var in struct {
		Generation int64 `json:"generation"`
		workbench.SupplierInput
	}
	if !decodeJSONLimit(w, r, &in, 32*1024) {
		return
	}
	if in.Generation < 1 {
		s.domainError(w, workbench.ErrInvalid)
		return
	}
	supplier, err := svc.CreateSupplier(r.Context(), session.user.ID, r.PathValue("w"), in.Generation, in.SupplierInput)
	if err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, http.StatusCreated, supplier)
}

func (s *Server) updateSupplier(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, true)
	if !ok {
		return
	}
	var in struct {
		Generation int64 `json:"generation"`
		Revision   int64 `json:"revision"`
		workbench.SupplierInput
	}
	if !decodeJSONLimit(w, r, &in, 32*1024) {
		return
	}
	if in.Generation < 1 || in.Revision < 1 {
		s.domainError(w, workbench.ErrInvalid)
		return
	}
	supplier, err := svc.UpdateSupplier(r.Context(), session.user.ID, r.PathValue("w"), r.PathValue("id"), in.Generation, in.Revision, in.SupplierInput)
	if err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, http.StatusOK, supplier)
}
