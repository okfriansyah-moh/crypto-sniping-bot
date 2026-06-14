package postgres

// Phase 7: Solana adapter methods.
// Table schemas are in database/migrations/20260101000012_solana_tables.sql.
// All SQL uses parameterized queries and ON CONFLICT portable semantics.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"crypto-sniping-bot/shared/database"
)

// UpsertSolanaEndpointState updates the circuit breaker state for a Solana endpoint.
func (d *DB) UpsertSolanaEndpointState(ctx context.Context, s database.SolanaEndpointState) error {
	const q = `
INSERT INTO solana_rpc_endpoint_state
    (endpoint_url, state, consecutive_failures, last_failure_at, circuit_opened_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (endpoint_url) DO UPDATE
    SET state                = EXCLUDED.state,
        consecutive_failures = EXCLUDED.consecutive_failures,
        last_failure_at      = EXCLUDED.last_failure_at,
        circuit_opened_at    = EXCLUDED.circuit_opened_at,
        updated_at           = EXCLUDED.updated_at
`
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := d.pool.ExecContext(ctx, q,
		s.EndpointURL, s.State, s.ConsecutiveFailures,
		nullableString(ptrString(s.LastFailureAt)),
		nullableString(ptrString(s.CircuitOpenedAt)),
		now,
	)
	if err != nil {
		return fmt.Errorf("upsert solana endpoint state: %w", err)
	}
	return nil
}

// GetSolanaEndpointState retrieves the circuit breaker state for an endpoint.
// Returns nil if not found.
func (d *DB) GetSolanaEndpointState(ctx context.Context, endpointURL string) (*database.SolanaEndpointState, error) {
	const q = `
SELECT endpoint_url, state, consecutive_failures,
       last_failure_at, circuit_opened_at, updated_at
FROM solana_rpc_endpoint_state
WHERE endpoint_url = $1
`
	var s database.SolanaEndpointState
	var lastFailure, circuitOpened sql.NullString
	err := d.pool.QueryRowContext(ctx, q, endpointURL).Scan(
		&s.EndpointURL, &s.State, &s.ConsecutiveFailures,
		&lastFailure, &circuitOpened, &s.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get solana endpoint state: %w", err)
	}
	if lastFailure.Valid {
		s.LastFailureAt = &lastFailure.String
	}
	if circuitOpened.Valid {
		s.CircuitOpenedAt = &circuitOpened.String
	}
	return &s, nil
}

// InsertSolanaSignature records a submitted Solana transaction.
// ON CONFLICT (execution_id) DO NOTHING — idempotent.
func (d *DB) InsertSolanaSignature(ctx context.Context, sig database.SolanaSignature) error {
	const q = `
INSERT INTO solana_signatures
    (execution_id, signature, status, slot, err_msg, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $6)
ON CONFLICT (execution_id) DO NOTHING
`
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := d.pool.ExecContext(ctx, q,
		sig.ExecutionID, sig.Signature, sig.Status, sig.Slot, sig.ErrMsg, now,
	)
	if err != nil {
		return fmt.Errorf("insert solana signature: %w", err)
	}
	return nil
}

// UpdateSolanaSignatureStatus transitions a signature's status.
func (d *DB) UpdateSolanaSignatureStatus(ctx context.Context, executionID string, status string, slot int64, errMsg string) error {
	const q = `
UPDATE solana_signatures
   SET status     = $2,
       slot       = $3,
       err_msg    = $4,
       updated_at = $5
 WHERE execution_id = $1
`
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := d.pool.ExecContext(ctx, q, executionID, status, slot, errMsg, now)
	if err != nil {
		return fmt.Errorf("update solana signature status: %w", err)
	}
	return nil
}

// UpsertSolanaEndpointHealth updates rolling health metrics for a Solana endpoint.
func (d *DB) UpsertSolanaEndpointHealth(ctx context.Context, h database.SolanaEndpointHealth) error {
	const q = `
INSERT INTO solana_endpoint_health
    (endpoint_url, p95_latency_ms, error_rate, success_count, failure_count, updated_at)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (endpoint_url) DO UPDATE
    SET p95_latency_ms = EXCLUDED.p95_latency_ms,
        error_rate     = EXCLUDED.error_rate,
        success_count  = EXCLUDED.success_count,
        failure_count  = EXCLUDED.failure_count,
        updated_at     = EXCLUDED.updated_at
`
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := d.pool.ExecContext(ctx, q,
		h.EndpointURL, h.P95LatencyMs, h.ErrorRate,
		h.SuccessCount, h.FailureCount, now,
	)
	if err != nil {
		return fmt.Errorf("upsert solana endpoint health: %w", err)
	}
	return nil
}

// ListSolanaEndpointsRanked returns all endpoint health rows ordered best-first.
func (d *DB) ListSolanaEndpointsRanked(ctx context.Context) ([]database.SolanaEndpointHealth, error) {
	const q = `
SELECT endpoint_url, p95_latency_ms, error_rate, success_count, failure_count, updated_at
FROM solana_endpoint_health
ORDER BY error_rate ASC, p95_latency_ms ASC
`
	rows, err := d.pool.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("list solana endpoints ranked: %w", err)
	}
	defer rows.Close()

	var result []database.SolanaEndpointHealth
	for rows.Next() {
		var h database.SolanaEndpointHealth
		if err := rows.Scan(
			&h.EndpointURL, &h.P95LatencyMs, &h.ErrorRate,
			&h.SuccessCount, &h.FailureCount, &h.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("list solana endpoints ranked: scan: %w", err)
		}
		result = append(result, h)
	}
	return result, rows.Err()
}

// UpsertSolanaIngestionWatermark records the last processed slot for a market.
func (d *DB) UpsertSolanaIngestionWatermark(ctx context.Context, market string, lastSlot uint64) error {
	const q = `
INSERT INTO solana_ingestion_watermark (market, last_slot, updated_at)
VALUES ($1, $2, $3)
ON CONFLICT (market) DO UPDATE
    SET last_slot  = EXCLUDED.last_slot,
        updated_at = EXCLUDED.updated_at
`
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := d.pool.ExecContext(ctx, q, market, lastSlot, now)
	if err != nil {
		return fmt.Errorf("upsert solana ingestion watermark: %w", err)
	}
	return nil
}

// GetSolanaIngestionWatermark returns the last processed slot for a market.
// Returns 0 if no watermark exists yet.
func (d *DB) GetSolanaIngestionWatermark(ctx context.Context, market string) (uint64, error) {
	const q = `SELECT last_slot FROM solana_ingestion_watermark WHERE market = $1`
	var slot uint64
	err := d.pool.QueryRowContext(ctx, q, market).Scan(&slot)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("get solana ingestion watermark: %w", err)
	}
	return slot, nil
}

// ptrString dereferences an optional string pointer to "".
func ptrString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
