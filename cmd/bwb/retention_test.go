package main

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

const retentionWorkspace = "65b303cb-0ca4-422f-a74c-af32e5ad97ea"

func TestRetentionCommandsRequireExplicitScopeAndApply(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	req, apply, err := parseRetention([]string{"retention-plan", "--workspace", retentionWorkspace, "--days", "30"}, now)
	if err != nil || apply || req.Limit != 1000 || !req.Before.Equal(now.Add(-30*24*time.Hour)) {
		t.Fatal("plan did not freeze the cutoff")
	}
	before := req.Before.Format(time.RFC3339Nano)
	confirmed, apply, err := parseRetention([]string{"retention-apply", "--workspace", retentionWorkspace, "--before", before, "--confirm", "--limit", "20"}, now.Add(time.Hour))
	if err != nil || !apply || confirmed.Limit != 20 || !confirmed.Before.Equal(req.Before) {
		t.Fatal("apply did not preserve reviewed cutoff")
	}
	for _, args := range [][]string{
		{"retention-plan", "--days", "30"},
		{"retention-plan", "--workspace", retentionWorkspace},
		{"retention-plan", "--workspace", retentionWorkspace, "--days", "0"},
		{"retention-plan", "--workspace", retentionWorkspace, "--days", "3651"},
		{"retention-plan", "--workspace", retentionWorkspace, "--days", "30", "--limit", "1001"},
		{"retention-plan", "--workspace", retentionWorkspace, "--days", "30", "--confirm"},
		{"retention-apply", "--workspace", retentionWorkspace, "--before", before},
		{"retention-apply", "--workspace", retentionWorkspace, "--days", "30", "--confirm"},
		{"retention-apply", "--workspace", retentionWorkspace, "--before", "now", "--confirm"},
		{"retention-apply", "--workspace", retentionWorkspace, "--before", now.Format(time.RFC3339), "--confirm"},
		{"retention-apply", "--workspace", "all", "--before", before, "--confirm"},
	} {
		if _, _, err := parseRetention(args, now); err == nil {
			t.Fatal("unsafe retention invocation accepted")
		}
	}
}

func TestInvalidRetentionCommandsNeverOpenDatabase(t *testing.T) {
	t.Setenv("DATABASE_URL_FILE", "/credential-path-never-echo")
	for _, args := range [][]string{{"retention-plan"}, {"retention-apply", "--workspace", retentionWorkspace, "--confirm"}, {"retention-plan", "--workspace", "sensitive-input-never-echo", "--days", "30"}} {
		var output bytes.Buffer
		handled, err := runRetention(t.Context(), args, &output)
		if !handled || err == nil || output.Len() != 0 || strings.Contains(err.Error(), "credential-path-never-echo") || strings.Contains(err.Error(), "sensitive-input-never-echo") {
			t.Fatal("invalid retention command accessed credentials or disclosed input")
		}
	}
}
