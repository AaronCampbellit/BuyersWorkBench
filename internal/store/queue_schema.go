package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

// MigrateQueue runs upstream River migrations in their own required transactions.
// It is called only by the privileged setup CLI, never API or worker startup.
func MigrateQueue(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	const lock int64 = 636217210043
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", lock); err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := conn.Exec(cleanup, "SELECT pg_advisory_unlock($1)", lock); err != nil {
			_ = conn.Conn().Close(cleanup)
		}
	}()
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("initialize queue migrations: %w", err)
	}
	if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
		return fmt.Errorf("migrate queue: %w", err)
	}
	_, err = conn.Exec(ctx, `DO $$ BEGIN
		IF EXISTS (SELECT 1 FROM pg_roles WHERE rolname='bwb_app') THEN
			GRANT SELECT,INSERT,UPDATE,DELETE ON river_job,river_leader,river_queue,river_notification TO bwb_app;
			GRANT USAGE,SELECT ON SEQUENCE river_job_id_seq,river_notification_id_seq TO bwb_app;
			GRANT SELECT ON river_migration TO bwb_app;
		END IF;
	END $$`)
	if err != nil {
		return fmt.Errorf("grant runtime queue privileges: %w", err)
	}
	return nil
}

// CheckQueue verifies upstream River migration readiness without changing schema.
func CheckQueue(ctx context.Context, pool *pgxpool.Pool) error {
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return err
	}
	result, err := migrator.Validate(ctx, nil)
	if err != nil {
		return errors.New("queue schema is unavailable; run bwb migrate using the migration role")
	}
	if !result.OK {
		return errors.New("queue schema migrations are pending; run bwb migrate using the migration role")
	}
	return nil
}
