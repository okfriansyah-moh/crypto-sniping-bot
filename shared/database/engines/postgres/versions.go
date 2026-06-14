package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"crypto-sniping-bot/shared/database"
)

// CreateStrategyVersion persists an immutable strategy version snapshot.
// Idempotent: ON CONFLICT DO NOTHING.
func (d *DB) CreateStrategyVersion(ctx context.Context, sv database.StrategyVersion) error {
	const q = `
INSERT INTO strategy_versions
    (strategy_version_id, config_snapshot, created_at, activated_at, deactivated_at,
     status, shadow_started_at, promoted_at, rolled_back_at, parent_version_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (strategy_version_id) DO NOTHING`

	createdAt := sv.CreatedAt
	if createdAt == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	status := sv.Status
	if status == "" {
		status = "draft"
	}

	_, err := d.pool.ExecContext(ctx, q,
		sv.StrategyVersionID,
		sv.ConfigSnapshot,
		createdAt,
		sv.ActivatedAt,
		sv.DeactivatedAt,
		status,
		sv.ShadowStartedAt,
		sv.PromotedAt,
		sv.RolledBackAt,
		sv.ParentVersionID,
	)
	if err != nil {
		return fmt.Errorf("create strategy version: %w", err)
	}
	return nil
}

// GetActiveStrategyVersion returns the currently active strategy version.
// Prefers the status="active" row; falls back to activated_at IS NOT NULL AND deactivated_at IS NULL.
func (d *DB) GetActiveStrategyVersion(ctx context.Context) (*database.StrategyVersion, error) {
	const q = `
SELECT strategy_version_id, config_snapshot, created_at, activated_at, deactivated_at,
       status, shadow_started_at, promoted_at, rolled_back_at, parent_version_id
FROM strategy_versions
WHERE activated_at IS NOT NULL
  AND deactivated_at IS NULL
ORDER BY activated_at DESC
LIMIT 1`

	sv, err := scanStrategyVersion(d.pool.QueryRowContext(ctx, q))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get active strategy version: %w", err)
	}
	return sv, nil
}

// GetStrategyVersion fetches a strategy version by ID.
// Returns ErrUnknownVersion if not found.
func (d *DB) GetStrategyVersion(ctx context.Context, versionID string) (*database.StrategyVersion, error) {
	const q = `
SELECT strategy_version_id, config_snapshot, created_at, activated_at, deactivated_at,
       status, shadow_started_at, promoted_at, rolled_back_at, parent_version_id
FROM strategy_versions
WHERE strategy_version_id = $1`

	sv, err := scanStrategyVersion(d.pool.QueryRowContext(ctx, q, versionID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrUnknownVersion
	}
	if err != nil {
		return nil, fmt.Errorf("get strategy version: %w", err)
	}
	return sv, nil
}

// ActivateStrategyVersion sets activated_at on the version and deactivates
// the previously active one. Called by Boot() once during startup.
func (d *DB) ActivateStrategyVersion(ctx context.Context, versionID string) error {
	return d.withTx(ctx, func(tx *sql.Tx) error {
		now := time.Now().UTC().Format(time.RFC3339Nano)

		// Deactivate current active version.
		const deactivate = `
UPDATE strategy_versions
   SET deactivated_at = $1,
       status = 'deactivated'
 WHERE activated_at IS NOT NULL
   AND deactivated_at IS NULL
   AND strategy_version_id != $2`
		if _, err := tx.ExecContext(ctx, deactivate, now, versionID); err != nil {
			return fmt.Errorf("deactivate old version: %w", err)
		}

		// Activate the new version.
		const activate = `
UPDATE strategy_versions
   SET activated_at = $1,
       status = 'active'
 WHERE strategy_version_id = $2`
		if _, err := tx.ExecContext(ctx, activate, now, versionID); err != nil {
			return fmt.Errorf("activate version: %w", err)
		}
		return nil
	})
}

// PinStrategyVersion creates a new StrategyVersion from config JSON and activates it.
// If the same config snapshot already exists, it is reactivated instead.
// Returns the active StrategyVersionID.
func PinStrategyVersion(ctx context.Context, db interface {
	CreateStrategyVersion(context.Context, database.StrategyVersion) error
	ActivateStrategyVersion(context.Context, string) error
}, configJSON []byte) (string, error) {
	versionID := contentHash(configJSON)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	sv := database.StrategyVersion{
		StrategyVersionID: versionID,
		ConfigSnapshot:    configJSON,
		CreatedAt:         now,
		Status:            "draft",
	}

	if err := db.CreateStrategyVersion(ctx, sv); err != nil {
		return "", fmt.Errorf("pin strategy version create: %w", err)
	}
	if err := db.ActivateStrategyVersion(ctx, versionID); err != nil {
		return "", fmt.Errorf("pin strategy version activate: %w", err)
	}
	return versionID, nil
}

// contentHash returns SHA256(data)[:8] as 16-char lowercase hex.
func contentHash(data []byte) string {
	return fmt.Sprintf("%x", hashBytes(data)[:8])
}

// rowScanner abstracts *sql.Row and *sql.Rows for scanStrategyVersion.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanStrategyVersion scans a single StrategyVersion row.
func scanStrategyVersion(row rowScanner) (*database.StrategyVersion, error) {
	var sv database.StrategyVersion
	err := row.Scan(
		&sv.StrategyVersionID,
		&sv.ConfigSnapshot,
		&sv.CreatedAt,
		&sv.ActivatedAt,
		&sv.DeactivatedAt,
		&sv.Status,
		&sv.ShadowStartedAt,
		&sv.PromotedAt,
		&sv.RolledBackAt,
		&sv.ParentVersionID,
	)
	if err != nil {
		return nil, err
	}
	return &sv, nil
}
