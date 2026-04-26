package postgres

// Phase 4 Signal Quality persistence: probability, slippage, latency model outputs.
// All SQL uses portable syntax (ON CONFLICT DO NOTHING, parameterized queries).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"crypto-sniping-bot/contracts"
)

// ── Probability ───────────────────────────────────────────────────────────────

// InsertProbabilityEstimate persists a ProbabilityEstimateDTO.
// ON CONFLICT DO NOTHING — idempotent per event_id.
func (d *DB) InsertProbabilityEstimate(ctx context.Context, dto contracts.ProbabilityEstimateDTO) error {
	const q = `
INSERT INTO probability_estimates (
    event_id, trace_id, correlation_id, causation_id, version_id,
    token_lifecycle_id, probability, calibration, model_version_id,
    expires_at, priority, estimated_at
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9,
    $10, $11, $12
)
ON CONFLICT (event_id) DO NOTHING`

	_, err := d.pool.ExecContext(ctx, q,
		dto.EventID, dto.TraceID, dto.CorrelationID, nullableString(dto.CausationID), dto.VersionID,
		dto.TokenLifecycleID, dto.Probability, dto.Calibration, dto.ModelVersionID,
		nullableString(dto.ExpiresAt), dto.Priority, dto.EstimatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert probability estimate: %w", err)
	}
	return nil
}

// GetProbabilityEstimateByTrace fetches the most-recent probability estimate by trace.
func (d *DB) GetProbabilityEstimateByTrace(ctx context.Context, traceID string) (*contracts.ProbabilityEstimateDTO, error) {
	const q = `
SELECT event_id, trace_id, correlation_id, COALESCE(causation_id, ''), version_id,
       token_lifecycle_id, probability, calibration, model_version_id,
       COALESCE(expires_at, ''), priority, estimated_at
FROM probability_estimates
WHERE trace_id = $1
ORDER BY inserted_at DESC
LIMIT 1`

	var dto contracts.ProbabilityEstimateDTO
	err := d.pool.QueryRowContext(ctx, q, traceID).Scan(
		&dto.EventID, &dto.TraceID, &dto.CorrelationID, &dto.CausationID, &dto.VersionID,
		&dto.TokenLifecycleID, &dto.Probability, &dto.Calibration, &dto.ModelVersionID,
		&dto.ExpiresAt, &dto.Priority, &dto.EstimatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get probability estimate by trace: %w", err)
	}
	return &dto, nil
}

// ── Slippage ──────────────────────────────────────────────────────────────────

// InsertSlippageEstimate persists a SlippageEstimateDTO.
func (d *DB) InsertSlippageEstimate(ctx context.Context, dto contracts.SlippageEstimateDTO) error {
	const q = `
INSERT INTO slippage_estimates (
    event_id, trace_id, correlation_id, causation_id, version_id,
    token_lifecycle_id, expected_p50_bps, expected_p95_bps, model_version_id,
    expires_at, priority, estimated_at
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9,
    $10, $11, $12
)
ON CONFLICT (event_id) DO NOTHING`

	_, err := d.pool.ExecContext(ctx, q,
		dto.EventID, dto.TraceID, dto.CorrelationID, nullableString(dto.CausationID), dto.VersionID,
		dto.TokenLifecycleID, dto.ExpectedP50Bps, dto.ExpectedP95Bps, dto.ModelVersionID,
		nullableString(dto.ExpiresAt), dto.Priority, dto.EstimatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert slippage estimate: %w", err)
	}
	return nil
}

// GetSlippageEstimateByTrace fetches the most-recent slippage estimate by trace.
func (d *DB) GetSlippageEstimateByTrace(ctx context.Context, traceID string) (*contracts.SlippageEstimateDTO, error) {
	const q = `
SELECT event_id, trace_id, correlation_id, COALESCE(causation_id, ''), version_id,
       token_lifecycle_id, expected_p50_bps, expected_p95_bps, model_version_id,
       COALESCE(expires_at, ''), priority, estimated_at
FROM slippage_estimates
WHERE trace_id = $1
ORDER BY inserted_at DESC
LIMIT 1`

	var dto contracts.SlippageEstimateDTO
	err := d.pool.QueryRowContext(ctx, q, traceID).Scan(
		&dto.EventID, &dto.TraceID, &dto.CorrelationID, &dto.CausationID, &dto.VersionID,
		&dto.TokenLifecycleID, &dto.ExpectedP50Bps, &dto.ExpectedP95Bps, &dto.ModelVersionID,
		&dto.ExpiresAt, &dto.Priority, &dto.EstimatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get slippage estimate by trace: %w", err)
	}
	return &dto, nil
}

// ── Latency ───────────────────────────────────────────────────────────────────

// InsertLatencyProfile persists a LatencyProfileDTO.
func (d *DB) InsertLatencyProfile(ctx context.Context, dto contracts.LatencyProfileDTO) error {
	const q = `
INSERT INTO latency_profiles (
    event_id, trace_id, correlation_id, causation_id, version_id,
    chain, expected_p50_ms, expected_p95_ms, window_size_seconds,
    expires_at, priority, estimated_at
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9,
    $10, $11, $12
)
ON CONFLICT (event_id) DO NOTHING`

	_, err := d.pool.ExecContext(ctx, q,
		dto.EventID, dto.TraceID, dto.CorrelationID, nullableString(dto.CausationID), dto.VersionID,
		dto.Chain, dto.ExpectedP50Ms, dto.ExpectedP95Ms, dto.WindowSizeSeconds,
		nullableString(dto.ExpiresAt), dto.Priority, dto.EstimatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert latency profile: %w", err)
	}
	return nil
}

// GetLatestLatencyProfile fetches the most-recent latency profile for a chain.
func (d *DB) GetLatestLatencyProfile(ctx context.Context, chain string) (*contracts.LatencyProfileDTO, error) {
	const q = `
SELECT event_id, trace_id, correlation_id, COALESCE(causation_id, ''), version_id,
       chain, expected_p50_ms, expected_p95_ms, window_size_seconds,
       COALESCE(expires_at, ''), priority, estimated_at
FROM latency_profiles
WHERE chain = $1
ORDER BY inserted_at DESC
LIMIT 1`

	var dto contracts.LatencyProfileDTO
	err := d.pool.QueryRowContext(ctx, q, chain).Scan(
		&dto.EventID, &dto.TraceID, &dto.CorrelationID, &dto.CausationID, &dto.VersionID,
		&dto.Chain, &dto.ExpectedP50Ms, &dto.ExpectedP95Ms, &dto.WindowSizeSeconds,
		&dto.ExpiresAt, &dto.Priority, &dto.EstimatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get latest latency profile: %w", err)
	}
	return &dto, nil
}
