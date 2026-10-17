package httpapi

import (
	"errors"
	"net"
	"net/url"
	"os"
	"strings"
)

// Config deliberately requires an explicit development environment before
// permitting a non-Secure cookie on the loopback-only local origin.
type Config struct {
	Origin       string
	SecureCookie bool
	devOrigins   map[string]struct{}
}

// AllowsOrigin compares the browser's Origin exactly against the configured
// primary origin and explicit local development aliases. Request headers are
// never normalized, and an absent Origin is never allowed.
func (c Config) AllowsOrigin(origin string) bool {
	if origin == "" {
		return false
	}
	if origin == c.Origin {
		return true
	}
	_, allowed := c.devOrigins[origin]
	return allowed
}

func ConfigFromEnv() (Config, error) {
	origin := os.Getenv("APP_ORIGIN")
	if origin == "" {
		origin = "http://127.0.0.1:4180"
	}
	environment := os.Getenv("APP_ENV")
	config, err := ParseConfig(origin, environment)
	if err != nil {
		return Config{}, err
	}
	aliases := os.Getenv("APP_DEV_ALLOWED_ORIGINS")
	if aliases == "" {
		return config, nil
	}
	if environment != "development" || config.SecureCookie {
		return Config{}, errors.New("APP_DEV_ALLOWED_ORIGINS requires APP_ENV=development and an HTTP loopback APP_ORIGIN")
	}
	entries := strings.Split(aliases, ",")
	if len(entries) > 16 {
		return Config{}, errors.New("APP_DEV_ALLOWED_ORIGINS permits at most 16 entries")
	}
	config.devOrigins = make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		alias, err := ParseConfig(entry, environment)
		if entry == "" || strings.ContainsAny(entry, "?#") || err != nil || alias.SecureCookie {
			return Config{}, errors.New("APP_DEV_ALLOWED_ORIGINS entries must be HTTP loopback origins without credentials, path, query, or fragment")
		}
		if alias.Origin != config.Origin {
			config.devOrigins[alias.Origin] = struct{}{}
		}
	}
	return config, nil
}

func ParseConfig(origin, environment string) (Config, error) {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return Config{}, errors.New("APP_ORIGIN must be one origin without credentials, path, query, or fragment")
	}
	origin = strings.TrimSuffix(origin, "/")
	if u.Scheme == "https" {
		return Config{Origin: origin, SecureCookie: true}, nil
	}
	ip := net.ParseIP(u.Hostname())
	loopback := u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
	if u.Scheme == "http" && environment == "development" && loopback {
		return Config{Origin: origin, SecureCookie: false}, nil
	}
	return Config{}, errors.New("APP_ORIGIN requires HTTPS; local HTTP requires APP_ENV=development and a loopback origin")
}
