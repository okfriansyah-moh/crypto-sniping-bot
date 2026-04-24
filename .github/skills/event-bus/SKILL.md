---
name: event-bus
type: skill
description: >
  PostgreSQL append-only event bus patterns. Use when implementing or reviewing
  event emission, worker consumption (SELECT FOR UPDATE SKIP LOCKED), consumer_offsets
  tracking, event routing, and replay logic. This is the system backbone — every
  state transition flows through it. Wrong implementations cause duplicate processing
  or irreproducible state.
---

# Event Bus Skill

## Purpose

Enforce correct implementation of the PostgreSQL append-only event bus that serves
as the authoritative log of all DTO transitions. Full system state is reconstructible
from this log alone.

**Core rule:** Producers INSERT. Consumers SELECT with SKIP LOCKED. Nobody updates or
deletes events.

---

## Rules

### Events Table (Canonical Schema)

```sql
-- database/migrations/20240001000001_create_events.sql
CREATE TABLE IF NOT EXISTS events (
    id          BIGSERIAL PRIMARY KEY,
    event_id    TEXT UNIQUE NOT NULL,          -- SHA256(event_type+payload_hash+created_at)[:16]
    event_type  TEXT NOT NULL,
    market      TEXT NOT NULL,                 -- "eth-uniswap-v2", "bsc-pancake-v2"
    payload     JSONB NOT NULL,
    trace_id    TEXT NOT NULL,
    correlation_id TEXT NOT NULL,
    causation_id   TEXT NOT NULL,              -- "" only for market_data_event (Layer 0)
    version_id     TEXT NOT NULL,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Consumer progress tracking (one row per consumer group)
CREATE TABLE IF NOT EXISTS consumer_offsets (
    consumer_group TEXT NOT NULL,
    last_event_id  BIGINT NOT NULL DEFAULT 0,
    updated_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (consumer_group)
);

CREATE INDEX idx_events_type_id ON events (event_type, id);
CREATE INDEX idx_events_market_id ON events (market, id);
```

### Canonical Event Types

```go
const (
    EventTypeMarketData    = "market_data_event"   // Layer 0: ingestion
    EventTypeDataQuality   = "data_quality_event"  // Layer 1: DQ result
    EventTypeFeature       = "feature_event"       // Layer 2: extracted features
    EventTypeEdge          = "edge_event"          // Layer 3: edge signal
    EventTypeProbability   = "probability_event"   // Layer 4: P/S/L estimates
    EventTypeValidation    = "validation_event"    // Layer 5: edge validation
    EventTypeSelection     = "selection_event"     // Layer 6: selection result
    EventTypeAllocation    = "allocation_event"    // Layer 7: capital allocation
    EventTypeExecution     = "execution_event"     // Layer 8: execution result
    EventTypePosition      = "position_event"      // Layer 9: position state
    EventTypeEvaluation    = "evaluation_event"    // Layer 10: learning input
    EventTypeAdjustment    = "adjustment_event"    // Layer 10: param update
    EventTypeTelegram      = "telegram_event"      // Telegram dispatcher input
    EventTypeSystem        = "system_event"        // Operational/mode changes
)
```

### Event Emission (Producer Pattern)

```go
// Only the orchestrator emits events — modules return DTOs, orchestrator emits
func emitEvent(
    ctx context.Context,
    adapter database.Adapter,
    eventType string,
    market string,
    payload interface{},
    parentEventID string,  // CausationID
    traceID string,
    correlationID string,
    versionID string,
) error {
    payloadJSON, err := json.Marshal(payload)
    if err != nil {
        return fmt.Errorf("marshal event payload: %w", err)
    }

    // EventID is content-addressable
    eventID := sha256content(eventType + string(payloadJSON) + correlationID)[:16]

    _, err = adapter.InsertEvent(ctx, database.Event{
        EventID:       eventID,
        EventType:     eventType,
        Market:        market,
        Payload:       payloadJSON,
        TraceID:       traceID,
        CorrelationID: correlationID,
        CausationID:   parentEventID,  // "" only for market_data_event
        VersionID:     versionID,
    })
    return err
}
```

### Consumer Worker Pattern (SELECT FOR UPDATE SKIP LOCKED)

```go
// Generic worker loop — every stage worker uses this pattern
func workerLoop(
    ctx context.Context,
    adapter database.Adapter,
    consumerGroup string,
    eventType string,
    processFn func(context.Context, database.Event) (interface{}, string, error),
    nextEventType string,
    cfg WorkerConfig,
) {
    for {
        select {
        case <-ctx.Done():
            return
        default:
        }

        // Claim next event atomically — SKIP LOCKED prevents duplicate processing
        event, err := adapter.ClaimNextEvent(ctx, consumerGroup, eventType)
        if err != nil {
            if errors.Is(err, database.ErrNoEvents) {
                time.Sleep(time.Duration(cfg.PollIntervalMs) * time.Millisecond)
                continue
            }
            logger.Error("claim event failed", "consumer", consumerGroup, "err", err)
            time.Sleep(time.Duration(cfg.ErrorDelayMs) * time.Millisecond)
            continue
        }

        // Process
        result, emitType, err := processFn(ctx, event)
        if err != nil {
            logger.Error("process event failed", "event_id", event.EventID, "err", err)
            adapter.MarkEventFailed(ctx, consumerGroup, event.ID, err.Error())
            continue
        }

        // Emit next event (only if there's a result — some stages may filter)
        if result != nil {
            if emitErr := emitEvent(ctx, adapter, emitType, event.Market,
                result, event.EventID, event.TraceID, event.CorrelationID, event.VersionID,
            ); emitErr != nil {
                logger.Error("emit event failed", "err", emitErr)
                // Do NOT mark processed — retry on next poll
                continue
            }
        }

        // Mark processed ONLY after successful emission (or intentional filter)
        adapter.MarkEventProcessed(ctx, consumerGroup, event.ID)
    }
}
```

### consumer_offsets Pattern (Alternative — Offset-Based)

```go
// Alternative: offset-based consumption for ordered processing
func claimByOffset(ctx context.Context, group string, eventType string, batchSize int) ([]Event, error) {
    // Adapter SQL:
    // SELECT e.* FROM events e
    // LEFT JOIN consumer_offsets co ON co.consumer_group = $1
    // WHERE e.event_type = $2
    //   AND e.id > COALESCE(co.last_event_id, 0)
    // ORDER BY e.id
    // LIMIT $3
    // FOR UPDATE SKIP LOCKED
}

func updateOffset(ctx context.Context, group string, lastID int64) error {
    // INSERT INTO consumer_offsets(consumer_group, last_event_id)
    // VALUES ($1, $2)
    // ON CONFLICT (consumer_group) DO UPDATE SET last_event_id = $2
}
```

### Replay Guarantee

```go
// Full state is reconstructible from events — replay steps:
// 1. Get all events for market in order (ORDER BY id ASC)
// 2. Re-run each module's pure function with event payload
// 3. Compare derived state against stored state
// For replay: use event.CreatedAt, never time.Now()

func replayMarket(ctx context.Context, adapter database.Adapter, market string, fromID int64) {
    events, _ := adapter.GetEventsSince(ctx, market, fromID)
    for _, event := range events {
        // Re-derive state from event using same module logic
        // NOTE: use event timestamps — deterministic
    }
}
```

### Event Routing Table

| Event Emitted By    | Consumed By Worker  | Emits Next           |
| ------------------- | ------------------- | -------------------- |
| ingestion (Layer 0) | data_quality_worker | data_quality_event   |
| data_quality_worker | feature_worker      | feature_event        |
| feature_worker      | edge_worker         | edge_event           |
| edge_worker         | probability_worker  | probability_event    |
| probability_worker  | validation_worker   | validation_event     |
| validation_worker   | selection_worker    | selection_event      |
| selection_worker    | capital_worker      | allocation_event     |
| capital_worker      | execution_worker    | execution_event      |
| execution_worker    | position_worker     | position_event       |
| position_worker     | evaluation_worker   | evaluation_event     |
| evaluation_worker   | learning_worker     | adjustment_event     |
| any worker          | telegram_dispatcher | (sends Telegram msg) |

### Anti-Patterns

```go
// ❌ Updating events
UPDATE events SET processed = true  // Wrong — events table is append-only

// ❌ Deleting events
DELETE FROM events WHERE created_at < $1  // FORBIDDEN — breaks replay

// ❌ Module directly emits to event bus
func (m *EdgeModule) Process(dto EdgeInputDTO) {
    adapter.InsertEvent(...)  // FORBIDDEN — only orchestrator emits
}

// ❌ Polling without SKIP LOCKED
SELECT * FROM events WHERE event_type = $1 AND processed = false LIMIT 1
// Wrong — causes duplicate processing across workers

// ❌ No consumer_offsets tracking
// Missing offset → reprocess all events on restart

// ✅ Correct
SELECT e.* FROM events e
WHERE e.event_type = $1
  AND e.id > $2  -- consumer offset
ORDER BY e.id
LIMIT 1
FOR UPDATE SKIP LOCKED
```

---

## Checklist

```
[ ] events table has event_id (content-addressable), trace_id, correlation_id, causation_id, version_id
[ ] consumer_offsets table tracks per-group progress
[ ] All consumption uses SELECT ... FOR UPDATE SKIP LOCKED
[ ] Events are never updated or deleted — append-only
[ ] Only the orchestrator emits events — modules return DTOs
[ ] EventID = SHA256(type + payload_hash + correlation_id)[:16]
[ ] CausationID = "" only for market_data_event (Layer 0)
[ ] Every event carries: trace_id, correlation_id, causation_id, version_id
[ ] MarkEventProcessed called ONLY after successful next-event emission
[ ] Replay tested: same events → same derived state
[ ] Worker loop handles ErrNoEvents with sleep (not error log spam)
[ ] Index on (event_type, id) for efficient consumption queries
```

---

## References

- Architecture: `docs/architecture.md` § 2.2–2.3 (Event Bus, Worker Loop)
- Architecture context: `docs/architecture-context/2_system_backbone.md`
- DB spec: `docs/db_adapter_spec.md` § 6.1 (events table)
- Roadmap: `docs/implementation_roadmap.md` Phase 0 §0.6
- Config: `config/pipeline.yaml` → `worker.poll_interval_ms`, `worker.error_delay_ms`
