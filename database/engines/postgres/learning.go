package postgres

// Phase 5 learning engine database implementations.
// Replaces stub implementations for SetStrategyVersionStatus, GetActiveStrategy,
// GetShadowStrategy, and adds shadow trade + learning read methods.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
)

// SetStrategyVersionStatus transitions a strategy version through its lifecycle.
// Legal transitions (from → to):
//
//	draft  → shadow | deactivated
//	shadow → active | deactivated
//	active → rolled_back | deactivated
//
// Promotion to active atomically demotes the current active version to deactivated.
func (d *DB) SetStrategyVersionStatus(ctx context.Context, versionID string, newStatus string, reason string) error {
	return d.withTx(ctx, func(tx *sql.Tx) error {
		// Fetch current status for legal transition check.
		var current string
		const fetchQ = `SELECT status FROM strategy_versions WHERE strategy_version_id = $1`
		if err := tx.QueryRowContext(ctx, fetchQ, versionID).Scan(&current); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return database.ErrUnknownVersion
			}
			return fmt.Errorf("set strategy version status: fetch: %w", err)
		}

		if !isLegalVersionTransition(current, newStatus) {
			return fmt.Errorf("%w: %s → %s", database.ErrIllegalTransition, current, newStatus)
		}

		now := time.Now().UTC().Format(time.RFC3339Nano)

		// If promoting to active, atomically deactivate the current active version.
		if newStatus == "active" {
			const deactivate = `
UPDATE strategy_versions
   SET status = 'deactivated', deactivated_at = $1
 WHERE status = 'active'
   AND strategy_version_id != $2`
			if _, err := tx.ExecContext(ctx, deactivate, now, versionID); err != nil {
				return fmt.Errorf("set strategy version status: deactivate old active: %w", err)
			}
		}

		// Build SET clause based on target status.
		switch newStatus {
		case "shadow":
			const q = `UPDATE strategy_versions SET status = $1, shadow_started_at = $2 WHERE strategy_version_id = $3`
			if _, err := tx.ExecContext(ctx, q, newStatus, now, versionID); err != nil {
				return fmt.Errorf("set strategy version status shadow: %w", err)
			}
		case "active":
			const q = `UPDATE strategy_versions SET status = $1, activated_at = $2, promoted_at = $2 WHERE strategy_version_id = $3`
			if _, err := tx.ExecContext(ctx, q, newStatus, now, versionID); err != nil {
				return fmt.Errorf("set strategy version status active: %w", err)
			}
		case "rolled_back":
			const q = `UPDATE strategy_versions SET status = $1, rolled_back_at = $2, deactivated_at = $2 WHERE strategy_version_id = $3`
			if _, err := tx.ExecContext(ctx, q, newStatus, now, versionID); err != nil {
				return fmt.Errorf("set strategy version status rolled_back: %w", err)
			}
		default:
			const q = `UPDATE strategy_versions SET status = $1, deactivated_at = $2 WHERE strategy_version_id = $3`
			if _, err := tx.ExecContext(ctx, q, newStatus, now, versionID); err != nil {
				return fmt.Errorf("set strategy version status default: %w", err)
			}
		}

		_ = reason // stored in audit log if needed in Phase 6
		return nil
	})
}

// isLegalVersionTransition enforces the strategy version status machine.
func isLegalVersionTransition(from, to string) bool {
	switch from {
	case "draft":
		return to == "shadow" || to == "deactivated"
	case "shadow":
		return to == "active" || to == "deactivated"
	case "active":
		return to == "rolled_back" || to == "deactivated"
	case "candidate":
		return to == "active" || to == "deactivated" || to == "shadow"
	default:
		return false
	}
}

// GetActiveStrategy returns the version with status="active".
func (d *DB) GetActiveStrategy(ctx context.Context) (*database.StrategyVersion, error) {
	const q = `
SELECT strategy_version_id, config_snapshot, created_at, activated_at, deactivated_at,
       status, shadow_started_at, promoted_at, rolled_back_at, parent_version_id
FROM strategy_versions
WHERE status = 'active'
LIMIT 1`

	sv, err := scanStrategyVersion(d.pool.QueryRowContext(ctx, q))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get active strategy: %w", err)
	}
	return sv, nil
}

// GetShadowStrategy returns the strategy version with status="shadow", if any.
// Returns nil (no error) when no shadow version exists.
func (d *DB) GetShadowStrategy(ctx context.Context) (*database.StrategyVersion, error) {
	const q = `
SELECT strategy_version_id, config_snapshot, created_at, activated_at, deactivated_at,
       status, shadow_started_at, promoted_at, rolled_back_at, parent_version_id
FROM strategy_versions
WHERE status = 'shadow'
LIMIT 1`

	sv, err := scanStrategyVersion(d.pool.QueryRowContext(ctx, q))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil // no shadow version is not an error
	}
	if err != nil {
		return nil, fmt.Errorf("get shadow strategy: %w", err)
	}
	return sv, nil
}

// InsertShadowTrade persists a new shadow trade observation row.
// Idempotent: ON CONFLICT (shadow_id) DO NOTHING.
func (d *DB) InsertShadowTrade(ctx context.Context, st database.ShadowTrade) error {
	const q = `
INSERT INTO shadow_trades
    (shadow_id, token_address, stage, rejected_at, observation_complete,
     observed_return_pct, classification, learning_record_id, version_id, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, CURRENT_TIMESTAMP)
ON CONFLICT (shadow_id) DO NOTHING`

	rejectedAt := st.RejectedAt
	if rejectedAt == "" {
		rejectedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	classification := st.Classification
	if classification == "" {
		classification = "TN"
	}

	_, err := d.pool.ExecContext(ctx, q,
		st.ShadowID, st.TokenAddress, st.Stage, rejectedAt,
		st.ObservationComplete, st.ObservedReturnPct, classification,
		st.LearningRecordID, st.VersionID,
	)
	if err != nil {
		return fmt.Errorf("insert shadow trade: %w", err)
	}
	return nil
}

// UpdateShadowTradeObservation marks a shadow trade observation as complete.
func (d *DB) UpdateShadowTradeObservation(ctx context.Context, shadowID string, observedReturnPct float64, classification string) error {
	const q = `
UPDATE shadow_trades
   SET observation_complete = TRUE,
       observed_return_pct  = $1,
       classification       = $2,
       updated_at           = CURRENT_TIMESTAMP
 WHERE shadow_id = $3`

	_, err := d.pool.ExecContext(ctx, q, observedReturnPct, classification, shadowID)
	if err != nil {
		return fmt.Errorf("update shadow trade observation: %w", err)
	}
	return nil
}

// GetShadowTradesByWindow returns pending shadow trades older than windowSeconds.
func (d *DB) GetShadowTradesByWindow(ctx context.Context, windowSeconds int) ([]database.ShadowTrade, error) {
	const q = `
SELECT shadow_id, token_address, stage,
       to_char(rejected_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
       observation_complete, observed_return_pct, classification,
       learning_record_id, version_id
FROM shadow_trades
WHERE observation_complete = FALSE
  AND rejected_at < NOW() - ($1 || ' seconds')::interval
ORDER BY rejected_at ASC`

	rows, err := d.pool.QueryContext(ctx, q, windowSeconds)
	if err != nil {
		return nil, fmt.Errorf("get shadow trades by window: %w", err)
	}
	defer rows.Close()

	var result []database.ShadowTrade
	for rows.Next() {
		var st database.ShadowTrade
		if err := rows.Scan(
			&st.ShadowID, &st.TokenAddress, &st.Stage,
			&st.RejectedAt,
			&st.ObservationComplete, &st.ObservedReturnPct, &st.Classification,
			&st.LearningRecordID, &st.VersionID,
		); err != nil {
			return nil, fmt.Errorf("get shadow trades by window: scan: %w", err)
		}
		result = append(result, st)
	}
	return result, rows.Err()
}

// GetLearningRecordsByWindow returns LearningRecordDTOs for a version in [start, end].
func (d *DB) GetLearningRecordsByWindow(ctx context.Context, versionID string, start, end time.Time) ([]contracts.LearningRecordDTO, error) {
	const q = `
SELECT event_id, trace_id, correlation_id, COALESCE(causation_id,''), version_id,
       record_id, token_lifecycle_id,
       shadow, outcome, classification, pnl_usd, pnl_pct, prediction_error, cohort,
       features_snapshot, edge_snapshot, validated_snapshot,
       simulated, expired_source, strategy_status,
       COALESCE(expires_at,''), priority, recorded_at,
       sybil_indicators
FROM learning_records
WHERE version_id = $1
  AND recorded_at >= $2
  AND recorded_at <= $3
ORDER BY recorded_at ASC`

	startStr := start.UTC().Format(time.RFC3339Nano)
	endStr := end.UTC().Format(time.RFC3339Nano)

	rows, err := d.pool.QueryContext(ctx, q, versionID, startStr, endStr)
	if err != nil {
		return nil, fmt.Errorf("get learning records by window: %w", err)
	}
	defer rows.Close()

	var result []contracts.LearningRecordDTO
	for rows.Next() {
		dto, err := scanLearningRecord(rows)
		if err != nil {
			return nil, fmt.Errorf("get learning records by window: scan: %w", err)
		}
		result = append(result, dto)
	}
	return result, rows.Err()
}

// GetEvaluationsByVersion returns EvaluationDTOs for a version, newest first.
func (d *DB) GetEvaluationsByVersion(ctx context.Context, versionID string) ([]contracts.EvaluationDTO, error) {
	const q = `
SELECT event_id, trace_id, correlation_id, COALESCE(causation_id,''), version_id,
       evaluation_id, window_start, window_end, sample_size,
       true_positive_count, false_positive_count, true_negative_count, false_negative_count,
       expectancy, max_drawdown_pct, brier_score, prediction_error_mean,
       COALESCE(expires_at,''), priority, evaluated_at
FROM evaluations
WHERE version_id = $1
ORDER BY evaluated_at DESC`

	rows, err := d.pool.QueryContext(ctx, q, versionID)
	if err != nil {
		return nil, fmt.Errorf("get evaluations by version: %w", err)
	}
	defer rows.Close()

	var result []contracts.EvaluationDTO
	for rows.Next() {
		var dto contracts.EvaluationDTO
		if err := rows.Scan(
			&dto.EventID, &dto.TraceID, &dto.CorrelationID, &dto.CausationID, &dto.VersionID,
			&dto.EvaluationID, &dto.WindowStart, &dto.WindowEnd, &dto.SampleSize,
			&dto.TruePositiveCount, &dto.FalsePositiveCount, &dto.TrueNegativeCount, &dto.FalseNegativeCount,
			&dto.Expectancy, &dto.MaxDrawdownPct, &dto.BrierScore, &dto.PredictionErrorMean,
			&dto.ExpiresAt, &dto.Priority, &dto.EvaluatedAt,
		); err != nil {
			return nil, fmt.Errorf("get evaluations by version: scan: %w", err)
		}
		result = append(result, dto)
	}
	return result, rows.Err()
}

// scanLearningRecord scans a single row into a LearningRecordDTO.
func scanLearningRecord(rows *sql.Rows) (contracts.LearningRecordDTO, error) {
	var dto contracts.LearningRecordDTO
	var featuresJSON, edgeJSON, validatedJSON []byte
	var sybilJSON []byte
	err := rows.Scan(
		&dto.EventID, &dto.TraceID, &dto.CorrelationID, &dto.CausationID, &dto.VersionID,
		&dto.RecordID, &dto.TokenLifecycleID,
		&dto.Shadow, &dto.Outcome, &dto.Classification,
		&dto.PnlUsd, &dto.PnlPct, &dto.PredictionError, &dto.Cohort,
		&featuresJSON, &edgeJSON, &validatedJSON,
		&dto.Simulated, &dto.ExpiredSource, &dto.StrategyStatus,
		&dto.ExpiresAt, &dto.Priority, &dto.RecordedAt,
		&sybilJSON,
	)
	if err != nil {
		return contracts.LearningRecordDTO{}, err
	}
	if len(featuresJSON) > 0 {
		_ = json.Unmarshal(featuresJSON, &dto.FeaturesSnapshot)
	}
	if len(edgeJSON) > 0 {
		_ = json.Unmarshal(edgeJSON, &dto.EdgeSnapshot)
	}
	if len(validatedJSON) > 0 {
		_ = json.Unmarshal(validatedJSON, &dto.ValidatedSnapshot)
	}
	if len(sybilJSON) > 0 {
		var s contracts.SybilIndicators
		if err := json.Unmarshal(sybilJSON, &s); err == nil {
			dto.SybilClusterIndicators = &s
		}
	}
	return dto, nil
}
