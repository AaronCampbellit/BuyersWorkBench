package httpapi

import (
	"bytes"
	"crypto/hmac"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testHandler(t *testing.T) http.Handler {
	t.Helper()
	h, err := New(nil, Config{Origin: "http://127.0.0.1:4180"}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestOriginAndPrivateRoutes(t *testing.T) {
	h := testHandler(t)
	cases := []struct {
		method, path, origin, body string
		status                     int
	}{
		{"GET", "/healthz", "", "", 200},
		{"GET", "/readyz", "", "", 503},
		{"GET", "/api/session", "", "", 401},
		{"GET", "/api/workspaces", "", "", 401},
		{"GET", "/api/workspaces/00000000-0000-0000-0000-000000000000", "", "", 401},
		{"GET", "/api/foundation", "", "", 401},
		{"POST", "/api/login", "", "{}", 403},
		{"POST", "/api/login", "http://evil.test", "{}", 403},
		{"POST", "/api/login", "http://127.0.0.1:4180.evil.test", "{}", 403},
		{"POST", "/api/logout", "http://evil.test", "{}", 403},
		{"POST", "/api/login", "http://127.0.0.1:4180", `{"email":"u@example.test","password":"x","role":"owner"}`, 400},
		{"POST", "/api/login", "http://127.0.0.1:4180", `{"email":"u@example.test","password":"x"} {}`, 400},
		{"POST", "/api/login", "http://127.0.0.1:4180", strings.Repeat(" ", 5000) + "{}", 400},
		{"POST", "/api/register", "http://127.0.0.1:4180", "{}", 404},
		{"POST", "/api/signup", "http://127.0.0.1:4180", "{}", 404},
		{"GET", "/api/orders", "", "", 404},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%s %s %s %d", c.method, c.path, c.origin, c.status), func(t *testing.T) {
			r := httptest.NewRequest(c.method, c.path, strings.NewReader(c.body))
			r.Header.Set("Origin", c.origin)
			r.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != c.status {
				t.Fatalf("got status %d want %d: %s", w.Code, c.status, w.Body.String())
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("sensitive responses must not be cached")
			}
		})
	}
}

func TestCookieAndCSRF(t *testing.T) {
	token, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	other, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	if !validToken(token) || !validToken(csrfToken(token)) || token == csrfToken(token) {
		t.Fatal("invalid token separation")
	}
	if hmac.Equal(tokenHash(csrfToken(token)), tokenHash(csrfToken(other))) {
		t.Fatal("CSRF token shared across sessions")
	}
	if bytes.Equal(tokenHash(token), []byte(token)) {
		t.Fatal("server must store a digest")
	}
	for _, secure := range []bool{false, true} {
		s := &Server{config: Config{SecureCookie: secure}}
		w := httptest.NewRecorder()
		s.setCookie(w, token)
		cookie := w.Result().Cookies()[0]
		if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.Path != "/api" || cookie.Domain != "" || cookie.Secure != secure || cookie.MaxAge != 43200 {
			t.Fatal("cookie scope or security settings incorrect")
		}
		w = httptest.NewRecorder()
		s.clearCookie(w)
		if w.Result().Cookies()[0].MaxAge != -1 {
			t.Fatal("logout must expire the cookie")
		}
	}
}

func TestConfigRequiresExplicitLocalDevelopment(t *testing.T) {
	cases := []struct {
		origin, env string
		ok, secure  bool
	}{
		{"https://buyers.example.test", "production", true, true},
		{"https://buyers.example.test/", "", true, true},
		{"http://127.0.0.1:4180", "development", true, false},
		{"http://localhost:4180", "development", true, false},
		{"http://[::1]:4180", "development", true, false},
		{"http://127.0.0.1:4180", "", false, false},
		{"http://192.168.1.2:4180", "development", false, false},
		{"https://user:pass@example.test", "production", false, false},
		{"https://example.test/path", "production", false, false},
		{"https://example.test?secret=x", "production", false, false},
		{"null", "development", false, false},
	}
	for _, c := range cases {
		config, err := ParseConfig(c.origin, c.env)
		if (err == nil) != c.ok {
			t.Errorf("%q %q: unexpected error presence", c.origin, c.env)
		}
		if c.ok && config.SecureCookie != c.secure {
			t.Errorf("%q: unexpected cookie security", c.origin)
		}
	}
}

func TestThrottleIsBoundedAndExpires(t *testing.T) {
	var throttle loginThrottle
	now := time.Now()
	for i := 0; i < 10; i++ {
		if !throttle.allow("127.0.0.1", "person@example.test", now) {
			t.Fatal("throttled before limit")
		}
	}
	if throttle.allow("127.0.0.1", "another@example.test", now) {
		t.Fatal("IP limit bypassed")
	}
	if throttle.allow("192.0.2.1", "person@example.test", now) {
		t.Fatal("identity limit bypassed")
	}
	if !throttle.allow("127.0.0.1", "person@example.test", now.Add(time.Minute)) {
		t.Fatal("temporary throttle did not expire")
	}
	if remoteIP("127.0.0.1:1234") != "127.0.0.1" {
		t.Fatal("bad peer address parsing")
	}
}
