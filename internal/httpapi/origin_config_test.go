package httpapi

import (
	"fmt"
	"strings"
	"testing"
)

func TestConfigExplicitDevelopmentOrigins(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_ORIGIN", "http://127.0.0.1:4180/")
	t.Setenv("APP_DEV_ALLOWED_ORIGINS", " http://localhost:54442/ ,http://localhost:54442,http://127.0.0.1:4180,http://[::1]:54442")
	config, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if config.SecureCookie || config.Origin != "http://127.0.0.1:4180" || len(config.devOrigins) != 2 {
		t.Fatal("development aliases must preserve the primary origin and deduplicate normalized entries")
	}
	for _, origin := range []string{"http://127.0.0.1:4180", "http://localhost:54442", "http://[::1]:54442"} {
		if !config.AllowsOrigin(origin) {
			t.Errorf("configured origin %q was rejected", origin)
		}
	}
	for _, origin := range []string{
		"", "null", "http://localhost:54443", "http://127.0.0.1:54442",
		"https://localhost:54442", "http://localhost:54442/", "http://LOCALHOST:54442",
		"http://localhost:54442.evil.test", "http://localhost.evil.test:54442",
		"http://localhost:54442?x=y", "http://localhost:54442#fragment",
		"http://localhost:54442,http://evil.test", " http://localhost:54442",
	} {
		if config.AllowsOrigin(origin) {
			t.Errorf("unconfigured origin %q was accepted", origin)
		}
	}
}

func TestConfigRejectsUnsafeDevelopmentOrigins(t *testing.T) {
	cases := []struct {
		name, environment, primary, aliases string
	}{
		{"production", "production", "https://buyers.example.test", "http://localhost:54442"},
		{"unspecified environment", "", "https://buyers.example.test", "http://localhost:54442"},
		{"HTTPS primary", "development", "https://localhost:4180", "http://localhost:54442"},
		{"HTTPS alias", "development", "http://127.0.0.1:4180", "https://localhost:54442"},
		{"remote host", "development", "http://127.0.0.1:4180", "http://example.test:54442"},
		{"private address", "development", "http://127.0.0.1:4180", "http://192.168.1.2:54442"},
		{"hostname suffix", "development", "http://127.0.0.1:4180", "http://localhost.evil.test:54442"},
		{"port lookalike", "development", "http://127.0.0.1:4180", "http://localhost:54442.evil.test"},
		{"credentials", "development", "http://127.0.0.1:4180", "http://user:pass@localhost:54442"},
		{"path", "development", "http://127.0.0.1:4180", "http://localhost:54442/path"},
		{"query", "development", "http://127.0.0.1:4180", "http://localhost:54442?x=y"},
		{"empty query", "development", "http://127.0.0.1:4180", "http://localhost:54442?"},
		{"fragment", "development", "http://127.0.0.1:4180", "http://localhost:54442#fragment"},
		{"empty fragment", "development", "http://127.0.0.1:4180", "http://localhost:54442#"},
		{"null", "development", "http://127.0.0.1:4180", "null"},
		{"space", "development", "http://127.0.0.1:4180", " "},
		{"leading empty entry", "development", "http://127.0.0.1:4180", ",http://localhost:54442"},
		{"trailing empty entry", "development", "http://127.0.0.1:4180", "http://localhost:54442,"},
		{"middle empty entry", "development", "http://127.0.0.1:4180", "http://localhost:54442,,http://localhost:54443"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("APP_ENV", tc.environment)
			t.Setenv("APP_ORIGIN", tc.primary)
			t.Setenv("APP_DEV_ALLOWED_ORIGINS", tc.aliases)
			if _, err := ConfigFromEnv(); err == nil {
				t.Fatal("unsafe development origin configuration was accepted")
			}
		})
	}
}

func TestConfigDevelopmentOriginBound(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_ORIGIN", "http://127.0.0.1:4180")
	entries := make([]string, 17)
	for i := range entries {
		entries[i] = fmt.Sprintf("http://localhost:%d", 54000+i)
	}
	t.Setenv("APP_DEV_ALLOWED_ORIGINS", strings.Join(entries[:16], ","))
	config, err := ConfigFromEnv()
	if err != nil || len(config.devOrigins) != 16 {
		t.Fatal("16 explicit development origins must be accepted")
	}
	t.Setenv("APP_DEV_ALLOWED_ORIGINS", strings.Join(entries, ","))
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("more than 16 development origin entries must be rejected")
	}
}

func TestConfigWithoutDevelopmentOriginsPreservesPolicy(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("APP_ORIGIN", "")
	t.Setenv("APP_DEV_ALLOWED_ORIGINS", "")
	config, err := ConfigFromEnv()
	if err != nil || !config.AllowsOrigin("http://127.0.0.1:4180") || config.AllowsOrigin("http://localhost:54442") {
		t.Fatal("default development origin policy changed")
	}
	t.Setenv("APP_ENV", "production")
	t.Setenv("APP_ORIGIN", "https://buyers.example.test/")
	config, err = ConfigFromEnv()
	if err != nil || !config.SecureCookie || !config.AllowsOrigin("https://buyers.example.test") || config.AllowsOrigin("http://localhost:54442") {
		t.Fatal("production primary origin policy changed")
	}
	if (Config{}).AllowsOrigin("") {
		t.Fatal("empty origin must never be allowed")
	}
}
