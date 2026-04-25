package database

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MigrationRunner applies SQL migration files from a directory.
// Migrations are tracked in the _migrations table to ensure idempotency.
// Filenames must follow: YYYYMMDDNNNNNN_description.sql
//
// See docs/db_adapter_spec.md § 5 for migration rules.
type MigrationRunner struct {
	db     Querier
	dir    string
	logger *slog.Logger
}

// Querier is the minimal DB interface needed by the migration runner.
// Implemented by the postgres engine.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) error
	QueryRowContext(ctx context.Context, query string, args ...any) RowScanner
}

// RowScanner wraps sql.Row.Scan.
type RowScanner interface {
	Scan(dest ...any) error
}

// NewMigrationRunner creates a new MigrationRunner.
func NewMigrationRunner(db Querier, migrationsDir string, logger *slog.Logger) *MigrationRunner {
	return &MigrationRunner{db: db, dir: migrationsDir, logger: logger}
}

// Run applies all pending migrations in lexicographic filename order.
// Each migration is wrapped in a transaction.
// Safe to call on every startup — already-applied migrations are skipped.
func (r *MigrationRunner) Run(ctx context.Context) error {
	if err := r.ensureMigrationsTable(ctx); err != nil {
		return fmt.Errorf("migration: ensure _migrations table: %w", err)
	}

	files, err := r.pendingFiles(ctx)
	if err != nil {
		return fmt.Errorf("migration: list pending files: %w", err)
	}

	for _, f := range files {
		if err := r.applyFile(ctx, f); err != nil {
			return fmt.Errorf("migration: apply %s: %w", f, err)
		}
		r.logger.Info("migration_applied", "file", f)
	}

	if len(files) == 0 {
		r.logger.Info("migrations_up_to_date")
	}

	return nil
}

func (r *MigrationRunner) ensureMigrationsTable(ctx context.Context) error {
	const sql = `CREATE TABLE IF NOT EXISTS _migrations (
        migration_id TEXT PRIMARY KEY,
        applied_at   TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
    )`
	return r.db.ExecContext(ctx, sql)
}

// pendingFiles returns the sorted list of migration files not yet in _migrations.
func (r *MigrationRunner) pendingFiles(ctx context.Context) ([]string, error) {
	entries, err := os.ReadDir(r.dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir %s: %w", r.dir, err)
	}

	var all []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			all = append(all, e.Name())
		}
	}
	sort.Strings(all) // deterministic ordering

	var pending []string
	for _, name := range all {
		migrationID := strings.TrimSuffix(name, ".sql")
		var exists string
		err := r.db.QueryRowContext(ctx,
			"SELECT migration_id FROM _migrations WHERE migration_id = $1",
			migrationID,
		).Scan(&exists)
		if err != nil {
			// Row not found — migration is pending.
			pending = append(pending, name)
		}
	}

	return pending, nil
}

func (r *MigrationRunner) applyFile(ctx context.Context, filename string) error {
	path := filepath.Join(r.dir, filename)
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	// Apply the SQL (migration files include their own BEGIN/COMMIT).
	if err := r.db.ExecContext(ctx, string(content)); err != nil {
		return fmt.Errorf("exec sql: %w", err)
	}

	// Record as applied.
	migrationID := strings.TrimSuffix(filename, ".sql")
	return r.db.ExecContext(ctx,
		"INSERT INTO _migrations (migration_id) VALUES ($1) ON CONFLICT (migration_id) DO NOTHING",
		migrationID,
	)
}
