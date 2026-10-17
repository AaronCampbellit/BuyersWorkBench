package httpapi

import (
	"context"
	"crypto/hmac"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"
	"time"

	"buyersworkbench/internal/auth"
	"buyersworkbench/internal/store"
	"buyersworkbench/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Server struct {
	operations map[string]map[string]any
	pool       *pgxpool.Pool
	config     Config
	logger     *slog.Logger
	dummyHash  string
	throttle   loginThrottle
	loginSlots chan struct{}
}

type User struct {
	ID                 string `json:"id"`
	DisplayName        string `json:"display_name"`
	Email              string `json:"email"`
	Role               string `json:"role"`
	MustChangePassword bool   `json:"must_change_password"`
}

type authenticatedSession struct {
	user     User
	token    string
	csrfHash []byte
}

func New(pool *pgxpool.Pool, config Config, logger *slog.Logger) (http.Handler, error) {
	if config.Origin == "" {
		return nil, errors.New("application origin required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	dummyHash, err := auth.HashPassword("unusable-dummy-password-for-constant-cost-verification")
	if err != nil {
		return nil, errors.New("initialize password verifier")
	}
	s := &Server{pool: pool, config: config, logger: logger, dummyHash: dummyHash, loginSlots: make(chan struct{}, 4), operations: map[string]map[string]any{}}
	mux := http.NewServeMux()
	s.route(mux, "GET /healthz", s.health)
	s.route(mux, "GET /readyz", s.ready)
	s.route(mux, "GET /api/session", s.session)
	s.route(mux, "POST /api/login", s.login)
	s.route(mux, "POST /api/logout", s.logout)
	s.route(mux, "POST /api/password", s.changePassword)
	s.route(mux, "POST /api/reauth", s.reauthenticate)
	s.route(mux, "GET /api/workspaces", s.workspaces)
	s.route(mux, "GET /api/workspaces/{id}", s.workspace)
	s.route(mux, "GET /api/foundation", s.foundation)
	s.registerWorkbench(mux)
	s.registerImports(mux)
	s.registerSnapshots(mux)
	s.route(mux, "GET /api/v1/openapi.json", s.openAPI)
	s.route(mux, "/", func(w http.ResponseWriter, r *http.Request) { fail(w, http.StatusNotFound, "Not found.") })
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		request := r.WithContext(ctx)
		response := &auditedResponse{ResponseWriter: w}
		mux.ServeHTTP(response, request)
		if response.status == 401 || response.status == 403 || response.status == 404 {
			if actor, ok := request.Context().Value(actorContextKey{}).(string); ok {
				s.auditDenied(request, actor, response.status)
			}
		}
	}), nil
}

func respond(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}
func fail(w http.ResponseWriter, status int, message string) {
	respond(w, status, map[string]string{"error": message})
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	respond(w, 200, map[string]string{"status": "ok"})
}
func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if s.pool == nil || s.pool.Ping(ctx) != nil || migrations.Check(ctx, s.pool) != nil || store.CheckQueue(ctx, s.pool) != nil {
		respond(w, 503, map[string]string{"status": "not_ready"})
		return
	}
	respond(w, 200, map[string]string{"status": "ready"})
}

func (s *Server) validOrigin(w http.ResponseWriter, r *http.Request) bool {
	if !s.config.AllowsOrigin(r.Header.Get("Origin")) {
		s.audit(r.Context(), "", "origin.denied", "denied")
		fail(w, http.StatusForbidden, "Request origin is not allowed.")
		return false
	}
	return true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	return decodeJSONLimit(w, r, dst, 4096)
}
func decodeJSONLimit(w http.ResponseWriter, r *http.Request, dst any, limit int64) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		fail(w, 415, "Content-Type must be application/json.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		fail(w, 400, "Invalid request.")
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		fail(w, 400, "Invalid request.")
		return false
	}
	return true
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if !s.validOrigin(w, r) {
		return
	}
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	subject := strings.ToLower(strings.TrimSpace(in.Email))
	if len(subject) > 254 || len(subject) == 0 || len(in.Password) > 1024 || len(in.Password) == 0 {
		fail(w, 401, "Email or password is incorrect.")
		return
	}
	if !s.throttle.allow(remoteIP(r.RemoteAddr), subject, time.Now()) {
		w.Header().Set("Retry-After", "60")
		s.audit(r.Context(), "", "login", "denied")
		fail(w, 429, "Too many attempts. Try again in a minute.")
		return
	}
	select {
	case s.loginSlots <- struct{}{}:
		defer func() { <-s.loginSlots }()
	default:
		w.Header().Set("Retry-After", "5")
		fail(w, 429, "Sign-in is busy. Try again shortly.")
		return
	}
	if s.pool == nil {
		fail(w, 503, "Service is not ready.")
		return
	}
	var user User
	var hash string
	var isOwner bool
	err := s.pool.QueryRow(r.Context(), `SELECT u.id::text,u.display_name,i.subject,u.is_owner,i.password_hash,i.must_change_password
		FROM users u JOIN identities i ON i.user_id=u.id
		WHERE i.provider='local' AND i.issuer='local' AND i.subject=$1 AND u.active AND u.disabled_at IS NULL AND (u.expires_at IS NULL OR u.expires_at>now())`, subject).
		Scan(&user.ID, &user.DisplayName, &user.Email, &isOwner, &hash, &user.MustChangePassword)
	found := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		s.logger.Error("login database operation failed")
		fail(w, 503, "Sign-in is temporarily unavailable.")
		return
	}
	if !found {
		hash = s.dummyHash
	}
	valid, verifyErr := auth.VerifyPassword(hash, in.Password)
	if !found || !valid || verifyErr != nil {
		s.audit(r.Context(), "", "login", "denied")
		fail(w, 401, "Email or password is incorrect.")
		return
	}
	user.Role = "reviewer"
	if isOwner {
		user.Role = "owner"
	}
	token, err := randomToken()
	if err != nil {
		fail(w, 503, "Sign-in is temporarily unavailable.")
		return
	}
	csrf := csrfToken(token)
	tx, err := s.pool.Begin(r.Context())
	if err != nil {
		fail(w, 503, "Sign-in is temporarily unavailable.")
		return
	}
	defer tx.Rollback(r.Context())
	if cookie, err := r.Cookie(cookieName); err == nil && validToken(cookie.Value) {
		_, err = tx.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE token_hash=$1 AND revoked_at IS NULL`, tokenHash(cookie.Value))
		if err != nil {
			fail(w, 503, "Sign-in is temporarily unavailable.")
			return
		}
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO sessions(token_hash,user_id,csrf_token_hash,created_at,expires_at,last_activity_at)
		VALUES($1,$2,$3,now(),now()+interval '12 hours',now())`, tokenHash(token), user.ID, tokenHash(csrf))
	if err != nil || tx.Commit(r.Context()) != nil {
		fail(w, 503, "Sign-in is temporarily unavailable.")
		return
	}
	s.setCookie(w, token)
	s.audit(r.Context(), user.ID, "login", "success")
	respond(w, 200, map[string]any{"user": user, "csrf_token": csrf})
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (*authenticatedSession, bool) {
	cookie, err := r.Cookie(cookieName)
	if err != nil || !validToken(cookie.Value) {
		fail(w, 401, "Authentication required.")
		return nil, false
	}
	if s.pool == nil {
		fail(w, 503, "Service is not ready.")
		return nil, false
	}
	var session authenticatedSession
	var isOwner bool
	err = s.pool.QueryRow(r.Context(), `UPDATE sessions s SET last_activity_at=now()
		FROM users u JOIN identities i ON i.user_id=u.id AND i.provider='local' AND i.issuer='local'
		WHERE s.token_hash=$1 AND s.user_id=u.id AND u.active AND u.disabled_at IS NULL
		AND (u.expires_at IS NULL OR u.expires_at>now())
		AND s.revoked_at IS NULL AND s.expires_at>now()
		AND s.created_at>now()-interval '12 hours' AND s.last_activity_at>now()-interval '30 minutes'
		RETURNING u.id::text,u.display_name,i.subject,u.is_owner,s.csrf_token_hash,i.must_change_password`, tokenHash(cookie.Value)).
		Scan(&session.user.ID, &session.user.DisplayName, &session.user.Email, &isOwner, &session.csrfHash, &session.user.MustChangePassword)
	if errors.Is(err, pgx.ErrNoRows) {
		s.clearCookie(w)
		fail(w, 401, "Authentication required.")
		return nil, false
	}
	if err != nil {
		s.logger.Error("session database operation failed")
		fail(w, 503, "Service is temporarily unavailable.")
		return nil, false
	}
	session.token = cookie.Value
	session.user.Role = "reviewer"
	if isOwner {
		session.user.Role = "owner"
	}
	*r = *r.WithContext(context.WithValue(r.Context(), actorContextKey{}, session.user.ID))
	if session.user.MustChangePassword && r.URL.Path != "/api/session" && r.URL.Path != "/api/password" && r.URL.Path != "/api/logout" {
		respond(w, 403, map[string]any{"error": "Change your temporary password before continuing.", "code": "password_change_required"})
		return nil, false
	}
	return &session, true
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	session, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	respond(w, 200, map[string]any{"user": session.user, "csrf_token": csrfToken(session.token)})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !s.validOrigin(w, r) {
		return
	}
	session, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	csrf := r.Header.Get("X-CSRF-Token")
	if !validToken(csrf) || !hmac.Equal(tokenHash(csrf), session.csrfHash) {
		fail(w, 403, "Invalid request token.")
		return
	}
	_, err := s.pool.Exec(r.Context(), `UPDATE sessions SET revoked_at=now() WHERE token_hash=$1`, tokenHash(session.token))
	if err != nil {
		fail(w, 503, "Sign-out is temporarily unavailable.")
		return
	}
	s.clearCookie(w)
	s.audit(r.Context(), session.user.ID, "logout", "success")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) audit(ctx context.Context, userID, action, outcome string) {
	if s.pool == nil {
		return
	}
	var actor any
	if userID != "" {
		actor = userID
	}
	_, err := s.pool.Exec(ctx, `INSERT INTO audit_events(id,actor_user_id,action,target_type,outcome) VALUES(gen_random_uuid(),$1,$2,'session',$3)`, actor, action, outcome)
	if err != nil {
		s.logger.Warn("authentication audit write failed")
	}
}
