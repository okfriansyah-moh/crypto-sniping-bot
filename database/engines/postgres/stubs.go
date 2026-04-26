package postgres

// This file contains stub implementations of database.Adapter methods
// that belong to later phases (Phase 3+). Each stub returns ErrNotImplemented
// until its migration is applied and the full implementation is added.
//
// Phase 2 methods have been moved to their own files:
//   - lifecycle.go   — token lifecycle state machine
//   - trading.go     — DTO persistence (data_quality, features, edges, etc.)
//   - positions.go   — nonce manager, open positions, GetPosition

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

// ── §11.2 Production Gap Extensions (Phase 5+) ───────────────────────────────

// MarkEventExpired marks an event as expired.
func (d *DB) MarkEventExpired(ctx context.Context, eventID string, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	const q = `UPDATE events SET processed = TRUE WHERE event_id = $1 AND processed = FALSE`
	if _, err := d.pool.ExecContext(ctx, q, eventID); err != nil {
		return fmt.Errorf("mark event expired: %w", err)
	}
	// Emit expired_event into the bus for audit trail.
	expiredPayload, _ := json.Marshal(map[string]string{
		"event_id":   eventID,
		"reason":     reason,
		"expired_at": now,
	})
	var cid *string
	cid = &eventID
	expiredEvt := database.Event{
		EventID:       contracts.ContentIDFromString("expired:" + eventID),
		EventType:     "expired_event",
		Payload:       expiredPayload,
		TraceID:       eventID,
		CorrelationID: eventID,
		CausationID:   cid,
		VersionID:     "",
	}
	_ = d.InsertEvent(ctx, expiredEvt) // best-effort
	return nil
}

// GetSystemState returns the singleton system state row.
func (d *DB) GetSystemState(ctx context.Context) (*contracts.SystemStateDTO, error) {
	const q = `
SELECT mode, drawdown_pct, drawdown_window_hours, open_positions,
       total_exposure_usd, active_strategy_id, shadow_strategy_id,
       last_transition_reason, to_char(updated_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'),
       version_id, state_version
FROM system_state
WHERE id = 1`

	var dto contracts.SystemStateDTO
	var stateVersion int64
	err := d.pool.QueryRowContext(ctx, q).Scan(
		&dto.Mode, &dto.DrawdownPct, &dto.DrawdownWindowHours, &dto.OpenPositions,
		&dto.TotalExposureUsd, &dto.ActiveStrategyID, &dto.ShadowStrategyID,
		&dto.LastTransitionReason, &dto.UpdatedAt,
		&dto.VersionID, &stateVersion,
	)
	if err != nil {
		return nil, fmt.Errorf("get system state: %w", err)
	}
	_ = stateVersion // returned separately via UpsertSystemState CAS
	return &dto, nil
}

// UpsertSystemState updates the system state using CAS on state_version.
func (d *DB) UpsertSystemState(ctx context.Context, state contracts.SystemStateDTO, expectedVersion int64) (int64, error) {
	const q = `
UPDATE system_state
   SET mode                    = $1,
       drawdown_pct            = $2,
       drawdown_window_hours   = $3,
       open_positions          = $4,
       total_exposure_usd      = $5,
       active_strategy_id      = $6,
       shadow_strategy_id      = $7,
       last_transition_reason  = $8,
       updated_at              = CURRENT_TIMESTAMP,
       version_id              = $9,
       state_version           = state_version + 1
 WHERE id = 1
   AND state_version = $10
RETURNING state_version`

	var newVersion int64
	err := d.pool.QueryRowContext(ctx, q,
		state.Mode, state.DrawdownPct, state.DrawdownWindowHours, state.OpenPositions,
		state.TotalExposureUsd, state.ActiveStrategyID, state.ShadowStrategyID,
		state.LastTransitionReason, state.VersionID,
		expectedVersion,
	).Scan(&newVersion)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, database.ErrStaleState
		}
		return 0, fmt.Errorf("upsert system state: %w", err)
	}
	return newVersion, nil
}

// GetExposureSummary returns aggregated capital exposure.
func (d *DB) GetExposureSummary(ctx context.Context) (*database.ExposureSummary, error) {
	const q = `
SELECT COALESCE(SUM(entry_size_usd), 0), COUNT(*)
FROM positions
WHERE status = 'open'`

	summary := &database.ExposureSummary{
		PerToken:  make(map[string]float64),
		PerCohort: make(map[string]float64),
	}
	if err := d.pool.QueryRowContext(ctx, q).Scan(&summary.TotalUsd, &summary.OpenPositions); err != nil {
		return nil, fmt.Errorf("get exposure summary: %w", err)
	}
	return summary, nil
}

// ArchiveEvents moves processed events older than olderThan to events_archive.
func (d *DB) ArchiveEvents(ctx context.Context, olderThan time.Time, batchSize int) (int, error) {
	const q = `
WITH moved AS (
    DELETE FROM events
    WHERE event_id IN (
        SELECT event_id FROM events
        WHERE processed = TRUE
          AND created_at < $1
        ORDER BY created_at ASC
        LIMIT $2
    )
    RETURNING event_id, event_type, payload, trace_id, correlation_id,
              causation_id, version_id, created_at, processed, claimed_at,
              priority, expires_at
)
INSERT INTO events_archive
    (event_id, event_type, payload, trace_id, correlation_id,
     causation_id, version_id, created_at, processed, claimed_at,
     priority, expires_at)
SELECT event_id, event_type, payload, trace_id, correlation_id,
       causation_id, version_id, created_at, processed, claimed_at,
       priority, expires_at
FROM moved
ON CONFLICT (event_id) DO NOTHING`

	res, err := d.pool.ExecContext(ctx, q, olderThan, batchSize)
	if err != nil {
		return 0, fmt.Errorf("archive events: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// GetEventsByTraceIncludeArchive returns events from both events and events_archive.
func (d *DB) GetEventsByTraceIncludeArchive(ctx context.Context, traceID string) ([]contracts.EventEnvelope, error) {
	const q = `
SELECT event_id, event_type, payload, trace_id, correlation_id,
       COALESCE(causation_id,''), version_id,
       to_char(created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
FROM (
    SELECT event_id, event_type, payload, trace_id, correlation_id,
           causation_id, version_id, created_at
    FROM events WHERE trace_id = $1
    UNION ALL
    SELECT event_id, event_type, payload, trace_id, correlation_id,
           causation_id, version_id, created_at
    FROM events_archive WHERE trace_id = $1
) combined
ORDER BY created_at ASC`

	rows, err := d.pool.QueryContext(ctx, q, traceID)
	if err != nil {
		return nil, fmt.Errorf("get events by trace include archive: %w", err)
	}
	defer rows.Close()

	var result []contracts.EventEnvelope
	for rows.Next() {
		var env contracts.EventEnvelope
		var payloadRaw []byte
		if err := rows.Scan(
			&env.EventID, &env.EventType, &payloadRaw,
			&env.TraceID, &env.CorrelationID, &env.CausationID, &env.VersionID,
			&env.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("get events by trace include archive: scan: %w", err)
		}
		env.Payload = string(payloadRaw)
		result = append(result, env)
	}
	return result, rows.Err()
}

// ComputeDrawdown computes the realized drawdown fraction over the given window.
func (d *DB) ComputeDrawdown(ctx context.Context, windowHours int) (float64, error) {
	if windowHours <= 0 {
		windowHours = 24
	}
	// exited_at is stored as TEXT (RFC3339Nano); cast to TIMESTAMPTZ for comparison.
	// Only consider positions that have actually exited (exited_at != '').
	const q = `
SELECT
    COALESCE(SUM(CASE WHEN pnl_usd < 0 THEN ABS(pnl_usd) ELSE 0 END), 0) AS total_loss_usd,
    COALESCE(SUM(entry_size_usd), 1) AS total_size_usd
FROM positions
WHERE exited_at != ''
  AND exited_at::TIMESTAMPTZ >= CURRENT_TIMESTAMP - ($1 * INTERVAL '1 hour')`

	row := d.pool.QueryRowContext(ctx, q, windowHours)
	var lossUsd, sizeUsd float64
	if err := row.Scan(&lossUsd, &sizeUsd); err != nil {
		return 0, fmt.Errorf("compute drawdown: %w", err)
	}
	if sizeUsd <= 0 {
		return 0, nil
	}
	return lossUsd / sizeUsd, nil
}
