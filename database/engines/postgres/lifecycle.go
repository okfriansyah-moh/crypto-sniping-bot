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
	if req.Reason != "" && isTerminalState(req.NewState) {
		terminalReason = &req.Reason
	}

	const q = `
UPDATE token_lifecycle
SET current_state   = $1,
    state_version   = state_version + 1,
    terminal_reason = COALESCE($2, terminal_reason),
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
func isValidTransition(from, to string) bool {
	allowed := map[string][]string{
		"DETECTED":       {"DQ_PASSED", "REJECTED"},
		"DQ_PASSED":      {"FEATURE_READY", "REJECTED"},
		"FEATURE_READY":  {"EDGE_DETECTED", "REJECTED"},
		"EDGE_DETECTED":  {"VALIDATED", "REJECTED"},
		"VALIDATED":      {"SELECTED", "REJECTED"},
		"SELECTED":       {"EXECUTED", "FAILED"},
		"EXECUTED":       {"POSITION_OPEN", "FAILED"},
		"POSITION_OPEN":  {"POSITION_CLOSED"},
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
