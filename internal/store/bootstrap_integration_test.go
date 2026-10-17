package store

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"buyersworkbench/internal/auth"
	"buyersworkbench/migrations"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Each explicitly enabled integration test owns an isolated, random schema.
// No production/public schema or unrelated data is truncated or deleted.
func isolatedDatabase(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	url := os.Getenv("BWB_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("set BWB_TEST_DATABASE_URL to explicitly enable isolated PostgreSQL integration tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	admin, err := pgx.Connect(ctx, url)
	if err != nil {
		t.Fatal("cannot connect to integration database")
	}
	id, err := newID()
	if err != nil {
		t.Fatal(err)
	}
	schema := "bwb_test_" + strings.ReplaceAll(id, "-", "")
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal("invalid integration database configuration")
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Close()
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := admin.Exec(cleanup, "DROP SCHEMA "+pgx.Identifier{schema}.Sanitize()+" CASCADE"); err != nil {
			t.Error("cannot clean isolated integration schema:", err)
		}
		_ = admin.Close(cleanup)
	})
	if err := migrations.Apply(ctx, pool); err != nil {
		t.Fatal(err)
	}
	return ctx, pool
}

func TestBootstrapOwnerIntegration(t *testing.T) {
	ctx, pool := isolatedDatabase(t)
	input := OwnerInput{"OWNER@example.test", "Local test owner", "Test-only supplied long passphrase"}
	first, err := BootstrapOwner(ctx, pool, input)
	if err != nil {
		t.Fatal(err)
	}
	if !first.Created || first.OwnerID == "" || first.PrivateWorkspaceID == first.PresentationWorkspaceID {
		t.Fatalf("unexpected bootstrap result: %+v", first)
	}
	var originalHash string
	if err := pool.QueryRow(ctx, "SELECT password_hash FROM identities WHERE user_id=$1", first.OwnerID).Scan(&originalHash); err != nil {
		t.Fatal(err)
	}
	second, err := BootstrapOwner(ctx, pool, OwnerInput{"owner@example.test", "Changed name", "different password must never replace original"})
	if err != nil {
		t.Fatal(err)
	}
	if second.Created || second.OwnerID != first.OwnerID || second.PresentationWorkspaceID != first.PresentationWorkspaceID {
		t.Fatal("same-owner bootstrap was not idempotent")
	}
	var finalHash, name string
	if err := pool.QueryRow(ctx, "SELECT i.password_hash,u.display_name FROM identities i JOIN users u ON u.id=i.user_id WHERE u.id=$1", first.OwnerID).Scan(&finalHash, &name); err != nil {
		t.Fatal(err)
	}
	if originalHash != finalHash || name != input.DisplayName {
		t.Fatal("bootstrap rerun overwrote credentials/profile")
	}
	if ok, err := auth.VerifyPassword(finalHash, input.Password); err != nil || !ok {
		t.Fatal("original password no longer works")
	}
	if _, err := BootstrapOwner(ctx, pool, OwnerInput{"other@example.test", "Second owner", input.Password}); err == nil {
		t.Fatal("second owner accepted")
	}
	var users, identities, workspaces, memberships, audits int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM users),(SELECT count(*) FROM identities),
		(SELECT count(*) FROM workspaces),(SELECT count(*) FROM memberships),(SELECT count(*) FROM audit_events)`).Scan(&users, &identities, &workspaces, &memberships, &audits); err != nil {
		t.Fatal(err)
	}
	if users != 1 || identities != 1 || workspaces != 2 || memberships != 2 || audits != 1 {
		t.Fatalf("unexpected bootstrap counts %d/%d/%d/%d/%d", users, identities, workspaces, memberships, audits)
	}
	status, err := Status(ctx, pool)
	if err != nil || !status.SchemaReady || !status.OwnerBootstrapped || len(status.Workspaces) != 2 {
		t.Fatalf("invalid status %+v: %v", status, err)
	}
	for _, workspace := range status.Workspaces {
		if workspace.Generation != 1 {
			t.Fatal("initial workspace generation must be one")
		}
		if workspace.Kind == "private" && (workspace.SeedVersion != nil || workspace.BusinessDate != nil) {
			t.Fatal("private workspace inherited fictional seed metadata")
		}
		if workspace.Kind == "presentation" && (workspace.SeedVersion == nil || *workspace.SeedVersion != SeedVersion || workspace.BusinessDate == nil || *workspace.BusinessDate != BusinessDate) {
			t.Fatal("fictional workspace seed metadata missing")
		}
	}
	if _, err := pool.Exec(ctx, "UPDATE memberships SET role='reviewer' WHERE workspace_id=$1", first.PrivateWorkspaceID); err == nil {
		t.Fatal("private workspace accepted reviewer membership")
	}
}

func TestBootstrapValidationAndConcurrencyIntegration(t *testing.T) {
	ctx, pool := isolatedDatabase(t)
	if _, err := BootstrapOwner(ctx, pool, OwnerInput{"owner@example.test", "Owner", "short"}); err == nil {
		t.Fatal("short bootstrap password accepted")
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&count); err != nil || count != 0 {
		t.Fatal("invalid bootstrap left partial user state")
	}
	var wg sync.WaitGroup
	results := make(chan BootstrapResult, 2)
	errors := make(chan error, 2)
	for range 2 {
		wg.Go(func() {
			result, err := BootstrapOwner(ctx, pool, OwnerInput{"owner@example.test", "Owner", "Concurrency test-only password"})
			results <- result
			errors <- err
		})
	}
	wg.Wait()
	close(results)
	close(errors)
	created := 0
	for err := range errors {
		if err != nil {
			t.Fatal(err)
		}
	}
	for result := range results {
		if result.Created {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("expected one bootstrap transaction, got %d", created)
	}
}

func TestMigrationChecksumGuardIntegration(t *testing.T) {
	ctx, pool := isolatedDatabase(t)
	if err := migrations.Apply(ctx, pool); err != nil {
		t.Fatalf("second migrate failed: %v", err)
	}
	if err := migrations.Check(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE bwb_schema_migrations SET checksum='tampered' WHERE version=1"); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Apply(ctx, pool); err == nil {
		t.Fatal("migrate accepted a modified applied migration")
	}
	if err := migrations.Check(ctx, pool); err == nil {
		t.Fatal("readiness accepted a modified applied migration")
	}
}

func TestRiverSchemaIntegration(t *testing.T) {
	ctx, pool := isolatedDatabase(t)
	if err := CheckQueue(ctx, pool); err == nil {
		t.Fatal("unmigrated queue appears ready")
	}
	if err := MigrateQueue(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err := MigrateQueue(ctx, pool); err != nil {
		t.Fatalf("second queue migrate failed: %v", err)
	}
	if err := CheckQueue(ctx, pool); err != nil {
		t.Fatal(err)
	}
}
