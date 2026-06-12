# Traceability Skill

## Purpose

Enforce the four-field traceability contract that makes every trade's decision chain
auditable, replayable, and attributable to a specific strategy version.

**Contract:** `TraceID | CorrelationID | CausationID | VersionID` — all four fields,
always, on every DTO that crosses a module boundary.

---

## Rules

### Four-Field Contract (Definitions)

```go
// Every cross-boundary DTO carries all four fields
type TraceFields struct {
    TraceID       string  // Unique ID for this pipeline run (stays same through all layers)
    CorrelationID string  // Same as TraceID unless this is a sub-operation
    CausationID   string  // EventID of the parent event that caused this DTO
                         // "" ONLY in Layer 0 (market_data_event — no parent)
    VersionID     string  // Active StrategyVersion ID at the time of this decision
}
```

### Assignment Rules Per Layer

```
Layer 0 (Ingestion):
  TraceID       = SHA256(chain + tx_hash + log_index)[:16]   (content-addressable)
  CorrelationID = TraceID
  CausationID   = ""   ← ONLY layer that sets this to ""
  VersionID     = activeVersion.VersionID

Layer 1-9 (All subsequent layers):
  TraceID       = COPY from incoming DTO (never modify)
  CorrelationID = COPY from incoming DTO (never modify)
  CausationID   = EventID of the event that triggered this processing
  VersionID     = COPY from incoming DTO (same version for entire trace)
```

### Propagation in Go (Correct Pattern)

```go
// Layer 0: Ingestion — assign all four fields
marketData := MarketDataDTO{
    EventID:       sha256content(chain + txHash + logIndex)[:16],
    TokenAddress:  tokenAddress,
    // ...
    TraceID:       sha256content(chain + txHash + logIndex)[:16],
    CorrelationID: sha256content(chain + txHash + logIndex)[:16],
    CausationID:   "",  // Layer 0 only
    VersionID:     activeVersion.VersionID,
}

// Layer 1-N: Data Quality — copy trace fields, set CausationID
dataQualityDTO := DataQualityDTO{
    TokenAddress:  marketData.TokenAddress,
    // ...
    TraceID:       marketData.TraceID,       // COPY — never generate new
    CorrelationID: marketData.CorrelationID, // COPY
    CausationID:   incomingEvent.EventID,    // parent event's EventID
    VersionID:     marketData.VersionID,     // COPY — same version for whole trace
}

// Helper: propagate trace fields from parent DTO
func propagateTrace(parent TraceFields, parentEventID string) TraceFields {
    return TraceFields{
        TraceID:       parent.TraceID,
        CorrelationID: parent.CorrelationID,
        CausationID:   parentEventID,
        VersionID:     parent.VersionID,
    }
}
```

### Adapter Validation (Hard Reject)

```go
// The database adapter MUST reject DTOs with missing trace fields
// This is the enforcement boundary — modules cannot bypass it

var ErrMissingTraceField = errors.New("missing required trace field")

func validateTraceFields(dto interface{}) error {
    v := reflect.ValueOf(dto)
    t := v.Type()

    for _, field := range []string{"TraceID", "CorrelationID", "VersionID"} {
        f := v.FieldByName(field)
        if !f.IsValid() || f.String() == "" {
            return fmt.Errorf("%w: %s is empty in %s", ErrMissingTraceField, field, t.Name())
        }
    }
    // CausationID can be "" in Layer 0 — no validation here
    return nil
}
```

### What You Can Do With Traceability

```go
// 1. Find all events for a single token's journey
events := adapter.GetEventsByTraceID(ctx, traceID)
// → complete audit trail from first detection to exit

// 2. Find all trades made under a specific strategy version
trades := adapter.GetTradesByVersionID(ctx, versionID)
// → isolate A/B experiment results

// 3. Find what caused a specific decision
parent := adapter.GetEventByCausationChain(ctx, eventID)
// → trace back to root detection event

// 4. Replay: re-run pipeline for a specific TraceID
replayTrace(ctx, traceID)
// → bit-for-bit reproducible because VersionID is embedded
```

### Trace ID Generation (Content-Addressable)

```go
// TraceID is derived from content — never random, never time-based
// Same blockchain event → same TraceID → idempotent replay

func generateTraceID(chain, txHash, logIndex string) string {
    return sha256content(chain + ":" + txHash + ":" + logIndex)[:16]
}

func sha256content(input string) string {
    h := sha256.Sum256([]byte(input))
    return hex.EncodeToString(h[:])
}
```

### Passing TraceFields in Context (Optional Pattern)

```go
// Some teams prefer passing trace fields via context for internal calls
// This is optional but consistent with Go conventions

type contextKey string
const traceFieldsKey contextKey = "trace_fields"

func withTraceFields(ctx context.Context, fields TraceFields) context.Context {
    return context.WithValue(ctx, traceFieldsKey, fields)
}

func traceFieldsFrom(ctx context.Context) (TraceFields, bool) {
    fields, ok := ctx.Value(traceFieldsKey).(TraceFields)
    return fields, ok
}
```

### Anti-Patterns

```go
// ❌ Generating new TraceID in non-Layer-0 module
featureDTO := FeatureDTO{
    TraceID: uuid.New().String(),  // Wrong — breaks the trace chain
}

// ❌ Missing CausationID (except Layer 0)
dataQualityDTO := DataQualityDTO{
    TraceID:       parent.TraceID,
    CorrelationID: parent.CorrelationID,
    CausationID:   "",  // Wrong in Layer 1+ — must be parent EventID
    VersionID:     parent.VersionID,
}

// ❌ Generating new VersionID mid-pipeline
featureDTO.VersionID = activeVersion.VersionID  // Wrong — copy from parent, don't reload

// ❌ Not validating trace fields before DB write
adapter.InsertEvent(ctx, event)  // Without validateTraceFields(event.Payload) — bypass

// ✅ Correct
trace := propagateTrace(parent.TraceFields, incomingEvent.EventID)
featureDTO := FeatureDTO{
    TokenAddress:  parent.TokenAddress,
    TraceID:       trace.TraceID,
    CorrelationID: trace.CorrelationID,
    CausationID:   trace.CausationID,
    VersionID:     trace.VersionID,
}
```

### LearningRecord Attribution

```go
// LearningRecord MUST carry all four trace fields for learning attribution
lr := LearningRecord{
    RecordID:          sha256content(token + versionID + entryTime)[:16],
    TokenAddress:      pos.TokenAddress,
    StrategyVersionID: pos.VersionID,  // MUST match the trade's VersionID
    TraceID:           pos.TraceID,
    CorrelationID:     pos.CorrelationID,
    CausationID:       pos.ExitEventID,
    VersionID:         pos.VersionID,
}
// Learning accuracy depends entirely on correct version attribution
```

---

## Checklist

```
[ ] TraceID assigned in Layer 0 as SHA256(chain+tx_hash+log_index)[:16]
[ ] TraceID COPIED unchanged through all subsequent layers
[ ] CorrelationID COPIED unchanged through all layers
[ ] CausationID = "" ONLY in Layer 0 (market_data_event)
[ ] CausationID = parent EventID in all other layers
[ ] VersionID COPIED from parent DTO — never reloaded mid-pipeline
[ ] Adapter validates non-empty TraceID, CorrelationID, VersionID on writes
[ ] ErrMissingTraceField returned on validation failure
[ ] propagateTrace helper used to prevent copy-paste errors
[ ] LearningRecord carries correct VersionID for A/B attribution
[ ] TraceID is idempotent: same on-chain event → same TraceID
[ ] Replay: same event + same VersionID → same decisions
```

---

## References

- Architecture: `docs/architecture.md` § 2.1 (DTO Contracts), § 4.5 (DTO Registry)
- Architecture context: `docs/architecture-context/2_system_backbone.md`
- DTO spec: `docs/dto_contracts.md` (TraceFields on all DTOs)
- Roadmap: `docs/implementation_roadmap.md` Phase 0 (foundation)