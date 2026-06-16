package postgres

// probe_pending.go — Postgres implementation of the probe pending queue.
// Tokens deferred when Helius probe budget is exhausted are stored here and
// drained by the ProbePendingWorker when due_at <= NOW().

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
)

const maxProbePendingPayloadBytes = 64 * 1024

// EnqueueProbePending inserts a deferred probe row. Idempotent on pending_id.
func (d *DB) EnqueueProbePending(ctx context.Context, req database.ProbePendingEnqueue) error {
	payload, err := json.Marshal(req.Payload)
	if err != nil {
		return fmt.Errorf("enqueue probe pending: marshal: %w", err)
	}
	if len(payload) > maxProbePendingPayloadBytes {
		return fmt.Errorf("enqueue probe pending: payload exceeds %d bytes", maxProbePendingPayloadBytes)
	}

	const q = `
INSERT INTO probe_pending_queue (
    pending_id, source_event_id, token_address, chain, market,
    priority, payload, enqueued_at, due_at, status, attempt_count, last_error
) VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9, 'pending', 0, '')
ON CONFLICT (pending_id) DO NOTHING`

	now := time.Now().UTC()
	if req.EnqueuedAt.IsZero() {
		req.EnqueuedAt = now
	}

	_, err = d.pool.ExecContext(ctx, q,
		req.PendingID,
		req.SourceEventID,
		req.TokenAddress,
		req.Chain,
		req.Market,
		req.Priority,
		payload,
		req.EnqueuedAt,
		req.DueAt,
	)
	if err != nil {
		return fmt.Errorf("enqueue probe pending: %w", err)
	}
	return nil
}

// ClaimDueProbePending atomically claims up to limit pending rows due for processing.
func (d *DB) ClaimDueProbePending(ctx context.Context, limit int) ([]database.ProbePendingRow, error) {
	if limit <= 0 {
		limit = 10
	}

	tx, err := d.pool.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("claim probe pending: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	const sel = `
SELECT pending_id, source_event_id, token_address, chain, market,
       priority, payload, enqueued_at, due_at, attempt_count
FROM probe_pending_queue
WHERE status = 'pending' AND due_at <= NOW()
ORDER BY priority ASC, enqueued_at ASC
LIMIT $1
FOR UPDATE SKIP LOCKED`

	rows, err := tx.QueryContext(ctx, sel, limit)
	if err != nil {
		return nil, fmt.Errorf("claim probe pending: select: %w", err)
	}
	defer rows.Close()

	var claimed []database.ProbePendingRow
	var ids []string
	for rows.Next() {
		var row database.ProbePendingRow
		var payload []byte
		if err := rows.Scan(
			&row.PendingID,
			&row.SourceEventID,
			&row.TokenAddress,
			&row.Chain,
			&row.Market,
			&row.Priority,
			&payload,
			&row.EnqueuedAt,
			&row.DueAt,
			&row.AttemptCount,
		); err != nil {
			return nil, fmt.Errorf("claim probe pending: scan: %w", err)
		}
		if err := json.Unmarshal(payload, &row.Payload); err != nil {
			return nil, fmt.Errorf("claim probe pending: unmarshal payload: %w", err)
		}
		claimed = append(claimed, row)
		ids = append(ids, row.PendingID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claim probe pending: rows: %w", err)
	}
	if len(ids) == 0 {
		return []database.ProbePendingRow{}, nil
	}

	const upd = `
UPDATE probe_pending_queue
SET status = 'claimed', attempt_count = attempt_count + 1
WHERE pending_id = ANY($1)`

	if _, err := tx.ExecContext(ctx, upd, ids); err != nil {
		return nil, fmt.Errorf("claim probe pending: update: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("claim probe pending: commit: %w", err)
	}
	return claimed, nil
}

// CompleteProbePending marks a row completed after successful re-emission.
func (d *DB) CompleteProbePending(ctx context.Context, pendingID string) error {
	const q = `UPDATE probe_pending_queue SET status = 'completed' WHERE pending_id = $1`
	if _, err := d.pool.ExecContext(ctx, q, pendingID); err != nil {
		return fmt.Errorf("complete probe pending: %w", err)
	}
	return nil
}

// FailProbePending returns a claimed row to pending or marks it expired.
func (d *DB) FailProbePending(ctx context.Context, pendingID, errMsg string, maxAttempts int) error {
	if maxAttempts <= 0 {
		maxAttempts = 24
	}
	const q = `
UPDATE probe_pending_queue
SET status = CASE WHEN attempt_count >= $3 THEN 'expired' ELSE 'pending' END,
    last_error = $2
WHERE pending_id = $1`
	if _, err := d.pool.ExecContext(ctx, q, pendingID, errMsg, maxAttempts); err != nil {
		return fmt.Errorf("fail probe pending: %w", err)
	}
	return nil
}

// ExpireStaleProbePending marks pending rows older than ttlHours as expired.
func (d *DB) ExpireStaleProbePending(ctx context.Context, ttlHours int) (int64, error) {
	if ttlHours <= 0 {
		ttlHours = 48
	}
	const q = `
UPDATE probe_pending_queue
SET status = 'expired', last_error = 'ttl_exceeded'
WHERE status IN ('pending', 'claimed')
  AND enqueued_at < NOW() - ($1 * INTERVAL '1 hour')`

	res, err := d.pool.ExecContext(ctx, q, ttlHours)
	if err != nil {
		return 0, fmt.Errorf("expire stale probe pending: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// GetProbePendingStats returns queue depth metrics for the operator dashboard.
func (d *DB) GetProbePendingStats(ctx context.Context) (*database.ProbePendingStats, error) {
	const q = `
SELECT
    COUNT(*) FILTER (WHERE status = 'pending') AS pending_count,
    COUNT(*) FILTER (WHERE status = 'pending' AND due_at <= NOW()) AS due_now,
    COUNT(*) FILTER (WHERE status = 'expired' AND enqueued_at >= NOW() - INTERVAL '24 hours') AS expired_24h,
    COUNT(*) FILTER (WHERE status = 'completed' AND enqueued_at >= NOW() - INTERVAL '24 hours') AS completed_24h,
    COUNT(*) FILTER (WHERE enqueued_at >= NOW() - INTERVAL '24 hours') AS deferred_24h
FROM probe_pending_queue`

	stats := &database.ProbePendingStats{}
	if err := d.pool.QueryRowContext(ctx, q).Scan(
		&stats.PendingCount,
		&stats.DueNow,
		&stats.Expired24h,
		&stats.Completed24h,
		&stats.Deferred24h,
	); err != nil {
		return nil, fmt.Errorf("get probe pending stats: %w", err)
	}
	return stats, nil
}

// GetLatestMarketDataForToken returns the most recent market_data row for a token.
func (d *DB) GetLatestMarketDataForToken(ctx context.Context, chain, tokenAddress string) (*contracts.MarketDataDTO, error) {
	const q = `
SELECT
    event_id, trace_id, correlation_id, COALESCE(causation_id, ''), version_id,
    chain, market, block_number, block_hash, tx_hash, log_index,
    event_topic, pool_address, token_address, base_address,
    token0_address, token1_address,
    amount0_raw, amount1_raw, reserve_base_raw, reserve_token_raw,
    block_timestamp, ingested_at, rpc_endpoint, transport,
    confirmation_depth, reorged, COALESCE(expires_at, ''), priority,
    COALESCE(symbol, ''), COALESCE(name, ''),
    COALESCE(liquidity_usd, 0), COALESCE(lp_stats_known, FALSE), COALESCE(wash_stats_known, FALSE),
    COALESCE(tx_count_1m, 0), COALESCE(unique_wallets_1m, 0), COALESCE(wallet_entropy, 0),
    COALESCE(repeat_ratio_1m, 0),
    COALESCE(holder_dist_known, FALSE), COALESCE(holder_count, 0),
    COALESCE(top5_holder_pct, 0), COALESCE(pool_age_seconds, 0),
    COALESCE(creator_address, '')
FROM market_data
WHERE chain = $1 AND token_address = $2
ORDER BY ingested_at DESC NULLS LAST
LIMIT 1`

	var dto contracts.MarketDataDTO
	err := d.pool.QueryRowContext(ctx, q, chain, tokenAddress).Scan(
		&dto.EventID, &dto.TraceID, &dto.CorrelationID, &dto.CausationID, &dto.VersionID,
		&dto.Chain, &dto.Market, &dto.BlockNumber, &dto.BlockHash, &dto.TxHash, &dto.LogIndex,
		&dto.EventTopic, &dto.PoolAddress, &dto.TokenAddress, &dto.BaseAddress,
		&dto.Token0Address, &dto.Token1Address,
		&dto.Amount0Raw, &dto.Amount1Raw, &dto.ReserveBaseRaw, &dto.ReserveTokenRaw,
		&dto.BlockTimestamp, &dto.IngestedAt, &dto.RpcEndpoint, &dto.Transport,
		&dto.ConfirmationDepth, &dto.Reorged, &dto.ExpiresAt, &dto.Priority,
		&dto.Symbol, &dto.Name,
		&dto.LiquidityUsd, &dto.LpStatsKnown, &dto.WashStatsKnown,
		&dto.TxCount1m, &dto.UniqueWallets1m, &dto.WalletEntropy,
		&dto.RepeatRatio1m,
		&dto.HolderDistKnown, &dto.HolderCount,
		&dto.Top5HolderPct, &dto.PoolAgeSeconds,
		&dto.CreatorAddress,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest market data for token: %w", err)
	}
	return &dto, nil
}

// ProbePendingID delegates to the shared database helper.
func ProbePendingID(sourceEventID string, dueAt time.Time) string {
	return database.ProbePendingID(sourceEventID, dueAt)
}
