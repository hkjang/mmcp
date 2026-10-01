// Package store owns the PostgreSQL pool and schema migrations.
package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

type DB struct {
	*pgxpool.Pool
}

func Open(ctx context.Context, dsn string) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse POSTGRES_DSN: %w", err)
	}
	if cfg.MaxConns < 10 {
		cfg.MaxConns = 10
	}
	cfg.ConnConfig.RuntimeParams["application_name"] = "mmcp"
	var pool *pgxpool.Pool
	// The database may start after us in a compose/k8s rollout; retry briefly.
	for attempt := 1; ; attempt++ {
		pool, err = pgxpool.NewWithConfig(ctx, cfg)
		if err == nil {
			if err = pool.Ping(ctx); err == nil {
				break
			}
			pool.Close()
		}
		if attempt >= 30 {
			return nil, fmt.Errorf("connect database: %w", err)
		}
		slog.Warn("database not ready, retrying", "attempt", attempt, "error", err.Error())
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return &DB{pool}, nil
}

// Migrate applies embedded migrations in order under an advisory lock so
// that several replicas starting together do not race.
func (db *DB) Migrate(ctx context.Context) ([]string, error) {
	conn, err := db.Acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock(727274)`); err != nil {
		return nil, err
	}
	defer conn.Exec(context.Background(), `SELECT pg_advisory_unlock(727274)`)
	if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`); err != nil {
		return nil, err
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	var applied []string
	for _, name := range names {
		version := strings.TrimSuffix(strings.TrimPrefix(name, "migrations/"), ".sql")
		var exists bool
		if err := conn.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, version).Scan(&exists); err != nil {
			return nil, err
		}
		if exists {
			continue
		}
		body, _ := migrations.ReadFile(name)
		err := pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(body)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations(version) VALUES ($1)`, version)
			return err
		})
		if err != nil {
			return nil, fmt.Errorf("migration %s: %w", version, err)
		}
		applied = append(applied, version)
	}
	return applied, nil
}

func (db *DB) SchemaVersion(ctx context.Context) string {
	var v string
	_ = db.QueryRow(ctx, `SELECT COALESCE(MAX(version),'') FROM schema_migrations`).Scan(&v)
	return v
}
