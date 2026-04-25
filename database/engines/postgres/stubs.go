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

func (d *DB) UpsertIngestionWatermark(ctx context.Context, chain string, blockNumber uint64) error {
	const q = `
		INSERT INTO ingestion_state (chain, last_processed_block, last_synced_at)
		VALUES ($1, $2, CURRENT_TIMESTAMP)
		ON CONFLICT (chain) DO UPDATE
		    SET last_processed_block = EXCLUDED.last_processed_block,
		        last_synced_at = CURRENT_TIMESTAMP`
	_, err := d.pool.ExecContext(ctx, q, chain, blockNumber)
	if err != nil {
		return fmt.Errorf("upsert ingestion watermark: %w", err)
	}
	return nil
}

func (d *DB) GetIngestionWatermark(ctx context.Context, chain string) (uint64, error) {
	const q = `SELECT last_processed_block FROM ingestion_state WHERE chain = $1`
	var block uint64
	err := d.pool.QueryRowContext(ctx, q, chain).Scan(&block)
	if err != nil {
		return 0, fmt.Errorf("get ingestion watermark: %w", err)
	}
	return block, nil
}

func (d *DB) InsertMarketData(ctx context.Context, dto contracts.MarketDataDTO) error {
	const q = `
		INSERT INTO market_data
		    (event_id, trace_id, correlation_id, causation_id, version_id,
		     chain, market, block_number, block_hash, tx_hash, log_index,
		     event_topic, pool_address, token_address, base_address,
		     token0_address, token1_address, amount0_raw, amount1_raw,
		     reserve_base_raw, reserve_token_raw, block_timestamp, ingested_at,
		     rpc_endpoint, transport, confirmation_depth, reorged)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22,$23,$24,$25,$26,$27)
		ON CONFLICT (event_id) DO NOTHING`
	_, err := d.pool.ExecContext(ctx, q,
		dto.EventID, dto.TraceID, dto.CorrelationID, dto.CausationID, dto.VersionID,
		dto.Chain, dto.Market, dto.BlockNumber, dto.BlockHash, dto.TxHash, dto.LogIndex,
		dto.EventTopic, dto.PoolAddress, dto.TokenAddress, dto.BaseAddress,
		dto.Token0Address, dto.Token1Address, dto.Amount0Raw, dto.Amount1Raw,
		dto.ReserveBaseRaw, dto.ReserveTokenRaw, dto.BlockTimestamp, dto.IngestedAt,
		dto.RpcEndpoint, dto.Transport, dto.ConfirmationDepth, dto.Reorged,
	)
	if err != nil {
		return fmt.Errorf("insert market data: %w", err)
	}
	return nil
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
