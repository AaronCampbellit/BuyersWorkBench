package httpapi

import "net/http"

type actorContextKey struct{}
type auditedResponse struct {
	http.ResponseWriter
	status int
}

func (w *auditedResponse) WriteHeader(code int) {
	if w.status != 0 {
		return
	}
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
func (w *auditedResponse) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.WriteHeader(200)
	}
	return w.ResponseWriter.Write(data)
}
func (w *auditedResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) auditDenied(r *http.Request, actor string, status int) {
	var target any
	kind := "api"
	if id := r.PathValue("w"); uuidPattern.MatchString(id) {
		target = id
		kind = "workspace"
	}
	_, err := s.pool.Exec(r.Context(), `INSERT INTO audit_events(id,actor_user_id,action,target_type,target_id,outcome,metadata) VALUES(gen_random_uuid(),$1,'request.denied',$2,$3,'denied',jsonb_build_object('status',$4::integer))`, actor, kind, target, status)
	if err != nil {
		s.logger.Warn("access denial audit write failed")
	}
}
