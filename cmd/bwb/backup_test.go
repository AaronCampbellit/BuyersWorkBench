package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupCommandsRequireNoDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL", "invalid://never-connect")
	t.Setenv("DATABASE_URL_FILE", "/missing/no-database-needed")
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "key")
	var output bytes.Buffer
	if err := run(t.Context(), []string{"backup-key", "--key-file", path}, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "{\"key_created\":true}\n" {
		t.Fatal("key creation exposed unexpected output")
	}
	if err := run(t.Context(), []string{"backup-key", "--key-file", path}, &output); err == nil {
		t.Fatal("backup command replaced existing key")
	}
	if handled, err := runBackup([]string{"backup-decrypt", "--key-file", path, "--input", "missing"}, &output); !handled || err == nil {
		t.Fatal("decryption accepted implicit plaintext output")
	}
}
