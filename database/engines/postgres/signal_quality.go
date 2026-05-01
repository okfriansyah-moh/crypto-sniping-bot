package postgres

// Phase 4 Signal Quality persistence: probability, slippage, latency model outputs.
// All SQL uses portable syntax (ON CONFLICT DO NOTHING, parameterized queries).

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
)

// ── Probability ───────────────────────────────────────────────────────────────

// InsertProbabilityEstimate persists a ProbabilityEstimateDTO.
// ON CONFLICT DO NOTHING — idempotent per event_id.
func (d *DB) InsertProbabilityEstimate(ctx context.Context, dto contracts.ProbabilityEstimateDTO) error {
	const q = `
INSERT INTO probability_estimates (
    event_id, trace_id, correlation_id, causation_id, version_id,
    token_lifecycle_id, probability, calibration, model_version_id,
    expires_at, priority, estimated_at,
    confidence, calibration_bin
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9,
    $10, $11, $12,
    $13, $14
)
ON CONFLICT (event_id) DO NOTHING`

	_, err := d.pool.ExecContext(ctx, q,
		dto.EventID, dto.TraceID, dto.CorrelationID, nullableString(dto.CausationID), dto.VersionID,
		dto.TokenLifecycleID, dto.Probability, dto.Calibration, dto.ModelVersionID,
		nullableString(dto.ExpiresAt), dto.Priority, dto.EstimatedAt,
		dto.Confidence, dto.CalibrationBin,
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
       COALESCE(expires_at, ''), priority, estimated_at,
       COALESCE(confidence, 0), COALESCE(calibration_bin, 0)
FROM probability_estimates
WHERE trace_id = $1
ORDER BY inserted_at DESC
LIMIT 1`

	var dto contracts.ProbabilityEstimateDTO
	err := d.pool.QueryRowContext(ctx, q, traceID).Scan(
		&dto.EventID, &dto.TraceID, &dto.CorrelationID, &dto.CausationID, &dto.VersionID,
		&dto.TokenLifecycleID, &dto.Probability, &dto.Calibration, &dto.ModelVersionID,
		&dto.ExpiresAt, &dto.Priority, &dto.EstimatedAt,
		&dto.Confidence, &dto.CalibrationBin,
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

// GetEstimatesByTrace fetches the most-recent probability and slippage
// estimates for trace_id in a single round-trip via FULL OUTER JOIN.
// Either side may legitimately be missing (producer hasn't committed yet);
// missing rows surface as nil. F-SEC-05.
func (d *DB) GetEstimatesByTrace(ctx context.Context, traceID string) (
	*contracts.ProbabilityEstimateDTO, *contracts.SlippageEstimateDTO, error,
) {
	const q = `
WITH p AS (
    SELECT event_id, trace_id, correlation_id, COALESCE(causation_id, '') AS causation_id, version_id,
           token_lifecycle_id, probability, calibration, model_version_id,
           COALESCE(expires_at, '') AS expires_at, priority, estimated_at,
           COALESCE(confidence, 0) AS confidence, COALESCE(calibration_bin, 0) AS calibration_bin
    FROM probability_estimates
    WHERE trace_id = $1
    ORDER BY inserted_at DESC
    LIMIT 1
), s AS (
    SELECT event_id, trace_id, correlation_id, COALESCE(causation_id, '') AS causation_id, version_id,
           token_lifecycle_id, expected_p50_bps, expected_p95_bps, model_version_id,
           COALESCE(expires_at, '') AS expires_at, priority, estimated_at
    FROM slippage_estimates
    WHERE trace_id = $1
    ORDER BY inserted_at DESC
    LIMIT 1
)
SELECT
    p.event_id, p.trace_id, p.correlation_id, p.causation_id, p.version_id,
    p.token_lifecycle_id, p.probability, p.calibration, p.model_version_id,
    p.expires_at, p.priority, p.estimated_at, p.confidence, p.calibration_bin,
    s.event_id, s.trace_id, s.correlation_id, s.causation_id, s.version_id,
    s.token_lifecycle_id, s.expected_p50_bps, s.expected_p95_bps, s.model_version_id,
    s.expires_at, s.priority, s.estimated_at
FROM p FULL OUTER JOIN s ON TRUE`

	var (
		pEventID, pTrace, pCorr, pCause, pVersion sql.NullString
		pTokLC, pModelVer, pExpires, pEstimatedAt sql.NullString
		pProb, pCalib                             sql.NullFloat64
		pPriority                                 sql.NullInt32
		pConfidence                               sql.NullFloat64
		pCalibBin                                 sql.NullInt32

		sEventID, sTrace, sCorr, sCause, sVersion sql.NullString
		sTokLC, sModelVer, sExpires, sEstimatedAt sql.NullString
		sP50, sP95                                sql.NullInt32
		sPriority                                 sql.NullInt32
	)

	err := d.pool.QueryRowContext(ctx, q, traceID).Scan(
		&pEventID, &pTrace, &pCorr, &pCause, &pVersion,
		&pTokLC, &pProb, &pCalib, &pModelVer,
		&pExpires, &pPriority, &pEstimatedAt, &pConfidence, &pCalibBin,
		&sEventID, &sTrace, &sCorr, &sCause, &sVersion,
		&sTokLC, &sP50, &sP95, &sModelVer,
		&sExpires, &sPriority, &sEstimatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("get estimates by trace: %w", err)
	}

	var prob *contracts.ProbabilityEstimateDTO
	if pEventID.Valid {
		prob = &contracts.ProbabilityEstimateDTO{
			EventID: pEventID.String, TraceID: pTrace.String, CorrelationID: pCorr.String,
			CausationID: pCause.String, VersionID: pVersion.String,
			TokenLifecycleID: pTokLC.String,
			Probability:      pProb.Float64, Calibration: pCalib.Float64,
			ModelVersionID: pModelVer.String,
			ExpiresAt:      pExpires.String, Priority: pPriority.Int32, EstimatedAt: pEstimatedAt.String,
			Confidence: pConfidence.Float64, CalibrationBin: pCalibBin.Int32,
		}
	}
	var slip *contracts.SlippageEstimateDTO
	if sEventID.Valid {
		slip = &contracts.SlippageEstimateDTO{
			EventID: sEventID.String, TraceID: sTrace.String, CorrelationID: sCorr.String,
			CausationID: sCause.String, VersionID: sVersion.String,
			TokenLifecycleID: sTokLC.String,
			ExpectedP50Bps:   sP50.Int32, ExpectedP95Bps: sP95.Int32,
			ModelVersionID: sModelVer.String,
			ExpiresAt:      sExpires.String, Priority: sPriority.Int32, EstimatedAt: sEstimatedAt.String,
		}
	}
	return prob, slip, nil
}

// GetSlippageAlpha returns the per-market α calibration coefficient for the
// CPMM slippage model (Layer 4). Reads from slippage_alpha_calibrations,
// populated by the AlphaAggregator worker (residual risk #3).
//
// Cold-start (no row for market): returns (1.0, nil) so the slippage worker
// falls back to its DefaultAlpha. Errors other than ErrNoRows are propagated.
func (d *DB) GetSlippageAlpha(ctx context.Context, market string) (float64, error) {
	const q = `
SELECT alpha
FROM slippage_alpha_calibrations
WHERE market = $1`

	var alpha float64
	err := d.pool.QueryRowContext(ctx, q, market).Scan(&alpha)
	if errors.Is(err, sql.ErrNoRows) {
		return 1.0, nil
	}
	if err != nil {
		return 1.0, fmt.Errorf("get slippage alpha: %w", err)
	}
	return alpha, nil
}

// GetRealizedFillSamples returns realized-vs-predicted slippage samples
// keyed by market (chain), for executions completed within the last
// sinceSeconds. Joins execution_results × slippage_estimates × allocations.
//
// Filters: success=true, slippage_realized_bps > 0, expected_p50_bps > 0,
// chain != ”. completed_at is RFC3339 text — string-compared since RFC3339
// is lexicographically sortable in UTC.
func (d *DB) GetRealizedFillSamples(ctx context.Context, sinceSeconds int) (map[string][]database.FillSample, error) {
	if sinceSeconds <= 0 {
		sinceSeconds = 3600
	}
	since := time.Now().UTC().Add(-time.Duration(sinceSeconds) * time.Second).Format(time.RFC3339Nano)

	const q = `
SELECT a.chain, s.expected_p50_bps, e.slippage_realized_bps, e.completed_at
FROM execution_results e
JOIN slippage_estimates s ON s.trace_id = e.trace_id
JOIN allocations a ON a.execution_id = e.execution_id
WHERE e.success = TRUE
  AND e.slippage_realized_bps > 0
  AND s.expected_p50_bps > 0
  AND a.chain <> ''
  AND e.completed_at >= $1`

	rows, err := d.pool.QueryContext(ctx, q, since)
	if err != nil {
		return nil, fmt.Errorf("get realized fill samples: %w", err)
	}
	defer rows.Close()

	out := make(map[string][]database.FillSample)
	for rows.Next() {
		var chain string
		var predicted, realized int32
		var completedAt string
		if err := rows.Scan(&chain, &predicted, &realized, &completedAt); err != nil {
			return nil, fmt.Errorf("scan fill sample: %w", err)
		}
		t, perr := time.Parse(time.RFC3339Nano, completedAt)
		if perr != nil {
			t, perr = time.Parse(time.RFC3339, completedAt)
			if perr != nil {
				continue // skip un-parseable timestamp rather than abort
			}
		}
		out[chain] = append(out[chain], database.FillSample{
			PredictedBps: float64(predicted),
			RealizedBps:  float64(realized),
			At:           t,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate fill samples: %w", err)
	}
	return out, nil
}

// UpsertSlippageAlpha persists computed α for a market. Idempotent.
func (d *DB) UpsertSlippageAlpha(
	ctx context.Context,
	market string,
	alpha, ewmaPred, ewmaReal float64,
	sampleCount int,
) error {
	const q = `
INSERT INTO slippage_alpha_calibrations
    (market, alpha, sample_count, computed_at, ewma_predicted, ewma_realized)
VALUES ($1, $2, $3, CURRENT_TIMESTAMP, $4, $5)
ON CONFLICT (market) DO UPDATE SET
    alpha          = EXCLUDED.alpha,
    sample_count   = EXCLUDED.sample_count,
    computed_at    = EXCLUDED.computed_at,
    ewma_predicted = EXCLUDED.ewma_predicted,
    ewma_realized  = EXCLUDED.ewma_realized`

	if _, err := d.pool.ExecContext(ctx, q, market, alpha, sampleCount, ewmaPred, ewmaReal); err != nil {
		return fmt.Errorf("upsert slippage alpha: %w", err)
	}
	return nil
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
