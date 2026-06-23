---
name: replay-engine-pattern
type: skill
description: >
  Deterministic event replay via prefix isolation in the PostgreSQL event bus.
  Use when implementing backtesting, strategy validation, or any system that
  needs to re-process historical events without contaminating production state.
  Replay events are DB-isolated via the `replay:` prefix — production workers
  never see replay events; replay workers never see production events.
---

# Replay Engine Pattern Skill

## Purpose

Enable deterministic backtesting and strategy validation by replaying historical
events through the same pipeline code that runs in production. The key constraint
is **complete isolation** — replay must not affect live trading state, and replay
results must be **bit-for-bit reproducible** (same replay input → identical output).

**Determinism requirement (from `docs/reference/architecture.md` § 6):**

- All timestamps come from historical event data, never from `time.Now()`
- No random values — same input always produces identical output
- Replay results compared to production results for parity validation

---

## Rules

### Prefix Convention

```go
// ALL replay events use the "replay:" prefix on BOTH event_id and event_type.
// This is the ONLY mechanism for DB-level isolation.
const ReplayPrefix = "replay:"

func IsReplayEvent(eventType string) bool {
    return strings.HasPrefix(eventType, ReplayPrefix)
}

func IsProductionEvent(eventType string) bool {
    return !IsReplayEvent(eventType)
}
```

### Creating Replay Events

```go
type ReplayCreateRequest struct {
    OriginalEvent  database.Event
    ReplayRunID    string  // SHA256(replay_params)[:16]
    HistoricTimestamp string // ISO 8601 — NEVER time.Now()
}

// CreateReplayEvent wraps an original event in the replay namespace.
// The replay event_id preserves the chain: replay:<run_id>:<orig_event_id>
func CreateReplayEvent(req ReplayCreateRequest) database.Event {
    return database.Event{
        EventID:       ReplayPrefix + req.ReplayRunID + ":" + req.OriginalEvent.EventID,
        EventType:     ReplayPrefix + req.OriginalEvent.EventType,
        Payload:       req.OriginalEvent.Payload,
        TraceID:       req.OriginalEvent.TraceID,
        CorrelationID: req.OriginalEvent.CorrelationID,
        CausationID:   req.OriginalEvent.EventID,  // causation = the original event
        VersionID:     req.OriginalEvent.VersionID,
        CreatedAt:     req.HistoricTimestamp,       // HISTORICAL timestamp — not wall-clock
        Processed:     false,
    }
}
```

### DB-Level SQL Isolation

```go
// Production workers use this WHERE clause on every ClaimNextEvent call.
// The adapter enforces this — modules never write their own SQL.

// Production worker SQL (applied by adapter.ClaimNextEvent):
//   WHERE event_type NOT LIKE 'replay:%'
//   AND processed = FALSE

// Replay worker SQL (applied by adapter.ClaimNextReplayEvent):
//   WHERE event_type LIKE 'replay:%'
//   AND event_type LIKE 'replay:<run_id>:%'   -- scoped to this run
//   AND processed = FALSE
```

### Replay Worker Loop Pattern

```go
// Replay workers are identical to production workers in logic,
// but they call ClaimNextReplayEvent instead of ClaimNextEvent.
// The orchestrator spins up replay workers with the replayRunID scoped.

func RunReplayWorker(
    ctx       context.Context,
    adapter   database.Adapter,
    runID     string,
    mod       SomeModule,
    cfg       Config,
) error {
    group := "replay_" + runID + "_data_quality_worker"

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }

        events, err := adapter.ClaimNextReplayEvents(ctx, group, runID,
            []string{ReplayPrefix + "market_data_event"}, cfg.BatchSize)
        if err != nil {
            return err
        }
        if len(events) == 0 {
            // Check if replay run is complete
            remaining, _ := adapter.CountPendingReplayEvents(ctx, runID)
            if remaining == 0 {
                return nil  // replay complete
            }
            time.Sleep(50 * time.Millisecond)
            continue
        }

        for _, ev := range events {
            dto := unmarshalMarketDataDTO(ev.Payload)
            // Use ev.CreatedAt as the "current time" — NOT time.Now()
            result, err := mod.Process(ctx, dto, parseTimestamp(ev.CreatedAt))
            if err != nil {
                adapter.NackReplayEvent(ctx, ev.EventID, err.Error())
                continue
            }
            replayOut := CreateReplayEvent(ReplayCreateRequest{
                OriginalEvent:     makeOutputEvent(result),
                ReplayRunID:       runID,
                HistoricTimestamp: ev.CreatedAt,
            })
            adapter.EmitReplayEvent(ctx, replayOut)
            adapter.AckReplayEvent(ctx, ev.EventID)
        }
    }
}
```

### Parity Check (Replay vs Production)

```go
// After replay completes, compare outcomes to production for the same inputs.
type ReplayComparisonResult struct {
    ReplayRunID    string
    MatchRate      float64  // [0,1]: 1.0 = identical
    MismatchCount  int
    IsDeterministic bool   // true if same replay run produces identical output
    Mismatches     []ReplayMismatch
}

type ReplayMismatch struct {
    EventID         string
    ProductionScore float64
    ReplayScore     float64
    Delta           float64
}

func CompareReplayToProduction(
    replayResults     []ScoredEvent,
    productionResults []ScoredEvent,
) ReplayComparisonResult {
    if len(replayResults) == 0 {
        return ReplayComparisonResult{}
    }

    prodMap := make(map[string]float64)
    for _, r := range productionResults {
        prodMap[r.EventID] = r.Score
    }

    var mismatches []ReplayMismatch
    for _, r := range replayResults {
        // Strip replay prefix for lookup
        origID := extractOriginalEventID(r.EventID)
        prodScore, ok := prodMap[origID]
        if !ok { continue }
        if math.Abs(r.Score-prodScore) > 1e-9 {
            mismatches = append(mismatches, ReplayMismatch{
                EventID:         origID,
                ProductionScore: prodScore,
                ReplayScore:     r.Score,
                Delta:           math.Abs(r.Score - prodScore),
            })
        }
    }

    matchRate := 1.0 - float64(len(mismatches))/float64(len(replayResults))
    return ReplayComparisonResult{
        MatchRate:       matchRate,
        MismatchCount:   len(mismatches),
        IsDeterministic: len(mismatches) == 0,
        Mismatches:      mismatches,
    }
}
```

---

## Anti-Patterns

```
❌ Using time.Now() inside any module during replay (destroys determinism)
❌ Sharing consumer_offsets groups between production and replay workers
❌ Running replay against a live production database without prefix isolation
❌ Storing replay output in the same state tables as production
❌ Auto-deploying a strategy based on replay results alone (replay ≠ forward test)
❌ Not comparing replay results to production parity (can't detect non-determinism)
```

---

## Checklist

- [ ] All replay events use `replay:` prefix on both `event_id` and `event_type`
- [ ] Replay workers use `ClaimNextReplayEvent` (not `ClaimNextEvent`)
- [ ] Timestamps come from historical event data, NEVER `time.Now()`
- [ ] Consumer offset groups are scoped to `replay_<run_id>_<worker>`
- [ ] Replay state lives in `events` table (prefix-isolated), not separate tables
- [ ] Parity check runs after replay to verify determinism
- [ ] Replay run can be deleted cleanly via `DELETE WHERE event_id LIKE 'replay:<run_id>:%'`

---

## References

- `docs/reference/architecture.md` § 6 — System Guarantees (determinism, idempotency)
- `docs/reference/architecture.md` § 4.1 — Strategy Versioning & Replay
- `.github/skills/event-bus/SKILL.md` — PostgreSQL event bus patterns
- `.github/skills/determinism/SKILL.md` — No-randomness enforcement
- `.github/skills/strategy-versioning/SKILL.md` — Version replay determinism
- `shared/database/adapter.go` — `ClaimNextEvent`, `Adapter` interface
