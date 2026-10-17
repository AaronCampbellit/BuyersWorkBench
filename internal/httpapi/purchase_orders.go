package httpapi

import (
	"net/http"

	"buyersworkbench/internal/domain"
	"buyersworkbench/internal/workbench"
)

func (s *Server) registerPurchaseOrders(mux *http.ServeMux) {
	s.route(mux, "GET /api/v1/workspaces/{w}/purchase-orders", s.purchaseOrderList)
	s.route(mux, "GET /api/v1/workspaces/{w}/purchase-orders/{id}", s.purchaseOrderDetail)
	s.route(mux, "POST /api/v1/workspaces/{w}/purchase-orders", s.purchaseOrderCreate)
	s.route(mux, "PATCH /api/v1/workspaces/{w}/purchase-orders/{id}", s.purchaseOrderUpdate)
	s.route(mux, "POST /api/v1/workspaces/{w}/purchase-orders/{id}/actions", s.purchaseOrderAction)
	s.route(mux, "POST /api/v1/workspaces/{w}/quotes/{id}/purchase-order", s.purchaseOrderFromQuote)
}

func (s *Server) purchaseOrderList(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, false)
	if !ok {
		return
	}
	view, err := svc.Overview(r.Context(), session.user.ID, r.PathValue("w"))
	if err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, http.StatusOK, map[string]any{"generation": view.Generation, "purchase_orders": view.PurchaseOrders})
}

func (s *Server) purchaseOrderDetail(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, false)
	if !ok {
		return
	}
	detail, err := svc.PurchaseOrder(r.Context(), session.user.ID, r.PathValue("w"), r.PathValue("id"))
	if err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, http.StatusOK, detail)
}

func (s *Server) purchaseOrderCreate(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, true)
	if !ok {
		return
	}
	var in struct {
		Generation     int64  `json:"generation"`
		IdempotencyKey string `json:"idempotency_key"`
		domain.PurchaseOrderInput
	}
	if !decodeJSONLimit(w, r, &in, 32*1024) {
		return
	}
	if in.Generation < 1 {
		s.domainError(w, workbench.ErrInvalid)
		return
	}
	order, err := svc.CreatePurchaseOrder(r.Context(), session.user.ID, r.PathValue("w"), in.Generation, in.IdempotencyKey, in.PurchaseOrderInput)
	if err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, http.StatusCreated, order)
}

func (s *Server) purchaseOrderUpdate(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, true)
	if !ok {
		return
	}
	var in struct {
		Generation int64 `json:"generation"`
		Revision   int64 `json:"revision"`
		domain.PurchaseOrderInput
	}
	if !decodeJSONLimit(w, r, &in, 32*1024) {
		return
	}
	if in.Generation < 1 || in.Revision < 1 {
		s.domainError(w, workbench.ErrInvalid)
		return
	}
	order, err := svc.UpdatePurchaseOrder(r.Context(), session.user.ID, r.PathValue("w"), r.PathValue("id"), in.Generation, in.Revision, in.PurchaseOrderInput)
	if err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, http.StatusOK, order)
}

func (s *Server) purchaseOrderAction(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, true)
	if !ok {
		return
	}
	var in struct {
		Generation     int64  `json:"generation"`
		Revision       int64  `json:"revision"`
		IdempotencyKey string `json:"idempotency_key"`
		domain.PurchaseOrderActionInput
	}
	if !decodeJSONLimit(w, r, &in, 32*1024) {
		return
	}
	if in.Generation < 1 || in.Revision < 1 {
		s.domainError(w, workbench.ErrInvalid)
		return
	}
	order, err := svc.ActOnPurchaseOrder(r.Context(), session.user.ID, r.PathValue("w"), r.PathValue("id"), in.Generation, in.Revision, in.IdempotencyKey, in.PurchaseOrderActionInput)
	if err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, http.StatusOK, order)
}

func (s *Server) purchaseOrderFromQuote(w http.ResponseWriter, r *http.Request) {
	session, svc, ok := s.workbenchSession(w, r, true)
	if !ok {
		return
	}
	var in struct {
		Generation     int64  `json:"generation"`
		Revision       int64  `json:"revision"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if in.Generation < 1 || in.Revision < 1 {
		s.domainError(w, workbench.ErrInvalid)
		return
	}
	order, err := svc.CreatePurchaseOrderFromQuote(r.Context(), session.user.ID, r.PathValue("w"), r.PathValue("id"), in.Generation, in.Revision, in.IdempotencyKey)
	if err != nil {
		s.domainError(w, err)
		return
	}
	respond(w, http.StatusCreated, order)
}
