package postgres

// DTO persistence implementations for Phase 2 pipeline stages.
// Each method maps a DTO struct to its projection table.
// All SQL uses parameterized queries and ON CONFLICT DO NOTHING semantics.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
)

// InsertDataQuality persists a DataQualityDTO.
// ON CONFLICT DO NOTHING — idempotent per event_id.
func (d *DB) InsertDataQuality(ctx context.Context, dto contracts.DataQualityDTO) error {
	reasonsJSON, err := json.Marshal(dto.RejectReasons)
	if err != nil {
		return fmt.Errorf("insert data quality: marshal reject reasons: %w", err)
	}
	flags := dto.Flags
	if flags == nil {
		flags = []string{}
	}
	flagsJSON, err := json.Marshal(flags)
	if err != nil {
		return fmt.Errorf("insert data quality: marshal flags: %w", err)
	}

	const q = `
INSERT INTO data_quality (
    event_id, trace_id, correlation_id, causation_id, version_id,
    token_lifecycle_id, token_address, chain,
    decision, risk_score,
    is_honeypot, is_fake_liquidity, is_wash_trading, is_rug_risk, is_tax_anomaly,
    buy_tax_bps, sell_tax_bps, lp_locked, lp_holder_count, contract_verified,
    reject_reasons, expires_at, priority, evaluated_at,
    honeypot_score, rug_score, wash_score, fake_liq_score, tax_score,
    profile, flags
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8,
    $9, $10,
    $11, $12, $13, $14, $15,
    $16, $17, $18, $19, $20,
    $21, $22, $23, $24,
    $25, $26, $27, $28, $29,
    $30, $31
)
ON CONFLICT (event_id) DO NOTHING`

	_, err = d.pool.ExecContext(ctx, q,
		dto.EventID, dto.TraceID, dto.CorrelationID, nullableString(dto.CausationID), dto.VersionID,
		dto.TokenLifecycleID, dto.TokenAddress, dto.Chain,
		dto.Decision, dto.RiskScore,
		dto.IsHoneypot, dto.IsFakeLiquidity, dto.IsWashTrading, dto.IsRugRisk, dto.IsTaxAnomaly,
		dto.BuyTaxBps, dto.SellTaxBps, dto.LpLocked, dto.LpHolderCount, dto.ContractVerified,
		reasonsJSON, nullableString(dto.ExpiresAt), dto.Priority, dto.EvaluatedAt,
		dto.HoneypotScore, dto.RugScore, dto.WashScore, dto.FakeLiqScore, dto.TaxScore,
		dto.Profile, flagsJSON,
	)
	if err != nil {
		return fmt.Errorf("insert data quality: %w", err)
	}
	return nil
}

// InsertFeature persists a FeatureDTO.
func (d *DB) InsertFeature(ctx context.Context, dto contracts.FeatureDTO) error {
	const q = `
INSERT INTO features (
    event_id, trace_id, correlation_id, causation_id, version_id,
    token_lifecycle_id, token_address,
    liquidity_score, tx_velocity_score, holder_distribution, wallet_entropy,
    contract_safety, token_age, volume_momentum, price_momentum,
    liquidity_usd_raw, tx_velocity_30s_raw, holder_count_raw, token_age_seconds_raw,
    expires_at, priority, extracted_at
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7,
    $8, $9, $10, $11,
    $12, $13, $14, $15,
    $16, $17, $18, $19,
    $20, $21, $22
)
ON CONFLICT (event_id) DO NOTHING`

	_, err := d.pool.ExecContext(ctx, q,
		dto.EventID, dto.TraceID, dto.CorrelationID, nullableString(dto.CausationID), dto.VersionID,
		dto.TokenLifecycleID, dto.TokenAddress,
		dto.LiquidityScore, dto.TxVelocityScore, dto.HolderDistribution, dto.WalletEntropy,
		dto.ContractSafety, dto.TokenAge, dto.VolumeMomentum, dto.PriceMomentum,
		dto.LiquidityUsdRaw, dto.TxVelocity30sRaw, dto.HolderCountRaw, dto.TokenAgeSecondsRaw,
		nullableString(dto.ExpiresAt), dto.Priority, dto.ExtractedAt,
	)
	if err != nil {
		return fmt.Errorf("insert feature: %w", err)
	}
	return nil
}

// InsertEdge persists an EdgeDTO.
func (d *DB) InsertEdge(ctx context.Context, dto contracts.EdgeDTO) error {
	const q = `
INSERT INTO edges (
    event_id, trace_id, correlation_id, causation_id, version_id,
    token_lifecycle_id, token_address,
    edge_type, edge_strength, edge_confidence, momentum_score, threshold_applied,
    opportunity_window_ms, expires_at, priority, detected_at,
    model_version_id
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7,
    $8, $9, $10, $11, $12,
    $13, $14, $15, $16,
    $17
)
ON CONFLICT (event_id) DO NOTHING`

	_, err := d.pool.ExecContext(ctx, q,
		dto.EventID, dto.TraceID, dto.CorrelationID, nullableString(dto.CausationID), dto.VersionID,
		dto.TokenLifecycleID, dto.TokenAddress,
		dto.EdgeType, dto.EdgeStrength, dto.EdgeConfidence, dto.MomentumScore, dto.ThresholdApplied,
		dto.OpportunityWindowMs, nullableString(dto.ExpiresAt), dto.Priority, dto.DetectedAt,
		dto.EdgeModelVersionID,
	)
	if err != nil {
		return fmt.Errorf("insert edge: %w", err)
	}
	return nil
}

// InsertValidatedEdge persists a ValidatedEdgeDTO.
func (d *DB) InsertValidatedEdge(ctx context.Context, dto contracts.ValidatedEdgeDTO) error {
	fallback := dto.FallbackReasons
	if fallback == nil {
		fallback = []string{}
	}
	fallbackJSON, err := json.Marshal(fallback)
	if err != nil {
		return fmt.Errorf("insert validated edge: marshal fallback reasons: %w", err)
	}

	const q = `
INSERT INTO validated_edges (
    event_id, trace_id, correlation_id, causation_id, version_id,
    token_lifecycle_id, token_address,
    decision, expected_value_bps, expected_gain_bps, expected_loss_bps, fixed_costs_bps,
    probability_used, slippage_p95_bps_used, ev_threshold_applied, reject_reason,
    expected_latency_ms, latency_gate_passed, expires_at, priority, validated_at,
    fallback_reasons
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7,
    $8, $9, $10, $11, $12,
    $13, $14, $15, $16,
    $17, $18, $19, $20, $21,
    $22
)
ON CONFLICT (event_id) DO NOTHING`

	_, err = d.pool.ExecContext(ctx, q,
		dto.EventID, dto.TraceID, dto.CorrelationID, nullableString(dto.CausationID), dto.VersionID,
		dto.TokenLifecycleID, dto.TokenAddress,
		dto.Decision, dto.ExpectedValueBps, dto.ExpectedGainBps, dto.ExpectedLossBps, dto.FixedCostsBps,
		dto.ProbabilityUsed, dto.SlippageP95BpsUsed, dto.EvThresholdApplied, dto.RejectReason,
		dto.ExpectedLatencyMs, dto.LatencyGatePassed, nullableString(dto.ExpiresAt), dto.Priority, dto.ValidatedAt,
		fallbackJSON,
	)
	if err != nil {
		return fmt.Errorf("insert validated edge: %w", err)
	}
	return nil
}

// InsertSelection persists a SelectionOutputDTO.
func (d *DB) InsertSelection(ctx context.Context, dto contracts.SelectionOutputDTO) error {
	const q = `
INSERT INTO selections (
    event_id, trace_id, correlation_id, causation_id, version_id,
    token_lifecycle_id, token_address,
    selected, rank, combined_score, diversity_bucket, is_exploration, reject_reason,
    expires_at, priority, selected_at
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7,
    $8, $9, $10, $11, $12, $13,
    $14, $15, $16
)
ON CONFLICT (event_id) DO NOTHING`

	_, err := d.pool.ExecContext(ctx, q,
		dto.EventID, dto.TraceID, dto.CorrelationID, nullableString(dto.CausationID), dto.VersionID,
		dto.TokenLifecycleID, dto.TokenAddress,
		dto.Selected, dto.Rank, dto.CombinedScore, dto.DiversityBucket, dto.IsExploration, dto.RejectReason,
		nullableString(dto.ExpiresAt), dto.Priority, dto.SelectedAt,
	)
	if err != nil {
		return fmt.Errorf("insert selection: %w", err)
	}
	return nil
}

// InsertAllocation persists an AllocationDTO.
func (d *DB) InsertAllocation(ctx context.Context, dto contracts.AllocationDTO) error {
	const q = `
INSERT INTO allocations (
    event_id, trace_id, correlation_id, causation_id, version_id,
    token_lifecycle_id, token_address, chain,
    execution_id, size_usd, size_base_raw, max_slippage_bps, wallet_address, wallet_shard,
    rejected, reject_reason, cohort_id,
    expires_at, priority, allocated_at
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8,
    $9, $10, $11, $12, $13, $14,
    $15, $16, $17,
    $18, $19, $20
)
ON CONFLICT (event_id) DO NOTHING`

	_, err := d.pool.ExecContext(ctx, q,
		dto.EventID, dto.TraceID, dto.CorrelationID, nullableString(dto.CausationID), dto.VersionID,
		dto.TokenLifecycleID, dto.TokenAddress, dto.Chain,
		dto.ExecutionID, dto.SizeUsd, dto.SizeBaseRaw, dto.MaxSlippageBps, dto.WalletAddress, dto.WalletShard,
		dto.Rejected, dto.RejectReason, dto.CohortID,
		nullableString(dto.ExpiresAt), dto.Priority, dto.AllocatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert allocation: %w", err)
	}
	return nil
}

// InsertExecutionResult persists an ExecutionResultDTO.
// Uses ON CONFLICT (execution_id) DO UPDATE to handle the pre-claim reservation row
// inserted by ClaimExecution. The reservation row has execution_id as its unique key;
// the final result updates all mutable fields in place.
// Idempotent: calling again with the same execution_id overwrites with identical data.
func (d *DB) InsertExecutionResult(ctx context.Context, dto contracts.ExecutionResultDTO) error {
	const q = `
INSERT INTO execution_results (
    event_id, trace_id, correlation_id, causation_id, version_id,
    token_lifecycle_id, execution_id, allocation_id,
    status, success, tx_hash, block_number, attempts, replaced, replacement_count,
    mempool_route, nonce_used, wallet_address, wallet_shard,
    final_gas_used, final_max_fee_wei, final_priority_fee_wei,
    realized_entry_price, slippage_realized_bps, latency_ms, error_code,
    mev_protected, execution_path, slippage_guard_bps, rejection_reason, simulated,
    expires_at, priority, completed_at
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8,
    $9, $10, $11, $12, $13, $14, $15,
    $16, $17, $18, $19,
    $20, $21, $22,
    $23, $24, $25, $26,
    $27, $28, $29, $30, $31,
    $32, $33, $34
)
ON CONFLICT (execution_id) DO UPDATE SET
    status                 = EXCLUDED.status,
    success                = EXCLUDED.success,
    tx_hash                = EXCLUDED.tx_hash,
    block_number           = EXCLUDED.block_number,
    attempts               = EXCLUDED.attempts,
    replaced               = EXCLUDED.replaced,
    replacement_count      = EXCLUDED.replacement_count,
    mempool_route          = EXCLUDED.mempool_route,
    nonce_used             = EXCLUDED.nonce_used,
    wallet_address         = EXCLUDED.wallet_address,
    wallet_shard           = EXCLUDED.wallet_shard,
    final_gas_used         = EXCLUDED.final_gas_used,
    final_max_fee_wei      = EXCLUDED.final_max_fee_wei,
    final_priority_fee_wei = EXCLUDED.final_priority_fee_wei,
    realized_entry_price   = EXCLUDED.realized_entry_price,
    slippage_realized_bps  = EXCLUDED.slippage_realized_bps,
    latency_ms             = EXCLUDED.latency_ms,
    error_code             = EXCLUDED.error_code,
    mev_protected          = EXCLUDED.mev_protected,
    execution_path         = EXCLUDED.execution_path,
    slippage_guard_bps     = EXCLUDED.slippage_guard_bps,
    rejection_reason       = EXCLUDED.rejection_reason,
    simulated              = EXCLUDED.simulated,
    completed_at           = EXCLUDED.completed_at`

	_, err := d.pool.ExecContext(ctx, q,
		dto.EventID, dto.TraceID, dto.CorrelationID, nullableString(dto.CausationID), dto.VersionID,
		dto.TokenLifecycleID, dto.ExecutionID, dto.AllocationID,
		dto.Status, dto.Success, dto.TxHash, dto.BlockNumber, dto.Attempts, dto.Replaced, dto.ReplacementCount,
		dto.MempoolRoute, dto.NonceUsed, dto.WalletAddress, dto.WalletShard,
		dto.FinalGasUsed, dto.FinalMaxFeeWei, dto.FinalPriorityFeeWei,
		dto.RealizedEntryPrice, dto.SlippageRealizedBps, dto.LatencyMs, dto.ErrorCode,
		dto.MEVProtected, dto.ExecutionPath, dto.SlippageGuardBps, dto.RejectionReason, dto.Simulated,
		nullableString(dto.ExpiresAt), dto.Priority, dto.CompletedAt,
	)
	if err != nil {
		return fmt.Errorf("insert execution result: %w", err)
	}
	return nil
}

// InsertPositionState persists a PositionStateDTO snapshot.
func (d *DB) InsertPositionState(ctx context.Context, dto contracts.PositionStateDTO) error {
	const q = `
INSERT INTO positions (
    event_id, trace_id, correlation_id, causation_id, version_id,
    token_lifecycle_id, position_id, execution_id, token_address, chain,
    status, entry_price, entry_size_usd, current_price,
    exit_price, exit_reason, pnl_usd, pnl_pct,
    tp1_bps, tp2_bps, sl_bps, max_hold_seconds,
    expires_at, priority, opened_at, exited_at, snapshot_at,
    peak_price, peak_observed_at, trailing_stop_bps, tp1_filled_pct_bps,
    last_volume_usd, last_volume_check_at
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9, $10,
    $11, $12, $13, $14,
    $15, $16, $17, $18,
    $19, $20, $21, $22,
    $23, $24, $25, $26, $27,
    $28, $29, $30, $31,
    $32, $33
)
ON CONFLICT (event_id) DO NOTHING`

	_, err := d.pool.ExecContext(ctx, q,
		dto.EventID, dto.TraceID, dto.CorrelationID, nullableString(dto.CausationID), dto.VersionID,
		dto.TokenLifecycleID, dto.PositionID, dto.ExecutionID, dto.TokenAddress, dto.Chain,
		dto.Status, dto.EntryPrice, dto.EntrySizeUsd, dto.CurrentPrice,
		dto.ExitPrice, dto.ExitReason, dto.PnlUsd, dto.PnlPct,
		dto.Tp1Bps, dto.Tp2Bps, dto.SlBps, dto.MaxHoldSeconds,
		nullableString(dto.ExpiresAt), dto.Priority, dto.OpenedAt, dto.ExitedAt, dto.SnapshotAt,
		dto.PeakPrice, dto.PeakObservedAt, dto.TrailingStopBps, dto.Tp1FilledPctBps,
		dto.LastVolumeUsd, dto.LastVolumeCheckAt,
	)
	if err != nil {
		return fmt.Errorf("insert position state: %w", err)
	}
	return nil
}

// InsertEvaluation persists an EvaluationDTO.
func (d *DB) InsertEvaluation(ctx context.Context, dto contracts.EvaluationDTO) error {
	const q = `
INSERT INTO evaluations (
    event_id, trace_id, correlation_id, causation_id, version_id,
    evaluation_id, window_start, window_end, sample_size,
    true_positive_count, false_positive_count, true_negative_count, false_negative_count,
    expectancy, max_drawdown_pct, brier_score, prediction_error_mean,
    expires_at, priority, evaluated_at
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7, $8, $9,
    $10, $11, $12, $13,
    $14, $15, $16, $17,
    $18, $19, $20
)
ON CONFLICT (event_id) DO NOTHING`

	_, err := d.pool.ExecContext(ctx, q,
		dto.EventID, dto.TraceID, dto.CorrelationID, nullableString(dto.CausationID), dto.VersionID,
		dto.EvaluationID, dto.WindowStart, dto.WindowEnd, dto.SampleSize,
		dto.TruePositiveCount, dto.FalsePositiveCount, dto.TrueNegativeCount, dto.FalseNegativeCount,
		dto.Expectancy, dto.MaxDrawdownPct, dto.BrierScore, dto.PredictionErrorMean,
		nullableString(dto.ExpiresAt), dto.Priority, dto.EvaluatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert evaluation: %w", err)
	}
	return nil
}

// InsertLearningRecord persists a LearningRecordDTO.
func (d *DB) InsertLearningRecord(ctx context.Context, dto contracts.LearningRecordDTO) error {
	featuresJSON, err := json.Marshal(dto.FeaturesSnapshot)
	if err != nil {
		return fmt.Errorf("insert learning record: marshal features: %w", err)
	}
	edgeJSON, err := json.Marshal(dto.EdgeSnapshot)
	if err != nil {
		return fmt.Errorf("insert learning record: marshal edge: %w", err)
	}
	validatedJSON, err := json.Marshal(dto.ValidatedSnapshot)
	if err != nil {
		return fmt.Errorf("insert learning record: marshal validated: %w", err)
	}

	// Sybil indicators are sparse — emit SQL NULL when the heuristic did
	// not fire so JSONB queries can filter on `sybil_indicators IS NOT NULL`.
	var sybilJSON interface{}
	if dto.SybilClusterIndicators != nil {
		b, err := json.Marshal(dto.SybilClusterIndicators)
		if err != nil {
			return fmt.Errorf("insert learning record: marshal sybil indicators: %w", err)
		}
		sybilJSON = b
	}

	const q = `
INSERT INTO learning_records (
    event_id, trace_id, correlation_id, causation_id, version_id,
    record_id, token_lifecycle_id,
    shadow, outcome, classification, pnl_usd, pnl_pct, prediction_error, cohort,
    features_snapshot, edge_snapshot, validated_snapshot,
    simulated, expired_source, strategy_status,
    expires_at, priority, recorded_at,
    sybil_indicators,
    ai_explanation_known, ai_loss_category, ai_explanation
) VALUES (
    $1, $2, $3, $4, $5,
    $6, $7,
    $8, $9, $10, $11, $12, $13, $14,
    $15, $16, $17,
    $18, $19, $20,
    $21, $22, $23,
    $24,
    $25, $26, $27
)
ON CONFLICT (event_id) DO NOTHING`

	_, err = d.pool.ExecContext(ctx, q,
		dto.EventID, dto.TraceID, dto.CorrelationID, nullableString(dto.CausationID), dto.VersionID,
		dto.RecordID, dto.TokenLifecycleID,
		dto.Shadow, dto.Outcome, dto.Classification, dto.PnlUsd, dto.PnlPct, dto.PredictionError, dto.Cohort,
		featuresJSON, edgeJSON, validatedJSON,
		dto.Simulated, dto.ExpiredSource, dto.StrategyStatus,
		nullableString(dto.ExpiresAt), dto.Priority, dto.RecordedAt,
		sybilJSON,
		dto.AIExplanationKnown, nullableString(dto.AILossCategory), nullableString(dto.AIExplanation),
	)
	if err != nil {
		return fmt.Errorf("insert learning record: %w", err)
	}
	return nil
}

// GetExecutionByLifecycle returns the ExecutionResultDTO for a lifecycle ID.
// Returns ErrNotFound if no execution record exists for the lifecycle.
func (d *DB) GetExecutionByLifecycle(ctx context.Context, lifecycleID string) (*contracts.ExecutionResultDTO, error) {
	const q = `
SELECT event_id, trace_id, correlation_id, COALESCE(causation_id,''), version_id,
       token_lifecycle_id, execution_id, allocation_id,
       status, success, tx_hash, block_number, attempts, replaced, replacement_count,
       mempool_route, nonce_used, wallet_address, wallet_shard,
       final_gas_used, final_max_fee_wei, final_priority_fee_wei,
       realized_entry_price, slippage_realized_bps, latency_ms, error_code,
       mev_protected, execution_path, slippage_guard_bps, rejection_reason, simulated,
       COALESCE(expires_at,''), priority, completed_at
FROM execution_results
WHERE token_lifecycle_id = $1
ORDER BY completed_at DESC
LIMIT 1`

	row := d.pool.QueryRowContext(ctx, q, lifecycleID)
	dto := &contracts.ExecutionResultDTO{}
	err := row.Scan(
		&dto.EventID, &dto.TraceID, &dto.CorrelationID, &dto.CausationID, &dto.VersionID,
		&dto.TokenLifecycleID, &dto.ExecutionID, &dto.AllocationID,
		&dto.Status, &dto.Success, &dto.TxHash, &dto.BlockNumber,
		&dto.Attempts, &dto.Replaced, &dto.ReplacementCount,
		&dto.MempoolRoute, &dto.NonceUsed, &dto.WalletAddress, &dto.WalletShard,
		&dto.FinalGasUsed, &dto.FinalMaxFeeWei, &dto.FinalPriorityFeeWei,
		&dto.RealizedEntryPrice, &dto.SlippageRealizedBps, &dto.LatencyMs, &dto.ErrorCode,
		&dto.MEVProtected, &dto.ExecutionPath, &dto.SlippageGuardBps, &dto.RejectionReason, &dto.Simulated,
		&dto.ExpiresAt, &dto.Priority, &dto.CompletedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, database.ErrNotFound
		}
		return nil, fmt.Errorf("get execution by lifecycle: %w", err)
	}
	return dto, nil
}

// GetAdaptiveDQStats returns counters for the adaptive risk-appetite controller:
// total data_quality decisions in the past sinceSeconds and the subset that
// rejected with a rug or honeypot reason. Rows missing created_at (pre-migration)
// fail the >= comparison and are excluded — matching their "ancient" semantics.
func (d *DB) GetAdaptiveDQStats(ctx context.Context, sinceSeconds int) (int, int, error) {
	if sinceSeconds <= 0 {
		return 0, 0, nil
	}
	const q = `
SELECT
  COUNT(*) AS total,
  COUNT(*) FILTER (
    WHERE decision = 'REJECT'
      AND (
        reject_reasons::text ILIKE '%rug%'
        OR reject_reasons::text ILIKE '%honeypot%'
      )
  ) AS rug_rejects
FROM data_quality
WHERE created_at >= NOW() - ($1 * interval '1 second')`

	var total, rugRejects int
	if err := d.pool.QueryRowContext(ctx, q, sinceSeconds).Scan(&total, &rugRejects); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("get adaptive dq stats: %w", err)
	}
	return total, rugRejects, nil
}
