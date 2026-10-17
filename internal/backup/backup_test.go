package backup

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuthenticatedEnvelopeRoundTripAndTampering(t *testing.T) {
	key := bytes.Repeat([]byte{0x51}, KeyBytes)
	plain := []byte("PGDMP fictional database records and session hashes")
	a, err := Encrypt(key, plain)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encrypt(key, plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(a, b) || bytes.Contains(a, plain) {
		t.Fatal("envelope reused a nonce or exposed plaintext")
	}
	got, err := Decrypt(key, a)
	if err != nil || !bytes.Equal(got, plain) {
		t.Fatal("round trip failed")
	}
	for _, position := range []int{0, len(header), len(a) - 1} {
		corrupt := bytes.Clone(a)
		corrupt[position] ^= 1
		if _, err := Decrypt(key, corrupt); err == nil {
			t.Fatal("modified ciphertext authenticated")
		}
	}
	if _, err := Decrypt(bytes.Repeat([]byte{0x52}, KeyBytes), a); err == nil {
		t.Fatal("incorrect key decrypted backup")
	}
	if _, err := Decrypt(key, a[:len(a)-1]); err == nil {
		t.Fatal("truncated backup authenticated")
	}
	if _, err := Encrypt(key[:31], plain); err == nil {
		t.Fatal("weak key accepted")
	}
	if _, err := Encrypt(key, nil); err == nil {
		t.Fatal("empty dump accepted")
	}
}

func TestRestrictedKeyAndCiphertextFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(dir, "key")
	if err := GenerateKey(key); err != nil {
		t.Fatal(err)
	}
	original, err := ReadKey(key)
	if err != nil || len(original) != KeyBytes {
		t.Fatal("invalid generated key")
	}
	if err := GenerateKey(key); err == nil {
		t.Fatal("existing key overwritten")
	}
	again, _ := ReadKey(key)
	if !bytes.Equal(original, again) {
		t.Fatal("key changed")
	}
	output := filepath.Join(dir, "dump.bwb")
	if _, err := EncryptFile(strings.NewReader("PGDMP safe test dump"), key, output); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Stat(output); info.Mode().Perm() != 0600 {
		t.Fatal("ciphertext file is not private")
	}
	var restored bytes.Buffer
	if err := DecryptFile(key, output, &restored); err != nil || restored.String() != "PGDMP safe test dump" {
		t.Fatal("file restore failed")
	}
	if _, err := EncryptFile(strings.NewReader("replacement"), key, output); err == nil {
		t.Fatal("backup overwritten")
	}
	if _, err := EncryptFile(strings.NewReader("replacement"), key, key); err == nil {
		t.Fatal("encryption key overwritten")
	}
	link := filepath.Join(dir, "key-link")
	if err := os.Symlink(key, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadKey(link); err == nil {
		t.Fatal("symlink key accepted")
	}
	if err := os.Chmod(key, 0640); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadKey(key); err == nil {
		t.Fatal("group-readable key accepted")
	}
}

func TestRefusePublicDirectoryAndBoundedInput(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := GenerateKey(filepath.Join(dir, "key")); err == nil {
		t.Fatal("public directory accepted")
	}
	if _, err := ReadBounded(strings.NewReader("four"), 3); err == nil {
		t.Fatal("unbounded backup input")
	}
	if got, err := ReadBounded(strings.NewReader("three"), 5); err != nil || string(got) != "three" {
		t.Fatal("valid bounded input failed")
	}
}
