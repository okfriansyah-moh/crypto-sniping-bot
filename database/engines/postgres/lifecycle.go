package postgres

// Token lifecycle state machine implementation for Phase 2.
// Tables: token_lifecycle, token_state_transitions, state_violations.
// All SQL uses parameterized queries and ON CONFLICT semantics.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
)

// StartLifecycle creates a new lifecycle entry at state DETECTED for the given token.
// The lifecycle ID is derived from the token address and chain (content-addressable).
// Idempotent: if a lifecycle already exists for the token, returns the existing ID.
func (d *DB) StartLifecycle(ctx context.Context, dto contracts.MarketDataDTO) (string, error) {
	lifecycleID := contracts.ContentIDFromString(dto.TokenAddress + ":" + dto.Chain)
	now := time.Now().UTC().Format(time.RFC3339Nano)

	const q = `
INSERT INTO token_lifecycle (token_lifecycle_id, token_address, current_state, state_version, created_at, updated_at)
VALUES ($1, $2, 'DETECTED', 0, $3, $3)
ON CONFLICT (token_lifecycle_id) DO NOTHING`

	if _, err := d.pool.ExecContext(ctx, q, lifecycleID, dto.TokenAddress, now); err != nil {
		return "", fmt.Errorf("start lifecycle: %w", err)
	}
	return lifecycleID, nil
}

// TransitionState applies a forward-only CAS transition on a token lifecycle.
// Uses UPDATE ... WHERE current_state = $expected AND state_version = $ver (optimistic lock).
// Returns ErrForbiddenTransition if the target is not a valid forward state.
// Returns ErrInvalidTransition if the CAS guard (state_version or current_state) fails.
func (d *DB) TransitionState(ctx context.Context, req database.TransitionRequest) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	// Validate forward transition topology.
	if !isValidTransition(req.ExpectedFromState, req.NewState) {
		return fmt.Errorf("transition %s→%s: %w", req.ExpectedFromState, req.NewState, database.ErrForbiddenTransition)
	}

	var terminalReason *string
	if isTerminalState(req.NewState) && req.Reason != "" {
		terminalReason = &req.Reason
	}
	// When transitioning OUT of a terminal state (e.g. REJECTED→DQ_PASSED rescan
	// recovery), terminalReason stays nil which explicitly clears the stale reason.
	// Using $2 directly (no COALESCE) ensures the column is always consistent with
	// the current state rather than preserving a stale value from a prior rejection.

	const q = `
UPDATE token_lifecycle
SET current_state   = $1,
    state_version   = state_version + 1,
    terminal_reason = $2,
    updated_at      = $3
WHERE token_lifecycle_id = $4
  AND current_state      = $5
  AND state_version      = $6`

	res, err := d.pool.ExecContext(ctx, q,
		req.NewState,
		terminalReason,
		now,
		req.LifecycleID,
		req.ExpectedFromState,
		req.ExpectedVersion,
	)
	if err != nil {
		return fmt.Errorf("transition state: %w", err)
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("transition state rows affected: %w", err)
	}
	if rows == 0 {
		// CAS guard failed — either wrong state or stale version.
		_ = d.InsertStateViolation(ctx, req.LifecycleID, req.ExpectedFromState, req.NewState, "cas_guard_failed")
		return fmt.Errorf("lifecycle %s: state=%s ver=%d: %w",
			req.LifecycleID, req.ExpectedFromState, req.ExpectedVersion, database.ErrInvalidTransition)
	}

	// Audit log — best effort; failure does not roll back the transition.
	d.recordTransition(ctx, req, now)
	return nil
}

// GetLifecycle fetches a lifecycle by ID.
func (d *DB) GetLifecycle(ctx context.Context, lifecycleID string) (*database.Lifecycle, error) {
	const q = `
SELECT token_lifecycle_id, token_address, current_state, state_version, terminal_reason, created_at, updated_at
FROM token_lifecycle
WHERE token_lifecycle_id = $1`

	return d.scanLifecycle(d.pool.QueryRowContext(ctx, q, lifecycleID))
}

// GetLifecycleByToken fetches the most-recent active lifecycle for a token address.
// Selects the row with the highest state_version to handle idempotent re-entry.
func (d *DB) GetLifecycleByToken(ctx context.Context, tokenAddress string) (*database.Lifecycle, error) {
	const q = `
SELECT token_lifecycle_id, token_address, current_state, state_version, terminal_reason, created_at, updated_at
FROM token_lifecycle
WHERE token_address = $1
ORDER BY updated_at DESC
LIMIT 1`

	lc, err := d.scanLifecycle(d.pool.QueryRowContext(ctx, q, tokenAddress))
	if err != nil {
		return nil, err
	}
	return lc, nil
}

// QuarantineToken marks a token as quarantined and transitions its lifecycle to REJECTED.
func (d *DB) QuarantineToken(ctx context.Context, tokenAddress string, reason string) error {
	lc, err := d.GetLifecycleByToken(ctx, tokenAddress)
	if err != nil {
		return fmt.Errorf("quarantine token: get lifecycle: %w", err)
	}
	req := database.TransitionRequest{
		LifecycleID:       lc.TokenLifecycleID,
		ExpectedFromState: lc.CurrentState,
		ExpectedVersion:   lc.StateVersion,
		NewState:          "REJECTED",
		Reason:            "quarantine:" + reason,
		ActorWorker:       "quarantine",
	}
	return d.TransitionState(ctx, req)
}

// InsertStateViolation records a CAS conflict violation for audit purposes.
func (d *DB) InsertStateViolation(ctx context.Context, lifecycleID, fromState, toState, reason string) error {
	const q = `
INSERT INTO state_violations (lifecycle_id, from_state, to_state, reason, recorded_at)
VALUES ($1, $2, $3, $4, $5)`

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if _, err := d.pool.ExecContext(ctx, q, lifecycleID, fromState, toState, reason, now); err != nil {
		return fmt.Errorf("insert state violation: %w", err)
	}
	return nil
}

// recordTransition inserts an audit row into token_state_transitions. Best-effort.
func (d *DB) recordTransition(ctx context.Context, req database.TransitionRequest, now string) {
	const q = `
INSERT INTO token_state_transitions
    (lifecycle_id, from_state, to_state, trace_id, correlation_id, reason, actor_worker, transitioned_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	_, _ = d.pool.ExecContext(ctx, q,
		req.LifecycleID,
		req.ExpectedFromState,
		req.NewState,
		req.TraceID,
		req.CorrelationID,
		req.Reason,
		req.ActorWorker,
		now,
	)
}

// scanLifecycle scans a *sql.Row into a *database.Lifecycle.
func (d *DB) scanLifecycle(row *sql.Row) (*database.Lifecycle, error) {
	var lc database.Lifecycle
	err := row.Scan(
		&lc.TokenLifecycleID,
		&lc.TokenAddress,
		&lc.CurrentState,
		&lc.StateVersion,
		&lc.TerminalReason,
		&lc.CreatedAt,
		&lc.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("scan lifecycle: %w", err)
	}
	return &lc, nil
}

// isValidTransition checks whether a state machine forward transition is permitted.
// Phase 2 best-effort: any forward transition is allowed; CAS guards reject concurrent races.
//
// REJECTED→DQ_PASSED: rescan re-evaluation path — the rescan worker re-emits
// market_data_event for tokens that were previously rejected (e.g. token_too_young).
// When the token now passes DQ the lifecycle must recover forward rather than stall.
//
// DQ_SKIPPED (Task 13): terminal state for EXPLORATION/VERY_EXPLORATION silent-drop.
// The rescan worker may re-evaluate a skipped token at a later time band; the state
// therefore allows recovery to DQ_PASSED or REJECTED.
func isValidTransition(from, to string) bool {
	allowed := map[string][]string{
		"DETECTED":      {"DQ_PASSED", "REJECTED", "DQ_SKIPPED"},
		"REJECTED":      {"DQ_PASSED", "DQ_SKIPPED"}, // rescan recovery: re-evaluated token now passes or skips
		"DQ_SKIPPED":    {"DQ_PASSED", "REJECTED"},   // rescan re-evaluation: skipped token later passes or is rejected
		"DQ_PASSED":     {"FEATURE_READY", "REJECTED"},
		"FEATURE_READY": {"EDGE_DETECTED", "REJECTED"},
		"EDGE_DETECTED": {"VALIDATED", "REJECTED"},
		"VALIDATED":     {"SELECTED", "REJECTED"},
		"SELECTED":      {"EXECUTED", "FAILED"},
		"EXECUTED":      {"POSITION_OPEN", "FAILED"},
		"POSITION_OPEN": {"POSITION_CLOSED"},
	}
	targets, ok := allowed[from]
	if !ok {
		return false
	}
	for _, t := range targets {
		if t == to {
			return true
		}
	}
	return false
}

// isTerminalState returns true for states from which no further transitions are allowed.
func isTerminalState(state string) bool {
	switch state {
	case "REJECTED", "FAILED", "POSITION_CLOSED":
		return true
	}
	return false
}

// pipelineStatsCountSQL is the parameterised aggregate used by GetPipelineStats.
// DQ_SKIPPED is excluded from dq_passed through validated because it is a
// terminal DQ-side audit state — SKIP emits no data_quality_event and tokens
// in DQ_SKIPPED have not progressed downstream.
var pipelineStatsCountSQL = `
WITH failed_from AS (
    -- For each FAILED token find the predecessor state.
    -- FAILED is terminal so there is exactly one X→FAILED row per lifecycle;
    -- DISTINCT ON + ORDER BY is a safety guard against duplicate rows.
    SELECT DISTINCT ON (t.lifecycle_id)
           t.lifecycle_id,
           t.from_state
    FROM   token_state_transitions t
    WHERE  t.to_state = 'FAILED'
    ORDER  BY t.lifecycle_id, t.transitioned_at DESC
)
SELECT
    COUNT(*)                                                                   AS detected,
    COUNT(*) FILTER (WHERE tl.current_state NOT IN (
        'DETECTED','REJECTED','DQ_SKIPPED'))                                   AS dq_passed,
    COUNT(*) FILTER (WHERE tl.current_state NOT IN (
        'DETECTED','REJECTED','DQ_PASSED','DQ_SKIPPED'))                       AS feature_ready,
    COUNT(*) FILTER (WHERE tl.current_state NOT IN (
        'DETECTED','REJECTED','DQ_PASSED','FEATURE_READY','DQ_SKIPPED'))       AS edge_detected,
    COUNT(*) FILTER (WHERE tl.current_state NOT IN (
        'DETECTED','REJECTED','DQ_PASSED','FEATURE_READY','EDGE_DETECTED','DQ_SKIPPED')) AS validated,
    -- selected: reached SELECTED or any later state (including FAILED from SELECTED+)
    COUNT(*) FILTER (WHERE tl.current_state IN (
        'SELECTED','EXECUTED','POSITION_OPEN','POSITION_CLOSED','EVALUATED','FAILED')) AS selected,
    -- executed: actually submitted a transaction (reached EXECUTED) or failed
    --           AFTER execution completed (EXECUTED→FAILED or POSITION_OPEN→FAILED)
    COUNT(*) FILTER (WHERE tl.current_state IN (
            'EXECUTED','POSITION_OPEN','POSITION_CLOSED','EVALUATED')
        OR (tl.current_state = 'FAILED'
            AND ff.from_state IN ('EXECUTED','POSITION_OPEN')))                AS executed,
    -- position_open: an on-chain position was actually opened (reached POSITION_OPEN)
    --                or failed while managing a live position (POSITION_OPEN→FAILED)
    COUNT(*) FILTER (WHERE tl.current_state IN (
            'POSITION_OPEN','POSITION_CLOSED','EVALUATED')
        OR (tl.current_state = 'FAILED'
            AND ff.from_state = 'POSITION_OPEN'))                              AS position_open,
    COUNT(*) FILTER (WHERE tl.current_state IN ('POSITION_CLOSED','EVALUATED'))AS position_closed,
    COUNT(*) FILTER (WHERE tl.current_state = 'EVALUATED')                     AS evaluated,
    COUNT(*) FILTER (WHERE tl.current_state = 'REJECTED')                      AS rejected,
    COUNT(*) FILTER (WHERE tl.current_state = 'FAILED')                        AS failed,
    -- Failure stage breakdown (sums to failed)
    COUNT(*) FILTER (WHERE tl.current_state = 'FAILED'
        AND ff.from_state = 'SELECTED')                                        AS failed_at_selected,
    COUNT(*) FILTER (WHERE tl.current_state = 'FAILED'
        AND ff.from_state = 'EXECUTED')                                        AS failed_at_executed,
    COUNT(*) FILTER (WHERE tl.current_state = 'FAILED'
        AND ff.from_state = 'POSITION_OPEN')                                   AS failed_at_position_open
FROM token_lifecycle tl
LEFT JOIN failed_from ff ON ff.lifecycle_id = tl.token_lifecycle_id
WHERE tl.created_at >= NOW() - ($1 * INTERVAL '1 hour')`

// GetPipelineStats returns cumulative funnel counts and the 10 most recently
// detected tokens for the given window.
//
// Counts are CUMULATIVE: each value represents "tokens that reached AT LEAST
// this stage". DETECTED = total tokens in window. REJECTED and FAILED are
// raw terminal counts. DQ_SKIPPED tokens are excluded from dq_passed through
// validated — they are terminal DQ-only outcomes, not downstream progress.
// A single single-row aggregate query replaces the former GROUP BY approach
// which returned point-in-time snapshot counts, making the denominator
// (Detected) nearly always zero for fast-moving tokens.
func (d *DB) GetPipelineStats(ctx context.Context, windowHours int) (*database.PipelineStats, error) {
	stats := &database.PipelineStats{WindowHours: windowHours}

	row := d.pool.QueryRowContext(ctx, pipelineStatsCountSQL, windowHours)
	if err := row.Scan(
		&stats.Detected,
		&stats.DQPassed,
		&stats.FeatureReady,
		&stats.EdgeDetected,
		&stats.Validated,
		&stats.Selected,
		&stats.Executed,
		&stats.PositionOpen,
		&stats.PositionClosed,
		&stats.Evaluated,
		&stats.Rejected,
		&stats.Failed,
		&stats.FailedAtSelected,
		&stats.FailedAtExecuted,
		&stats.FailedAtPositionOpen,
	); err != nil {
		return nil, fmt.Errorf("get pipeline stats counts: %w", err)
	}

	// ── Recent tokens (last 10, newest first) ─────────────────────────────────
	// LEFT JOIN market_data to pick up symbol/name/chain when persisted (Solana tokens).
	const recentQ = `
SELECT tl.token_address,
       COALESCE(md.symbol, '') AS symbol,
       COALESCE(md.name,   '') AS name,
       tl.current_state,
       COALESCE(md.chain,  '') AS chain,
       tl.created_at
FROM token_lifecycle tl
LEFT JOIN LATERAL (
    SELECT symbol, name, chain
    FROM market_data
    WHERE token_address = tl.token_address
    ORDER BY block_number DESC
    LIMIT 1
) md ON TRUE
WHERE tl.created_at >= NOW() - ($1 * INTERVAL '1 hour')
ORDER BY tl.created_at DESC
LIMIT 10`

	rrows, err := d.pool.QueryContext(ctx, recentQ, windowHours)
	if err != nil {
		return nil, fmt.Errorf("get pipeline stats recent: %w", err)
	}
	defer rrows.Close()

	for rrows.Next() {
		var rt database.RecentToken
		if err := rrows.Scan(
			&rt.TokenAddress, &rt.Symbol, &rt.Name,
			&rt.State, &rt.Chain, &rt.DetectedAt,
		); err != nil {
			return nil, fmt.Errorf("get pipeline stats recent scan: %w", err)
		}
		stats.Recent = append(stats.Recent, rt)
	}
	if err := rrows.Err(); err != nil {
		return nil, fmt.Errorf("get pipeline stats recent rows: %w", err)
	}

	return stats, nil
}

// GetRescanStats returns emission counts for the rescan worker over the given
// window, grouped by band name (extracted from the transport column prefix
// "rescan_<band_name>"). This is a concrete method on *DB — it is NOT part
// of the database.Adapter interface so callers use a local type assertion.
func (d *DB) GetRescanStats(ctx context.Context, windowHours int) (*database.RescanStats, error) {
	const q = `
SELECT transport, COUNT(*) AS cnt
FROM market_data
WHERE transport LIKE 'rescan_%'
  AND ingested_at >= NOW() - ($1 * INTERVAL '1 hour')
GROUP BY transport
ORDER BY cnt DESC`

	rows, err := d.pool.QueryContext(ctx, q, windowHours)
	if err != nil {
		return nil, fmt.Errorf("get rescan stats: %w", err)
	}
	defer rows.Close()

	stats := &database.RescanStats{
		WindowHours: windowHours,
		ByBand:      make(map[string]int64),
	}
	for rows.Next() {
		var transport string
		var cnt int64
		if err := rows.Scan(&transport, &cnt); err != nil {
			return nil, fmt.Errorf("get rescan stats scan: %w", err)
		}
		// strip "rescan_" prefix to get band name
		band := transport
		if len(transport) > 7 {
			band = transport[7:] // "rescan_15m" → "15m"
		}
		stats.ByBand[band] = cnt
		stats.TotalEmitted += cnt
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get rescan stats rows: %w", err)
	}
	return stats, nil
}

// rescanPipelineStatsCountSQL is the parameterised aggregate used by GetRescanPipelineStats.
// Anchored on distinct rescan_tokens (market_data.transport LIKE 'rescan_%').
// DQ_SKIPPED exclusions mirror pipelineStatsCountSQL — skipped tokens must not
// inflate dq_passed through validated.
var rescanPipelineStatsCountSQL = `
WITH rescan_tokens AS (
    SELECT DISTINCT token_address
    FROM market_data
    WHERE transport LIKE 'rescan_%'
      AND NULLIF(ingested_at, '')::timestamptz >= NOW() - ($1::int * INTERVAL '1 hour')
),
failed_from AS (
    SELECT DISTINCT ON (t.lifecycle_id)
           t.lifecycle_id,
           t.from_state
    FROM   token_state_transitions t
    WHERE  t.to_state = 'FAILED'
    ORDER  BY t.lifecycle_id, t.transitioned_at DESC
)
SELECT
    COUNT(*)                                                                   AS detected,
    COUNT(*) FILTER (WHERE tl.current_state NOT IN (
        'DETECTED','REJECTED','DQ_SKIPPED'))                                   AS dq_passed,
    COUNT(*) FILTER (WHERE tl.current_state NOT IN (
        'DETECTED','REJECTED','DQ_PASSED','DQ_SKIPPED'))                       AS feature_ready,
    COUNT(*) FILTER (WHERE tl.current_state NOT IN (
        'DETECTED','REJECTED','DQ_PASSED','FEATURE_READY','DQ_SKIPPED'))       AS edge_detected,
    COUNT(*) FILTER (WHERE tl.current_state NOT IN (
        'DETECTED','REJECTED','DQ_PASSED','FEATURE_READY','EDGE_DETECTED','DQ_SKIPPED')) AS validated,
    COUNT(*) FILTER (WHERE tl.current_state IN (
        'SELECTED','EXECUTED','POSITION_OPEN','POSITION_CLOSED','EVALUATED','FAILED')) AS selected,
    COUNT(*) FILTER (WHERE tl.current_state IN (
            'EXECUTED','POSITION_OPEN','POSITION_CLOSED','EVALUATED')
        OR (tl.current_state = 'FAILED'
            AND ff.from_state IN ('EXECUTED','POSITION_OPEN')))                AS executed,
    COUNT(*) FILTER (WHERE tl.current_state IN (
            'POSITION_OPEN','POSITION_CLOSED','EVALUATED')
        OR (tl.current_state = 'FAILED'
            AND ff.from_state = 'POSITION_OPEN'))                              AS position_open,
    COUNT(*) FILTER (WHERE tl.current_state IN ('POSITION_CLOSED','EVALUATED')) AS position_closed,
    COUNT(*) FILTER (WHERE tl.current_state = 'EVALUATED')                     AS evaluated,
    COUNT(*) FILTER (WHERE tl.current_state = 'REJECTED')                      AS rejected,
    COUNT(*) FILTER (WHERE tl.current_state = 'FAILED')                        AS failed,
    COUNT(*) FILTER (WHERE tl.current_state = 'FAILED'
        AND ff.from_state = 'SELECTED')                                        AS failed_at_selected,
    COUNT(*) FILTER (WHERE tl.current_state = 'FAILED'
        AND ff.from_state = 'EXECUTED')                                        AS failed_at_executed,
    COUNT(*) FILTER (WHERE tl.current_state = 'FAILED'
        AND ff.from_state = 'POSITION_OPEN')                                   AS failed_at_position_open
FROM token_lifecycle tl
INNER JOIN rescan_tokens rt ON rt.token_address = tl.token_address
LEFT JOIN failed_from ff ON ff.lifecycle_id = tl.token_lifecycle_id`

// GetRescanPipelineStats returns a pipeline funnel snapshot for tokens that
// were re-emitted via the rescan worker (market_data.transport LIKE 'rescan_%')
// within the given window. Funnel semantics match GetPipelineStats: counts are
// cumulative relative to the DETECTED base. DQ_SKIPPED tokens are excluded from
// dq_passed through validated — they are terminal DQ-only outcomes, not downstream progress.
//
// This is a concrete method on *DB — it is NOT part of the database.Adapter
// interface. Callers type-assert to rescanPipelineQueryer (defined in cmd/).
func (d *DB) GetRescanPipelineStats(ctx context.Context, windowHours int) (*database.RescanPipelineStats, error) {
	stats := &database.RescanPipelineStats{
		WindowHours: windowHours,
		ByBand:      make(map[string]int64),
	}

	row := d.pool.QueryRowContext(ctx, rescanPipelineStatsCountSQL, windowHours)
	if err := row.Scan(
		&stats.Detected,
		&stats.DQPassed,
		&stats.FeatureReady,
		&stats.EdgeDetected,
		&stats.Validated,
		&stats.Selected,
		&stats.Executed,
		&stats.PositionOpen,
		&stats.PositionClosed,
		&stats.Evaluated,
		&stats.Rejected,
		&stats.Failed,
		&stats.FailedAtSelected,
		&stats.FailedAtExecuted,
		&stats.FailedAtPositionOpen,
	); err != nil {
		return nil, fmt.Errorf("get rescan pipeline stats counts: %w", err)
	}

	// ── Per-band emission counts ───────────────────────────────────────────────
	// Count distinct tokens per band (not raw emission rows) so the numbers
	// are comparable with the funnel counts above.
	const bandQ = `
SELECT transport, COUNT(DISTINCT token_address) AS cnt
FROM market_data
WHERE transport LIKE 'rescan_%'
  AND NULLIF(ingested_at, '')::timestamptz >= NOW() - ($1::int * INTERVAL '1 hour')
GROUP BY transport
ORDER BY transport`

	brows, err := d.pool.QueryContext(ctx, bandQ, windowHours)
	if err != nil {
		return nil, fmt.Errorf("get rescan pipeline band counts: %w", err)
	}
	defer brows.Close()

	for brows.Next() {
		var transport string
		var cnt int64
		if err := brows.Scan(&transport, &cnt); err != nil {
			return nil, fmt.Errorf("get rescan pipeline band scan: %w", err)
		}
		band := transport
		if len(transport) > 7 {
			band = transport[7:] // "rescan_15m" → "15m"
		}
		stats.ByBand[band] = cnt
		stats.TotalEmitted += cnt
	}
	if err := brows.Err(); err != nil {
		return nil, fmt.Errorf("get rescan pipeline band rows: %w", err)
	}

	// ── Recent rescanned tokens (last 10, newest rescan emission first) ────────
	const recentQ = `
SELECT sub.token_address,
       COALESCE(sub.symbol, '')           AS symbol,
       COALESCE(sub.name,   '')           AS name,
       COALESCE(tl.current_state, '')     AS current_state,
       COALESCE(sub.chain,  '')           AS chain,
       sub.ingested_at
FROM (
    SELECT DISTINCT ON (token_address)
           token_address, symbol, name, chain, ingested_at
    FROM   market_data
    WHERE  transport LIKE 'rescan_%'
      AND  NULLIF(ingested_at, '')::timestamptz >= NOW() - ($1::int * INTERVAL '1 hour')
    ORDER  BY token_address, ingested_at DESC
) sub
LEFT JOIN token_lifecycle tl ON tl.token_address = sub.token_address
ORDER BY sub.ingested_at DESC
LIMIT 10`

	rrows, err := d.pool.QueryContext(ctx, recentQ, windowHours)
	if err != nil {
		return nil, fmt.Errorf("get rescan pipeline recent: %w", err)
	}
	defer rrows.Close()

	for rrows.Next() {
		var rt database.RecentToken
		if err := rrows.Scan(
			&rt.TokenAddress, &rt.Symbol, &rt.Name,
			&rt.State, &rt.Chain, &rt.DetectedAt,
		); err != nil {
			return nil, fmt.Errorf("get rescan pipeline recent scan: %w", err)
		}
		stats.Recent = append(stats.Recent, rt)
	}
	if err := rrows.Err(); err != nil {
		return nil, fmt.Errorf("get rescan pipeline recent rows: %w", err)
	}

	return stats, nil
}

// GetExecutionLog returns the last `limit` tokens that reached SELECTED or
// further in the pipeline (including those that FAILED). Results are ordered
// by lifecycle.updated_at DESC so the most recent activity appears first.
// Full token address, symbol, chain, lifecycle state, execution status, error
// code, and tx hash are returned.
func (d *DB) GetExecutionLog(ctx context.Context, limit int) ([]database.ExecutionLogRow, error) {
	if limit <= 0 {
		limit = 20
	}
	const q = `
SELECT
    tl.token_address,
    COALESCE(md.symbol,       '') AS symbol,
    COALESCE(md.chain,        '') AS chain,
    tl.current_state,
    COALESCE(er.status,       '') AS exec_status,
    -- er.error_code and tl.terminal_reason are TEXT NOT NULL DEFAULT ''.
    -- COALESCE only treats NULL as missing, so we wrap each TEXT field in
    -- NULLIF(.., '') to make an empty string fall through to the next source.
    COALESCE(NULLIF(er.error_code, ''), NULLIF(tl.terminal_reason, ''), '') AS error_code,
    COALESCE(er.tx_hash,      '') AS tx_hash,
    tl.updated_at
FROM token_lifecycle tl
LEFT JOIN LATERAL (
    SELECT symbol, name, chain
    FROM   market_data
    WHERE  token_address = tl.token_address
    ORDER  BY block_number DESC
    LIMIT  1
) md ON TRUE
LEFT JOIN LATERAL (
    SELECT status, error_code, tx_hash
    FROM   execution_results
    WHERE  token_lifecycle_id = tl.token_lifecycle_id
    ORDER  BY completed_at DESC
    LIMIT  1
) er ON TRUE
WHERE tl.current_state IN (
    'SELECTED','EXECUTED','POSITION_OPEN','POSITION_CLOSED','EVALUATED','FAILED'
)
ORDER BY tl.updated_at DESC
LIMIT $1`

	rows, err := d.pool.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, fmt.Errorf("get execution log: %w", err)
	}
	defer rows.Close()

	var out []database.ExecutionLogRow
	for rows.Next() {
		var r database.ExecutionLogRow
		var updatedAt time.Time
		if err := rows.Scan(
			&r.TokenAddress, &r.Symbol, &r.Chain,
			&r.LifecycleState, &r.Status, &r.ErrorCode,
			&r.TxHash, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("get execution log scan: %w", err)
		}
		r.UpdatedAt = updatedAt.UTC().Format(time.RFC3339Nano)
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("get execution log rows: %w", err)
	}
	return out, nil
}
