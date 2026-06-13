package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"crypto-sniping-bot/database"
)

// DB is the Postgres implementation of database.Adapter.
// All SQL uses portable syntax: ON CONFLICT DO NOTHING, CURRENT_TIMESTAMP,
// parameterized queries ($1, $2, ...).
//
// See docs/reference/db_adapter_spec.md for the full specification.
type DB struct {
	pool   *sql.DB
	logger *slog.Logger
	cfg    database.Config
}

// New creates a new uninitialized DB. Call Initialize before use.
func New(logger *slog.Logger) *DB {
	return &DB{logger: logger}
}

// Initialize establishes the database connection pool.
// Must be called before any other method.
func (d *DB) Initialize(ctx context.Context, cfg database.Config) error {
	sslMode := cfg.SSLMode
	if sslMode == "" {
		sslMode = "disable"
	}
	if sslMode == "disable" {
		// Log at INFO for known local-dev hosts (localhost / docker-compose
		// service name); WARN everywhere else to remind operators that TLS
		// is off in a potentially remote environment.
		localHosts := map[string]bool{"localhost": true, "127.0.0.1": true, "postgres": true, "db": true}
		if localHosts[cfg.Host] {
			d.logger.Info("postgres_tls_disabled",
				"host", cfg.Host,
				"note", "TLS disabled — local dev environment")
		} else {
			d.logger.Warn("postgres_tls_disabled",
				"host", cfg.Host,
				"note", "set ssl_mode to 'require' or 'verify-full' in production")
		}
	}
	dsn := fmt.Sprintf(
		"host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
		cfg.Host, cfg.Port, cfg.Database, cfg.User, cfg.Password, sslMode,
	)

	pool, err := sql.Open("pgx", dsn)
	if err != nil {
		return fmt.Errorf("postgres: open pool: %w", err)
	}

	pool.SetMaxOpenConns(cfg.MaxOpenConns)
	pool.SetMaxIdleConns(cfg.MaxIdleConns)
	pool.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetimeSecs) * time.Second)

	if err := pool.PingContext(ctx); err != nil {
		return fmt.Errorf("postgres: ping: %w", err)
	}

	d.pool = pool
	d.cfg = cfg
	d.logger.Info("postgres_connected", "host", cfg.Host, "database", cfg.Database)
	return nil
}

// RunMigrations applies all pending SQL migrations.
func (d *DB) RunMigrations(ctx context.Context) error {
	dir := d.cfg.MigrationsDir
	if dir == "" {
		// Default to database/migrations/ relative to module root.
		_, file, _, ok := runtime.Caller(0)
		if !ok {
			return fmt.Errorf("postgres: cannot determine migrations directory")
		}
		// file = .../database/engines/postgres/postgres.go
		dir = filepath.Join(filepath.Dir(file), "..", "..", "migrations")
	}
	runner := database.NewMigrationRunner(d, dir, d.logger)
	return runner.Run(ctx)
}

// Close releases the connection pool.
func (d *DB) Close(_ context.Context) error {
	if d.pool == nil {
		return nil
	}
	return d.pool.Close()
}

// ExecContext implements database.Querier for the migration runner.
func (d *DB) ExecContext(ctx context.Context, query string, args ...any) error {
	_, err := d.pool.ExecContext(ctx, query, args...)
	return err
}

// QueryRowContext implements database.Querier for the migration runner.
func (d *DB) QueryRowContext(ctx context.Context, query string, args ...any) database.RowScanner {
	return d.pool.QueryRowContext(ctx, query, args...)
}

// withTx executes fn inside a transaction, rolling back on error.
func (d *DB) withTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := d.pool.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
