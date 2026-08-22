package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationsFS holds the schema history. Embedding it means the binary
// carries its own migrations: no files to ship alongside, and no chance
// of a deployment running against a directory that was not updated.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrationLockID is an arbitrary but fixed key for the advisory lock.
// Any constant works as long as nothing else in the database picks the
// same one.
const migrationLockID int64 = 0x676F5F687474705F // "go_http_"

// Migrate applies every migration that has not run yet.
//
// This is deliberately hand-written rather than goose or golang-migrate.
// Both of those expose a library API built on database/sql, and this
// service uses pgxpool directly, so adopting one would mean opening a
// second connection stack through pgx/v5/stdlib purely for migrations.
// The whole mechanism is small enough not to be worth that.
//
// There are no down migrations. Rolling back means writing a new forward
// migration, which is also what you end up doing in production, where
// reversing a migration that already dropped a column cannot restore the
// data anyway.
func Migrate(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	// Session-level advisory lock. Two replicas starting at once is the
	// normal case under an orchestrator, and without this they would race
	// to create the same table. The lock is held on this one connection,
	// which is why the pool is bypassed for the rest of the function.
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		// Uses context.WithoutCancel so the lock is still released when
		// the caller's context was cancelled mid-migration.
		if _, err := conn.Exec(context.WithoutCancel(ctx),
			"SELECT pg_advisory_unlock($1)", migrationLockID); err != nil {
			log.Error("failed to release migration lock", "error", err)
		}
	}()

	if err := ensureMigrationTable(ctx, conn.Conn()); err != nil {
		return err
	}

	applied, err := appliedVersions(ctx, conn.Conn())
	if err != nil {
		return err
	}

	pending, err := pendingMigrations(applied)
	if err != nil {
		return err
	}

	if len(pending) == 0 {
		log.Debug("no pending migrations", "applied", len(applied))
		return nil
	}

	for _, m := range pending {
		if err := applyMigration(ctx, conn.Conn(), m); err != nil {
			return fmt.Errorf("migration %s: %w", m.version, err)
		}
		log.Info("applied migration", "version", m.version)
	}

	return nil
}

type migration struct {
	version string
	sql     string
}

func ensureMigrationTable(ctx context.Context, conn *pgx.Conn) error {
	const stmt = `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT        PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`

	if _, err := conn.Exec(ctx, stmt); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	return nil
}

func appliedVersions(ctx context.Context, conn *pgx.Conn) (map[string]bool, error) {
	rows, err := conn.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan version: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}

	return applied, nil
}

// pendingMigrations returns the not-yet-applied migrations in filename
// order, which is why the files are named with a zero-padded prefix.
func pendingMigrations(applied map[string]bool) ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	var pending []migration
	for _, name := range names {
		version := strings.TrimSuffix(name, ".sql")
		if applied[version] {
			continue
		}

		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		pending = append(pending, migration{version: version, sql: string(body)})
	}

	return pending, nil
}

// applyMigration runs one migration and records it, in a single
// transaction.
//
// PostgreSQL supports transactional DDL, so a CREATE TABLE that fails
// half way rolls back along with the schema_migrations insert. That is
// what keeps this migrator honest without any repair logic: the database
// is never left believing it applied something it did not. MySQL cannot
// do this, which is why migrators there need far more machinery.
func applyMigration(ctx context.Context, conn *pgx.Conn, m migration) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if _, err := tx.Exec(ctx, m.sql); err != nil {
		return fmt.Errorf("execute: %w", err)
	}

	if _, err := tx.Exec(ctx,
		"INSERT INTO schema_migrations (version) VALUES ($1)", m.version); err != nil {
		return fmt.Errorf("record version: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
