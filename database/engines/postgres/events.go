package postgres

import (
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"crypto-sniping-bot/database"
)

// InsertEvent appends an event to the event bus.
// Idempotent: ON CONFLICT (event_id) DO NOTHING.
// Validates trace_id, correlation_id, and version_id.
// Returns ErrMissingTraceField if required trace fields are absent.
//
// Phase 8 routing fields (chain, consumer, logical_order_key, partition_key,
// block_number) are auto-populated when not set by the caller:
//   - logical_order_key: big-endian nanosecond timestamp (deterministic ordering)
//   - partition_key: HASHTEXT(correlation_id) % 256 (deterministic sharding)
//   - chain/consumer: empty string default (legacy events; set explicitly for Phase 8 workers)
func (d *DB) InsertEvent(ctx context.Context, evt database.Event) error {
	if evt.TraceID == "" || evt.CorrelationID == "" || evt.VersionID == "" {
		return database.ErrMissingTraceField
	}

	createdAt := evt.CreatedAt
	if createdAt == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339Nano)
	}

	// Auto-compute logical_order_key from nanosecond timestamp for deterministic ordering.
	logicalOrderKey := evt.LogicalOrderKey
	if len(logicalOrderKey) == 0 {
		t, parseErr := time.Parse(time.RFC3339Nano, createdAt)
		if parseErr != nil {
			t = time.Now().UTC()
		}
		logicalOrderKey = make([]byte, 8)
		binary.BigEndian.PutUint64(logicalOrderKey, uint64(t.UnixNano()))
	}

	// Auto-compute partition_key as a simple hash of correlation_id mod 256.
	partitionKey := evt.PartitionKey
	if partitionKey == 0 && evt.CorrelationID != "" {
		var h int
		for _, c := range evt.CorrelationID {
			h = h*31 + int(c)
		}
		if h < 0 {
			h = -h
		}
		partitionKey = h % 256
	}

	const q = `
		INSERT INTO events
		    (event_id, event_type, payload, trace_id, correlation_id, causation_id, version_id,
		     created_at, processed, chain, consumer, logical_order_key, partition_key, block_number)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, FALSE, $9, $10, $11, $12, $13)
		ON CONFLICT (event_id) DO NOTHING`

	_, err := d.pool.ExecContext(ctx, q,
		evt.EventID,
		evt.EventType,
		evt.Payload,
		evt.TraceID,
		evt.CorrelationID,
		evt.CausationID,
		evt.VersionID,
		createdAt,
		evt.Chain,
		evt.Consumer,
		logicalOrderKey,
		partitionKey,
		evt.BlockNumber,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23503" {
			return database.ErrOrphanEvent
		}
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

// ClaimNextEvent atomically claims the next unprocessed event for a worker group.
//
// Uses UPDATE ... RETURNING to atomically set claimed_at in a single statement,
// preventing the race condition that a bare SELECT ... FOR UPDATE SKIP LOCKED
// in autocommit mode creates (the row lock would be released before
// MarkEventProcessed is called, allowing two workers to process the same event).
//
// A stale-claim guard reclaims events whose claimed_at exceeded
// ClaimTimeoutSecs from Config (default 300 s), recovering from crashed workers.
//
// Ordering: priority DESC, created_at ASC (higher-priority events served first).
// Excludes events past their expires_at TTL.
//
// Note: group is passed for future consumer_offsets tracking (per-group progress
// isolation). The current implementation uses a single processed=TRUE flag, which
// is correct for a strictly linear pipeline (no fan-out). Fan-out support will
// require per-group offset rows and must not set processed=TRUE globally.
func (d *DB) ClaimNextEvent(ctx context.Context, group string, eventTypes []string) (*database.Event, error) {
	if len(eventTypes) == 0 {
		return nil, nil
	}

	claimTimeout := d.cfg.ClaimTimeoutSecs
	if claimTimeout <= 0 {
		claimTimeout = 300
	}

	// Atomic claim: UPDATE sets claimed_at, RETURNING delivers the row.
	// The subquery uses FOR UPDATE SKIP LOCKED so concurrent workers skip
	// a row that is already being updated by a peer.
	// Stale claims older than claimTimeout are also eligible.
	// TTL filter: exclude events whose expires_at has elapsed.
	const q = `
		UPDATE events
		SET claimed_at = CURRENT_TIMESTAMP
		WHERE event_id = (
		    SELECT event_id FROM events
		    WHERE processed = FALSE
		      AND event_type = ANY($1)
		      AND (claimed_at IS NULL
		           OR claimed_at < CURRENT_TIMESTAMP - ($2 * INTERVAL '1 second'))
		      AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
		    ORDER BY priority DESC, created_at ASC
		    FOR UPDATE SKIP LOCKED
		    LIMIT 1
		)
		RETURNING event_id, event_type, payload, trace_id, correlation_id, causation_id, version_id, created_at, processed`

	row := d.pool.QueryRowContext(ctx, q, eventTypes, claimTimeout)
	evt, err := scanEvent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim next event: %w", err)
	}
	return evt, nil
}

// MarkEventProcessed marks an event as handled and clears its claim.
func (d *DB) MarkEventProcessed(ctx context.Context, eventID string) error {
	const q = `UPDATE events SET processed = TRUE, claimed_at = NULL WHERE event_id = $1`
	_, err := d.pool.ExecContext(ctx, q, eventID)
	if err != nil {
		return fmt.Errorf("mark event processed: %w", err)
	}
	return nil
}

// ReleaseEventClaim clears claimed_at so the event is immediately eligible
// for re-claiming by the next worker. Call on stage handler failure to bypass
// the stale-claim timeout window.
func (d *DB) ReleaseEventClaim(ctx context.Context, eventID string) error {
	const q = `UPDATE events SET claimed_at = NULL WHERE event_id = $1 AND processed = FALSE`
	_, err := d.pool.ExecContext(ctx, q, eventID)
	if err != nil {
		return fmt.Errorf("release event claim: %w", err)
	}
	return nil
}

// GetEventByID fetches a specific event by ID.
func (d *DB) GetEventByID(ctx context.Context, eventID string) (*database.Event, error) {
	const q = `
		SELECT event_id, event_type, payload, trace_id, correlation_id, causation_id, version_id, created_at, processed
		FROM events
		WHERE event_id = $1`

	row := d.pool.QueryRowContext(ctx, q, eventID)
	evt, err := scanEvent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, database.ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get event by id: %w", err)
	}
	return evt, nil
}

// GetEventsByTrace returns all events for a trace ID, ordered by created_at.
func (d *DB) GetEventsByTrace(ctx context.Context, traceID string) ([]database.Event, error) {
	const q = `
		SELECT event_id, event_type, payload, trace_id, correlation_id, causation_id, version_id, created_at, processed
		FROM events
		WHERE trace_id = $1
		ORDER BY created_at`

	return d.queryEvents(ctx, q, traceID)
}

// GetEventsByCorrelation returns all events for a correlation ID.
func (d *DB) GetEventsByCorrelation(ctx context.Context, correlationID string) ([]database.Event, error) {
	const q = `
		SELECT event_id, event_type, payload, trace_id, correlation_id, causation_id, version_id, created_at, processed
		FROM events
		WHERE correlation_id = $1
		ORDER BY created_at`

	return d.queryEvents(ctx, q, correlationID)
}

// GetFailureChain reconstructs the causal chain leading to a failed event.
func (d *DB) GetFailureChain(ctx context.Context, failedEventID string) ([]database.Event, error) {
	// Walk the causation chain backwards from the failed event.
	var chain []database.Event
	current := failedEventID
	seen := make(map[string]bool)

	for current != "" && !seen[current] {
		seen[current] = true
		evt, err := d.GetEventByID(ctx, current)
		if err != nil {
			break
		}
		chain = append(chain, *evt)
		if evt.CausationID == nil {
			break
		}
		current = *evt.CausationID
	}

	// Reverse to get chronological order.
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain, nil
}

func (d *DB) queryEvents(ctx context.Context, q string, args ...any) ([]database.Event, error) {
	rows, err := d.pool.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("query events: %w", err)
	}
	defer rows.Close()

	var events []database.Event
	for rows.Next() {
		evt, err := scanEventRow(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, *evt)
	}
	return events, rows.Err()
}

// scanEvent scans a *sql.Row into an Event.
func scanEvent(row *sql.Row) (*database.Event, error) {
	var evt database.Event
	err := row.Scan(
		&evt.EventID,
		&evt.EventType,
		&evt.Payload,
		&evt.TraceID,
		&evt.CorrelationID,
		&evt.CausationID,
		&evt.VersionID,
		&evt.CreatedAt,
		&evt.Processed,
	)
	if err != nil {
		return nil, err
	}
	return &evt, nil
}

// scanEventRow scans a *sql.Rows into an Event.
func scanEventRow(rows *sql.Rows) (*database.Event, error) {
	var evt database.Event
	err := rows.Scan(
		&evt.EventID,
		&evt.EventType,
		&evt.Payload,
		&evt.TraceID,
		&evt.CorrelationID,
		&evt.CausationID,
		&evt.VersionID,
		&evt.CreatedAt,
		&evt.Processed,
	)
	if err != nil {
		return nil, fmt.Errorf("scan event: %w", err)
	}
	return &evt, nil
}
