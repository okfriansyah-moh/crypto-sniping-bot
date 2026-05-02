package postgres

// Real implementations of the ingestion adapter methods (Phase 1).
// Table schemas are in database/migrations/20260101000007_ingestion_tables.sql.
// All SQL uses parameterized queries and ON CONFLICT DO NOTHING semantics.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"crypto-sniping-bot/contracts"
)

// UpsertIngestionWatermark records the last processed block for a chain.
// INSERT ... ON CONFLICT UPDATE ensures idempotency.
func (d *DB) UpsertIngestionWatermark(ctx context.Context, chain string, blockNumber uint64) error {
	const q = `
INSERT INTO ingestion_state (chain, last_processed_block, updated_at)
VALUES ($1, $2, $3)
ON CONFLICT (chain) DO UPDATE
    SET last_processed_block = EXCLUDED.last_processed_block,
        updated_at           = EXCLUDED.updated_at
`
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := d.pool.ExecContext(ctx, q, chain, blockNumber, now); err != nil {
		return fmt.Errorf("upsert ingestion watermark: %w", err)
	}
	return nil
}

// GetIngestionWatermark returns the last processed block for a chain.
// Returns 0 (no error) if no watermark exists yet.
func (d *DB) GetIngestionWatermark(ctx context.Context, chain string) (uint64, error) {
	const q = `SELECT last_processed_block FROM ingestion_state WHERE chain = $1`
	var block uint64
	err := d.pool.QueryRowContext(ctx, q, chain).Scan(&block)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("get ingestion watermark: %w", err)
	}
	return block, nil
}

// InsertMarketData persists a MarketDataDTO.
// ON CONFLICT DO NOTHING — safe to call multiple times with the same event_id.
func (d *DB) InsertMarketData(ctx context.Context, dto contracts.MarketDataDTO) error {
	const q = `
INSERT INTO market_data (
    event_id, trace_id, correlation_id, causation_id, version_id,
    chain, market, block_number, block_hash, tx_hash, log_index,
    event_topic, pool_address, token_address, base_address,
    token0_address, token1_address,
    amount0_raw, amount1_raw, reserve_base_raw, reserve_token_raw,
    block_timestamp, ingested_at, rpc_endpoint, transport,
    confirmation_depth, reorged, expires_at, priority,
    symbol, name,
    liquidity_usd, lp_stats_known, wash_stats_known,
    tx_count_1m, unique_wallets_1m, wallet_entropy, repeat_ratio_1m,
    holder_dist_known, holder_count, top5_holder_pct, pool_age_seconds
) VALUES (
    $1,  $2,  $3,  $4,  $5,
    $6,  $7,  $8,  $9,  $10, $11,
    $12, $13, $14, $15,
    $16, $17,
    $18, $19, $20, $21,
    $22, $23, $24, $25,
    $26, $27, $28, $29,
    $30, $31,
    $32, $33, $34,
    $35, $36, $37, $38,
    $39, $40, $41, $42
)
ON CONFLICT (event_id) DO NOTHING
`
	causationID := nullableString(dto.CausationID)
	expiresAt := nullableString(dto.ExpiresAt)

	_, err := d.pool.ExecContext(ctx, q,
		dto.EventID, dto.TraceID, dto.CorrelationID, causationID, dto.VersionID,
		dto.Chain, dto.Market, dto.BlockNumber, dto.BlockHash, dto.TxHash, dto.LogIndex,
		dto.EventTopic, dto.PoolAddress, dto.TokenAddress, dto.BaseAddress,
		dto.Token0Address, dto.Token1Address,
		dto.Amount0Raw, dto.Amount1Raw, dto.ReserveBaseRaw, dto.ReserveTokenRaw,
		dto.BlockTimestamp, dto.IngestedAt, dto.RpcEndpoint, dto.Transport,
		dto.ConfirmationDepth, dto.Reorged, expiresAt, dto.Priority,
		dto.Symbol, dto.Name,
		dto.LiquidityUsd, dto.LpStatsKnown, dto.WashStatsKnown,
		dto.TxCount1m, dto.UniqueWallets1m, dto.WalletEntropy, dto.RepeatRatio1m,
		dto.HolderDistKnown, dto.HolderCount, dto.Top5HolderPct, dto.PoolAgeSeconds,
	)
	if err != nil {
		return fmt.Errorf("insert market data: %w", err)
	}
	return nil
}

// GetMarketData retrieves a MarketDataDTO by event_id.
// Returns nil (no error) if not found.
func (d *DB) GetMarketData(ctx context.Context, eventID string) (*contracts.MarketDataDTO, error) {
	const q = `
SELECT
    event_id, trace_id, correlation_id, COALESCE(causation_id, ''), version_id,
    chain, market, block_number, block_hash, tx_hash, log_index,
    event_topic, pool_address, token_address, base_address,
    token0_address, token1_address,
    amount0_raw, amount1_raw, reserve_base_raw, reserve_token_raw,
    block_timestamp, ingested_at, rpc_endpoint, transport,
    confirmation_depth, reorged, COALESCE(expires_at, ''), priority,
    COALESCE(liquidity_usd, 0.0),
    COALESCE(lp_stats_known, FALSE),
    COALESCE(wash_stats_known, FALSE),
    COALESCE(tx_count_1m, 0),
    COALESCE(unique_wallets_1m, 0),
    COALESCE(wallet_entropy, 0.0),
    COALESCE(repeat_ratio_1m, 0.0),
    COALESCE(holder_dist_known, FALSE),
    COALESCE(holder_count, 0),
    COALESCE(top5_holder_pct, 0.0),
    COALESCE(pool_age_seconds, 0)
FROM market_data
WHERE event_id = $1
`
	var dto contracts.MarketDataDTO
	err := d.pool.QueryRowContext(ctx, q, eventID).Scan(
		&dto.EventID, &dto.TraceID, &dto.CorrelationID, &dto.CausationID, &dto.VersionID,
		&dto.Chain, &dto.Market, &dto.BlockNumber, &dto.BlockHash, &dto.TxHash, &dto.LogIndex,
		&dto.EventTopic, &dto.PoolAddress, &dto.TokenAddress, &dto.BaseAddress,
		&dto.Token0Address, &dto.Token1Address,
		&dto.Amount0Raw, &dto.Amount1Raw, &dto.ReserveBaseRaw, &dto.ReserveTokenRaw,
		&dto.BlockTimestamp, &dto.IngestedAt, &dto.RpcEndpoint, &dto.Transport,
		&dto.ConfirmationDepth, &dto.Reorged, &dto.ExpiresAt, &dto.Priority,
		&dto.LiquidityUsd, &dto.LpStatsKnown, &dto.WashStatsKnown,
		&dto.TxCount1m, &dto.UniqueWallets1m, &dto.WalletEntropy, &dto.RepeatRatio1m,
		&dto.HolderDistKnown, &dto.HolderCount, &dto.Top5HolderPct, &dto.PoolAgeSeconds,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get market data: %w", err)
	}
	return &dto, nil
}

// nullableString converts an empty string to nil (SQL NULL), otherwise returns the value.
func nullableString(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
