package postgres

// hardening.go — Phase 8 production hardening adapter methods.
// Implements the determinism + exactly-once + failure-safety contract
// (architecture § 4.10–4.11). All SQL is portable (ON CONFLICT DO NOTHING,
// parameterized queries, CURRENT_TIMESTAMP).

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
)

// ── § 4.10.A  Event ordering ─────────────────────────────────────────────────

// ClaimNextEvents claims a batch of unprocessed events for a consumer + chain
// in strict ascending logical_order_key order within the worker's partition.
func (d *DB) ClaimNextEvents(ctx context.Context, q database.EventClaimQuery) ([]contracts.EventEnvelope, error) {
	if q.NumWorkers <= 0 {
		q.NumWorkers = 1
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 10
	}

	// Build event-type filter.
	if len(q.EventTypes) == 0 {
		return nil, fmt.Errorf("ClaimNextEvents: EventTypes must not be empty")
	}
	// Sort for determinism.
	sorted := make([]string, len(q.EventTypes))
	copy(sorted, q.EventTypes)
	sort.Strings(sorted)

	placeholders := make([]string, len(sorted))
	args := []any{q.Chain, q.Consumer, q.WorkerID, q.NumWorkers, limit}
	for i, et := range sorted {
		placeholders[i] = fmt.Sprintf("$%d", len(args)+1)
		args = append(args, et)
	}

	const qBase = `
WITH claimed AS (
    SELECT event_id, event_type, payload, trace_id, correlation_id,
           causation_id, version_id, created_at
    FROM events
    WHERE chain          = $1
      AND consumer       = $2
      AND processed      = FALSE
      AND (HASHTEXT(correlation_id) % $4) = $3
      AND invalidated_at IS NULL
      AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
      AND event_type = ANY($6)
    ORDER BY logical_order_key ASC
    LIMIT $5
    FOR UPDATE SKIP LOCKED
)
UPDATE events e
   SET claimed_at = CURRENT_TIMESTAMP
FROM claimed
WHERE e.event_id = claimed.event_id
RETURNING e.event_id, e.event_type, e.payload, e.trace_id, e.correlation_id,
          COALESCE(e.causation_id,''), e.version_id,
          to_char(e.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')`

	// Build the $6 array literal for event types.
	args[4] = limit
	// Replace $6 placeholder with proper array.
	query := strings.Replace(qBase, "= ANY($6)", "= ANY($6::text[])", 1)
	_ = query // We build a dynamic variant below.

	// Use a dynamic query with the types inlined as a positional array param.
	finalQuery := fmt.Sprintf(`
WITH claimed AS (
    SELECT event_id, event_type, payload, trace_id, correlation_id,
           causation_id, version_id, created_at
    FROM events
    WHERE chain          = $1
      AND consumer       = $2
      AND processed      = FALSE
      AND (HASHTEXT(correlation_id) %% $4) = $3
      AND invalidated_at IS NULL
      AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
      AND event_type IN (%s)
    ORDER BY logical_order_key ASC, event_id ASC
    LIMIT $5
    FOR UPDATE SKIP LOCKED
)
UPDATE events e
   SET claimed_at = CURRENT_TIMESTAMP
FROM claimed
WHERE e.event_id = claimed.event_id
RETURNING e.event_id, e.event_type, e.payload::text, e.trace_id, e.correlation_id,
          COALESCE(e.causation_id,''), e.version_id,
          to_char(e.created_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')`,
		strings.Join(placeholders, ","))

	rows, err := d.pool.QueryContext(ctx, finalQuery, args...)
	if err != nil {
		return nil, fmt.Errorf("ClaimNextEvents: %w", err)
	}
	defer rows.Close()

	var result []contracts.EventEnvelope
	for rows.Next() {
		var env contracts.EventEnvelope
		if err := rows.Scan(
			&env.EventID, &env.EventType, &env.Payload,
			&env.TraceID, &env.CorrelationID, &env.CausationID, &env.VersionID,
			&env.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("ClaimNextEvents scan: %w", err)
		}
		result = append(result, env)
	}
	return result, rows.Err()
}

// ── § 4.10.C  Dead-letter queue ──────────────────────────────────────────────

// IncrementEventRetry increments retry_count for an event and returns the new count.
func (d *DB) IncrementEventRetry(ctx context.Context, eventID, consumer string) (int, error) {
	const q = `
UPDATE events
   SET retry_count = retry_count + 1
WHERE event_id = $1
RETURNING retry_count`
	var count int
	err := d.pool.QueryRowContext(ctx, q, eventID).Scan(&count)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, database.ErrNotFound
		}
		return 0, fmt.Errorf("IncrementEventRetry: %w", err)
	}
	return count, nil
}

// MoveToDLQ moves a failed event to dead_letter_events and marks it processed.
func (d *DB) MoveToDLQ(ctx context.Context, e database.DLQEntry) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if e.MovedToDLQAt == "" {
		e.MovedToDLQAt = now
	}
	if e.FirstFailedAt == "" {
		e.FirstFailedAt = now
	}
	if e.LastFailedAt == "" {
		e.LastFailedAt = now
	}

	payloadJSON := e.PayloadJSON
	if payloadJSON == nil {
		payloadJSON = []byte("{}")
	}

	return d.withTx(ctx, func(tx *sql.Tx) error {
		const ins = `
INSERT INTO dead_letter_events
    (event_id, chain, consumer, reason, error_message, retry_count,
     first_failed_at, last_failed_at, moved_to_dlq_at,
     payload_snapshot, trace_id, correlation_id, causation_id, version_id)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
ON CONFLICT (event_id) DO NOTHING`
		if _, err := tx.ExecContext(ctx, ins,
			e.EventID, e.Chain, e.Consumer, e.Reason, e.ErrorMessage, e.RetryCount,
			e.FirstFailedAt, e.LastFailedAt, e.MovedToDLQAt,
			payloadJSON, e.TraceID, e.CorrelationID, e.CausationID, e.VersionID,
		); err != nil {
			return fmt.Errorf("MoveToDLQ insert: %w", err)
		}

		// Mark the source event processed so the partition advances.
		const upd = `UPDATE events SET processed = TRUE WHERE event_id = $1`
		if _, err := tx.ExecContext(ctx, upd, e.EventID); err != nil {
			return fmt.Errorf("MoveToDLQ mark processed: %w", err)
		}
		return nil
	})
}

// RequeueFromDLQ re-inserts a DLQ event into the events table for reprocessing.
func (d *DB) RequeueFromDLQ(ctx context.Context, eventID string) error {
	return d.withTx(ctx, func(tx *sql.Tx) error {
		// Reset processed flag and retry_count on the source event.
		const upd = `
UPDATE events
   SET processed   = FALSE,
       retry_count = 0,
       claimed_at  = NULL
WHERE event_id = $1`
		res, err := tx.ExecContext(ctx, upd, eventID)
		if err != nil {
			return fmt.Errorf("RequeueFromDLQ reset: %w", err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return database.ErrNotFound
		}

		// Remove from DLQ.
		const del = `DELETE FROM dead_letter_events WHERE event_id = $1`
		if _, err := tx.ExecContext(ctx, del, eventID); err != nil {
			return fmt.Errorf("RequeueFromDLQ delete dlq: %w", err)
		}
		return nil
	})
}

// ListDLQ returns dead-letter events matching the filter.
func (d *DB) ListDLQ(ctx context.Context, filter database.DLQFilter) ([]database.DLQEntry, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}

	args := []any{limit}
	conds := []string{}
	if filter.Consumer != "" {
		args = append(args, filter.Consumer)
		conds = append(conds, fmt.Sprintf("consumer = $%d", len(args)))
	}
	if filter.Reason != "" {
		args = append(args, filter.Reason)
		conds = append(conds, fmt.Sprintf("reason = $%d", len(args)))
	}

	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}

	q := fmt.Sprintf(`
SELECT event_id, chain, consumer, reason, COALESCE(error_message,''),
       retry_count,
       to_char(first_failed_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"'),
       to_char(last_failed_at  AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"'),
       to_char(moved_to_dlq_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"'),
       trace_id, correlation_id, causation_id, version_id,
       COALESCE(payload_snapshot::text,'{}')
FROM dead_letter_events
%s
ORDER BY moved_to_dlq_at DESC
LIMIT $1`, where)

	rows, err := d.pool.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("ListDLQ: %w", err)
	}
	defer rows.Close()

	var result []database.DLQEntry
	for rows.Next() {
		var e database.DLQEntry
		var payloadStr string
		if err := rows.Scan(
			&e.EventID, &e.Chain, &e.Consumer, &e.Reason, &e.ErrorMessage,
			&e.RetryCount, &e.FirstFailedAt, &e.LastFailedAt, &e.MovedToDLQAt,
			&e.TraceID, &e.CorrelationID, &e.CausationID, &e.VersionID, &payloadStr,
		); err != nil {
			return nil, fmt.Errorf("ListDLQ scan: %w", err)
		}
		e.PayloadJSON = []byte(payloadStr)
		result = append(result, e)
	}
	return result, rows.Err()
}

// ── § 4.10.D  Exactly-once execution lock ────────────────────────────────────

// ClaimExecution reserves an execution_id via INSERT ... ON CONFLICT DO NOTHING.
// Returns claimed=true if this is the first claim; false if already claimed.
func (d *DB) ClaimExecution(ctx context.Context, dto contracts.AllocationDTO) (bool, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	const q = `
INSERT INTO execution_results
    (event_id, trace_id, correlation_id, causation_id, version_id,
     token_lifecycle_id, execution_id, allocation_id,
     status, success, completed_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'reserved', FALSE, $9)
ON CONFLICT (execution_id) DO NOTHING`

	res, err := d.pool.ExecContext(ctx, q,
		dto.EventID, dto.TraceID, dto.CorrelationID, dto.CausationID, dto.VersionID,
		dto.TokenLifecycleID, dto.ExecutionID, dto.EventID,
		now,
	)
	if err != nil {
		return false, fmt.Errorf("ClaimExecution: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ── § 4.10.E  Position consistency ───────────────────────────────────────────

// UpsertPositionFromExecution creates a position row keyed on source_execution_id.
// Returns created=true on first insert, false on conflict (idempotent retry).
func (d *DB) UpsertPositionFromExecution(ctx context.Context, p contracts.PositionStateDTO) (bool, error) {
	const q = `
INSERT INTO positions
    (event_id, trace_id, correlation_id, causation_id, version_id,
     token_lifecycle_id, position_id, execution_id, token_address, chain,
     status, entry_price, entry_size_usd, current_price,
     exit_price, exit_reason, pnl_usd, pnl_pct,
     tp1_bps, tp2_bps, sl_bps, max_hold_seconds,
     expires_at, priority, opened_at, exited_at, snapshot_at,
     source_execution_id)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,
        $19,$20,$21,$22,$23,$24,$25,$26,$27,$28)
ON CONFLICT (source_execution_id) WHERE source_execution_id IS NOT NULL DO NOTHING`

	res, err := d.pool.ExecContext(ctx, q,
		p.EventID, p.TraceID, p.CorrelationID, p.CausationID, p.VersionID,
		p.TokenLifecycleID, p.PositionID, p.ExecutionID, p.TokenAddress, p.Chain,
		p.Status, p.EntryPrice, p.EntrySizeUsd, p.CurrentPrice,
		p.ExitPrice, p.ExitReason, p.PnlUsd, p.PnlPct,
		p.Tp1Bps, p.Tp2Bps, p.SlBps, p.MaxHoldSeconds,
		nilIfEmpty(p.ExpiresAt), p.Priority, p.OpenedAt, p.ExitedAt, p.SnapshotAt,
		p.ExecutionID, // source_execution_id = execution_id
	)
	if err != nil {
		return false, fmt.Errorf("UpsertPositionFromExecution: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ListOpenPositionsForReconciliation returns all open positions with wallet addresses and last-known on-chain amount.
func (d *DB) ListOpenPositionsForReconciliation(ctx context.Context) ([]database.ReconciliationPosition, error) {
	const q = `
SELECT DISTINCT ON (p.position_id)
    p.position_id, p.token_address, p.chain, p.execution_id,
    COALESCE(e.wallet_address, '') AS wallet_address,
    COALESCE(p.amount_raw, '')     AS amount_raw
FROM positions p
LEFT JOIN execution_results e ON e.execution_id = p.execution_id
WHERE p.status = 'open'
ORDER BY p.position_id, p.snapshot_at DESC`

	rows, err := d.pool.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("ListOpenPositionsForReconciliation: %w", err)
	}
	defer rows.Close()

	var result []database.ReconciliationPosition
	for rows.Next() {
		var r database.ReconciliationPosition
		if err := rows.Scan(
			&r.PositionID, &r.TokenAddress, &r.Chain, &r.ExecutionID, &r.WalletAddress, &r.AmountRaw,
		); err != nil {
			return nil, fmt.Errorf("ListOpenPositionsForReconciliation scan: %w", err)
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// AdjustPositionAmount records an on-chain amount reconciliation event for an open position
// without mutating price fields used by exit evaluation.
// It writes the on-chain token balance to positions.amount_raw (not current_price, which
// is a price decimal used by the position engine for TP/SL evaluation).
func (d *DB) AdjustPositionAmount(ctx context.Context, positionID, onchainAmount, reason string) error {
	return d.withTx(ctx, func(tx *sql.Tx) error {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		// Update amount_raw (on-chain token balance) and snapshot_at only.
		// current_price is a float price string used for exit evaluation — do NOT overwrite it.
		const upd = `
UPDATE positions
   SET amount_raw  = $2,
       snapshot_at = $3
WHERE position_id = $1 AND status = 'open'`
		if _, err := tx.ExecContext(ctx, upd, positionID, onchainAmount, now); err != nil {
			return fmt.Errorf("AdjustPositionAmount update: %w", err)
		}

		const ins = `
INSERT INTO reconciliation_events (position_id, db_amount, onchain_amount, action, reason, observed_at)
VALUES ($1, 0, $2, 'adjusted', $3, CURRENT_TIMESTAMP)
ON CONFLICT DO NOTHING`
		if _, err := tx.ExecContext(ctx, ins, positionID, onchainAmount, reason); err != nil {
			return fmt.Errorf("AdjustPositionAmount recon event: %w", err)
		}
		return nil
	})
}

// ClosePositionForced closes an open position when on-chain balance is zero.
// Uses status='exited' (not 'closed') so downstream evaluation and learning workers
// that check for status='exited' can pick up the position for PnL recording.
func (d *DB) ClosePositionForced(ctx context.Context, positionID, reason string) error {
	return d.withTx(ctx, func(tx *sql.Tx) error {
		now := time.Now().UTC().Format(time.RFC3339Nano)
		const upd = `
UPDATE positions
   SET status      = 'exited',
       exit_reason = $2,
       exited_at   = $3,
       snapshot_at = $3
WHERE position_id = $1 AND status = 'open'`
		if _, err := tx.ExecContext(ctx, upd, positionID, reason, now); err != nil {
			return fmt.Errorf("ClosePositionForced update: %w", err)
		}

		const ins = `
INSERT INTO reconciliation_events (position_id, db_amount, onchain_amount, action, reason, observed_at)
VALUES ($1, 0, 0, 'closed', $2, CURRENT_TIMESTAMP)
ON CONFLICT DO NOTHING`
		if _, err := tx.ExecContext(ctx, ins, positionID, reason); err != nil {
			return fmt.Errorf("ClosePositionForced recon event: %w", err)
		}
		return nil
	})
}

// ── § 4.10.F  Latency feedback loop ─────────────────────────────────────────

// InsertLatencyEvent records one execution latency sample.
func (d *DB) InsertLatencyEvent(ctx context.Context, le database.LatencyEvent) error {
	observedAt := le.ObservedAt
	if observedAt == "" {
		observedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	const q = `
INSERT INTO latency_events
    (execution_id, chain, endpoint, version_id, op_kind,
     decision_to_send_ms, send_to_first_observe_ms, first_observe_to_confirm_ms,
     total_ms, outcome, observed_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	if _, err := d.pool.ExecContext(ctx, q,
		le.ExecutionID, le.Chain, le.Endpoint, le.VersionID, le.OpKind,
		le.DecisionToSendMs, le.SendToFirstObserveMs, le.FirstObserveToConfirmMs,
		le.TotalMs, le.Outcome, observedAt,
	); err != nil {
		return fmt.Errorf("InsertLatencyEvent: %w", err)
	}
	return nil
}

// GetLatencyProfile aggregates latency samples over windowSec and returns P50/P95.
func (d *DB) GetLatencyProfile(ctx context.Context, chain, endpoint, opKind string, windowSec int) (contracts.LatencyProfileDTO, error) {
	if windowSec <= 0 {
		windowSec = 300
	}
	const q = `
SELECT
    COALESCE(PERCENTILE_CONT(0.50) WITHIN GROUP (ORDER BY total_ms), 0)::BIGINT AS p50,
    COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (ORDER BY total_ms), 0)::BIGINT AS p95,
    COUNT(*) AS sample_count
FROM latency_events
WHERE chain    = $1
  AND endpoint = $2
  AND op_kind  = $3
  AND observed_at >= CURRENT_TIMESTAMP - ($4 * INTERVAL '1 second')`

	var p50, p95 int64
	var count int64
	if err := d.pool.QueryRowContext(ctx, q, chain, endpoint, opKind, windowSec).Scan(&p50, &p95, &count); err != nil {
		return contracts.LatencyProfileDTO{}, fmt.Errorf("GetLatencyProfile: %w", err)
	}

	return contracts.LatencyProfileDTO{
		EventID:           contracts.ContentIDFromString(chain + ":" + endpoint + ":" + opKind),
		Chain:             chain,
		ExpectedP50Ms:     int32(p50),
		ExpectedP95Ms:     int32(p95),
		WindowSizeSeconds: int32(windowSec),
		EstimatedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}, nil
}

// ── § 4.10.G  Config consistency ─────────────────────────────────────────────

// DrainAndCheckPipelineIdle polls until all claimed-but-unprocessed events
// complete, or timeoutSec elapses.
func (d *DB) DrainAndCheckPipelineIdle(ctx context.Context, timeoutSec int) (bool, error) {
	if timeoutSec <= 0 {
		timeoutSec = 30
	}
	deadline := time.Now().Add(time.Duration(timeoutSec) * time.Second)
	const q = `
SELECT COUNT(*) FROM events
WHERE processed = FALSE
  AND claimed_at IS NOT NULL
  AND claimed_at > CURRENT_TIMESTAMP - INTERVAL '10 minutes'`

	for {
		var inflight int64
		if err := d.pool.QueryRowContext(ctx, q).Scan(&inflight); err != nil {
			return false, fmt.Errorf("DrainAndCheckPipelineIdle: %w", err)
		}
		if inflight == 0 {
			return true, nil
		}
		if time.Now().After(deadline) {
			return false, nil
		}
		select {
		case <-ctx.Done():
			return false, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// PromoteStrategyVersion atomically promotes newVersionID after drain.
func (d *DB) PromoteStrategyVersion(ctx context.Context, newVersionID string, drainTimeoutSec int) error {
	idle, err := d.DrainAndCheckPipelineIdle(ctx, drainTimeoutSec)
	if err != nil {
		return fmt.Errorf("PromoteStrategyVersion drain: %w", err)
	}
	if !idle {
		return database.ErrDrainTimeout
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	return d.withTx(ctx, func(tx *sql.Tx) error {
		// Deactivate current active version.
		const deact = `
UPDATE strategy_versions
   SET status          = 'deactivated',
       deactivated_at  = $1
WHERE status = 'active'`
		if _, err := tx.ExecContext(ctx, deact, now); err != nil {
			return fmt.Errorf("PromoteStrategyVersion deactivate: %w", err)
		}

		// Promote the new version.
		const act = `
UPDATE strategy_versions
   SET status       = 'active',
       promoted_at  = $1,
       activated_at = COALESCE(activated_at, $1)
WHERE strategy_version_id = $2`
		res, err := tx.ExecContext(ctx, act, now, newVersionID)
		if err != nil {
			return fmt.Errorf("PromoteStrategyVersion activate: %w", err)
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return database.ErrUnknownVersion
		}
		return nil
	})
}

// ── § 4.10.H  Circuit breaker / kill switch ──────────────────────────────────

// SetSystemHalt sets or clears the global kill switch.
func (d *DB) SetSystemHalt(ctx context.Context, halt bool, reason, operator string) error {
	const q = `
UPDATE system_halt
   SET halted     = $1,
       reason     = $2,
       operator   = $3,
       updated_at = CURRENT_TIMESTAMP
WHERE id = 1`
	if _, err := d.pool.ExecContext(ctx, q, halt, reason, operator); err != nil {
		return fmt.Errorf("SetSystemHalt: %w", err)
	}
	return nil
}

// IsSystemHalted reads the current kill switch state.
func (d *DB) IsSystemHalted(ctx context.Context) (bool, string, error) {
	const q = `SELECT halted, reason FROM system_halt WHERE id = 1`
	var halted bool
	var reason string
	if err := d.pool.QueryRowContext(ctx, q).Scan(&halted, &reason); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, "", nil
		}
		return false, "", fmt.Errorf("IsSystemHalted: %w", err)
	}
	return halted, reason, nil
}

// ── § 4.10.I  Replay validation ──────────────────────────────────────────────

// ComputeStateHash computes a deterministic digest over canonical pipeline state.
// Covers positions (open), executions (confirmed), active strategy_version.
// Fields are sorted deterministically before hashing.
func (d *DB) ComputeStateHash(ctx context.Context) (string, error) {
	// Gather open positions ordered deterministically.
	const posQ = `
SELECT position_id, execution_id, token_address, chain, status,
       entry_price, entry_size_usd, pnl_usd
FROM positions
WHERE status = 'open'
ORDER BY position_id ASC`

	posRows, err := d.pool.QueryContext(ctx, posQ)
	if err != nil {
		return "", fmt.Errorf("ComputeStateHash positions: %w", err)
	}
	defer posRows.Close()

	type posRow struct {
		PositionID  string
		ExecutionID string
		Token       string
		Chain       string
		Status      string
		EntryPrice  string
		SizeUsd     float64
		PnlUsd      float64
	}
	var positions []posRow
	for posRows.Next() {
		var r posRow
		if err := posRows.Scan(&r.PositionID, &r.ExecutionID, &r.Token, &r.Chain,
			&r.Status, &r.EntryPrice, &r.SizeUsd, &r.PnlUsd); err != nil {
			return "", fmt.Errorf("ComputeStateHash position scan: %w", err)
		}
		positions = append(positions, r)
	}
	if err := posRows.Err(); err != nil {
		return "", err
	}
	sort.Slice(positions, func(i, j int) bool { return positions[i].PositionID < positions[j].PositionID })

	// Gather confirmed executions.
	const execQ = `
SELECT execution_id, token_lifecycle_id, status, success, tx_hash
FROM execution_results
WHERE status = 'confirmed'
ORDER BY execution_id ASC`

	execRows, err := d.pool.QueryContext(ctx, execQ)
	if err != nil {
		return "", fmt.Errorf("ComputeStateHash executions: %w", err)
	}
	defer execRows.Close()

	type execRow struct {
		ExecutionID      string
		TokenLifecycleID string
		Status           string
		Success          bool
		TxHash           string
	}
	var execs []execRow
	for execRows.Next() {
		var r execRow
		if err := execRows.Scan(&r.ExecutionID, &r.TokenLifecycleID, &r.Status, &r.Success, &r.TxHash); err != nil {
			return "", fmt.Errorf("ComputeStateHash exec scan: %w", err)
		}
		execs = append(execs, r)
	}
	if err := execRows.Err(); err != nil {
		return "", err
	}
	sort.Slice(execs, func(i, j int) bool { return execs[i].ExecutionID < execs[j].ExecutionID })

	// Active strategy version.
	var activeVersionID string
	_ = d.pool.QueryRowContext(ctx,
		`SELECT strategy_version_id FROM strategy_versions WHERE status='active' LIMIT 1`,
	).Scan(&activeVersionID)

	snapshot := map[string]any{
		"active_version": activeVersionID,
		"positions":      positions,
		"executions":     execs,
	}
	b, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("ComputeStateHash marshal: %w", err)
	}
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h[:]), nil
}

// ── § 4.11.B  Partition leases ───────────────────────────────────────────────

// ClaimPartitions acquires partition leases for the worker.
func (d *DB) ClaimPartitions(ctx context.Context, chain, consumer, workerID string, n, ttlSec int) ([]int, error) {
	if ttlSec <= 0 {
		ttlSec = 60
	}
	now := time.Now().UTC()
	expires := now.Add(time.Duration(ttlSec) * time.Second).Format(time.RFC3339Nano)

	var granted []int
	for i := 0; i < n; i++ {
		const q = `
INSERT INTO partition_leases (chain, consumer, partition_key, worker_id, leased_at, expires_at)
VALUES ($1, $2, $3, $4, CURRENT_TIMESTAMP, $5)
ON CONFLICT (chain, consumer, partition_key) DO UPDATE
    SET worker_id  = EXCLUDED.worker_id,
        leased_at  = CURRENT_TIMESTAMP,
        expires_at = EXCLUDED.expires_at
WHERE partition_leases.expires_at < CURRENT_TIMESTAMP
   OR partition_leases.worker_id = EXCLUDED.worker_id`
		res, err := d.pool.ExecContext(ctx, q, chain, consumer, i, workerID, expires)
		if err != nil {
			return granted, fmt.Errorf("ClaimPartitions partition %d: %w", i, err)
		}
		if nr, _ := res.RowsAffected(); nr > 0 {
			granted = append(granted, i)
		}
	}
	sort.Ints(granted)
	return granted, nil
}

// RenewPartitions extends the expiry on all leases held by workerID.
// Extension duration is read from d.cfg.PartitionLeaseTTLSec (default 60).
func (d *DB) RenewPartitions(ctx context.Context, chain, consumer, workerID string) error {
	ttlSec := d.cfg.PartitionLeaseTTLSec
	if ttlSec <= 0 {
		ttlSec = 60
	}
	const q = `
UPDATE partition_leases
   SET expires_at = CURRENT_TIMESTAMP + ($4 * INTERVAL '1 second')
WHERE chain = $1 AND consumer = $2 AND worker_id = $3`
	if _, err := d.pool.ExecContext(ctx, q, chain, consumer, workerID, ttlSec); err != nil {
		return fmt.Errorf("RenewPartitions: %w", err)
	}
	return nil
}

// ReleasePartitions removes all leases held by workerID.
func (d *DB) ReleasePartitions(ctx context.Context, chain, consumer, workerID string) error {
	const q = `DELETE FROM partition_leases WHERE chain=$1 AND consumer=$2 AND worker_id=$3`
	if _, err := d.pool.ExecContext(ctx, q, chain, consumer, workerID); err != nil {
		return fmt.Errorf("ReleasePartitions: %w", err)
	}
	return nil
}

// ── § 4.11.C  Crash-safe recovery ────────────────────────────────────────────

// ListInFlightExecutions returns execution_attempts in reserved/sent state.
func (d *DB) ListInFlightExecutions(ctx context.Context) ([]database.InFlightExecution, error) {
	const q = `
SELECT execution_id, attempt_number, tx_hash, status, nonce,
       COALESCE(gas_price_wei::text,'0'),
       to_char(sent_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"'),
       to_char(observed_at AT TIME ZONE 'UTC','YYYY-MM-DD"T"HH24:MI:SS"Z"')
FROM execution_attempts
WHERE status IN ('reserved','sent')
ORDER BY execution_id ASC, attempt_number ASC`

	rows, err := d.pool.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("ListInFlightExecutions: %w", err)
	}
	defer rows.Close()

	var result []database.InFlightExecution
	for rows.Next() {
		var e database.InFlightExecution
		if err := rows.Scan(
			&e.ExecutionID, &e.AttemptNumber, &e.TxHash, &e.Status, &e.Nonce,
			&e.GasPriceWei, &e.SentAt, &e.ObservedAt,
		); err != nil {
			return nil, fmt.Errorf("ListInFlightExecutions scan: %w", err)
		}
		result = append(result, e)
	}
	return result, rows.Err()
}

// FinalizeExecution records the on-chain outcome for an execution attempt.
func (d *DB) FinalizeExecution(ctx context.Context, executionID string, receipt database.ExecutionReceipt) error {
	return d.withTx(ctx, func(tx *sql.Tx) error {
		const updAttempt = `
UPDATE execution_attempts
   SET status      = $2,
       tx_hash     = $3,
       observed_at = CURRENT_TIMESTAMP
WHERE execution_id = $1 AND status IN ('reserved','sent')`
		if _, err := tx.ExecContext(ctx, updAttempt, executionID, receipt.Status, receipt.TxHash); err != nil {
			return fmt.Errorf("FinalizeExecution attempt: %w", err)
		}

		success := receipt.Status == "confirmed"
		const updExec = `
UPDATE execution_results
   SET status       = $2,
       success      = $3,
       tx_hash      = COALESCE(NULLIF(tx_hash,''), $4),
       block_number = $5,
       completed_at = $6
WHERE execution_id = $1`
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if _, err := tx.ExecContext(ctx, updExec,
			executionID, receipt.Status, success, receipt.TxHash, receipt.BlockNumber, now,
		); err != nil {
			return fmt.Errorf("FinalizeExecution exec_results: %w", err)
		}
		return nil
	})
}

// AbortReservedExecution frees a reserved execution so it can be retried.
func (d *DB) AbortReservedExecution(ctx context.Context, executionID, reason string) error {
	return d.withTx(ctx, func(tx *sql.Tx) error {
		const del = `DELETE FROM execution_attempts WHERE execution_id=$1 AND status='reserved'`
		if _, err := tx.ExecContext(ctx, del, executionID); err != nil {
			return fmt.Errorf("AbortReservedExecution delete attempt: %w", err)
		}
		const upd = `
UPDATE execution_results
   SET status = 'aborted', success = FALSE, error_code = $2, completed_at = $3
WHERE execution_id = $1 AND status = 'reserved'`
		if _, err := tx.ExecContext(ctx, upd, executionID, reason,
			time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
			return fmt.Errorf("AbortReservedExecution exec_results: %w", err)
		}
		return nil
	})
}

// MarkExecutionLost marks an execution as lost (tx never landed).
func (d *DB) MarkExecutionLost(ctx context.Context, executionID, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	const q = `
UPDATE execution_results
   SET status     = 'lost',
       success    = FALSE,
       error_code = $2,
       completed_at = $3
WHERE execution_id = $1`
	if _, err := d.pool.ExecContext(ctx, q, executionID, reason, now); err != nil {
		return fmt.Errorf("MarkExecutionLost: %w", err)
	}
	return nil
}

// ── § 4.11.D  Reorg handling ─────────────────────────────────────────────────

// RecordReorg records a chain reorganization event.
func (d *DB) RecordReorg(ctx context.Context, chain string, oldBlock, newBlock int64, depth int) error {
	const q = `
INSERT INTO reorg_events (chain, old_block, new_block, depth, affected_count, detected_at)
VALUES ($1, $2, $3, $4, 0, CURRENT_TIMESTAMP)
ON CONFLICT (chain, old_block, detected_at) DO NOTHING`
	if _, err := d.pool.ExecContext(ctx, q, chain, oldBlock, newBlock, depth); err != nil {
		return fmt.Errorf("RecordReorg: %w", err)
	}
	return nil
}

// InvalidateBlockRange marks unprocessed events in [fromBlock, toBlock] as invalidated.
// When block_number is populated on the event (Phase 8+), filtering is precise.
// When block_number = 0 (legacy events), events are NOT invalidated by range queries
// (fromBlock > 0) to avoid incorrectly dropping unrelated events during a reorg.
// Only events with processed = FALSE are eligible — already-processed events are left intact.
func (d *DB) InvalidateBlockRange(ctx context.Context, chain string, fromBlock, toBlock int64) (int, error) {
	const q = `
UPDATE events
   SET invalidated_at = CURRENT_TIMESTAMP
WHERE chain           = $1
  AND processed       = FALSE
  AND invalidated_at  IS NULL
  AND (
       $2 = 0  -- no block range specified: invalidate all unprocessed chain events
       OR (block_number > 0 AND block_number >= $2 AND block_number <= $3)
  )`
	res, err := d.pool.ExecContext(ctx, q, chain, fromBlock, toBlock)
	if err != nil {
		return 0, fmt.Errorf("InvalidateBlockRange: %w", err)
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// MarkPositionsUncertain marks open positions as uncertain after a reorg.
func (d *DB) MarkPositionsUncertain(ctx context.Context, chain string, fromBlock int64, reason string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	const q = `
UPDATE positions
   SET status      = 'uncertain',
       exit_reason = $3,
       snapshot_at = $4
WHERE chain = $1 AND status = 'open'`
	if _, err := d.pool.ExecContext(ctx, q, chain, fromBlock, reason, now); err != nil {
		return fmt.Errorf("MarkPositionsUncertain: %w", err)
	}
	return nil
}

// ReResolveExecutionAfterReorg updates confirmation_status post-reorg.
func (d *DB) ReResolveExecutionAfterReorg(ctx context.Context, executionID, txHash string, outcome database.ReorgOutcome) error {
	const q = `
UPDATE execution_results
   SET confirmation_status = $2,
       tx_hash             = COALESCE(NULLIF($3,''), tx_hash)
WHERE execution_id = $1`
	if _, err := d.pool.ExecContext(ctx, q, executionID, string(outcome), txHash); err != nil {
		return fmt.Errorf("ReResolveExecutionAfterReorg: %w", err)
	}
	return nil
}

// ── § 4.11.E  Evaluation invariant ──────────────────────────────────────────

// RecordExecutionForEvaluation registers an execution in the evaluation invariant.
func (d *DB) RecordExecutionForEvaluation(ctx context.Context, executionID string, deadlineSec int) error {
	if deadlineSec <= 0 {
		deadlineSec = 3600
	}
	const q = `
INSERT INTO evaluation_invariant (execution_id, has_evaluation, deadline_at)
VALUES ($1, FALSE, CURRENT_TIMESTAMP + ($2 * INTERVAL '1 second'))
ON CONFLICT (execution_id) DO NOTHING`
	if _, err := d.pool.ExecContext(ctx, q, executionID, deadlineSec); err != nil {
		return fmt.Errorf("RecordExecutionForEvaluation: %w", err)
	}
	return nil
}

// MarkEvaluationDone marks the evaluation invariant as complete.
func (d *DB) MarkEvaluationDone(ctx context.Context, executionID string) error {
	const q = `
UPDATE evaluation_invariant
   SET has_evaluation = TRUE, completed_at = CURRENT_TIMESTAMP
WHERE execution_id = $1`
	if _, err := d.pool.ExecContext(ctx, q, executionID); err != nil {
		return fmt.Errorf("MarkEvaluationDone: %w", err)
	}
	return nil
}

// ListMissingEvaluations returns executions whose deadline has passed.
func (d *DB) ListMissingEvaluations(ctx context.Context) ([]database.MissingEvaluation, error) {
	const q = `
SELECT execution_id,
       to_char(deadline_at AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')
FROM evaluation_invariant
WHERE has_evaluation = FALSE
  AND deadline_at < CURRENT_TIMESTAMP
ORDER BY deadline_at ASC`

	rows, err := d.pool.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("ListMissingEvaluations: %w", err)
	}
	defer rows.Close()

	var result []database.MissingEvaluation
	for rows.Next() {
		var m database.MissingEvaluation
		if err := rows.Scan(&m.ExecutionID, &m.DeadlineAt); err != nil {
			return nil, fmt.Errorf("ListMissingEvaluations scan: %w", err)
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// ── § 4.11.F  Backpressure ───────────────────────────────────────────────────

// GetUnprocessedCount returns the number of unprocessed events for consumer+chain.
func (d *DB) GetUnprocessedCount(ctx context.Context, chain, consumer string) (int64, error) {
	const q = `
SELECT COUNT(*) FROM events
WHERE processed = FALSE
  AND chain    = $1
  AND consumer = $2
  AND invalidated_at IS NULL`
	var count int64
	if err := d.pool.QueryRowContext(ctx, q, chain, consumer).Scan(&count); err != nil {
		return 0, fmt.Errorf("GetUnprocessedCount: %w", err)
	}
	return count, nil
}

// RecordDrop records a dropped ingestion event.
func (d *DB) RecordDrop(ctx context.Context, chain, reason, tokenAddress, score string) error {
	const q = `
INSERT INTO ingestion_drops (chain, reason, token_address, score, dropped_at)
VALUES ($1, $2, $3, $4::NUMERIC, CURRENT_TIMESTAMP)`
	if _, err := d.pool.ExecContext(ctx, q, chain, reason, tokenAddress, score); err != nil {
		return fmt.Errorf("RecordDrop: %w", err)
	}
	return nil
}

// ── Helpers ──────────────────────────────────────────────────────────────────

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
