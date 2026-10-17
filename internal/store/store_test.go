package store

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenConfigurationErrorsDoNotExposeCredentials(t *testing.T) {
	t.Setenv("DATABASE_URL_FILE", "")
	t.Setenv("DATABASE_URL", "postgres://owner:credential-do-not-log@%invalid/")
	if _, err := Open(context.Background()); err == nil || strings.Contains(err.Error(), "credential-do-not-log") {
		t.Fatal("invalid configuration was accepted or exposed its secret")
	}
}

func TestOpenDoesNotFallBackFromExplicitURLFile(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://unused:credential-do-not-log@127.0.0.1:1/unused")
	path := filepath.Join(t.TempDir(), "dsn")
	t.Setenv("DATABASE_URL_FILE", path)
	if _, err := Open(context.Background()); err == nil || err.Error() != "cannot read DATABASE_URL_FILE" {
		t.Fatal("missing URL file silently fell back to an environment secret")
	}
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(context.Background()); err == nil || err.Error() != "set DATABASE_URL_FILE or DATABASE_URL" {
		t.Fatal("empty URL file silently fell back to an environment secret")
	}
}
