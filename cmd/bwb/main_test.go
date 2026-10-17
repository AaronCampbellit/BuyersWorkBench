package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPasswordFileRequiresRestrictedPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "owner-password")
	if err := os.WriteFile(path, []byte("  preserve password spaces  \r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	password, err := readPasswordFile(path)
	if err != nil || string(password) != "  preserve password spaces  " {
		t.Fatalf("password changed or unreadable: %v", err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPasswordFile(path); err == nil {
		t.Fatal("world-readable password file accepted")
	}
	if _, err := readPasswordFile(filepath.Dir(path)); err == nil {
		t.Fatal("directory accepted as password file")
	}
}
