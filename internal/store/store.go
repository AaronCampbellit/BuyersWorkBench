// Package store configures the database and provides privileged local bootstrap.
package store

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Open prefers a mounted/read-restricted URL file to an environment secret.
// Parse/connect errors intentionally exclude the URL, username and password.
func Open(ctx context.Context) (*pgxpool.Pool, error) {
	url := ""
	if path := os.Getenv("DATABASE_URL_FILE"); path != "" {
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, errors.New("cannot read DATABASE_URL_FILE")
		}
		url = strings.TrimSpace(string(body))
	} else {
		url = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	}
	if url == "" {
		return nil, errors.New("set DATABASE_URL_FILE or DATABASE_URL")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, errors.New("database connection configuration is invalid")
	}
	cfg.MaxConns = 10
	cfg.MinConns = 0
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.MaxConnLifetime = time.Hour
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, errors.New("cannot initialize database pool")
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, errors.New("database connection failed; check database availability and configured credentials")
	}
	return pool, nil
}
