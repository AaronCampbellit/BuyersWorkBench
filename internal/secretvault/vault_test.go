package secretvault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

const testConnectionA = "13d34b41-cc0f-4f2f-944e-e86702f2ad2b"
const testConnectionB = "fed0543e-784a-4baf-ad94-6eeebc164c78"

func fixture(t *testing.T) (*Vault, string, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(dir, "key")
	if err := GenerateKey(key); err != nil {
		t.Fatal(err)
	}
	vaultDir := filepath.Join(dir, "vault")
	vault, err := Open(vaultDir, key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { vault.Close() })
	return vault, vaultDir, key
}

func write(t *testing.T, path string, value []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, value, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func TestRoundtripReferenceIsolationAndClose(t *testing.T) {
	vault, dir, _ := fixture(t)
	if _, err := vault.Get(testConnectionA); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing secret: %v", err)
	}
	value := []byte("  unparsed credential\r\nwith preserved whitespace  ")
	metadata, err := vault.Put(strings.ToUpper(testConnectionA), value)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.ConnectionID != testConnectionA || metadata.SecretRef != "vault:v1:"+testConnectionA || metadata.Bytes != len(value) || metadata.UpdatedAt.IsZero() {
		t.Fatal("incorrect metadata")
	}
	onDisk, err := os.ReadFile(filepath.Join(dir, testConnectionA+".vault"))
	if err != nil || bytes.Contains(onDisk, value) {
		t.Fatal("plaintext was written to the vault")
	}
	got, err := vault.Get(testConnectionA)
	if err != nil || !bytes.Equal(got, value) {
		t.Fatal("secret roundtrip failed")
	}
	got[0] ^= 1
	Clear(got)
	again, err := vault.Get(testConnectionA)
	if err != nil || !bytes.Equal(again, value) {
		t.Fatal("caller mutation affected persisted secret")
	}
	Clear(again)
	if _, err = vault.Get(testConnectionB); !errors.Is(err, ErrNotFound) {
		t.Fatal("connection A leaked into connection B")
	}
	if _, err = vault.Put(testConnectionB, []byte("separate value")); err != nil {
		t.Fatal(err)
	}
	if err = vault.Delete(testConnectionA); err != nil {
		t.Fatal(err)
	}
	if _, err = vault.Status(testConnectionA); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted value remains available")
	}
	if _, err = vault.Get(testConnectionB); err != nil {
		t.Fatal("deletion affected a different connection")
	}
	if err = vault.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = vault.Get(testConnectionB); err == nil {
		t.Fatal("closed vault accepted access")
	}
	if err = vault.Close(); err != nil {
		t.Fatal("repeat Close failed")
	}
}

func TestWrongKeyTamperingAndConnectionBinding(t *testing.T) {
	vault, dir, key := fixture(t)
	if _, err := vault.Put(testConnectionA, []byte("secret original")); err != nil {
		t.Fatal(err)
	}
	pathA := filepath.Join(dir, testConnectionA+".vault")
	ciphertext, err := os.ReadFile(pathA)
	if err != nil {
		t.Fatal(err)
	}
	wrongKey := filepath.Join(filepath.Dir(key), "different-key")
	if err = GenerateKey(wrongKey); err != nil {
		t.Fatal(err)
	}
	if other, err := Open(dir, wrongKey); err == nil {
		other.Close()
		t.Fatal("wrong vault key accepted")
	}
	write(t, filepath.Join(dir, testConnectionB+".vault"), ciphertext, 0600)
	if _, err = vault.Get(testConnectionB); err == nil {
		t.Fatal("ciphertext accepted for a different connection")
	}
	for _, method := range []string{"get", "status", "put", "delete"} {
		t.Run(method, func(t *testing.T) {
			switch method {
			case "get":
				_, err = vault.Get(testConnectionB)
			case "status":
				_, err = vault.Status(testConnectionB)
			case "put":
				_, err = vault.Put(testConnectionB, []byte("replacement"))
			case "delete":
				err = vault.Delete(testConnectionB)
			}
			if err == nil {
				t.Fatal("wrong binding accepted")
			}
		})
	}
	for _, offset := range []int{0, len(header), len(ciphertext) - 1} {
		tampered := append([]byte(nil), ciphertext...)
		tampered[offset] ^= 1
		write(t, pathA, tampered, 0600)
		if _, err = vault.Get(testConnectionA); err == nil {
			t.Fatal("tampered ciphertext accepted")
		}
	}
	write(t, pathA, ciphertext, 0600)
	if err = os.Remove(filepath.Join(dir, markerName)); err != nil {
		t.Fatal(err)
	}
	if other, err := Open(dir, wrongKey); err == nil {
		other.Close()
		t.Fatal("wrong key claimed a vault with a missing marker")
	}
}

func TestPermissionAndSymlinkRefusals(t *testing.T) {
	for _, kind := range []string{"key-mode", "key-link", "key-ancestor-link", "vault-mode", "vault-link", "entry-mode", "entry-link", "value-mode", "value-link", "value-executable", "value-directory"} {
		t.Run(kind, func(t *testing.T) {
			vault, dir, key := fixture(t)
			if _, err := vault.Put(testConnectionA, []byte("protected")); err != nil {
				t.Fatal(err)
			}
			entry := filepath.Join(dir, testConnectionA+".vault")
			var err error
			switch kind {
			case "key-mode":
				if err = os.Chmod(key, 0644); err != nil {
					t.Fatal(err)
				}
				_, err = Open(dir, key)
			case "key-link":
				link := filepath.Join(filepath.Dir(key), "key-link")
				if err = os.Symlink(key, link); err != nil {
					t.Fatal(err)
				}
				_, err = Open(dir, link)
			case "key-ancestor-link":
				link := filepath.Join(filepath.Dir(key), "parent-link")
				if err = os.Symlink(filepath.Dir(key), link); err != nil {
					t.Fatal(err)
				}
				_, err = Open(dir, filepath.Join(link, "key"))
			case "vault-mode":
				if err = os.Chmod(dir, 0755); err != nil {
					t.Fatal(err)
				}
				_, err = Open(dir, key)
				if _, getErr := vault.Get(testConnectionA); getErr == nil {
					t.Fatal("open handle ignored loosened directory permissions")
				}
			case "vault-link":
				link := filepath.Join(filepath.Dir(key), "vault-link")
				if err = os.Symlink(dir, link); err != nil {
					t.Fatal(err)
				}
				_, err = Open(link, key)
			case "entry-mode":
				if err = os.Chmod(entry, 0644); err != nil {
					t.Fatal(err)
				}
				_, err = vault.Get(testConnectionA)
				if _, putErr := vault.Put(testConnectionA, []byte("replacement")); putErr == nil {
					t.Fatal("replacement repaired unsafe entry silently")
				}
			case "entry-link":
				if err = os.Remove(entry); err != nil {
					t.Fatal(err)
				}
				if err = os.Symlink(key, entry); err != nil {
					t.Fatal(err)
				}
				_, err = vault.Get(testConnectionA)
			case "value-mode", "value-link", "value-executable", "value-directory":
				path := filepath.Join(filepath.Dir(key), "value")
				if kind == "value-directory" {
					err = os.Mkdir(path, 0700)
				} else {
					write(t, path, []byte("private value"), 0600)
				}
				if err != nil {
					t.Fatal(err)
				}
				if kind == "value-mode" {
					if err = os.Chmod(path, 0644); err != nil {
						t.Fatal(err)
					}
				}
				if kind == "value-executable" {
					if err = os.Chmod(path, 0700); err != nil {
						t.Fatal(err)
					}
				}
				if kind == "value-link" {
					link := path + "-link"
					if err = os.Symlink(path, link); err != nil {
						t.Fatal(err)
					}
					path = link
				}
				_, err = ReadValueFile(path)
			}
			if err == nil {
				t.Fatal("unsafe file or directory accepted")
			}
		})
	}
}

func TestAtomicReplacementLimitsAndConcurrentReaders(t *testing.T) {
	vault, dir, _ := fixture(t)
	valueA := bytes.Repeat([]byte("a"), MaxBytes)
	valueB := bytes.Repeat([]byte("b"), MaxBytes)
	if _, err := vault.Put(testConnectionA, valueA); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, testConnectionA+".vault")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, invalid := range [][]byte{nil, make([]byte, MaxBytes+1)} {
		if _, err := vault.Put(testConnectionA, invalid); !errors.Is(err, ErrInputSize) {
			t.Fatal("invalid size accepted")
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("rejected update changed the previous entry")
	}
	var wg sync.WaitGroup
	readFailures := make(chan error, 1)
	wg.Go(func() {
		for range 40 {
			encrypted, err := os.ReadFile(path)
			if err != nil {
				readFailures <- err
				return
			}
			value, err := vault.decrypt(testConnectionA, encrypted)
			if err != nil {
				readFailures <- err
				return
			}
			valid := bytes.Equal(value, valueA) || bytes.Equal(value, valueB)
			Clear(value)
			if !valid {
				readFailures <- errors.New("reader saw a partial replacement")
				return
			}
		}
	})
	for i := range 12 {
		value := valueA
		if i%2 == 0 {
			value = valueB
		}
		if _, err := vault.Put(testConnectionA, value); err != nil {
			t.Fatal(err)
		}
	}
	wg.Wait()
	select {
	case err := <-readFailures:
		t.Fatal(err)
	default:
	}
	after, err = os.ReadFile(path)
	if err != nil || bytes.Equal(before, after) {
		t.Fatal("replacement reused the original ciphertext")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatal("temporary files were retained")
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal("vault file permissions differ from 0600")
		}
	}
}

func TestKeyFormatExclusiveCreationAndInputValidation(t *testing.T) {
	vault, dir, key := fixture(t)
	original, err := os.ReadFile(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(original) != KeyBytes {
		t.Fatal("key format is not raw 32 bytes")
	}
	if err = GenerateKey(key); err == nil {
		t.Fatal("existing key overwritten")
	}
	after, err := os.ReadFile(key)
	if err != nil || !bytes.Equal(after, original) {
		t.Fatal("existing key changed")
	}
	for _, value := range [][]byte{nil, make([]byte, 31), make([]byte, 33), bytes.Repeat([]byte("a"), 64)} {
		path := filepath.Join(filepath.Dir(key), "bad-key")
		write(t, path, value, 0600)
		if other, err := Open(dir, path); err == nil {
			other.Close()
			t.Fatal("non-raw key accepted")
		}
	}
	for _, id := range []string{"", "../secret", "00000000-0000-0000-0000-000000000000", testConnectionA + "/suffix", "vault:v1:" + testConnectionA} {
		if _, err = vault.Put(id, []byte("value")); !errors.Is(err, ErrInvalidConnection) {
			t.Fatal("invalid connection accepted")
		}
	}
	path := filepath.Join(filepath.Dir(key), "read-only")
	write(t, path, []byte("  preserve\r\n"), 0400)
	value, err := ReadValueFile(path)
	if err != nil || string(value) != "  preserve\r\n" {
		t.Fatal("restricted value changed")
	}
	if err = os.Chmod(path, 0600); err != nil {
		t.Fatal(err)
	}
	write(t, path, bytes.Repeat([]byte("x"), MaxBytes+1), 0600)
	if _, err = ReadValueFile(path); err == nil {
		t.Fatal("oversized file accepted")
	}
}
