package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"crypto-sniping-bot/database"
)

// InsertEvent appends an event to the event bus.
// Idempotent: ON CONFLICT (event_id) DO NOTHING.
// Validates trace_id, correlation_id, and version_id.
// Returns ErrMissingTraceField if required trace fields are absent.
func (d *DB) InsertEvent(ctx context.Context, evt database.Event) error {
	if evt.TraceID == "" || evt.CorrelationID == "" || evt.VersionID == "" {
		return database.ErrMissingTraceField
	}

	const q = `
		INSERT INTO events
		    (event_id, event_type, payload, trace_id, correlation_id, causation_id, version_id, created_at, processed)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, FALSE)
		ON CONFLICT (event_id) DO NOTHING`

	createdAt := evt.CreatedAt
	if createdAt == "" {
		createdAt = time.Now().UTC().Format(time.RFC3339Nano)
	}

	_, err := d.pool.ExecContext(ctx, q,
		evt.EventID,
		evt.EventType,
		evt.Payload,
		evt.TraceID,
		evt.CorrelationID,
		evt.CausationID,
		evt.VersionID,
		createdAt,
	)
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

// ClaimNextEvent atomically claims the next unprocessed event for a worker group
// using SELECT ... FOR UPDATE SKIP LOCKED.
// Returns nil if the queue is empty.
func (d *DB) ClaimNextEvent(ctx context.Context, group string, eventTypes []string) (*database.Event, error) {
	if len(eventTypes) == 0 {
		return nil, nil
	}

	// Build parameterized ANY($n) array.
	const q = `
		SELECT event_id, event_type, payload, trace_id, correlation_id, causation_id, version_id, created_at, processed
		FROM events
		WHERE processed = FALSE
		  AND event_type = ANY($1)
		ORDER BY created_at
		FOR UPDATE SKIP LOCKED
		LIMIT 1`

	row := d.pool.QueryRowContext(ctx, q, eventTypes)
	evt, err := scanEvent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("claim next event: %w", err)
	}
	return evt, nil
}

// MarkEventProcessed marks an event as handled.
func (d *DB) MarkEventProcessed(ctx context.Context, eventID string) error {
	const q = `UPDATE events SET processed = TRUE WHERE event_id = $1`
	_, err := d.pool.ExecContext(ctx, q, eventID)
	if err != nil {
		return fmt.Errorf("mark event processed: %w", err)
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
