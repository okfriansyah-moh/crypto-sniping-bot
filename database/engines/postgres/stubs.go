package postgres

// This file contains stub implementations of database.Adapter methods
// that belong to later phases (Phase 1–6). Each stub returns ErrNotImplemented
// until its migration is applied and the full implementation is added.
//
// Protected file: database/ is Phase 0 only. All methods must be declared here.

import (
	"context"
	"fmt"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
)

// ── Ingestion (Layer 0 — Phase 1) ────────────────────────────────────────────

// UpsertIngestionWatermark is a Phase 1 stub. The ingestion_state table is
// created in the Phase 1 migration. Returns ErrNotImplemented until then.
func (d *DB) UpsertIngestionWatermark(_ context.Context, _ string, _ uint64) error {
	return database.ErrNotImplemented
}

// GetIngestionWatermark is a Phase 1 stub. Returns ErrNotImplemented until
// the ingestion_state table is created in the Phase 1 migration.
func (d *DB) GetIngestionWatermark(_ context.Context, _ string) (uint64, error) {
	return 0, database.ErrNotImplemented
}

// InsertMarketData is a Phase 1 stub. The market_data table is created in
// the Phase 1 migration. Returns ErrNotImplemented until then.
func (d *DB) InsertMarketData(_ context.Context, _ contracts.MarketDataDTO) error {
	return database.ErrNotImplemented
}

func (d *DB) GetMarketData(_ context.Context, _ string) (*contracts.MarketDataDTO, error) {
	return nil, database.ErrNotImplemented
}

// ── Token Lifecycle State Machine (Phase 2) ───────────────────────────────────

func (d *DB) StartLifecycle(_ context.Context, _ contracts.MarketDataDTO) (string, error) {
	return "", database.ErrNotImplemented
}

func (d *DB) TransitionState(_ context.Context, _ database.TransitionRequest) error {
	return database.ErrNotImplemented
}

func (d *DB) GetLifecycle(_ context.Context, _ string) (*database.Lifecycle, error) {
	return nil, database.ErrNotImplemented
}

func (d *DB) GetLifecycleByToken(_ context.Context, _ string) (*database.Lifecycle, error) {
	return nil, database.ErrNotImplemented
}

func (d *DB) QuarantineToken(_ context.Context, _ string, _ string) error {
	return database.ErrNotImplemented
}

func (d *DB) InsertStateViolation(_ context.Context, _, _, _, _ string) error {
	return database.ErrNotImplemented
}

// ── DTO Persistence (Phase 2) ─────────────────────────────────────────────────

func (d *DB) InsertDataQuality(_ context.Context, _ contracts.DataQualityDTO) error {
	return database.ErrNotImplemented
}

func (d *DB) InsertFeature(_ context.Context, _ contracts.FeatureDTO) error {
	return database.ErrNotImplemented
}

func (d *DB) InsertEdge(_ context.Context, _ contracts.EdgeDTO) error {
	return database.ErrNotImplemented
}

func (d *DB) InsertValidatedEdge(_ context.Context, _ contracts.ValidatedEdgeDTO) error {
	return database.ErrNotImplemented
}

func (d *DB) InsertSelection(_ context.Context, _ contracts.SelectionOutputDTO) error {
	return database.ErrNotImplemented
}

func (d *DB) InsertAllocation(_ context.Context, _ contracts.AllocationDTO) error {
	return database.ErrNotImplemented
}

func (d *DB) InsertExecutionResult(_ context.Context, _ contracts.ExecutionResultDTO) error {
	return database.ErrNotImplemented
}

func (d *DB) InsertPositionState(_ context.Context, _ contracts.PositionStateDTO) error {
	return database.ErrNotImplemented
}

func (d *DB) InsertEvaluation(_ context.Context, _ contracts.EvaluationDTO) error {
	return database.ErrNotImplemented
}

func (d *DB) InsertLearningRecord(_ context.Context, _ contracts.LearningRecordDTO) error {
	return database.ErrNotImplemented
}

// ── Nonce Manager (Phase 2) ───────────────────────────────────────────────────

func (d *DB) AllocateNonce(_ context.Context, _ string, _ string) (uint64, error) {
	return 0, database.ErrNotImplemented
}

func (d *DB) ReconcileNonce(_ context.Context, _ string, _ string, _ uint64) error {
	return database.ErrNotImplemented
}

// ── Positions (Phase 2) ───────────────────────────────────────────────────────

func (d *DB) GetOpenPositions(_ context.Context) ([]contracts.PositionStateDTO, error) {
	return nil, database.ErrNotImplemented
}

func (d *DB) GetPosition(_ context.Context, _ string) (*contracts.PositionStateDTO, error) {
	return nil, database.ErrNotImplemented
}

// ── ActivateStrategyVersion wrapper for interface ─────────────────────────────

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
