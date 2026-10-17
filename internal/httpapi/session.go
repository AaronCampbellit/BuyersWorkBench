package httpapi

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"time"
)

const (
	cookieName       = "bwb_session"
	absoluteLifetime = 12 * time.Hour
	idleLifetime     = 30 * time.Minute
)

func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func tokenHash(token string) []byte { h := sha256.Sum256([]byte(token)); return h[:] }

// The CSRF token is independently domain-separated from the opaque cookie.
// Knowing it cannot be used to authenticate or recover the session cookie.
func csrfToken(token string) string {
	h := hmac.New(sha256.New, []byte(token))
	h.Write([]byte("buyersworkbench csrf v1"))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

func validToken(token string) bool {
	b, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil && len(b) == 32 && base64.RawURLEncoding.EncodeToString(b) == token
}

func (s *Server) setCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/api", HttpOnly: true,
		Secure: s.config.SecureCookie, SameSite: http.SameSiteStrictMode, MaxAge: int(absoluteLifetime.Seconds()), Expires: time.Now().Add(absoluteLifetime)})
}

func (s *Server) clearCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: "", Path: "/api", HttpOnly: true,
		Secure: s.config.SecureCookie, SameSite: http.SameSiteStrictMode, MaxAge: -1, Expires: time.Unix(1, 0)})
}
