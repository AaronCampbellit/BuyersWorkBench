package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"buyersworkbench/internal/secretvault"
)

const commandConnectionA = "47a2dba3-dce3-4259-813a-1ce2c52b317e"
const commandConnectionB = "a7d908f7-40d2-469f-a25e-3956b7f0b083"

func TestSupplierSecretCommandsStayLocalAndNeverPrintPlaintext(t *testing.T) {
	t.Setenv("DATABASE_URL", "invalid://never-connect")
	t.Setenv("DATABASE_URL_FILE", "/no-database-needed")
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(dir, "key")
	vaultDir := filepath.Join(dir, "vault")
	valueFile := filepath.Join(dir, "value")
	privateValue := []byte("sensitive-opaque-test-value\nno network session\n")
	if err := os.WriteFile(valueFile, privateValue, 0600); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := run(t.Context(), []string{"supplier-secret-key", "--key-file", key}, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "{\"key_created\":true}\n" {
		t.Fatal("unexpected key creation output")
	}
	base := []string{"--connection", commandConnectionA, "--key-file", key, "--vault-dir", vaultDir}
	invoke := func(command string, extra ...string) map[string]any {
		t.Helper()
		output.Reset()
		args := append([]string{command}, base...)
		args = append(args, extra...)
		if err := run(t.Context(), args, &output); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(output.Bytes(), privateValue) || strings.Contains(output.String(), "sensitive-opaque-test-value") {
			t.Fatal("CLI disclosed a secret")
		}
		var result map[string]any
		if err := json.Unmarshal(output.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		return result
	}
	if result := invoke("supplier-secret-status"); result["configured"] != false {
		t.Fatal("missing secret was reported configured")
	}
	if result := invoke("supplier-secret-set", "--value-file", valueFile); result["stored"] != true {
		t.Fatal("set failed")
	}
	result := invoke("supplier-secret-status")
	if result["configured"] != true {
		t.Fatal("stored secret was reported absent")
	}
	metadata, ok := result["metadata"].(map[string]any)
	if !ok || metadata["secret_ref"] != "vault:v1:"+commandConnectionA || metadata["bytes"] != float64(len(privateValue)) {
		t.Fatal("metadata did not match stored value")
	}
	vault, err := secretvault.Open(vaultDir, key)
	if err != nil {
		t.Fatal(err)
	}
	defer vault.Close()
	got, err := vault.Get(commandConnectionA)
	if err != nil || !bytes.Equal(got, privateValue) {
		t.Fatal("CLI changed credential content")
	}
	secretvault.Clear(got)
	if _, err = vault.Put(commandConnectionB, []byte("other connection")); err != nil {
		t.Fatal(err)
	}
	if result = invoke("supplier-secret-delete"); result["deleted"] != true {
		t.Fatal("delete failed")
	}
	if result = invoke("supplier-secret-delete"); result["deleted"] != true {
		t.Fatal("repeated delete failed")
	}
	if result = invoke("supplier-secret-status"); result["configured"] != false {
		t.Fatal("delete retained the connection secret")
	}
	if _, err = vault.Get(commandConnectionB); err != nil {
		t.Fatal("delete revoked another connection")
	}
}

func TestSupplierSecretCommandErrorsAreRedacted(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	key := filepath.Join(dir, "key")
	if err := secretvault.GenerateKey(key); err != nil {
		t.Fatal(err)
	}
	valueFile := filepath.Join(dir, "sensitive-path-never-echo")
	if err := os.WriteFile(valueFile, []byte("secret-never-echo"), 0644); err != nil {
		t.Fatal(err)
	}
	base := []string{"--connection", commandConnectionA, "--key-file", key, "--vault-dir", filepath.Join(dir, "vault")}
	for _, args := range [][]string{
		{"supplier-secret-key", "--key-file", key},
		append(append([]string{"supplier-secret-set"}, base...), "--value-file", valueFile),
		append(append([]string{"supplier-secret-set"}, base...), "--value", "secret-never-echo"),
		append(append([]string{"supplier-secret-status"}, base...), "--dump"),
		{"supplier-secret-get", "--connection", commandConnectionA},
		{"supplier-secret-set", "--connection", "../../sensitive-path-never-echo"},
	} {
		var output bytes.Buffer
		err := run(t.Context(), args, &output)
		if err == nil {
			t.Fatal("unsafe command accepted")
		}
		if output.Len() != 0 || strings.Contains(err.Error(), "secret-never-echo") || strings.Contains(err.Error(), dir) || strings.Contains(err.Error(), "sensitive-path-never-echo") {
			t.Fatal("error disclosed raw input or filesystem detail")
		}
	}
}
