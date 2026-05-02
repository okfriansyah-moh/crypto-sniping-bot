package postgres

// Nonce manager implementation for Phase 2 execution engine.
// Uses database-backed atomic increment to prevent nonce collisions.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
)

// AllocateNonce atomically reserves the next nonce for a wallet on a chain.
// If no nonce record exists, initializes from zero.
// Uses UPDATE ... RETURNING inside a transaction for atomic increment.
func (d *DB) AllocateNonce(ctx context.Context, walletAddress string, chain string) (uint64, error) {
	var nonce uint64
	err := d.withTx(ctx, func(tx *sql.Tx) error {
		now := time.Now().UTC().Format(time.RFC3339Nano)

		// Upsert: initialize to 0 if no record exists, then increment and return.
		const q = `
INSERT INTO wallet_nonce_state (wallet_address, chain, nonce_value, updated_at)
VALUES ($1, $2, 1, $3)
ON CONFLICT (wallet_address, chain) DO UPDATE
    SET nonce_value = wallet_nonce_state.nonce_value + 1,
        updated_at  = EXCLUDED.updated_at
RETURNING nonce_value`

		row := tx.QueryRowContext(ctx, q, walletAddress, chain, now)
		if err := row.Scan(&nonce); err != nil {
			return fmt.Errorf("allocate nonce scan: %w", err)
		}
		// The returned value is the post-increment value; subtract 1 to get the nonce to use.
		nonce--
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("allocate nonce: %w", err)
	}
	return nonce, nil
}

// ReconcileNonce updates the local nonce state to match the on-chain nonce.
// Called when an RPC "nonce too low" error is received to sync state with chain truth.
func (d *DB) ReconcileNonce(ctx context.Context, walletAddress string, chain string, onchainNonce uint64) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	const q = `
INSERT INTO wallet_nonce_state (wallet_address, chain, nonce_value, updated_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (wallet_address, chain) DO UPDATE
    SET nonce_value = GREATEST(wallet_nonce_state.nonce_value, EXCLUDED.nonce_value),
        updated_at  = EXCLUDED.updated_at`

	if _, err := d.pool.ExecContext(ctx, q, walletAddress, chain, onchainNonce, now); err != nil {
		return fmt.Errorf("reconcile nonce: %w", err)
	}
	return nil
}

// GetOpenPositions returns all currently open positions (latest snapshot per position).
func (d *DB) GetOpenPositions(ctx context.Context) ([]contracts.PositionStateDTO, error) {
	// Select the latest snapshot for each position_id where status = 'open'.
	const q = `
SELECT DISTINCT ON (position_id)
    event_id, trace_id, correlation_id, COALESCE(causation_id,''), version_id,
    token_lifecycle_id, position_id, execution_id, token_address, chain,
    status, entry_price, entry_size_usd, COALESCE(current_price,''),
    COALESCE(exit_price,''), COALESCE(exit_reason,''), pnl_usd, pnl_pct,
    tp1_bps, tp2_bps, sl_bps, max_hold_seconds,
    COALESCE(expires_at,''), priority, opened_at, COALESCE(exited_at,''), snapshot_at
FROM positions
WHERE status = 'open'
ORDER BY position_id, snapshot_at DESC`

	rows, err := d.pool.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("get open positions: %w", err)
	}
	defer rows.Close()

	var result []contracts.PositionStateDTO
	for rows.Next() {
		dto, err := scanPositionState(rows)
		if err != nil {
			return nil, fmt.Errorf("get open positions: scan: %w", err)
		}
		result = append(result, dto)
	}
	return result, rows.Err()
}

// GetPosition fetches the latest snapshot of a single position by ID.
func (d *DB) GetPosition(ctx context.Context, positionID string) (*contracts.PositionStateDTO, error) {
	const q = `
SELECT
    event_id, trace_id, correlation_id, COALESCE(causation_id,''), version_id,
    token_lifecycle_id, position_id, execution_id, token_address, chain,
    status, entry_price, entry_size_usd, COALESCE(current_price,''),
    COALESCE(exit_price,''), COALESCE(exit_reason,''), pnl_usd, pnl_pct,
    tp1_bps, tp2_bps, sl_bps, max_hold_seconds,
    COALESCE(expires_at,''), priority, opened_at, COALESCE(exited_at,''), snapshot_at
FROM positions
WHERE position_id = $1
ORDER BY snapshot_at DESC
LIMIT 1`

	rows, err := d.pool.QueryContext(ctx, q, positionID)
	if err != nil {
		return nil, fmt.Errorf("get position: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, database.ErrNotFound
	}
	dto, err := scanPositionState(rows)
	if err != nil {
		return nil, fmt.Errorf("get position: scan: %w", err)
	}
	return &dto, rows.Err()
}

func scanPositionState(rows *sql.Rows) (contracts.PositionStateDTO, error) {
	var dto contracts.PositionStateDTO
	err := rows.Scan(
		&dto.EventID, &dto.TraceID, &dto.CorrelationID, &dto.CausationID, &dto.VersionID,
		&dto.TokenLifecycleID, &dto.PositionID, &dto.ExecutionID, &dto.TokenAddress, &dto.Chain,
		&dto.Status, &dto.EntryPrice, &dto.EntrySizeUsd, &dto.CurrentPrice,
		&dto.ExitPrice, &dto.ExitReason, &dto.PnlUsd, &dto.PnlPct,
		&dto.Tp1Bps, &dto.Tp2Bps, &dto.SlBps, &dto.MaxHoldSeconds,
		&dto.ExpiresAt, &dto.Priority, &dto.OpenedAt, &dto.ExitedAt, &dto.SnapshotAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return contracts.PositionStateDTO{}, database.ErrNotFound
	}
	if err != nil {
		return contracts.PositionStateDTO{}, fmt.Errorf("scan position state: %w", err)
	}
	return dto, nil
}

// GetClosedPositions returns latest snapshots for positions that exited within
// the last sinceSeconds. Used by /pnl for win-rate and realized PnL summaries.
// sinceSeconds <= 0 defaults to the last 7 days.
func (d *DB) GetClosedPositions(ctx context.Context, sinceSeconds int) ([]contracts.PositionStateDTO, error) {
	if sinceSeconds <= 0 {
		sinceSeconds = 7 * 24 * 3600
	}
	since := time.Now().UTC().Add(-time.Duration(sinceSeconds) * time.Second).Format(time.RFC3339Nano)

	// DISTINCT ON requires ORDER BY to start with position_id so that
	// PostgreSQL selects the latest snapshot per position. The outer
	// query then re-orders globally by exited_at DESC so callers receive
	// newest-first results as documented.
	const q = `
SELECT event_id, trace_id, correlation_id, causation_id, version_id,
    token_lifecycle_id, position_id, execution_id, token_address, chain,
    status, entry_price, entry_size_usd, current_price,
    exit_price, exit_reason, pnl_usd, pnl_pct,
    tp1_bps, tp2_bps, sl_bps, max_hold_seconds,
    expires_at, priority, opened_at, exited_at, snapshot_at
FROM (
    SELECT DISTINCT ON (position_id)
        event_id, trace_id, correlation_id, COALESCE(causation_id,'') AS causation_id, version_id,
        token_lifecycle_id, position_id, execution_id, token_address, chain,
        status, entry_price, entry_size_usd, COALESCE(current_price,'') AS current_price,
        COALESCE(exit_price,'') AS exit_price, COALESCE(exit_reason,'') AS exit_reason, pnl_usd, pnl_pct,
        tp1_bps, tp2_bps, sl_bps, max_hold_seconds,
        COALESCE(expires_at,'') AS expires_at, priority, opened_at, COALESCE(exited_at,'') AS exited_at, snapshot_at
    FROM positions
    WHERE status IN ('exited', 'closed')
      AND COALESCE(exited_at, snapshot_at) >= $1
    ORDER BY position_id, snapshot_at DESC
) latest
ORDER BY COALESCE(exited_at, snapshot_at) DESC`

	rows, err := d.pool.QueryContext(ctx, q, since)
	if err != nil {
		return nil, fmt.Errorf("get closed positions: %w", err)
	}
	defer rows.Close()

	var result []contracts.PositionStateDTO
	for rows.Next() {
		dto, err := scanPositionState(rows)
		if err != nil {
			return nil, fmt.Errorf("get closed positions: scan: %w", err)
		}
		result = append(result, dto)
	}
	return result, rows.Err()
}

// FindPositionByPrefix returns the latest snapshot of an OPEN position whose
// position_id or token_address starts with prefix (case-insensitive). Returns
// database.ErrAmbiguous when more than one open position matches.
func (d *DB) FindPositionByPrefix(ctx context.Context, prefix string) (*contracts.PositionStateDTO, error) {
	if prefix == "" {
		return nil, database.ErrNotFound
	}
	pattern := prefix + "%"

	const q = `
WITH latest AS (
    SELECT DISTINCT ON (position_id)
        event_id, trace_id, correlation_id, COALESCE(causation_id,'') AS causation_id, version_id,
        token_lifecycle_id, position_id, execution_id, token_address, chain,
        status, entry_price, entry_size_usd, COALESCE(current_price,'') AS current_price,
        COALESCE(exit_price,'') AS exit_price, COALESCE(exit_reason,'') AS exit_reason, pnl_usd, pnl_pct,
        tp1_bps, tp2_bps, sl_bps, max_hold_seconds,
        COALESCE(expires_at,'') AS expires_at, priority, opened_at, COALESCE(exited_at,'') AS exited_at, snapshot_at
    FROM positions
    WHERE status = 'open'
    ORDER BY position_id, snapshot_at DESC
)
SELECT * FROM latest
WHERE position_id ILIKE $1 OR token_address ILIKE $1
LIMIT 2`

	rows, err := d.pool.QueryContext(ctx, q, pattern)
	if err != nil {
		return nil, fmt.Errorf("find position by prefix: %w", err)
	}
	defer rows.Close()

	if !rows.Next() {
		return nil, database.ErrNotFound
	}
	first, err := scanPositionState(rows)
	if err != nil {
		return nil, fmt.Errorf("find position by prefix: scan: %w", err)
	}
	if rows.Next() {
		return nil, database.ErrAmbiguous
	}
	return &first, rows.Err()
}
