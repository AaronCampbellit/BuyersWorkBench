package httpapi

import (
	"crypto/hmac"
	"net/http"
	"time"

	"buyersworkbench/internal/auth"
)

func (s *Server) mutationSession(w http.ResponseWriter, r *http.Request) (*authenticatedSession, bool) {
	if !s.validOrigin(w, r) {
		return nil, false
	}
	session, ok := s.authenticate(w, r)
	if !ok {
		return nil, false
	}
	csrf := r.Header.Get("X-CSRF-Token")
	if !validToken(csrf) || !hmac.Equal(tokenHash(csrf), session.csrfHash) {
		fail(w, 403, "Invalid request token.")
		return nil, false
	}
	return session, true
}

func (s *Server) changePassword(w http.ResponseWriter, r *http.Request) {
	session, ok := s.mutationSession(w, r)
	if !ok {
		return
	}
	var in struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if len(in.Current) > 1024 || len(in.New) > 1024 || len(in.New) < 14 || in.New == in.Current {
		fail(w, 422, "Use a different password with 14 to 1024 characters.")
		return
	}
	if !s.throttle.allow(remoteIP(r.RemoteAddr), "password:"+session.user.ID, time.Now()) {
		fail(w, 429, "Too many attempts. Try again in a minute.")
		return
	}
	select {
	case s.loginSlots <- struct{}{}:
		defer func() { <-s.loginSlots }()
	default:
		fail(w, 429, "Password change is busy. Try again shortly.")
		return
	}
	var oldHash string
	err := s.pool.QueryRow(r.Context(), `SELECT password_hash FROM identities WHERE user_id=$1 AND provider='local' AND issuer='local'`, session.user.ID).Scan(&oldHash)
	valid, verifyErr := auth.VerifyPassword(oldHash, in.Current)
	if err != nil || verifyErr != nil || !valid {
		fail(w, 401, "Current password is incorrect.")
		return
	}
	hash, err := auth.HashPassword(in.New)
	if err != nil {
		fail(w, 422, "Use a valid password with at least 14 characters.")
		return
	}
	var changed bool
	if err = s.pool.QueryRow(r.Context(), `SELECT bwb_change_password($1,$2)`, tokenHash(session.token), hash).Scan(&changed); err != nil || !changed {
		fail(w, 503, "Password change is temporarily unavailable.")
		return
	}
	session.user.MustChangePassword = false
	respond(w, 200, map[string]any{"user": session.user, "csrf_token": csrfToken(session.token)})
}

func (s *Server) reauthenticate(w http.ResponseWriter, r *http.Request) {
	session, ok := s.mutationSession(w, r)
	if !ok {
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	if len(in.Password) < 1 || len(in.Password) > 1024 {
		fail(w, 401, "Password is incorrect.")
		return
	}
	if !s.throttle.allow(remoteIP(r.RemoteAddr), "reauth:"+session.user.ID, time.Now()) {
		fail(w, 429, "Too many attempts. Try again in a minute.")
		return
	}
	select {
	case s.loginSlots <- struct{}{}:
		defer func() { <-s.loginSlots }()
	default:
		fail(w, 429, "Sign-in is busy. Try again shortly.")
		return
	}
	var hash string
	err := s.pool.QueryRow(r.Context(), `SELECT password_hash FROM identities WHERE user_id=$1 AND provider='local' AND issuer='local'`, session.user.ID).Scan(&hash)
	valid, verifyErr := auth.VerifyPassword(hash, in.Password)
	if err != nil || verifyErr != nil || !valid {
		fail(w, 401, "Password is incorrect.")
		return
	}
	if _, err = s.pool.Exec(r.Context(), `UPDATE sessions SET reauthenticated_at=now() WHERE token_hash=$1`, tokenHash(session.token)); err != nil {
		fail(w, 503, "Reauthentication is temporarily unavailable.")
		return
	}
	s.audit(r.Context(), session.user.ID, "reauthenticate", "success")
	w.WriteHeader(204)
}
func (s *Server) recentAuthentication(w http.ResponseWriter, r *http.Request, session *authenticatedSession) bool {
	var recent bool
	err := s.pool.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM sessions WHERE token_hash=$1 AND user_id=$2 AND reauthenticated_at>now()-interval '5 minutes' AND revoked_at IS NULL)`, tokenHash(session.token), session.user.ID).Scan(&recent)
	if err != nil {
		fail(w, 503, "Authentication is temporarily unavailable.")
		return false
	}
	if !recent {
		respond(w, 403, map[string]string{"error": "Confirm your password before approving private data sharing.", "code": "reauthentication_required"})
		return false
	}
	return true
}
