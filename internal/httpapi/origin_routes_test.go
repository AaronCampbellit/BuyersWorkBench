package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConfiguredDevelopmentOriginRoutes(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_ORIGIN", "http://127.0.0.1:4180")
	t.Setenv("APP_DEV_ALLOWED_ORIGINS", "http://localhost:54442")
	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	handler, err := New(nil, config, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		origin string
		status int
	}{
		{"http://127.0.0.1:4180", http.StatusUnauthorized},
		{"http://localhost:54442", http.StatusUnauthorized},
		{"http://localhost:54443", http.StatusForbidden},
		{"http://127.0.0.1:54442", http.StatusForbidden},
		{"http://[::1]:54442", http.StatusForbidden},
		{"http://localhost:54442.evil.test", http.StatusForbidden},
		{"http://localhost:54442/", http.StatusForbidden},
		{"null", http.StatusForbidden},
		{"", http.StatusForbidden},
	} {
		t.Run(test.origin, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader("{}"))
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Origin", test.origin)
			// Proxy headers are not evidence that an unlisted Origin is trusted.
			request.Header.Set("X-Forwarded-Host", "localhost:54442")
			request.Header.Set("X-Forwarded-Proto", "http")
			request.Header.Set("Forwarded", "for=127.0.0.1;host=localhost:54442;proto=http")
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != test.status {
				t.Fatalf("status %d, want %d", recorder.Code, test.status)
			}
		})
	}
}
