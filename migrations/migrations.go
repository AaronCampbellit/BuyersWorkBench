// Package migrations applies the embedded, checksum-verified application schema.
package migrations

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed *.sql
var files embed.FS

const migrationLock int64 = 636217210041

type migration struct {
	version             int64
	name, sql, checksum string
}

func embedded() ([]migration, error) {
	entries, err := fs.Glob(files, "*.sql")
	if err != nil {
		return nil, err
	}
	var result []migration
	for _, name := range entries {
		version, err := strconv.ParseInt(strings.SplitN(name, "_", 2)[0], 10, 64)
		if err != nil || version <= 0 {
			return nil, fmt.Errorf("invalid migration filename %q", name)
		}
		body, err := files.ReadFile(name)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(body)
		result = append(result, migration{version, name, string(body), hex.EncodeToString(sum[:])})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].version < result[j].version })
	for i := 1; i < len(result); i++ {
		if result[i].version == result[i-1].version {
			return nil, errors.New("duplicate embedded migration version")
		}
	}
	return result, nil
}

// Apply serializes migrators on a dedicated connection and records each SQL file
// in the same transaction as its schema changes. Applied files are immutable.
func Apply(ctx context.Context, pool *pgxpool.Pool) error {
	wanted, err := embedded()
	if err != nil {
		return err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire migration connection: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLock); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(cleanup, "SELECT pg_advisory_unlock($1)", migrationLock); err != nil {
			// Never return a still-locked connection to the pool.
			_ = conn.Conn().Close(cleanup)
		}
	}()
	_, err = conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS bwb_schema_migrations (
		version bigint PRIMARY KEY, name text NOT NULL, checksum text NOT NULL,
		applied_at timestamptz NOT NULL DEFAULT now())`)
	if err != nil {
		return fmt.Errorf("create migration ledger: %w", err)
	}
	applied, err := readApplied(ctx, conn)
	if err != nil {
		return err
	}
	if err := verifyKnown(wanted, applied, false); err != nil {
		return err
	}
	for _, m := range wanted {
		if _, ok := applied[m.version]; ok {
			continue
		}
		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration: %w", err)
		}
		if _, err = tx.Exec(ctx, m.sql); err == nil {
			_, err = tx.Exec(ctx, "INSERT INTO bwb_schema_migrations(version,name,checksum) VALUES($1,$2,$3)", m.version, m.name, m.checksum)
		}
		if err != nil {
			_ = tx.Rollback(context.Background())
			return fmt.Errorf("apply migration %s: %w", m.name, err)
		}
		if err = tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", m.name, err)
		}
	}
	return nil
}

type querier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

type appliedMigration struct{ name, checksum string }

func readApplied(ctx context.Context, db querier) (map[int64]appliedMigration, error) {
	rows, err := db.Query(ctx, "SELECT version,name,checksum FROM bwb_schema_migrations ORDER BY version")
	if err != nil {
		return nil, errors.New("application schema is unavailable; run bwb migrate using the migration role")
	}
	defer rows.Close()
	result := make(map[int64]appliedMigration)
	for rows.Next() {
		var version int64
		var m appliedMigration
		if err := rows.Scan(&version, &m.name, &m.checksum); err != nil {
			return nil, fmt.Errorf("read migration ledger: %w", err)
		}
		result[version] = m
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read migration ledger: %w", err)
	}
	return result, nil
}

func verifyKnown(wanted []migration, applied map[int64]appliedMigration, requireAll bool) error {
	known := make(map[int64]bool)
	missing := false
	for _, m := range wanted {
		known[m.version] = true
		got, ok := applied[m.version]
		if !ok {
			missing = true
			if requireAll {
				return fmt.Errorf("migration %s has not been applied", m.name)
			}
			continue
		}
		if missing {
			return errors.New("migration ledger has a gap; manual repair is required")
		}
		if got.name != m.name || got.checksum != m.checksum {
			return fmt.Errorf("migration %s checksum/name mismatch; restore the original migration", m.name)
		}
	}
	for version := range applied {
		if !known[version] {
			return fmt.Errorf("database has unknown migration version %d; use the matching application build", version)
		}
	}
	return nil
}

// Check performs a read-only readiness check; runtime processes never migrate.
func Check(ctx context.Context, pool *pgxpool.Pool) error {
	wanted, err := embedded()
	if err != nil {
		return err
	}
	applied, err := readApplied(ctx, pool)
	if err != nil {
		return err
	}
	return verifyKnown(wanted, applied, true)
}
