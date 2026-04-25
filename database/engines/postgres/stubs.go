package postgres

// This file contains stub implementations of database.Adapter methods
// that belong to later phases (Phase 3+). Each stub returns ErrNotImplemented
// until its migration is applied and the full implementation is added.
//
// Phase 2 methods have been moved to their own files:
//   - lifecycle.go   — token lifecycle state machine
//   - trading.go     — DTO persistence (data_quality, features, edges, etc.)
//   - positions.go   — nonce manager, open positions, GetPosition

import (
	"context"
	"fmt"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
)

// pinActivatorAdapter wraps DB to satisfy PinStrategyVersion's interface.
type pinActivatorAdapter struct {
	db *DB
}

func (a *pinActivatorAdapter) CreateStrategyVersion(ctx context.Context, sv database.StrategyVersion) error {
	return a.db.CreateStrategyVersion(ctx, sv)
}

func (a *pinActivatorAdapter) ActivateStrategyVersion(ctx context.Context, id string) error {
	return a.db.ActivateStrategyVersion(ctx, id)
}

// PinConfig creates and activates a StrategyVersion from the given config JSON.
// Returns the active StrategyVersionID.
func (d *DB) PinConfig(ctx context.Context, configJSON []byte) (string, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	versionID := fmt.Sprintf("%x", hashBytes(configJSON)[:8])

	sv := database.StrategyVersion{
		StrategyVersionID: versionID,
		ConfigSnapshot:    configJSON,
		CreatedAt:         now,
	}

	if err := d.CreateStrategyVersion(ctx, sv); err != nil {
		return "", err
	}
	if err := d.ActivateStrategyVersion(ctx, versionID); err != nil {
		return "", err
	}
	return versionID, nil
}

// ── §11.2 Production Gap Extensions (Phase 5+) ───────────────────────────────

// MarkEventExpired is a Phase 5 stub for event TTL expiry.
func (d *DB) MarkEventExpired(_ context.Context, _ string, _ string) error {
return database.ErrNotImplemented
}

// GetSystemState is a Phase 5 stub for the system_state singleton.
func (d *DB) GetSystemState(_ context.Context) (*contracts.SystemStateDTO, error) {
return nil, database.ErrNotImplemented
}

// UpsertSystemState is a Phase 5 stub for system state CAS update.
func (d *DB) UpsertSystemState(_ context.Context, _ contracts.SystemStateDTO, _ int64) (int64, error) {
return 0, database.ErrNotImplemented
}

// GetExposureSummary is a Phase 5 stub for aggregated capital exposure.
func (d *DB) GetExposureSummary(_ context.Context) (*database.ExposureSummary, error) {
return nil, database.ErrNotImplemented
}

// SetStrategyVersionStatus is a Phase 5 stub for strategy version lifecycle.
func (d *DB) SetStrategyVersionStatus(_ context.Context, _ string, _ string, _ string) error {
return database.ErrNotImplemented
}

// GetActiveStrategy is a Phase 5 stub returning the active StrategyVersion.
func (d *DB) GetActiveStrategy(_ context.Context) (*database.StrategyVersion, error) {
return nil, database.ErrNotImplemented
}

// GetShadowStrategy is a Phase 5 stub returning the shadow StrategyVersion, if any.
func (d *DB) GetShadowStrategy(_ context.Context) (*database.StrategyVersion, error) {
return nil, database.ErrNotImplemented
}

// ArchiveEvents is a Phase 5 stub for moving old processed events to archive.
func (d *DB) ArchiveEvents(_ context.Context, _ time.Time, _ int) (int, error) {
return 0, database.ErrNotImplemented
}

// GetEventsByTraceIncludeArchive is a Phase 5 stub for full-history trace queries.
func (d *DB) GetEventsByTraceIncludeArchive(_ context.Context, _ string) ([]contracts.EventEnvelope, error) {
return nil, database.ErrNotImplemented
}
