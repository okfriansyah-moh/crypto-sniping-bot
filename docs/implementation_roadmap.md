# Implementation Roadmap — Deterministic Event-Driven Microstructure Sniper (Execution-Grade)

> **Buildable without interpretation.** Every phase specifies exact file paths, function signatures, DTO flow, worker loop, adapter calls, failure handling, and exit criteria. A senior engineer can implement each phase by following this document alone — no architectural guessing required.
>
> **Source-of-truth cross-references:**
>
> - Architecture layers and invariants: `docs/architecture.md`
> - Database schema + adapter interface: `docs/db_adapter_spec.md`
> - DTO registry (field-level): `docs/dto_contracts.md`

---

## Table of Contents

- [Phase 0 — Core Infrastructure (P0)](#phase-0--core-infrastructure-p0)
- [Phase 1 — Detection & Ingestion (P1)](#phase-1--detection--ingestion-p1)
- [First Trade Minimal Path](#first-trade-minimal-path)
- [Phase 2 — Minimal Trading Pipeline (FIRST TRADE) (P1)](#phase-2--minimal-trading-pipeline-first-trade-p1)
  - [2.1 Data Quality (basic)](#21-data-quality-layer-1-minimal)
  - [2.2 Feature Extraction (basic 5 features)](#22-feature-extraction-layer-2-minimal-5-features)
  - [2.3 Edge Discovery (single rule)](#23-edge-discovery-layer-3-single-rule)
  - [2.4 Edge Validation (basic EV gate + latency check)](#24-edge-validation-basic-ev-gate)
  - [2.5 Selection (top 1)](#25-selection-top-1)
  - [2.6 Capital (fixed size + minimal envelope)](#26-capital-fixed-size)
  - [2.7 Execution (real tx)](#27-execution-real-tx)
  - [2.8 Position (TP/SL)](#28-position-tpsl)
  - [2.9 Orchestrator Wiring](#29-orchestrator-wiring)
  - [2.10 Migration](#210-migration)
  - [2.11 Testing](#211-testing)
- [Phase 3 — Evaluation & Correctness (P1.5)](#phase-3--evaluation--correctness-p15)
- [Phase 4 — Signal Quality (P1.5)](#phase-4--signal-quality-models--full-dqfeatures-p15)
- [Phase 5 — Learning Engine (P2)](#phase-5--learning-engine-p2)
- [Phase 6 — Resource Control, Scaling & Production Hardening (P2)](#phase-6--resource-control-wallet-sharding-scaling-p2)
- [Go-Live Checklist](#go-live-checklist)
- [DB Adapter Mapping](#db-adapter-mapping)
- [DTO Pipeline Map](#dto-pipeline-map)
- [Phase Dependency Graph](#phase-dependency-graph)
- [Cross-Cutting Invariants](#cross-cutting-invariants)

---

# Global Conventions

## 0.1 Priority Layers

| Priority | Phases     | Description                                              |
| -------- | ---------- | -------------------------------------------------------- |
| **P0**   | Phase 0    | Execution blocker — DB, event bus, adapter, orchestrator |
| **P1**   | Phase 1, 2 | Ingestion + first-trade vertical slice                   |
| **P1.5** | Phase 3, 4 | Evaluation & correctness, model-based signal quality     |
| **P2**   | Phase 5, 6 | Adaptive learning, resource control, scale               |

**Rule:** No Phase N may merge to `main` until Phase N-1 exit criteria are all checked.

## 0.2 Module Layout Pattern

Every pipeline module under `internal/modules/<name>/` follows this skeleton:

```
internal/modules/<name>/
├── <name>.go          // Public entry: Process(ctx, inputDTO) (outputDTO, error)
├── internal/          // Private helpers (not importable from outside the module)
│   └── <logic>.go
└── <name>_test.go     // Unit tests with fixture DTOs; zero DB, zero network
```

Every worker under `internal/workers/` is a thin dispatcher:

```go
// internal/workers/run_<name>.go
package workers

func Run<Name>(ctx context.Context, adapter database.Adapter, mod *<name>.Module, cfg Config) error {
    for {
        evt, err := adapter.ClaimNextEvent(ctx, "<name>_worker", []string{"<input_event_type>"})
        if err != nil { return err }
        if evt == nil { sleep(cfg.IdleBackoff); continue }

        input, err := contracts.Decode<Input>(evt.Payload)
        if err != nil { markFailed(adapter, evt, err); continue }

        output, err := mod.Process(ctx, input)
        if err != nil { handleFailure(adapter, evt, err, cfg); continue }

        if err := persistAndEmit(ctx, adapter, evt, output); err != nil { return err }
        if err := adapter.MarkEventProcessed(ctx, evt.EventID); err != nil { return err }
    }
}
```

## 0.3 Traceability Contract (All DTOs, All Phases)

Every DTO emitted by any phase MUST carry these four fields (see `docs/dto_contracts.md` § 1):

| Field           | Propagation Rule                                                             |
| --------------- | ---------------------------------------------------------------------------- |
| `TraceID`       | Copy from input DTO. Generated fresh only in Layer 0 (Phase 1).              |
| `CorrelationID` | Copy from input DTO. Generated fresh only in Layer 0 (Phase 1).              |
| `CausationID`   | Set to `inputEvent.EventID`. Empty string `""` ONLY for Layer 0 root events. |
| `VersionID`     | Copy from active `StrategyVersion` pinned at orchestrator start.             |

Adapter rejects writes missing these fields (returns `ErrMissingTraceField`).

## 0.4 Idempotency Contract

Every INSERT uses `ON CONFLICT (<pk>) DO NOTHING`. Every DTO `EventID` is content-addressable:

```
EventID = SHA256(canonical_json(payload))[:16]
```

Replay of the same event stream produces bit-for-bit identical database state.

## 0.5 Config-Driven Thresholds

Every threshold, weight, cap, percentile, timeout, and bucket lives in YAML under `config/`. Hardcoded numeric constants in module code are forbidden (exceptions: protocol-defined values like EIP-55 address length).

## 0.6 Global Event→Worker Routing

Every event type maps to exactly one consumer worker group. Workers declare their group via `ClaimNextEvent(ctx, group, eventTypes)`. This table is the authoritative source — no worker may claim event types not listed here for its group.

| Event Type                 | Consumer Worker Group         | Worker File                   | Phase |
| -------------------------- | ----------------------------- | ----------------------------- | ----- |
| `market_data_event`        | `data_quality_worker`         | `run_data_quality.go`         | 2     |
| `data_quality_event`       | `features_worker`             | `run_features.go`             | 2     |
| `feature_event`            | `edge_worker`                 | `run_edge.go`                 | 2     |
| `edge_event`               | `validation_worker`           | `run_validation.go`           | 2     |
| `validated_edge_event`     | `validation_worker`†          | (join target — also consumed) | 2     |
| `validated_edge_event`     | `selection_worker`            | `run_selection.go`            | 2     |
| `selection_event`          | `capital_worker`              | `run_capital.go`              | 2     |
| `allocation_event`         | `execution_worker`            | `run_execution.go`            | 2     |
| `execution_event`          | `position_open_worker`        | `run_position_open.go`        | 2     |
| `position_event`           | `evaluation_worker`           | `run_evaluation.go`           | 3     |
| `position_event`           | `learning_recorder_worker`    | `run_learning_record.go`      | 5     |
| `evaluation_event`         | `adjustment_worker`           | `run_updater.go`              | 5     |
| `probability_event`        | `probability_worker`          | `run_probability.go`          | 4     |
| `slippage_event`           | `slippage_worker`             | `run_slippage.go`             | 4     |
| `latency_event`            | `latency_worker`              | `run_latency.go`              | 4     |
| `learning_record_event`    | `shadow_recorder_worker`      | `run_shadow_recorder.go`      | 5     |
| `strategy_promotion_event` | `rollback_watchdog_worker`    | `run_rollback_watchdog.go`    | 5     |
| `system_event`             | monitoring only — no consumer | —                             | 6     |
| `halted_event`             | monitoring only — no consumer | —                             | 6     |
| `archive_event`            | monitoring only — no consumer | —                             | 6     |
| `expired_event`            | monitoring only — no consumer | —                             | 2     |

**Source workers (no input event — emit into the bus):**

| Worker                   | Emits                         | Type     | Worker File              | Phase |
| ------------------------ | ----------------------------- | -------- | ------------------------ | ----- |
| `run_ingestion.go`       | `market_data_event`           | source   | `run_ingestion.go`       | 1     |
| `run_position_poll.go`   | `position_event`              | periodic | `run_position_poll.go`   | 2     |
| `run_latency.go`         | `latency_event`               | periodic | `run_latency.go`         | 4     |
| `run_shadow_observer.go` | _(updates shadow_trades DB)_  | periodic | `run_shadow_observer.go` | 5     |
| `run_evaluator.go`       | `evaluation_event`            | periodic | `run_evaluator.go`       | 5     |
| `run_risk_controller.go` | `system_event`/`halted_event` | periodic | `run_risk_controller.go` | 6     |
| `run_archive.go`         | `archive_event`               | periodic | `run_archive.go`         | 6     |

**Invariants:**

- Every event claimed via `ClaimNextEvent` is claimed by exactly **one** primary group (fan-out = emit multiple output events, never two groups on the same input event).
- `position_event` has two consumer groups (`evaluation_worker` in Phase 3, `learning_recorder_worker` in Phase 5) — this requires the adapter's `consumer_offsets` table to track per-group processing state independently.
- Periodic workers do **not** use `ClaimNextEvent` — they run on a ticker goroutine and read directly from the DB or emit root events.

## 0.7 Canonical Lifecycle CAS Pattern

**Every worker that calls `TransitionState` MUST follow this exact 3-step sequence.** Omitting `ExpectedStateVersion` creates a write-skew window where two concurrent workers can race on the same lifecycle row.

```go
// Step 1: Read current lifecycle (includes StateVersion counter)
lc, err := adapter.GetLifecycleByToken(ctx, tokenAddress)
if err != nil { return err }

// Step 2: Defensive pre-check (avoids unnecessary write attempt)
if lc.CurrentState != expectedFromState {
    // Another worker already transitioned this token — skip safely
    _ = adapter.MarkEventProcessed(ctx, evt.EventID)
    return nil
}

// Step 3: CAS write — StateVersion prevents concurrent write-skew
err = adapter.TransitionState(ctx, database.TransitionRequest{
    LifecycleID:          lc.LifecycleID,
    ExpectedFromState:    expectedFromState,  // e.g. "DQ_PASSED"
    NewState:             newState,           // e.g. "FEATURE_READY"
    ExpectedStateVersion: lc.StateVersion,   // CAS guard (int64, incremented per transition)
    Reason:               reason,            // "" for happy-path transitions
    ActorWorker:          workerGroup,       // e.g. "features_worker"
})
if errors.Is(err, database.ErrInvalidTransition) {
    // CAS failed → concurrent worker beat us; record and continue without halting
    _ = adapter.InsertStateViolation(ctx, lc.LifecycleID, expectedFromState, newState, "cas_conflict")
    _ = adapter.MarkEventProcessed(ctx, evt.EventID)
    return nil
}
if err != nil { return err }
```

**Adapter SQL (implement only in `database/engines/` — never in modules):**

```sql
UPDATE token_lifecycle
   SET current_state  = $new_state,
       state_version  = state_version + 1,
       updated_at     = CURRENT_TIMESTAMP
 WHERE token_lifecycle_id  = $id
   AND current_state       = $expected_from_state
   AND state_version       = $expected_state_version;  -- CAS guard
-- 0 rows updated → adapter returns ErrInvalidTransition
```

**`TransitionRequest` fields (declared in `database/adapter.go`):**

| Field                  | Type     | Required | Description                                              |
| ---------------------- | -------- | -------- | -------------------------------------------------------- |
| `LifecycleID`          | `string` | Yes      | PK of the `token_lifecycle` row                          |
| `ExpectedFromState`    | `string` | Yes      | State the row must be in for the update to proceed       |
| `NewState`             | `string` | Yes      | Target state; validated against `AllowedTransitions`     |
| `ExpectedStateVersion` | `int64`  | Yes      | Current `state_version` value — the CAS write-skew guard |
| `Reason`               | `string` | No       | Human-readable reason (stored in audit row)              |
| `ActorWorker`          | `string` | No       | Worker group name (stored in audit row)                  |

**Shorthand convention:** Phase 3–6 worker pseudocode uses `{lc.ID, STATE_A→STATE_B}` as shorthand for a full `TransitionRequest` with all required fields, including `ExpectedStateVersion: lc.StateVersion`. Never call `TransitionState` without this field.

---

# Phase Implementations

---

## Phase 0 — Core Infrastructure (P0)

### Objective

Build the foundational substrate: Postgres event bus, migration runner, `database.Adapter` interface, config loader, structured logger, orchestrator skeleton, and generic worker dispatcher with `SELECT ... FOR UPDATE SKIP LOCKED`. Without this, nothing else runs.

### BLOCKERS

**None — this is the first phase.** Phase 0 has no prerequisites.

### Scope

**In scope:** Postgres schema creation, adapter interface, migration runner, config loader, structured logger, orchestrator boot, generic worker loop (`SKIP LOCKED`), `StrategyVersion` pin at startup.

**Explicitly excluded:** Any business logic, token filtering, market data ingestion, RPC calls to blockchain nodes, trading modules.

### Event Types Emitted

| Event Type             | Emitter      | DTO           |
| ---------------------- | ------------ | ------------- |
| `pipeline_run_started` | Orchestrator | metadata only |

### File Structure

```
cmd/
├── root.go
├── server.go                       // Main daemon entry point
└── migrate.go                      // CLI: `sniper migrate`

config/
├── pipeline.yaml                   // Core config (DB, workers, priorities)
├── chains.yaml                     // RPC endpoints, factory addresses
├── execution.yaml                  // Gas, nonce, slippage caps
├── gas.yaml                        // EIP-1559 defaults
└── priority.yaml                   // Resource-control priority weights

internal/app/
├── config/config.go                // Config loader + validator
└── logging/logger.go               // Structured JSON logger

database/
├── adapter.go                      // Adapter interface + Event, Lifecycle, StrategyVersion, TransitionRequest, PipelineRun types
├── errors.go                       // ErrOrphanEvent, ErrInvalidTransition, ErrMissingTraceField, ErrUnknownVersion, ErrNonceGap
├── migrations.go                   // Runner: reads files, records in _migrations
├── engines/postgres/
│   ├── postgres.go                 // pgx pool, Initialize, Close, transaction helpers
│   ├── events.go                   // InsertEvent, ClaimNextEvent, MarkEventProcessed, GetEventByID
│   ├── runs.go                     // CreateRun, UpdateRunStage, UpdateRunStatus, GetRun
│   └── versions.go                 // CreateStrategyVersion, GetActiveStrategyVersion, GetStrategyVersion
└── migrations/
    └── 20260101000001_initial_schema.sql

internal/orchestrator/
├── orchestrator.go                 // Boot: loads config, pins StrategyVersion, starts workers
├── worker.go                       // Generic worker loop (ClaimNext → process → emit → mark)
├── registry.go                     // StageHandler registration
└── checkpoint.go                   // Checkpoint writes

contracts/
├── trace.go                        // TraceFields (embedded struct), helpers
└── event_envelope.go               // Canonical event payload wrapper + Decode helpers

internal/orchestrator/orchestrator_test.go
database/adapter_test.go
database/engines/postgres/postgres_test.go
```

### Function Contracts

```go
// database/adapter.go
package database

type Adapter interface {
    Initialize(ctx context.Context, cfg Config) error
    RunMigrations(ctx context.Context) error
    Close(ctx context.Context) error

    InsertEvent(ctx context.Context, evt Event) error
    ClaimNextEvent(ctx context.Context, group string, eventTypes []string) (*Event, error)
    MarkEventProcessed(ctx context.Context, eventID string) error
    GetEventByID(ctx context.Context, eventID string) (*Event, error)

    CreateRun(ctx context.Context, run PipelineRun) error
    UpdateRunStage(ctx context.Context, runID, stage string) error
    UpdateRunStatus(ctx context.Context, runID, status string) error
    GetRun(ctx context.Context, runID string) (*PipelineRun, error)

    CreateStrategyVersion(ctx context.Context, sv StrategyVersion) error
    GetActiveStrategyVersion(ctx context.Context) (*StrategyVersion, error)
    GetStrategyVersion(ctx context.Context, versionID string) (*StrategyVersion, error)
}

// internal/app/config/config.go
func Load(paths ...string) (*Config, error)
func (c *Config) Validate() error
func (c *Config) Snapshot() ([]byte, error)  // canonical JSON for StrategyVersion hash

// internal/orchestrator/orchestrator.go
func Boot(ctx context.Context, adapter database.Adapter, cfg *config.Config) (*Orchestrator, error)
func (o *Orchestrator) RegisterStage(eventType string, handler StageHandler)
func (o *Orchestrator) Run(ctx context.Context) error

// internal/orchestrator/worker.go
type StageHandler interface {
    Process(ctx context.Context, evt *database.Event) (*database.Event, error)
}

func RunWorker(ctx context.Context, adapter database.Adapter, group string, eventTypes []string, handler StageHandler, idleBackoff time.Duration) error
```

### DTO Pipeline

Phase 0 is **infrastructure-only**. No DTO-to-DTO pipeline transformations occur.

| Component           | Input     | Output                   | Event Out       |
| ------------------- | --------- | ------------------------ | --------------- |
| Orchestrator boot   | —         | `pipeline_run_started`   | (metadata only) |
| Strategy pin        | —         | `StrategyVersion` (DB)   | —               |
| Generic worker loop | any event | next event (via handler) | next event type |

### Lifecycle Transitions

Phase 0 does not operate on token lifecycles. `StartLifecycle` and `TransitionState` are defined in this phase's adapter interface but are first **called** in Phase 2 workers.

### Traceability

Phase 0 establishes the adapter-level enforcement contract. All fields are validated at write time when the adapter is fully assembled in Phase 3. In Phase 0, the schema + error types are defined:

- `ErrMissingTraceField` — returned if `TraceID`, `CorrelationID`, or `VersionID` is empty on `InsertEvent`
- `ErrOrphanEvent` — returned if `CausationID` references a non-existent `EventID` (checked in Phase 3+)
- Exception: `CausationID = ""` is allowed **only** for ingestion root events (Layer 0 source)

### DTO Flow

Phase 0 produces no DTOs. It only provides the substrate that Phase 1+ use. The only Phase 0 artifact on the event bus is the `pipeline_run_started` event (metadata).

### Worker Flow

Generic loop (referenced by every later phase):

```
1. adapter.ClaimNextEvent(group, eventTypes)
     → Postgres: SELECT ... FROM events
                 WHERE processed = FALSE AND event_type = ANY($1)
                 ORDER BY created_at
                 FOR UPDATE SKIP LOCKED LIMIT 1
2. If nil → sleep(cfg.worker.idle_backoff_ms), continue
3. handler.Process(ctx, evt) → next event (or err)
4. On err → handleFailure (retry / dead-letter)
5. adapter.InsertEvent(next)         // ON CONFLICT DO NOTHING
6. adapter.MarkEventProcessed(evt.EventID)
```

### Adapter Calls (Complete)

```
adapter.RunMigrations(ctx)                 // startup — applies all pending migrations
adapter.CreateStrategyVersion(ctx, sv)     // startup — pin active config version
adapter.GetActiveStrategyVersion(ctx)      // startup — confirm pin; all workers read at start
adapter.CreateRun(ctx, run)                // per-market pipeline boot
adapter.UpdateRunStage(ctx, runID, stage)  // checkpoint after every stage
adapter.UpdateRunStatus(ctx, runID, status)// on completion / failure
adapter.InsertEvent(ctx, evt)              // every emit (used by all later workers)
adapter.ClaimNextEvent(ctx, group, types)  // every dequeue (generic worker loop)
adapter.MarkEventProcessed(ctx, eventID)   // every consume
```

### Migration — `20260101000001_initial_schema.sql`

Tables: `events`, `consumer_offsets`, `pipeline_runs`, `strategy_versions`, `_migrations`. See `docs/db_adapter_spec.md` § 6.1 & § 6.8 for exact DDL.

### Failure Handling

| Condition                               | Action                                                                                       |
| --------------------------------------- | -------------------------------------------------------------------------------------------- |
| Migration failure                       | Abort startup with non-zero exit code                                                        |
| DB connection lost at runtime           | Exponential backoff (100ms → 30s), then halt workers                                         |
| `InsertEvent` duplicate (`ON CONFLICT`) | Silently ignored — idempotent by design                                                      |
| Worker panic                            | Recover in `RunWorker`, log with trace, re-enter loop                                        |
| Stage handler error                     | Do NOT mark processed. Event stays claimed until transaction timeout → requeue automatically |

### Exit Criteria

- [ ] `sniper migrate` creates all 5 tables (`events`, `consumer_offsets`, `pipeline_runs`, `strategy_versions`, `_migrations`) on empty DB
- [ ] Re-running `sniper migrate` is a no-op (idempotent via `_migrations` table)
- [ ] `adapter.InsertEvent()` called twice with same `event_id` → exactly 1 row in `events`
- [ ] Two concurrent workers claiming same event → only one succeeds (`SKIP LOCKED` verified in integration test)
- [ ] `config.Load()` fails fast with clear error if required key is missing
- [ ] Structured logs include `trace_id`, `correlation_id`, `version_id` in every entry
- [ ] `StrategyVersion` pinned once at boot; `GetActiveStrategyVersion()` returns same ID across all workers
- [ ] `go test ./database/... ./internal/orchestrator/...` passes

---

## Phase 1 — Detection & Ingestion (P1)

**Architecture:** Layer 0 (`docs/architecture.md` § 3.0)

### Objective

Subscribe to on-chain logs, normalize them into `MarketDataDTO`, and emit `market_data_event` to the bus. Zero business logic. Zero filtering. **Pure ingestion.**

### BLOCKERS

**Phase 0 exit criteria must all be checked before starting Phase 1.**

Specifically: `adapter.InsertEvent()` idempotency verified, `SKIP LOCKED` worker loop tested, `StrategyVersion` pin confirmed, `go test ./database/... ./internal/orchestrator/...` passes.

### Scope

**In scope:** WebSocket `eth_subscribe` primary transport, HTTP `eth_getLogs` fallback, heartbeat/reconnect, gap recovery on reconnect, reorg detection via confirmation depth, `MarketDataDTO` normalization.

**Explicitly excluded:** Token filtering, DQ checks, any scoring or classification. This phase only produces `market_data_event` records — nothing more.

### Event Types Emitted

| Event Type          | Emitter          | DTO             |
| ------------------- | ---------------- | --------------- |
| `market_data_event` | ingestion worker | `MarketDataDTO` |

### File Structure

```
contracts/
└── market_data.go                       // MarketDataDTO (Layer 0 root)

internal/modules/ingestion/
├── ingestion.go                         // Public Module: Start(ctx), Stop(ctx)
├── normalize.go                         // rawLog → MarketDataDTO per chain
├── subscribe.go                         // WebSocket eth_subscribe logs
├── poll.go                              // HTTP eth_getLogs fallback
├── heartbeat.go                         // WS ping/pong
├── reconnect.go                         // Exponential backoff + endpoint failover
├── gap_recovery.go                      // Fill [last_processed_block+1, current]
├── reorg.go                             // Confirmation-depth check + reorg marking
├── topics.go                            // Topic hash registry (PairCreated, Mint, Swap, Burn)
├── internal/
│   └── wallet_side.go                   // Select token vs base from Uniswap pair
└── ingestion_test.go

internal/workers/
└── run_ingestion.go                     // Dispatcher (special — no input event; source events)

database/migrations/
└── 20260101000002_ingestion_tables.sql  // ingestion_state, rpc_endpoint_state, tokens
```

### Function Contracts

```go
// contracts/market_data.go  — see docs/dto_contracts.md § 3.1 for full field list
type MarketDataDTO struct {
    EventID, TraceID, CorrelationID, CausationID, VersionID string
    Chain, Market                                            string
    BlockNumber                                              uint64
    BlockHash, TxHash                                        string
    LogIndex                                                 uint32
    EventTopic                                               string
    PoolAddress, TokenAddress, BaseAddress                   string
    Token0Address, Token1Address                             string
    Amount0Raw, Amount1Raw                                   string
    ReserveBaseRaw, ReserveTokenRaw                          string
    BlockTimestamp, IngestedAt                               string
    RpcEndpoint, Transport                                   string
    ConfirmationDepth                                        uint32
    Reorged                                                  bool
}

// internal/modules/ingestion/ingestion.go
type Module struct { /* ... */ }

func New(cfg Config, versionID string, emit EventEmitter) *Module
func (m *Module) Start(ctx context.Context) error
func (m *Module) Stop(ctx context.Context) error

// Callback invoked by emit — writes to event bus via adapter
type EventEmitter func(ctx context.Context, dto contracts.MarketDataDTO) error

// internal/modules/ingestion/normalize.go
func NormalizePairCreated(log rpc.Log, chain string, endpoint string, versionID string) (contracts.MarketDataDTO, error)
func NormalizeMint(log rpc.Log, chain string, endpoint string, versionID string) (contracts.MarketDataDTO, error)
func NormalizeSwap(log rpc.Log, chain string, endpoint string, versionID string) (contracts.MarketDataDTO, error)
func NormalizeBurn(log rpc.Log, chain string, endpoint string, versionID string) (contracts.MarketDataDTO, error)

// internal/modules/ingestion/gap_recovery.go
func RecoverGap(ctx context.Context, client rpc.Client, chain string, fromBlock, toBlock uint64) ([]rpc.Log, error)
```

### DTO Pipeline

Phase 1 is the **root** of all DTO chains. It transforms raw blockchain logs into the first typed DTO.

| Stage     | Input       | Output          | Output Event        |
| --------- | ----------- | --------------- | ------------------- |
| Ingestion | raw RPC log | `MarketDataDTO` | `market_data_event` |

`ExpiresAt` is NOT set on `MarketDataDTO` — the ingestion event has no TTL (it is the source).

### Lifecycle Transitions

Phase 1 does **not** start or transition lifecycles. `StartLifecycle` is first called in the Phase 2 DQ worker when it consumes the `market_data_event`. Ingestion is the source — there is no prior lifecycle state.

### Traceability

Phase 1 **initializes** the trace chain that all downstream DTOs must propagate:

| Field           | Initialization Rule                                                                       |
| --------------- | ----------------------------------------------------------------------------------------- |
| `TraceID`       | `SHA256(chain ‖ tx_hash ‖ log_index)[:16]` — same as `EventID` (new journey starts here)  |
| `CorrelationID` | `SHA256(TraceID ‖ block_number)[:16]`                                                     |
| `CausationID`   | `""` — ingestion is the **only** phase allowed to emit root events with empty CausationID |
| `VersionID`     | Active `StrategyVersion.StrategyVersionID` pinned at worker start                         |

### DTO Flow

```
Input:  (external) raw blockchain log from RPC
Output: MarketDataDTO
        event_type = "market_data_event"
        payload    = MarketDataDTO canonical JSON
        causation_id = "" (Layer 0 is ROOT — only phase allowed to emit orphan root events)
```

`EventID = SHA256(chain || tx_hash || log_index)[:16]`
`TraceID = EventID` (a new token journey starts here)
`CorrelationID = SHA256(trace_id || current_block)[:16]`

### Worker Flow

Ingestion has **no input event** — it's the source. Worker:

```
1. Establish WebSocket subscription to configured factory addresses
2. On each incoming log:
   a. normalize(log)  →  MarketDataDTO (with fresh TraceID/CorrelationID, CausationID="")
   b. adapter.InsertEvent(Event{EventType: "market_data_event", Payload: dto})
      // ON CONFLICT DO NOTHING protects against duplicate delivery
   c. adapter.InsertMarketData(dto)
   d. adapter.UpsertIngestionWatermark(chain, block_number)
3. On WebSocket drop:
   a. heartbeat.OnTimeout() → reconnect.WithBackoff(endpoints)
   b. On reconnect: gap_recovery.RecoverGap(lastProcessedBlock+1, currentBlock)
   c. For each recovered log → goto step 2, but Transport = "gap_recovery"
4. If block N has reorged (depth < confirmation_depth of config.chains[X].reorg_protection):
   → emit duplicate MarketDataDTO with Reorged = true, new EventID
```

### Adapter Calls (Complete)

```
adapter.InsertEvent(ctx, evt)                       // every log — ON CONFLICT DO NOTHING
adapter.InsertMarketData(ctx, dto)                  // every log (projection)
adapter.UpsertIngestionWatermark(ctx, chain, block) // every block batch
adapter.GetIngestionWatermark(ctx, chain)           // on boot (resume from last processed block)
adapter.GetActiveStrategyVersion(ctx)               // on worker start (pin VersionID)
```

### Config (`config/chains.yaml`)

```yaml
chains:
  ethereum:
    chain_id: 1
    rpc_endpoints:
      - url: wss://eth-mainnet.ws.primary
        weight: 100
      - url: wss://eth-mainnet.ws.secondary
        weight: 50
    factories:
      - address: "0x5C69bEe701ef814a2B6a3EDD4B1652CB9cc5aA6f" # Uniswap V2
        abi: uniswap_v2_factory
      - address: "0x1F98431c8aD98523631AE4a59f267346ea31F984" # Uniswap V3
        abi: uniswap_v3_factory
    confirmation_depth: 2
    reorg_protection_blocks: 5
    ingestion_backoff:
      initial_ms: 100
      max_ms: 30000
      multiplier: 2.0
  bsc:
    chain_id: 56
    rpc_endpoints:
      - url: wss://bsc.ws.primary
        weight: 100
    factories:
      - address: "0xcA143Ce32Fe78f1f7019d7d551a6402fC5350c73" # PancakeSwap V2
        abi: pancake_v2_factory
    confirmation_depth: 3
    reorg_protection_blocks: 10
```

### Failure Handling

| Condition                                    | Action                                                                                 |
| -------------------------------------------- | -------------------------------------------------------------------------------------- |
| WebSocket disconnect                         | Exponential backoff with endpoint rotation; during disconnect, polling fallback active |
| All RPC endpoints circuit-open               | Halt ingestion, emit `ingestion_halted` system event, alert via Telegram               |
| Normalization error on single log            | Log + skip (do NOT halt ingestion); increment `normalize_error_count`                  |
| `InsertEvent` duplicate                      | Silent (idempotent) — expected during gap recovery overlap                             |
| `last_processed_block` moves backward        | Forbidden — adapter rejects non-monotonic updates (`ErrWatermarkRegression`)           |
| Reorg detected at depth < confirmation_depth | Re-emit log with `Reorged = true`, new `EventID`; downstream filters handle            |

### Exit Criteria

- [ ] Live WebSocket connection to Uniswap V2/V3 + PancakeSwap V2 factories established
- [ ] `PairCreated`, `Mint`, `Swap` events captured and normalized to `MarketDataDTO` with correct chain/market labels
- [ ] `EventID` determinism: same `(chain, tx_hash, log_index)` → same `EventID` across replays
- [ ] Duplicate WebSocket+gap-recovery delivery → 1 row in `events`
- [ ] WebSocket drop test: kill connection for 30s → on reconnect, gap recovery fills missing blocks; `last_processed_block` advances monotonically
- [ ] p95 ingestion latency `< 500ms` on Ethereum, `< 200ms` on BSC (measured: `ingested_at - block_timestamp`)
- [ ] Replay test: feed fixture JSON logs → produces bit-for-bit identical `MarketDataDTO` records
- [ ] Zero `database/` imports anywhere under `internal/modules/ingestion/`
- [ ] Zero SQL strings anywhere under `internal/modules/ingestion/`

---

## First Trade Minimal Path

> **Gate:** Before building any Phase 2 module, confirm this sequence is achievable end-to-end on testnet with the simplifications listed. If any step is blocked, fix Phase 0 or Phase 1 before continuing.

### Ordered Execution Sequence

```
market_data_event
    │
    ▼  [data_quality worker]
    │  Decision = PASS | RISKY_PASS  →  continue
    │  Decision = REJECT             →  lifecycle DETECTED → REJECTED (terminal)
    ▼
data_quality_event
    │
    ▼  [features worker]
    │  5 features only: liquidity_usd, tx_velocity_30s, holder_count,
    │                   lp_locked_flag, contract_verified_flag
    ▼
feature_event
    │
    ▼  [edge worker]
    │  Rule: token_age < max_age AND velocity > min_vel AND liquidity > min_liq
    │  ok=false → lifecycle FEATURE_READY → REJECTED (terminal, no output event)
    ▼
edge_event
    │
    ▼  [validation worker]
    │  TTL check first: if ExpiresAt < now() → emit expired_event, skip
    │  EV gate: P × gain - (1-P) × loss - costs > ev_threshold  (fixed priors from config)
    │  Latency check: p95_latency_ms > opportunity_window_ms → REJECT reason=latency_exceeds_window
    │  Decision=REJECT → lifecycle EDGE_DETECTED → REJECTED (terminal)
    ▼
validated_edge_event  (Decision=ACCEPT only)
    │
    ▼  [selection worker]
    │  max_concurrent_positions = 1 (Phase 2)
    │  if open_positions >= 1 → Selected=false (no output event emitted for non-selected)
    ▼
selection_event  (Selected=true only)
    │
    ▼  [capital worker]
    │  CheckEnvelope: total_exposure + size > max_total_exposure → Rejected=true, stop
    │  SizeUsd = config.capital.fixed_entry_size_usd  (fixed, no model)
    │  ExecutionID = SHA256(CorrelationID)[:16]
    ▼
allocation_event  (Rejected=false only)
    │
    ▼  [execution worker]
    │  TTL check: if ExpiresAt < now() → emit expired_event, skip
    │  AllocateNonce (DB-backed, monotonic)
    │  EIP-1559 gas estimation
    │  Build Uniswap V2 calldata with slippage cap
    │  Sign + sendRawTransaction (public mempool, single attempt in Phase 2)
    │  Wait receipt (config.execution.receipt_timeout_ms)
    │  Status = confirmed | reverted | dropped | failed
    ▼
execution_event
    │
    ▼  [position_open worker]  (Status=confirmed only)
    │  Record entry: TokenAddress, EntryPrice, SizeUsd, WalletAddress
    │  lifecycle SELECTED → EXECUTED → POSITION_OPEN
    ▼
position_event (Status=open)
    │
    ▼  [position_poll worker]  (periodic, every poll_interval_seconds)
    │  Fetch current price from RPC
    │  Check: current_price >= entry * (1 + tp1_pct/100)  → ExitReason=TP1
    │         current_price <= entry * (1 - sl_pct/100)   → ExitReason=SL
    │         now - opened_at >= max_hold_seconds          → ExitReason=TIME
    │  On exit: submit sell tx, record PnlUsd/PnlPct
    │  lifecycle POSITION_OPEN → POSITION_CLOSED
    ▼
position_event (Status=exited)
```

### Phase 2 Simplifications (Enforced — no exceptions)

| What              | Phase 2 Value                           | Full value in |
| ----------------- | --------------------------------------- | ------------- |
| Probability model | Fixed prior from `config.validation.*`  | Phase 4       |
| Slippage model    | Fixed prior from `config.validation.*`  | Phase 4       |
| Wallet count      | 1 wallet, 1 concurrent position         | Phase 6       |
| Tx retry/replace  | Single attempt only; no bumped-gas loop | Phase 3       |
| RPC routing       | Public mempool only                     | Phase 4       |
| Learning feedback | None — no parameter updates in Phase 2  | Phase 5       |
| Adaptive exits    | Fixed TP1/SL/TIME from config           | Phase 5       |

### Testnet Requirements

- **Network:** Goerli (ETH) or BSC Testnet — never mainnet until Phase 3 is complete
- **Verification:** tx hash observable on testnet explorer; receipt status = 1
- **Traceability:** `events` table shows full causal chain `market_data_event → … → position_event (exited)` with all trace fields populated
- **Replay:** feed same `market_data_event` fixture → deterministic output (execution bypassed via `config.execution.mode=shadow`)

---

## Phase 2 — Minimal Trading Pipeline (FIRST TRADE) (P1)

**Architecture:** Layers 1, 2, 3, 5, 6, 7, 8, 9 (`docs/architecture.md` §§ 3.1–3.9)

### Objective

Execute the **first real on-chain trade end-to-end** using simplest viable rules. Deliver a working MVP vertical slice. Advanced features (learning, model-based probability, wallet sharding, adaptive exits) are explicitly deferred to later phases.

### BLOCKERS

**Phase 1 exit criteria must all be checked before starting Phase 2.**

Specifically: live ingestion verified, `market_data_event` rows present in `events` table, duplicate delivery idempotency confirmed, replay produces deterministic output, zero SQL in `internal/modules/ingestion/`.

### Scope

**In scope:** All 8 pipeline stages (DQ → Features → Edge → Validation → Selection → Capital → Execution → Position), TTL expiry enforcement in worker loop, minimal capital cap (`max_total_exposure_usd`), basic latency budget check in validation, nonce management, TP/SL/TIME exits. One wallet. One concurrent position.

**Explicitly excluded:** Multiple wallets (Phase 6), probability model (Phase 4), adaptive exits (Phase 5), full capital envelope with cohort caps (Phase 6), transaction replacement (Phase 3), state machine CAS enforcement (Phase 3), private RPC routing (Phase 4).

### Event Types Emitted

| Event Type             | Emitter                     | DTO                  |
| ---------------------- | --------------------------- | -------------------- |
| `data_quality_event`   | data_quality worker         | `DataQualityDTO`     |
| `feature_event`        | features worker             | `FeatureDTO`         |
| `edge_event`           | edge worker                 | `EdgeDTO`            |
| `validated_edge_event` | validation worker           | `ValidatedEdgeDTO`   |
| `selection_event`      | selection worker            | `SelectionOutputDTO` |
| `allocation_event`     | capital worker              | `AllocationDTO`      |
| `execution_event`      | execution worker            | `ExecutionResultDTO` |
| `position_event`       | position worker (open/exit) | `PositionStateDTO`   |
| `expired_event`        | any worker (TTL gate)       | `ExpiredEventDTO`    |

### TTL Expiry Enforcement (from § 7.2)

Add to the generic worker loop (§ 0.2) **before** calling `mod.Process()`:

```go
// internal/orchestrator/worker.go
if input.ExpiresAt != "" && parseISO(input.ExpiresAt).Before(time.Now().UTC()) {
    expiredEvt := buildExpiredEvent(evt, "ttl_expired")
    _ = adapter.InsertEvent(ctx, expiredEvt)   // event_type = "expired_event"
    _ = adapter.MarkEventProcessed(ctx, evt.EventID)
    continue
}
```

**DTO TTL values from `config/pipeline.yaml`:**

| DTO                | Config Key                   | Default |
| ------------------ | ---------------------------- | ------- |
| `EdgeDTO`          | `edge.ttl_seconds`           | 8       |
| `ValidatedEdgeDTO` | `validated_edge.ttl_seconds` | 5       |
| `AllocationDTO`    | `allocation.ttl_seconds`     | 3       |

Every emitting worker sets `ExpiresAt = time.Now().Add(cfg.<dto>.ttl_seconds).Format(time.RFC3339)`.

### Minimal Capital Envelope (from § 7.4)

Add to `internal/modules/capital/`:

```go
// internal/modules/capital/envelope.go
func (m *Module) CheckEnvelope(ctx context.Context, proposed contracts.AllocationDTO) (ok bool, rejectReason string)
// Phase 2: rejects when:
//   total_exposure_usd + proposed.SizeUsd > cfg.capital.max_total_exposure_usd
//   open_positions_count >= cfg.capital.max_concurrent_positions
```

`Process()` calls `CheckEnvelope()` after sizing. If rejected, emits `allocation_event` with `Rejected=true, RejectReason="envelope_exceeded"`. Capital worker does **not** proceed to execution.

Phase 6 extends this with per-token and per-cohort caps.

### Basic Latency Budget Check (from § 7.5)

Add to `internal/modules/validation/ev_gate.go`:

```go
// After EV computation, before Decision:
if latencyProfile.P95Ms + config.execution.build_submit_p95_ms > edge.OpportunityWindowMs {
    validated.Decision = "REJECT"
    validated.RejectReason = "latency_exceeds_window"
    return validated, nil
}
```

`EdgeDTO.OpportunityWindowMs` is computed from `VolumeMomentum` in edge.go:
`opportunity_window_ms = config.edge.base_window_ms * (1 + edge_strength * config.edge.window_momentum_factor)`

Phase 3 refines this with priority-weighted urgency. Phase 4 uses the full `LatencyProfileDTO` model.

### DTO Pipeline

Complete Input → Output DTO map for all 8 pipeline stages in Phase 2:

| Stage      | Layer | Input DTO            | Input Event            | Output DTO           | Output Event           |
| ---------- | ----- | -------------------- | ---------------------- | -------------------- | ---------------------- |
| DQ         | 1     | `MarketDataDTO`      | `market_data_event`    | `DataQualityDTO`     | `data_quality_event`   |
| Features   | 2     | `DataQualityDTO`     | `data_quality_event`   | `FeatureDTO`         | `feature_event`        |
| Edge       | 3     | `FeatureDTO`         | `feature_event`        | `EdgeDTO`            | `edge_event`           |
| Validation | 5     | `EdgeDTO`            | `edge_event`           | `ValidatedEdgeDTO`   | `validated_edge_event` |
| Selection  | 6     | `ValidatedEdgeDTO`   | `validated_edge_event` | `SelectionOutputDTO` | `selection_event`      |
| Capital    | 7     | `SelectionOutputDTO` | `selection_event`      | `AllocationDTO`      | `allocation_event`     |
| Execution  | 8     | `AllocationDTO`      | `allocation_event`     | `ExecutionResultDTO` | `execution_event`      |
| Position   | 9     | `ExecutionResultDTO` | `execution_event`      | `PositionStateDTO`   | `position_event`       |

All DTOs carry `TraceID`, `CorrelationID`, `CausationID`, `VersionID`. See `docs/dto_contracts.md` for full field definitions.

### Lifecycle Transitions

| Worker     | From State      | To State        | Condition              | Adapter Call      |
| ---------- | --------------- | --------------- | ---------------------- | ----------------- |
| DQ         | `DETECTED`      | `DQ_PASSED`     | Decision ≠ REJECT      | `TransitionState` |
| DQ         | `DETECTED`      | `REJECTED`      | Decision = REJECT      | `TransitionState` |
| Features   | `DQ_PASSED`     | `FEATURE_READY` | always                 | `TransitionState` |
| Edge       | `FEATURE_READY` | `EDGE_DETECTED` | ok=true                | `TransitionState` |
| Edge       | `FEATURE_READY` | `REJECTED`      | ok=false               | `TransitionState` |
| Validation | `EDGE_DETECTED` | `VALIDATED`     | Decision=ACCEPT        | `TransitionState` |
| Validation | `EDGE_DETECTED` | `REJECTED`      | Decision=REJECT        | `TransitionState` |
| Selection  | `VALIDATED`     | `SELECTED`      | Selected=true          | `TransitionState` |
| Execution  | `SELECTED`      | `EXECUTED`      | Status=confirmed       | `TransitionState` |
| Execution  | `SELECTED`      | `FAILED`        | Status=reverted/failed | `TransitionState` |
| Position   | `EXECUTED`      | `POSITION_OPEN` | position opened        | `TransitionState` |

**Note:** Phase 2 calls `TransitionState` best-effort (failure logged but does not halt the token). Phase 3 makes all transitions mandatory.

### Traceability

All DTOs emitted in Phase 2 MUST propagate these fields:

| Field           | Rule                                                                            |
| --------------- | ------------------------------------------------------------------------------- |
| `TraceID`       | Initialized at Layer 0 ingestion (`TraceID = EventID`). Copy forward unchanged. |
| `CorrelationID` | Copy from input DTO unchanged.                                                  |
| `CausationID`   | Set to `inputEvent.EventID` (the event being consumed by this worker).          |
| `VersionID`     | Pinned at worker startup via `adapter.GetActiveStrategyVersion()`.              |

Phase 2 best-effort propagation only. Phase 3 adds adapter-level enforcement (`ErrMissingTraceField`).

### 2.1 Data Quality (Layer 1, minimal)

**File Structure**

```
contracts/
└── data_quality.go

internal/modules/data_quality/
├── data_quality.go                        // Process(ctx, MarketDataDTO) → DataQualityDTO
├── honeypot.go                            // Static call simulation of sell path
├── fake_liquidity.go                      // LP holder / LP-locked check
├── tax_reject.go                          // Hard reject if tax > max_tax_pct
├── internal/simulation.go                 // eth_call helpers
└── data_quality_test.go

internal/workers/
└── run_data_quality.go
```

**Function Contracts**

```go
type Module struct { /* ... */ }
func New(cfg Config, adapter database.ReadOnlyAdapter) *Module
func (m *Module) Process(ctx context.Context, in contracts.MarketDataDTO) (contracts.DataQualityDTO, error)

// Internal helpers (package-private)
func (m *Module) checkHoneypot(ctx context.Context, token, base string, chain string) bool
func (m *Module) checkFakeLiquidity(ctx context.Context, pool string, chain string) bool
func (m *Module) readTaxBps(ctx context.Context, token string, chain string) (buyBps, sellBps int32, err error)
```

**DTO Flow**

```
Input:  event_type = "market_data_event"        payload = MarketDataDTO
Output: event_type = "data_quality_event"       payload = DataQualityDTO

Propagation:
  TraceID        ← MarketDataDTO.TraceID
  CorrelationID  ← MarketDataDTO.CorrelationID
  CausationID    ← marketDataEvent.EventID
  VersionID      ← MarketDataDTO.VersionID
```

**Worker Flow**

```
1. adapter.ClaimNextEvent("data_quality_worker", ["market_data_event"])
2. contracts.DecodeMarketData(evt.Payload)
3. adapter.StartLifecycle(marketDataDTO)  →  lifecycleID      // creates lifecycle at state DETECTED
4. dqDTO := module.Process(ctx, marketDataDTO)
5. if dqDTO.Decision == "REJECT":
      adapter.TransitionState(lifecycleID, DETECTED → REJECTED, reason)
6. else:
      adapter.TransitionState(lifecycleID, DETECTED → DQ_PASSED)
7. adapter.InsertDataQuality(dqDTO)
8. adapter.InsertEvent(Event{Type: "data_quality_event", Payload: dqDTO, CausationID: evt.EventID})
9. adapter.MarkEventProcessed(evt.EventID)
```

**Adapter Calls (Complete)**

```
adapter.ClaimNextEvent(ctx, "data_quality_worker", ["market_data_event"])
adapter.StartLifecycle(ctx, dto)                             // creates token_lifecycle at DETECTED
adapter.TransitionState(ctx, request)                       // DETECTED→DQ_PASSED or DETECTED→REJECTED
adapter.InsertDataQuality(ctx, dto)
adapter.InsertEvent(ctx, event)
adapter.MarkEventProcessed(ctx, eventID)
```

**Failure Handling**

- RPC error on static call → retry 3× with jittered backoff; final failure → `dqDTO.Decision = REJECT`, `RejectReasons = ["rpc_unavailable"]`
- Normalization error (missing reserves) → REJECT with reason
- Panic → recover, log, mark event failed (do not advance)

**Exit Criteria (2.1)**

- [ ] Every `market_data_event` produces exactly one `data_quality_event`
- [ ] Zero DB driver imports in `internal/modules/data_quality/`

---

### 2.2 Feature Extraction (Layer 2, minimal 5 features)

**File Structure**

```
contracts/
└── feature.go

internal/modules/features/
├── features.go                            // Process(ctx, DataQualityDTO) → FeatureDTO
├── normalize.go                           // Raw → [0,1] with rolling-window clamp
├── liquidity.go
├── tx_velocity.go
├── holder_count.go
├── contract_flags.go
└── features_test.go

internal/workers/
└── run_features.go
```

**Function Contracts**

```go
func (m *Module) Process(ctx context.Context, in contracts.DataQualityDTO) (contracts.FeatureDTO, error)
```

Emits 5 features only in Phase 2: `LiquidityScore`, `TxVelocityScore`, `ContractSafety`, `TokenAge`, `VolumeMomentum`. Remaining fields set to zero value; `Confidence.*` = 0.0 for unfilled fields.

**DTO Flow**

```
Input:  "data_quality_event"   → DataQualityDTO   (only decision ∈ {PASS, RISKY_PASS} proceeds; REJECT stops)
Output: "feature_event"        → FeatureDTO
```

**Worker Flow**

```
1. ClaimNextEvent("features_worker", ["data_quality_event"])
2. Decode DataQualityDTO; if Decision == REJECT → MarkEventProcessed & skip
3. TransitionState(DQ_PASSED → FEATURE_READY)
4. featureDTO := module.Process(ctx, dqDTO)
5. adapter.InsertFeature(featureDTO)
6. adapter.InsertEvent("feature_event", featureDTO, CausationID=evt.EventID)
7. MarkEventProcessed
```

**Adapter Calls (Complete)**

```
adapter.ClaimNextEvent(ctx, "features_worker", ["data_quality_event"])
adapter.TransitionState(ctx, request)        // DQ_PASSED→FEATURE_READY
adapter.InsertFeature(ctx, dto)
adapter.InsertEvent(ctx, event)
adapter.MarkEventProcessed(ctx, eventID)
```

**Exit Criteria (2.2)**

- [ ] All 5 minimum features populated with values in `[0.0, 1.0]`
- [ ] Deterministic: same `DataQualityDTO` → same `FeatureDTO`

---

### 2.3 Edge Discovery (Layer 3, single rule)

**File Structure**

```
contracts/
└── edge.go

internal/modules/edge/
├── edge.go                                // Process(ctx, FeatureDTO) → (EdgeDTO, ok)
├── new_launch_rule.go                     // Single Phase-2 rule
└── edge_test.go

internal/workers/
└── run_edge.go
```

**Function Contracts**

```go
func (m *Module) Process(ctx context.Context, in contracts.FeatureDTO) (edge contracts.EdgeDTO, ok bool, err error)
```

`ok = false` when no edge detected → worker does NOT emit an event (pipeline stops for this token, lifecycle transitions to REJECTED with reason `no_edge`).

**Phase-2 rule:**

```
ok = (TokenAge * maxAgeSeconds < config.edge.max_age_seconds) AND
     (TxVelocityScore > config.edge.min_velocity_score) AND
     (LiquidityScore > config.edge.min_liquidity_score)
EdgeStrength   = 0.5 * TxVelocityScore + 0.5 * VolumeMomentum
EdgeConfidence = min(Confidence.TxVelocityScore, Confidence.LiquidityScore)
```

**DTO Flow**

```
Input:  "feature_event"  → FeatureDTO
Output: "edge_event"     → EdgeDTO   (only when ok == true)
```

**Worker Flow**

```
1. ClaimNextEvent → FeatureDTO
2. TransitionState(FEATURE_READY → EDGE_DETECTED) only on ok; else FEATURE_READY → REJECTED
3. On ok: InsertEdge, InsertEvent("edge_event"), MarkEventProcessed
4. On !ok: MarkEventProcessed (no output event; lifecycle terminal)
```

**Adapter Calls (Complete)**

```
adapter.ClaimNextEvent(ctx, "edge_worker", ["feature_event"])
adapter.TransitionState(ctx, request)    // FEATURE_READY→EDGE_DETECTED (ok) or FEATURE_READY→REJECTED (!ok)
adapter.InsertEdge(ctx, dto)             // only when ok == true
adapter.InsertEvent(ctx, event)          // only when ok == true
adapter.MarkEventProcessed(ctx, eventID)
```

**Exit Criteria (2.3)**

- [ ] Edge detection deterministic given identical inputs
- [ ] Rejected paths recorded with `terminal_reason = "no_edge"` in `token_lifecycle`

---

### 2.4 Edge Validation (Layer 5, fixed priors)

**File Structure**

```
contracts/
└── validated_edge.go

internal/modules/validation/
├── validation.go                          // Process(ctx, EdgeDTO) → ValidatedEdgeDTO
├── ev_gate.go
└── validation_test.go

internal/workers/
└── run_validation.go
```

**Function Contracts**

```go
func (m *Module) Process(ctx context.Context, in contracts.EdgeDTO) (contracts.ValidatedEdgeDTO, error)
```

Phase 2 uses **fixed priors from config** (real model deferred to Phase 4):

```
P                 = config.validation.prior_probability   (e.g., 0.35)
ExpectedGainBps   = config.validation.prior_gain_bps      (e.g., 3000)
ExpectedLossBps   = config.validation.prior_loss_bps      (e.g., 4000)
FixedCostsBps     = config.execution.fixed_costs_bps      (e.g., 150)
SlippageP95       = config.validation.prior_slippage_bps  (e.g., 200)

EV = P × ExpectedGainBps - (1-P) × ExpectedLossBps - FixedCostsBps - SlippageP95
Decision = "ACCEPT" if EV > config.validation.ev_threshold_bps else "REJECT"
```

**DTO Flow**

```
Input:  "edge_event"            → EdgeDTO
Output: "validated_edge_event"  → ValidatedEdgeDTO
```

**Worker Flow**

```
1. ClaimNextEvent("validation_worker", ["edge_event"])
2. dto := DecodeEdge(evt.Payload)
3. output := module.Process(ctx, dto)       // EV gate with fixed priors
4. if output.Decision == "ACCEPT":
      TransitionState(EDGE_DETECTED → VALIDATED)
      InsertValidatedEdge(output)
      InsertEvent("validated_edge_event", output, CausationID=evt.EventID)
   else:
      TransitionState(EDGE_DETECTED → REJECTED, output.RejectReason)
      // no output event — lifecycle terminates
5. MarkEventProcessed(evt.EventID)
```

**Adapter Calls (Complete)**

```
adapter.ClaimNextEvent(ctx, "validation_worker", ["edge_event"])
adapter.TransitionState(ctx, request)       // EDGE_DETECTED→VALIDATED or EDGE_DETECTED→REJECTED
adapter.InsertValidatedEdge(ctx, dto)       // only on ACCEPT
adapter.InsertEvent(ctx, event)             // only on ACCEPT
adapter.MarkEventProcessed(ctx, eventID)
```

**Exit Criteria (2.4)**

- [ ] Every `edge_event` produces exactly one `validated_edge_event`
- [ ] EV computation deterministic and replayable

---

### 2.5 Selection (Layer 6, top 1)

**File Structure**

```
contracts/
└── selection.go

internal/modules/selection/
├── selection.go                           // Process(ctx, ValidatedEdgeDTO) → SelectionOutputDTO
├── concurrency_gate.go                    // Blocks selection if max_open_positions reached
└── selection_test.go

internal/workers/
└── run_selection.go
```

**Function Contracts**

```go
func (m *Module) Process(ctx context.Context, in contracts.ValidatedEdgeDTO, openCount int) (contracts.SelectionOutputDTO, error)
```

Phase 2: `max_open_positions = 1`. If `openCount >= 1`, `Selected = false, RejectReason = "max_positions_reached"`.

**DTO Flow**

```
Input:  "validated_edge_event"  → ValidatedEdgeDTO  (ACCEPT only)
Output: "selection_event"       → SelectionOutputDTO
```

**Worker Flow**

```
1. ClaimNextEvent("selection_worker", ["validated_edge_event"])
2. Decode; if Decision == REJECT → skip
3. openCount := len(adapter.GetOpenPositions(ctx))
4. selectionDTO := module.Process(ctx, vEdge, openCount)
5. If selectionDTO.Selected: TransitionState(VALIDATED → SELECTED)
6. InsertSelection + InsertEvent + MarkEventProcessed
```

**Adapter Calls (Complete)**

```
adapter.ClaimNextEvent(ctx, "selection_worker", ["validated_edge_event"])
adapter.GetOpenPositions(ctx)                   // check concurrency gate
adapter.TransitionState(ctx, request)           // VALIDATED→SELECTED (only if Selected==true)
adapter.InsertSelection(ctx, dto)
adapter.InsertEvent(ctx, event)
adapter.MarkEventProcessed(ctx, eventID)
```

**Exit Criteria (2.5)**

- [ ] `Selected = true` iff `openCount < max_open_positions`

---

### 2.6 Capital (Layer 7, fixed size)

**File Structure**

```
contracts/
└── allocation.go

internal/modules/capital/
├── capital.go                             // Process(ctx, SelectionOutputDTO) → AllocationDTO
└── capital_test.go

internal/workers/
└── run_capital.go
```

**Function Contracts**

```go
func (m *Module) Process(ctx context.Context, in contracts.SelectionOutputDTO) (contracts.AllocationDTO, error)
```

Phase 2: single wallet from `config.capital.wallet_address`. Fixed size from `config.capital.fixed_entry_size_usd`.

```
ExecutionID = SHA256(CorrelationID)[:16]
SizeUsd     = config.capital.fixed_entry_size_usd
```

**DTO Flow**

```
Input:  "selection_event"  → SelectionOutputDTO  (Selected == true)
Output: "allocation_event" → AllocationDTO
```

**Worker Flow**

```
1. ClaimNextEvent("capital_worker", ["selection_event"])
2. dto := DecodeSelection(evt.Payload)
3. if !dto.Selected → MarkEventProcessed & skip
4. output := module.Process(ctx, dto)   // CheckEnvelope + fixed sizing
5. InsertAllocation(output)
6. InsertEvent("allocation_event", output, CausationID=evt.EventID)
7. MarkEventProcessed(evt.EventID)
```

**Failure Handling**

- Capital envelope check fails (`SizeUsd > config.capital.max_size_usd`) → emit `allocation_event{Status:"blocked", ErrorCode:"envelope_exceeded"}`; do not advance lifecycle.
- Module panic → recover, log, mark event failed (do not advance).

**Adapter Calls (Complete)**

```
adapter.ClaimNextEvent(ctx, "capital_worker", ["selection_event"])
adapter.InsertAllocation(ctx, dto)
adapter.InsertEvent(ctx, event)
adapter.MarkEventProcessed(ctx, eventID)
```

**Exit Criteria (2.6)**

- [ ] Same CorrelationID → same ExecutionID (idempotent)

---

### 2.7 Execution (Layer 8, minimal but real)

**File Structure**

```
contracts/
└── execution.go

internal/modules/execution/
├── execution.go                           // Process(ctx, AllocationDTO) → ExecutionResultDTO
├── nonce.go                               // Atomic DB-backed nonce allocation (§ 3.8.19)
├── gas.go                                 // EIP-1559 estimation (§ 3.8.20)
├── build_calldata.go                      // Uniswap V2 swapExactETHForTokens calldata
├── submit.go                              // Sign + sendRawTransaction
├── wait_receipt.go                        // Poll for confirmation
├── internal/
│   └── abi.go
└── execution_test.go

internal/workers/
└── run_execution.go

database/migrations/
└── 20260101000003_trading_tables.sql      // wallet_nonce_state, executions, positions, tokens
```

**Function Contracts**

```go
func (m *Module) Process(ctx context.Context, in contracts.AllocationDTO) (contracts.ExecutionResultDTO, error)

// internal/modules/execution/nonce.go
type NonceAllocator interface {
    Allocate(ctx context.Context, wallet, chain string) (uint64, error)
    Reconcile(ctx context.Context, wallet, chain string) error
}

// internal/modules/execution/gas.go
type GasEstimator interface {
    Estimate(ctx context.Context, chain string) (maxFeeWei, priorityFeeWei *big.Int, err error)
}
```

**Phase 2 scope (minimal):**

- Single attempt (no replacement loop — deferred to Phase 3)
- Public mempool only (private routing → Phase 4)
- Slippage cap embedded in calldata via `amountOutMin`
- Nonce allocation IS present (prerequisite for correctness)

**DTO Flow**

```
Input:  "allocation_event"  → AllocationDTO
Output: "execution_event"   → ExecutionResultDTO
```

**Worker Flow**

```
1. ClaimNextEvent("execution_worker", ["allocation_event"])
2. allocDTO := decode
   lc := adapter.GetLifecycleByToken(ctx, allocDTO.TokenAddress)
   if lc.CurrentState != "SELECTED" {         // CAS pre-check — prevent duplicate execution
       adapter.MarkEventProcessed(ctx, evt.EventID); return nil
   }
3. nonce, err := adapter.AllocateNonce(ctx, allocDTO.WalletAddress, allocDTO.Chain)
      // AllocateNonce: SELECT nonce_value FOR UPDATE, increment, UPDATE wallet_nonce_state
      // Returns ErrNonceLocked if wallet has an in-flight tx (Phase 2: single wallet = no overlap)
4. if err != nil: mark event failed, emit execution_event{Status:"failed", ErrorCode:"nonce_alloc_error"}; continue
5. maxFee, prioFee, _ := gasEstimator.Estimate(ctx, allocDTO.Chain)
6. calldata := buildCalldata(allocDTO)
7. tx := signTx(wallet, nonce, maxFee, prioFee, calldata)
8. txHash, rpcErr := rpc.SendRawTransaction(tx)
9. if rpcErr matches "nonce too low":
      actualNonce, _ := rpc.GetTransactionCount(wallet, "latest")
      adapter.ReconcileNonce(ctx, allocDTO.WalletAddress, allocDTO.Chain, actualNonce)
      // ReconcileNonce: UPDATE wallet_nonce_state SET nonce_value = $actual WHERE wallet_address = $w
      // Requeue: reinsert allocation_event for retry (do NOT mark processed)
      continue
10. receipt := waitReceipt(txHash, timeout=config.execution.receipt_timeout_ms)
11. result := ExecutionResultDTO{
         Status: receipt.Status == 1 ? "confirmed" : "reverted",
         TxHash: txHash, Attempts: 1, NonceUsed: nonce,
         FinalGasUsed: receipt.GasUsed,
         LatencyMs: ...,
         RealizedEntryPrice: decodeFromLogs(receipt),
    }
12. adapter.InsertExecutionResult(result)
13. adapter.InsertEvent("execution_event", result)
14. TransitionState(SELECTED → EXECUTED) on confirmed; SELECTED → FAILED otherwise
15. MarkEventProcessed
```

**DB Calls**

```
adapter.GetLifecycleByToken(ctx, tokenAddress)                // step 2 — CAS pre-check (idempotency guard)
adapter.AllocateNonce(ctx, wallet, chain)                     // step 3 — atomic increment in wallet_nonce_state
adapter.ReconcileNonce(ctx, wallet, chain, actualNonce)       // step 9 — sync DB nonce with on-chain truth
adapter.InsertExecutionResult(ctx, result)                    // step 12
adapter.InsertEvent(ctx, event)                               // step 13
adapter.TransitionState(ctx, request)                         // step 14
adapter.MarkEventProcessed(ctx, eventID)                      // step 15
```

**Failure Handling (Phase 2 minimal)**

**Failure Classification — enforced in all execution workers:**

| Classification | Conditions                                                           |
| -------------- | -------------------------------------------------------------------- |
| **RETRIABLE**  | nonce too low, tx underpriced, network timeout, receipt timeout      |
| **FATAL**      | revert, insufficient balance, invalid calldata, gas exceeds hard cap |

_Rule: FATAL → immediately emit `execution_event{Status:"failed"}` and transition `SELECTED→FAILED`; no retry. RETRIABLE → handle per condition below; Phase 3 adds bounded retry loop._

| Condition                  | Classification  | Action                                                                                    |
| -------------------------- | --------------- | ----------------------------------------------------------------------------------------- |
| `AllocateNonce` error      | RETRIABLE       | Mark event failed; emit `execution_event{Status:"failed", ErrorCode:"nonce_alloc_error"}` |
| `sendRawTransaction` error | FATAL (Phase 2) | Status = `"failed"`, ErrorCode from RPC; no retry in Phase 2                              |
| Nonce too low (RPC error)  | RETRIABLE       | `ReconcileNonce` with on-chain actual; requeue event; do NOT mark processed               |
| Receipt timeout            | RETRIABLE       | Status = `"dropped"`, ErrorCode = `"timeout"`; Phase 3 adds replacement loop              |
| Tx reverted                | FATAL           | Status = `"reverted"`; lifecycle `SELECTED→FAILED`; position worker uses loss-exit path   |
| Insufficient balance       | FATAL           | Status = `"failed"`, ErrorCode = `"insufficient_balance"`; lifecycle `SELECTED→FAILED`    |
| Invalid calldata           | FATAL           | Status = `"failed"`, ErrorCode = `"invalid_calldata"`; lifecycle `SELECTED→FAILED`; alert |

**Exit Criteria (2.7)**

- [ ] Successful swap tx observable on testnet explorer
- [ ] `tx_hash` populated; receipt confirmed
- [ ] Nonce monotonic per wallet (verified by query)
- [ ] `ON CONFLICT (execution_id) DO NOTHING` verified idempotent

---

### 2.8 Position (Layer 9, TP/SL/TIME)

**File Structure**

```
contracts/
└── position.go

internal/modules/position/
├── position.go                            // OpenPosition(ExecutionResultDTO), PollExit(PositionStateDTO)
├── tp_sl.go                               // Static exit rules
├── time_exit.go                           // Max hold duration
├── sell_tx.go                             // Build + submit sell tx
└── position_test.go

internal/workers/
├── run_position_open.go                   // Triggered by execution_event
└── run_position_poll.go                   // Periodic (cron) — poll open positions
```

**Function Contracts**

```go
func (m *Module) OpenPosition(ctx context.Context, in contracts.ExecutionResultDTO) (contracts.PositionStateDTO, error)
func (m *Module) PollExit(ctx context.Context, current contracts.PositionStateDTO, currentPrice string) (contracts.PositionStateDTO, error)
```

Phase 2 exit rules:

```
ExitReason = "TP1"  if current_price >= entry_price * (1 + config.position.tp1_pct / 100)
ExitReason = "SL"   if current_price <= entry_price * (1 - config.position.sl_pct / 100)
ExitReason = "TIME" if now - opened_at >= config.position.max_hold_seconds
```

**DTO Flow**

```
Input A (open): "execution_event"       → ExecutionResultDTO  (Status == "confirmed")
Output A:       "position_event" (open) → PositionStateDTO (Status=open)

Input B (poll): current PositionStateDTO + live price
Output B:       "position_event" (snapshot/exit) → PositionStateDTO
```

**Worker Flow (open)**

```
1. ClaimNextEvent("position_open_worker", ["execution_event"])
2. exec := decode; if !exec.Success → skip
   lc := adapter.GetLifecycleByToken(ctx, exec.TokenAddress)
   if lc.CurrentState != "EXECUTED" {          // CAS pre-check — prevent duplicate position open
       adapter.MarkEventProcessed(ctx, evt.EventID); return nil
   }
3. posDTO := OpenPosition(ctx, exec)
4. adapter.InsertPositionState(posDTO)
5. TransitionState(EXECUTED → POSITION_OPEN)
6. InsertEvent("position_event", posDTO)
7. MarkEventProcessed
```

**Worker Flow (poll — runs every `config.position.poll_interval_seconds`)**

```
For each position in adapter.GetOpenPositions(ctx):
  1. currentPrice := rpc.QueryPoolPrice(position.TokenAddress, position.Chain)
  2. next := PollExit(ctx, position, currentPrice)
  3. if next.Status == "exited":
       a. Submit sell tx (same flow as entry, opposite direction)
       b. InsertPositionState(next) with ExitPrice, PnlUsd, PnlPct populated
       c. TransitionState(POSITION_OPEN → POSITION_CLOSED)
       d. InsertEvent("position_event", next)
  4. else if state changed (snapshot only):
       InsertPositionState(next)
       InsertEvent("position_event", next)
```

**Adapter Calls (Complete)**

```
// Position Open Worker:
adapter.ClaimNextEvent(ctx, "position_open_worker", ["execution_event"])
adapter.GetLifecycleByToken(ctx, tokenAddress)                // CAS pre-check — state must be EXECUTED
adapter.InsertPositionState(ctx, dto)
adapter.TransitionState(ctx, request)        // EXECUTED→POSITION_OPEN
adapter.InsertEvent(ctx, event)
adapter.MarkEventProcessed(ctx, eventID)

// Position Poll Worker (periodic — no ClaimNextEvent):
adapter.GetOpenPositions(ctx)                // enumerate all open positions
adapter.InsertPositionState(ctx, dto)        // every snapshot and exit
adapter.TransitionState(ctx, request)        // POSITION_OPEN→POSITION_CLOSED (on exit only)
adapter.InsertEvent(ctx, event)              // every snapshot and exit
// No MarkEventProcessed — periodic worker, not event-driven
```

**Exit Criteria (2.8)**

- [ ] Position opens on every `execution_event` with `Success == true`
- [ ] Sell tx submitted when TP1 / SL / TIME met
- [ ] `pnl_usd` and `pnl_pct` populated on exit
- [ ] Lifecycle reaches `POSITION_CLOSED` state

---

### 2.9 Phase 2 Migration — `20260101000003_trading_tables.sql`

Adds `wallet_nonce_state`, `executions`, `positions`, `tokens`. See `docs/db_adapter_spec.md` § 6.3, § 6.5, § 6.6.

### 2.10 Phase 2 Orchestrator Wiring

```go
// cmd/server.go (excerpt)
orch := orchestrator.Boot(...)
orch.RegisterWorker("ingestion",          workers.RunIngestion)
orch.RegisterWorker("data_quality",       workers.RunDataQuality)
orch.RegisterWorker("features",           workers.RunFeatures)
orch.RegisterWorker("edge",               workers.RunEdge)
orch.RegisterWorker("validation",         workers.RunValidation)
orch.RegisterWorker("selection",          workers.RunSelection)
orch.RegisterWorker("capital",            workers.RunCapital)
orch.RegisterWorker("execution",          workers.RunExecution)
orch.RegisterWorker("position_open",      workers.RunPositionOpen)
orch.RegisterWorker("position_poll",      workers.RunPositionPoll)  // cron-style
orch.Run(ctx)
```

### 2.11 Phase 2 Exit Criteria (Cumulative — **FIRST TRADE GATE**)

- [ ] **At least 1 real on-chain swap executes end-to-end on testnet** (Goerli or BSC testnet)
- [ ] Full causal chain observable: `market_data_event → data_quality_event → feature_event → edge_event → validated_edge_event → selection_event → allocation_event → execution_event → position_event (open) → position_event (exit)`
- [ ] Every DTO has non-empty `TraceID`, `CorrelationID`, `VersionID`; non-root DTOs have non-empty `CausationID`
- [ ] Replay: feed the same Phase 1 fixture → produces bit-for-bit identical DTOs (execution bypassed in replay mode via config flag)
- [ ] `grep -r 'import.*database' internal/modules/` returns zero matches
- [ ] `grep -rnE 'INSERT|SELECT|UPDATE|DELETE' internal/modules/` returns zero matches (no SQL in modules)
- [ ] All thresholds in `config/pipeline.yaml`; no magic numbers in module code
- [ ] `go test ./internal/modules/...` passes
- [ ] **Section I — Standard Execution Quality Gate:**
  - [ ] Lifecycle transitions: every `TransitionState` call observable in `token_state_transition` audit table; no state skips
  - [ ] Events emitted: `SELECT event_type, COUNT(*) FROM events GROUP BY event_type` shows expected counts matching processed input events
  - [ ] Adapter calls: all writes go through adapter methods; zero `database/` imports in `internal/modules/`
  - [ ] Trace propagation: `SELECT * FROM events WHERE trace_id = ? ORDER BY created_at` produces a contiguous causal chain for any token

---

## Phase 3 — Evaluation & Correctness (P1.5)

**Architecture:** § 4.7 State Machine, § 4.8 Traceability, § 3.8.19–3.8.25 Execution Realism, § 0.4 Feedback Loop (Evaluate step)

### Objective

Harden Phase 2 for production safety: enforce the token lifecycle state machine with CAS guards, reject orphan events, add transaction replacement / retry, expand `ExecutionResultDTO`. Implement the **Evaluation Engine** — the mandatory pre-learning step that computes `PredictionError`, `FalsePositive`, `FalseNegative`, and `ExecutionError` from every exited position into an `EvaluationDTO`. Add priority-aware event ordering and improved latency window computation.

### BLOCKERS

**Phase 2 exit criteria must all be checked before starting Phase 3.**

Specifically: at least 1 real testnet trade confirmed end-to-end, full causal chain observable in `events`, all modules have zero `database/` imports and zero SQL, TTL and minimal capital envelope active, `go test ./internal/modules/...` passes.

### Scope

**In scope:** Token lifecycle state machine with CAS guards, traceability enforcement (orphan rejection), transaction retry/replacement loop, Telegram dispatcher, priority-aware `ClaimNextEvent` ordering (exits before entries), improved `OpportunityWindowMs` computation, circuit breaker per RPC endpoint.

**Explicitly excluded:** Full probability/slippage models (Phase 4), learning feedback (Phase 5), wallet sharding (Phase 6), kill switch / operational modes (Phase 6). State machine is enforced here — it was present in Phase 2 as best-effort only.

### Event Types Emitted

| Event Type         | Emitter              | DTO / Notes                              |
| ------------------ | -------------------- | ---------------------------------------- |
| `telegram_event`   | any worker (alert)   | freeform alert payload                   |
| `system_violation` | state machine module | invalid transition detected              |
| `quarantine_event` | state machine module | token quarantined after N violations     |
| `evaluation_event` | evaluation engine    | `EvaluationDTO` — mandatory pre-learning |

All Phase 2 event types (`market_data_event` through `position_event`) are still emitted; Phase 3 adds enforcement around them.

### Priority-Aware Event Processing (from § 7.1)

Add to `internal/resource_control/`:

```go
// internal/resource_control/priority.go
func ComputePriority(eventType string, expiresAt string, now time.Time) int
// Returns base weight from config/priority.yaml plus urgency bonus:
//   priority = base_weight + clamp((expires_at - now) / max_ttl, 0, 1) * urgency_coef
// Exit-path events (position_event exit, execution_replacement) → PRIORITY_EXIT ≥ 900 (never dropped)
```

Every emitting worker calls `ComputePriority()` and sets `Event.Priority` before `adapter.InsertEvent()`. Workers that emit exit-path events use `PRIORITY_EXIT` constant from `config/priority.yaml`.

`adapter.ClaimNextEvent()` ordering updated (adapter-side change only — no module SQL):

```sql
ORDER BY priority DESC, created_at ASC
FOR UPDATE SKIP LOCKED LIMIT 1
```

**config/priority.yaml base weights:**

| Event Type              | Base Priority |
| ----------------------- | ------------- |
| `position_event` (exit) | 1000          |
| `execution_replacement` | 900           |
| `position_event` (open) | 500           |
| `allocation_event`      | 400           |
| `validated_edge_event`  | 300           |
| `edge_event`            | 200           |
| `feature_event`         | 150           |
| `data_quality_event`    | 120           |
| `market_data_event`     | 100           |
| `adjustment_event`      | 50            |

### Improved Latency Window (from § 7.5)

Refine `EdgeDTO.OpportunityWindowMs` computation to account for current RPC latency:

```go
// internal/modules/edge/new_launch_rule.go (update)
opportunity_window_ms = cfg.edge.base_window_ms *
    (1 + edge_strength * cfg.edge.window_momentum_factor) -
    latency_overhead_ms   // subtract measured RPC overhead from Phase 1 watermark
```

Validation rejects when `latencyProfile.P95Ms > edge.OpportunityWindowMs` (same check as Phase 2, now with dynamic window). Full P/S/L model replaces static profile in Phase 4.

### File Structure

```
internal/modules/state_machine/
├── state_machine.go                       // ValidateTransition, ApplyTransition
├── transitions.go                         // Allowed-transition matrix (const)
├── quarantine.go                          // Quarantine logic
└── state_machine_test.go

internal/modules/traceability/
├── validator.go                           // Validate(dto) → reject orphans / missing fields
└── validator_test.go

internal/modules/execution/
├── replacement.go                         // Same-nonce bump (§ 3.8.21)
├── retry.go                               // State machine: confirmed/reverted/stuck/dropped (§ 3.8.23)
└── circuit_breaker.go                     // Per-endpoint breaker

internal/telegram/
├── dispatcher.go                          // Reads telegram_event from bus, sends messages
├── commands.go                            // /status, /pnl, /positions, /kill, /resume, /version
└── bot.go

database/migrations/
└── 20260101000004_state_machine.sql       // token_lifecycle, token_state_transition, token_state_violation

internal/modules/evaluation/
├── evaluation.go                          // Process(ctx, PositionStateDTO) → EvaluationDTO (joins stored ExecutionResultDTO)
└── evaluation_test.go                     // Unit tests with fixture DTOs; zero DB, zero network

internal/workers/
└── run_evaluation.go                      // Consumes position_event (Status=exited); emits evaluation_event
```

### Function Contracts

```go
// internal/modules/state_machine/state_machine.go
var AllowedTransitions = map[string][]string{
    "DETECTED":        {"DQ_PASSED", "REJECTED"},
    "DQ_PASSED":       {"FEATURE_READY", "REJECTED"},
    "FEATURE_READY":   {"EDGE_DETECTED", "REJECTED"},
    "EDGE_DETECTED":   {"VALIDATED", "REJECTED"},
    "VALIDATED":       {"SELECTED", "REJECTED"},
    "SELECTED":        {"EXECUTED", "FAILED"},
    "EXECUTED":        {"POSITION_OPEN", "FAILED"},
    "POSITION_OPEN":   {"POSITION_CLOSED", "FAILED"},
    "POSITION_CLOSED": {"EVALUATED"},
    "EVALUATED":       {}, // terminal
    "REJECTED":        {}, // terminal
    "FAILED":          {}, // terminal
}

func ValidateTransition(from, to string) error
// Adapter implementation:
func (a *PostgresAdapter) TransitionState(ctx context.Context, req TransitionRequest) error {
    // UPDATE token_lifecycle
    //    SET current_state = $new, state_version = state_version + 1, updated_at = CURRENT_TIMESTAMP
    //  WHERE token_lifecycle_id = $id
    //    AND current_state      = $expected_from
    //    AND state_version      = $expected_version
    // RETURNING state_version
    // If 0 rows → ErrInvalidTransition
    // Also INSERT INTO token_state_transition (audit row)
}

// internal/modules/traceability/validator.go
func ValidateTrace(dto any) error  // enforces TraceID/CorrelationID/CausationID/VersionID rules

// internal/modules/execution/replacement.go
func (m *Module) Replace(ctx context.Context, stuck StuckTx) (ExecutionResultDTO, error)
```

### 3.1 Evaluation Engine (Layer 10 — pre-learning gate)

**Purpose:** Produce `EvaluationDTO` from every exited position. Phase 5 Learning Engine MUST NOT run without `evaluation_event` as input — this phase makes that possible.

**File:** `internal/modules/evaluation/evaluation.go`

```go
// Process joins PositionStateDTO (exit) with stored ExecutionResultDTO
// and computes all four error signals.
func (m *Module) Process(ctx context.Context, pos contracts.PositionStateDTO) (contracts.EvaluationDTO, error)
```

**Computations:**

| Metric            | Formula                                                                                                             |
| ----------------- | ------------------------------------------------------------------------------------------------------------------- |
| `PredictionError` | `WinProbability (from ProbabilityEstimateDTO) − (1.0 if PnlPct > 0 else 0.0)`                                       |
| `FalsePositive`   | `AcceptedByPipeline=true AND PnlPct < cfg.evaluation.fp_loss_threshold_pct`                                         |
| `FalseNegative`   | Retrieved from `shadow_trades` — rejected tokens that subsequently pumped `> cfg.evaluation.fn_gain_threshold_pct`  |
| `ExecutionError`  | `AllocationDTO.SlippageBps − realizedSlippageBps` (realizedSlippageBps from `ExecutionResultDTO.ActualSlippageBps`) |
| `Expectancy`      | `P × avgWin − (1−P) × avgLoss` per cohort over rolling window `cfg.evaluation.window_seconds`                       |

**Worker:** `internal/workers/run_evaluation.go`

- Consumes: `position_event` (Status=exited only)
- Produces: `evaluation_event` → payload `EvaluationDTO`

```go
// internal/workers/run_evaluation.go
func RunEvaluation(ctx context.Context, adapter database.Adapter, mod *evaluation.Module, cfg Config) error
```

### DTO Pipeline

| Worker / Stage        | Input DTO          | Input Event             | Output DTO           | Output Event       |
| --------------------- | ------------------ | ----------------------- | -------------------- | ------------------ |
| state machine (CAS)   | any DTO            | any event               | none (side-effect)   | `system_violation` |
| execution (updated)   | `AllocationDTO`    | `allocation_event`      | `ExecutionResultDTO` | `execution_event`  |
| **evaluation engine** | `PositionStateDTO` | `position_event` (exit) | `EvaluationDTO`      | `evaluation_event` |

### Lifecycle Transitions

Phase 3 **enforces** all transitions that Phase 2 applied best-effort and **adds** `POSITION_CLOSED`:

| Worker       | From State        | To State          | Condition                           | Adapter Call      |
| ------------ | ----------------- | ----------------- | ----------------------------------- | ----------------- |
| data_quality | `DETECTED`        | `DQ_PASSED`       | Decision ≠ REJECT                   | `TransitionState` |
| data_quality | `DETECTED`        | `REJECTED`        | Decision = REJECT                   | `TransitionState` |
| features     | `DQ_PASSED`       | `FEATURE_READY`   | always                              | `TransitionState` |
| edge         | `FEATURE_READY`   | `EDGE_DETECTED`   | ok=true                             | `TransitionState` |
| edge         | `FEATURE_READY`   | `REJECTED`        | ok=false                            | `TransitionState` |
| validation   | `EDGE_DETECTED`   | `VALIDATED`       | Decision=ACCEPT                     | `TransitionState` |
| validation   | `EDGE_DETECTED`   | `REJECTED`        | Decision=REJECT                     | `TransitionState` |
| selection    | `VALIDATED`       | `SELECTED`        | Selected=true                       | `TransitionState` |
| execution    | `SELECTED`        | `EXECUTED`        | Status=confirmed                    | `TransitionState` |
| execution    | `SELECTED`        | `FAILED`          | terminal failure (max replacements) | `TransitionState` |
| position     | `EXECUTED`        | `POSITION_OPEN`   | position opened                     | `TransitionState` |
| position     | `POSITION_OPEN`   | `POSITION_CLOSED` | position exited                     | `TransitionState` |
| evaluation   | `POSITION_CLOSED` | `EVALUATED`       | always (on evaluation_event emit)   | `TransitionState` |

**CAS guarantee:** `TransitionState` uses `WHERE current_state = $from AND token_lifecycle_id = $id`. If `rows_updated = 0` → insert `token_state_violation`, quarantine on Nth violation.

### Traceability

All DTOs emitted in Phase 3 MUST propagate these fields (enforced by adapter at write time):

| Field           | Rule                                                                  |
| --------------- | --------------------------------------------------------------------- |
| `TraceID`       | Copy from input DTO — never regenerated mid-pipeline                  |
| `CorrelationID` | Copy from input DTO                                                   |
| `CausationID`   | Set to `inputEvent.EventID` (the event being consumed by this worker) |
| `VersionID`     | Pinned at worker startup via `adapter.GetActiveStrategyVersion()`     |

Adapter returns `ErrMissingTraceField` on write if any field empty; `ErrOrphanEvent` if `CausationID` references a non-existent `EventID`.

### DTO Flow (Cross-Cutting)

No new DTOs introduced (EvaluationDTO is defined in `docs/dto_contracts.md` § 3.11). Phase 3 **extends the enforcement rules** that all existing DTOs must already comply with.

### Worker Flows — All Pipeline Stages with Mandatory CAS

Phase 3 upgrades every Phase 2 worker so that `TransitionState` failure **halts the token** (returns error, does not mark event processed). The following shows the complete, enforced flow for each stage.

> **CAS shorthand (§ 0.7):** All `TransitionState` calls follow the canonical pattern from **§ 0.7**. The notation `{lc.ID, STATE_A→STATE_B}` in the pseudocode below is shorthand for `TransitionRequest{LifecycleID: lc.LifecycleID, ExpectedFromState: "STATE_A", NewState: "STATE_B", ExpectedStateVersion: lc.StateVersion, ActorWorker: "<worker_group>"}`. The `ExpectedStateVersion` field is **mandatory** in all calls — never omit it.

**Data Quality Worker (updated)**

```
1. ClaimNextEvent("data_quality_worker", ["market_data_event"])   // ORDER BY priority DESC
2. dto := DecodeMarketData(evt.Payload)
3. lc, err := adapter.StartLifecycle(ctx, dto.TokenAddress, dto.TraceID)
   // ON CONFLICT (token_address) WHERE active → ErrLifecycleAlreadyActive → skip duplicate
4. output := module.Process(ctx, dto)
5. if output.Decision == "REJECT":
      if err := adapter.TransitionState(ctx, {lc.ID, DETECTED→REJECTED, reason}); err != nil {
          insertViolation(…); return err  // halt this token
      }
   else:
      if err := adapter.TransitionState(ctx, {lc.ID, DETECTED→DQ_PASSED}); err != nil { return err }
6. adapter.InsertDataQuality(output)
7. adapter.InsertEvent(ctx, {Type:"data_quality_event", Payload:output, CausationID:evt.EventID})
8. adapter.MarkEventProcessed(ctx, evt.EventID)
```

**Features Worker (updated)**

```
1. ClaimNextEvent("features_worker", ["data_quality_event"])
2. dto := DecodeDataQuality(evt.Payload); if Decision==REJECT → MarkEventProcessed & skip
3. lc := adapter.GetLifecycleByToken(ctx, dto.TokenAddress)  // pre-check state == DQ_PASSED
4. if err := adapter.TransitionState(ctx, {lc.ID, DQ_PASSED→FEATURE_READY}); err != nil { return err }
5. output := module.Process(ctx, dto)
6. adapter.InsertFeature(output)
7. adapter.InsertEvent(ctx, {Type:"feature_event", Payload:output, CausationID:evt.EventID})
8. adapter.MarkEventProcessed(ctx, evt.EventID)
```

**Edge Worker (updated)**

```
1. ClaimNextEvent("edge_worker", ["feature_event"])
2. dto := DecodeFeature(evt.Payload)
3. lc := adapter.GetLifecycleByToken(ctx, dto.TokenAddress)
4. output, ok, err := module.Process(ctx, dto)
5. if ok:
      if err := adapter.TransitionState(ctx, {lc.ID, FEATURE_READY→EDGE_DETECTED}); err != nil { return err }
      adapter.InsertEdge(output)
      adapter.InsertEvent(ctx, {Type:"edge_event", Payload:output, CausationID:evt.EventID})
   else:
      adapter.TransitionState(ctx, {lc.ID, FEATURE_READY→REJECTED, "no_edge"})
      // no output event — lifecycle terminates
6. adapter.MarkEventProcessed(ctx, evt.EventID)
```

**Validation Worker (updated)**

```
1. ClaimNextEvent("validation_worker", ["edge_event"])
2. dto := DecodeEdge(evt.Payload)
3. lc := adapter.GetLifecycleByToken(ctx, dto.TokenAddress)
4. output := module.Process(ctx, dto)       // uses fixed priors (Phase 2/3) or models (Phase 4)
5. if output.Decision == "ACCEPT":
      if err := adapter.TransitionState(ctx, {lc.ID, EDGE_DETECTED→VALIDATED}); err != nil { return err }
      adapter.InsertValidatedEdge(output)
      adapter.InsertEvent(ctx, {Type:"validated_edge_event", Payload:output, CausationID:evt.EventID})
   else:
      adapter.TransitionState(ctx, {lc.ID, EDGE_DETECTED→REJECTED, output.RejectReason})
6. adapter.MarkEventProcessed(ctx, evt.EventID)
```

**Selection Worker (updated)**

```
1. ClaimNextEvent("selection_worker", ["validated_edge_event"])
2. dto := DecodeValidatedEdge(evt.Payload); if Decision==REJECT → skip
3. lc := adapter.GetLifecycleByToken(ctx, dto.TokenAddress)
4. openCount := len(adapter.GetOpenPositions(ctx))
5. output := module.Process(ctx, dto, openCount)
6. if output.Selected:
      if err := adapter.TransitionState(ctx, {lc.ID, VALIDATED→SELECTED}); err != nil { return err }
      adapter.InsertSelection(output)
      adapter.InsertEvent(ctx, {Type:"selection_event", Payload:output, CausationID:evt.EventID})
   else:
      // Not selected — lifecycle stays VALIDATED (reusable slot); no output event
7. adapter.MarkEventProcessed(ctx, evt.EventID)
```

**Capital Worker (updated)**

```
1. ClaimNextEvent("capital_worker", ["selection_event"])
2. dto := DecodeSelection(evt.Payload)
3. if !dto.Selected → skip
4. lc := adapter.GetLifecycleByToken(ctx, dto.TokenAddress)
   if lc.CurrentState != "SELECTED" {          // CAS pre-check — validate token still in SELECTED state
       adapter.MarkEventProcessed(ctx, evt.EventID); return nil
   }
5. output := module.Process(ctx, dto)           // CheckEnvelope + sizing
6. adapter.InsertAllocation(output)
7. adapter.InsertEvent(ctx, {Type:"allocation_event", Payload:output, CausationID:evt.EventID})
8. adapter.MarkEventProcessed(ctx, evt.EventID)
// Note: no lifecycle transition here — lifecycle stays SELECTED until execution
```

**Evaluation Worker (new in Phase 3)**

```
1. ClaimNextEvent("evaluation_worker", ["position_event"])
2. posDTO := DecodePosition(evt.Payload); if Status != "exited" → MarkEventProcessed & skip
3. lc := adapter.GetLifecycleByToken(ctx, posDTO.TokenAddress)
4. output := module.Process(ctx, posDTO)
   // joins PositionStateDTO + ExecutionResultDTO + shadow_trades to compute all error signals
5. if err := adapter.TransitionState(ctx, {lc.ID, POSITION_CLOSED→EVALUATED}); err != nil { return err }
6. adapter.InsertEvaluation(output)
7. adapter.InsertEvent(ctx, {Type:"evaluation_event", Payload:output, CausationID:evt.EventID})
8. adapter.MarkEventProcessed(ctx, evt.EventID)
```

### Worker Flow (Execution with retry/replacement)

```
Replace Phase 2's single-attempt flow with state machine:

1. Allocate nonce, estimate gas, build + sign + submit tx
2. state := "submitted"
3. loop until state ∈ {confirmed, reverted, dropped, failed_terminal}:
     wait config.execution.poll_interval_ms
     if receipt:
         if status == 1: state = "confirmed"; break
         else:            state = "reverted"; break
     elif elapsed > config.execution.replacement_threshold_ms AND attempts < max_replacements:
         bumpedGas := currentMaxFee * config.execution.fee_bump_multiplier
         newTx    := reSign(same_nonce, bumpedGas, sameCalldata)
         submit(newTx)
         attempts++
     elif elapsed > config.execution.drop_timeout_ms:
         state = "dropped"; break
4. result.Attempts, result.Replaced, result.ReplacementCount populated
```

**Retry Policy (config-driven, Phase 3+):**

```
max_retry                = config.execution.max_retry           // default: 3
max_replacements         = config.execution.max_replacements    // default: 2
retry_backoff_ms         = [100, 400, 1600]                     // exponential: 100ms base, ×4 per step
replacement_threshold_ms = config.execution.replacement_threshold_ms  // default: 10_000
drop_timeout_ms          = config.execution.drop_timeout_ms           // default: 60_000
fee_bump_multiplier      = config.execution.fee_bump_multiplier       // default: 1.15 (≥10% per EIP-1559)
```

**Failure Classification (enforced in Phase 3+):**

| Classification | Conditions                                                                                 |
| -------------- | ------------------------------------------------------------------------------------------ |
| **RETRIABLE**  | nonce too low, tx underpriced, network timeout, receipt timeout (within max_replacements)  |
| **FATAL**      | revert, insufficient balance, invalid calldata, gas > hard cap, max_replacements exhausted |

_Rule: FATAL → emit `execution_event{Status:"failed"}`, transition `SELECTED→FAILED` immediately. RETRIABLE → bump gas and resubmit (same nonce) up to `max_replacements`; if exhausted → FATAL._

### Adapter Calls (Complete)

**Event bus:**

```
adapter.ClaimNextEvent(ctx, group, eventTypes)     // dequeue — ORDER BY priority DESC, created_at ASC
adapter.InsertEvent(ctx, event)                    // every emit
adapter.MarkEventProcessed(ctx, eventID)           // after every consume
```

**Lifecycle (CAS-enforced in Phase 3):**

```
adapter.StartLifecycle(ctx, lifecycleID, token)    // Phase 2 already calls; Phase 3 enforces unique-active index
adapter.TransitionState(ctx, TransitionRequest)    // every stage — NOW mandatory, failure halts the token
adapter.QuarantineToken(ctx, tokenAddress, reason) // after N violations (cfg.state_machine.quarantine_threshold)
adapter.GetLifecycleByToken(ctx, tokenAddress)     // pre-check current state before claiming
```

**Evaluation (new in Phase 3):**

```
adapter.InsertEvaluation(ctx, EvaluationDTO)
adapter.GetExecutionByLifecycle(ctx, lifecycleID)  // join ExecutionResultDTO to PositionStateDTO
adapter.GetShadowTradesByWindow(ctx, start, end)   // false-negative candidates
```

**Traceability / observability:**

```
adapter.GetEventsByTrace(ctx, traceID)
adapter.GetEventsByCorrelation(ctx, correlationID)
adapter.GetFailureChain(ctx, lifecycleID)
```

**Version:**

```
adapter.GetActiveStrategyVersion(ctx)
```

### Failure Handling

| Condition                                   | Action                                                                                             |
| ------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| Invalid state transition (CAS fails)        | Insert into `token_state_violation`; stage aborts this token; on Nth violation → `QuarantineToken` |
| DTO missing TraceID/CorrelationID/VersionID | Adapter returns `ErrMissingTraceField`; event not written; alert raised                            |
| `CausationID` references non-existent event | Adapter returns `ErrOrphanEvent`; alert; event dropped                                             |
| Stuck tx > `drop_timeout_ms`                | Mark dropped; emit execution_event with Status=dropped                                             |
| Replacement fails `max_replacements` times  | Final status=failed; lifecycle SELECTED→FAILED                                                     |
| RPC endpoint failing (circuit breaker)      | Route through next endpoint; if all open → halt                                                    |

### Exit Criteria

- [ ] No DTO writable with invalid state transition — CAS rejection observable in integration test
- [ ] `orphan_event_count` metric stays at 0 under load
- [ ] Stuck tx replacement observable at least once on testnet (synthetic low-gas submission)
- [ ] Telegram `/kill` command halts new entries within 1 event-bus tick (new selection events drop); exits continue
- [ ] Telegram `/status` returns active positions, current StrategyVersion, ingestion health
- [ ] Replay test: same event stream produces same `token_lifecycle` trajectory (audit rows identical)
- [ ] `token_state_violation` populated when invalid transitions attempted
- [ ] **Section I — Standard Execution Quality Gate:**
  - [ ] DTO integrity: all emitted DTOs have non-empty `TraceID`, `CorrelationID`, `VersionID`; `CausationID` non-empty for all non-root events
  - [ ] Adapter calls: `grep -r 'import.*database' internal/modules/` returns zero matches after Phase 3 additions
  - [ ] Deterministic replay: same fixture event stream → same `token_lifecycle` states and `token_state_transition` rows bit-for-bit

---

## Phase 4 — Signal Quality (Models + Full DQ/Features) (P1.5)

**Architecture:** Layer 1 (full `docs/architecture.md` § 3.1), Layer 2 (full § 3.2), Layer 3 (full § 3.3), Layer 4 (§ 3.4)

### Objective

Replace Phase 2 simple rules with full algorithmic stack: all DQ detectors, all features, momentum model, and P/S/L models feeding `ValidatedEdgeDTO`.

### BLOCKERS

**Phase 3 exit criteria must all be checked before starting Phase 4.**

Specifically: state machine CAS guards active, traceability enforcement live (no orphan events), tx replacement verified at least once, Telegram `/kill` functional, replay of Phase 2 trade produces identical `token_lifecycle` trajectory.

### Scope

**In scope:** Full DQ detector suite (wash trading, rug risk, tax anomaly, weighted risk score), full feature set (holder distribution, wallet entropy, drift detection), momentum model, P/S/L models (`ProbabilityEstimateDTO`, `SlippageEstimateDTO`, `LatencyProfileDTO`), private RPC routing (Flashbots + Beaverbuild). Phase 4 replaces Phase 2's fixed priors with model outputs.

**Explicitly excluded:** Adaptive weight updates (Phase 5), A/B promotion (Phase 5), kill switch (Phase 6), wallet sharding (Phase 6). Models are fixed-coefficient in Phase 4 — learning closes the loop in Phase 5.

### Event Types Emitted

| Event Type          | Emitter            | DTO                      |
| ------------------- | ------------------ | ------------------------ |
| `probability_event` | probability worker | `ProbabilityEstimateDTO` |
| `slippage_event`    | slippage worker    | `SlippageEstimateDTO`    |
| `latency_event`     | latency worker     | `LatencyProfileDTO`      |

All Phase 2/3 event types continue to be emitted. Phase 4 inserts three new fan-out events from `feature_event` before `validated_edge_event`.

### DTO Pipeline

Phase 4 **extends** the Phase 2 pipeline by inserting fan-out model events between Layer 2 and Layer 5:

| Stage           | Layer | Input DTO            | Input Event          | Output DTO               | Output Event           |
| --------------- | ----- | -------------------- | -------------------- | ------------------------ | ---------------------- |
| DQ (full)       | 1     | `MarketDataDTO`      | `market_data_event`  | `DataQualityDTO`         | `data_quality_event`   |
| Features (full) | 2     | `DataQualityDTO`     | `data_quality_event` | `FeatureDTO`             | `feature_event`        |
| Probability     | 4     | `FeatureDTO`         | `feature_event`      | `ProbabilityEstimateDTO` | `probability_event`    |
| Slippage        | 4     | `FeatureDTO`         | `feature_event`      | `SlippageEstimateDTO`    | `slippage_event`       |
| Latency         | 4     | _(chain-keyed)_      | _(periodic)_         | `LatencyProfileDTO`      | `latency_event`        |
| Validation      | 5     | `EdgeDTO` + 3 models | `edge_event`         | `ValidatedEdgeDTO`       | `validated_edge_event` |

### Lifecycle Transitions

Phase 4 adds no new lifecycle states. All transitions are unchanged from Phase 2/3. Phase 4 improves **input quality** to the `VALIDATED → SELECTED → EXECUTED` path by replacing fixed priors with model outputs.

| Worker     | From State      | To State    | Condition                     | Adapter Call      |
| ---------- | --------------- | ----------- | ----------------------------- | ----------------- |
| Validation | `EDGE_DETECTED` | `VALIDATED` | EV ≥ threshold AND model ok   | `TransitionState` |
| Validation | `EDGE_DETECTED` | `REJECTED`  | EV < threshold OR model_error | `TransitionState` |

### Traceability

All new DTOs (`ProbabilityEstimateDTO`, `SlippageEstimateDTO`, `LatencyProfileDTO`) MUST carry:

| Field           | Rule                                                                    |
| --------------- | ----------------------------------------------------------------------- |
| `TraceID`       | Copy from `FeatureDTO.TraceID` unchanged                                |
| `CorrelationID` | Copy from `FeatureDTO.CorrelationID` unchanged                          |
| `CausationID`   | Set to `featureEvent.EventID` (the event being consumed by this worker) |
| `VersionID`     | Pinned at worker startup via `adapter.GetActiveStrategyVersion()`       |

### File Structure

```
internal/modules/data_quality/
├── wash_trading.go                        // § 3.1.7.1
├── rug_risk.go                            // § 3.1.7.2
├── tax_anomaly.go                         // § 3.1.7.5
└── risk_score.go                          // Weighted aggregation § 3.1.7.6

internal/modules/features/
├── holder_distribution.go
├── wallet_entropy.go
├── drift_detector.go                      // § 3.2.11

internal/modules/edge/
├── momentum.go                            // § 3.3.9
├── adaptive_threshold.go                  // § 3.3.10
└── new_pool_gate.go                       // § 3.3.8

contracts/
├── probability.go
├── slippage.go
└── latency.go

internal/modules/models/
├── probability.go                         // Logistic regression
├── probability_fit.go                     // Offline fit from LearningRecord history
├── slippage.go                            // Empirical curve by (liquidity, size) bucket
├── latency.go                             // Rolling percentiles per chain
└── models_test.go

internal/workers/
├── run_probability.go
├── run_slippage.go
└── run_latency.go

internal/modules/execution/
└── private_rpc.go                         // Flashbots + Beaverbuild (§ 3.8.22)
```

### Function Contracts

```go
// internal/modules/models/probability.go
type ProbabilityModel interface {
    Predict(ctx context.Context, features contracts.FeatureDTO) (contracts.ProbabilityEstimateDTO, error)
    ModelVersionID() string
}

// internal/modules/models/slippage.go
type SlippageModel interface {
    Estimate(ctx context.Context, liquidityUsd, sizeUsd float64, chain string) (contracts.SlippageEstimateDTO, error)
}

// internal/modules/models/latency.go
type LatencyModel interface {
    Profile(ctx context.Context, chain string) (contracts.LatencyProfileDTO, error)
}
```

### DTO Flow (Updated)

```
feature_event ──┬─> probability_event  (ProbabilityEstimateDTO)
                ├─> slippage_event     (SlippageEstimateDTO)   [per-candidate]
                └─> latency_event      (LatencyProfileDTO)     [periodic, keyed by chain]

validation worker now reads all four events and replaces Phase 2 fixed priors:
  P               := ProbabilityEstimateDTO.Probability
  SlippageP95Bps  := SlippageEstimateDTO.ExpectedP95Bps
```

### Worker Flows (Probability, Slippage, Latency)

**Probability Worker**

```
1. ClaimNextEvent("probability_worker", ["feature_event"])
2. featureDTO := DecodeFeature(evt.Payload)
   // Trace propagation: TraceID/CorrelationID/VersionID ← featureDTO; CausationID ← evt.EventID
3. probDTO := probabilityModel.Predict(ctx, featureDTO)
   // probDTO.Probability validated: must be in (0.0, 1.0); NaN/out-of-range → REJECT with model_error
4. adapter.InsertProbabilityEstimate(ctx, probDTO)
5. adapter.InsertEvent(ctx, {Type:"probability_event", Payload:probDTO, CausationID:evt.EventID,
       TraceID:featureDTO.TraceID, CorrelationID:featureDTO.CorrelationID, VersionID:activeVersionID})
6. adapter.MarkEventProcessed(ctx, evt.EventID)
```

**Slippage Worker**

```
1. ClaimNextEvent("slippage_worker", ["feature_event"])
2. featureDTO := DecodeFeature(evt.Payload)
3. slipDTO := slippageModel.Estimate(ctx, featureDTO.LiquidityUsd, proposedSizeUsd, featureDTO.Chain)
   // proposedSizeUsd read from config.capital.fixed_entry_size_usd (Phase 2) or AllocationDTO (Phase 4+)
   // If bucket data missing → fall back to config.slippage.fallback_p95_bps
4. adapter.InsertSlippageEstimate(ctx, slipDTO)
5. adapter.InsertEvent(ctx, {Type:"slippage_event", Payload:slipDTO, CausationID:evt.EventID,
       TraceID:featureDTO.TraceID, CorrelationID:featureDTO.CorrelationID, VersionID:activeVersionID})
6. adapter.MarkEventProcessed(ctx, evt.EventID)
```

**Latency Worker (periodic, keyed by chain)**

```
1. Runs on cron: every config.models.latency_profile_interval_seconds per chain
   // No input event — latency profile is chain-level, not per-token
2. for each chain in config.chains:
      latDTO := latencyModel.Profile(ctx, chain)
      // Rolls up p50/p95/p99 latency from last config.models.latency_window_seconds of RPC calls
      // latDTO.TraceID = SHA256("latency"||chain||epoch_window_id)[:16]  (stable per window)
      // latDTO.CausationID = ""  (periodic root event, like ingestion)
      // latDTO.VersionID  = activeVersionID
3. adapter.InsertLatencyProfile(ctx, latDTO)
4. adapter.InsertEvent(ctx, {Type:"latency_event", Payload:latDTO})
   // ON CONFLICT DO NOTHING if same chain+window already profiled
```

**Validation Worker (updated — replaces Phase 2/3 fixed priors)**

The validation worker in Phase 4 now reads all three model events before computing EV. It waits for a configurable window (`config.validation.model_join_timeout_ms`) for probability and slippage events to be available for the same `TraceID`. If either times out, the validation falls back to fixed priors from config.

```
1. ClaimNextEvent("validation_worker", ["edge_event"])
2. edgeDTO := DecodeEdge(evt.Payload)
3. lc := adapter.GetLifecycleByToken(ctx, edgeDTO.TokenAddress)
4. // Join model outputs for this TraceID:
   probDTO  := adapter.GetProbabilityEstimateByTrace(ctx, edgeDTO.TraceID)
        // if nil after timeout → use config.validation.prior_probability
   slipDTO  := adapter.GetSlippageEstimateByTrace(ctx, edgeDTO.TraceID)
        // if nil after timeout → use config.validation.prior_slippage_bps
   latDTO   := adapter.GetLatestLatencyProfile(ctx, edgeDTO.Chain)
        // always available (periodic cache); never nil
5. EV = probDTO.Probability * ExpectedGainBps
        - (1 - probDTO.Probability) * ExpectedLossBps
        - FixedCostsBps
        - slipDTO.ExpectedP95Bps
6. Latency check: if latDTO.P95Ms + cfg.execution.build_submit_p95_ms > edgeDTO.OpportunityWindowMs:
      Decision = "REJECT", RejectReason = "latency_exceeds_window"
7. output := ValidatedEdgeDTO{Decision: ..., EVBps: EV, ProbabilityUsed: probDTO.Probability,
        SlippageP95BpsUsed: slipDTO.ExpectedP95Bps, ...}
8. if Decision == "ACCEPT":
      adapter.TransitionState(ctx, {lc.ID, EDGE_DETECTED→VALIDATED})
      adapter.InsertValidatedEdge(output)
      adapter.InsertEvent(ctx, {Type:"validated_edge_event", Payload:output, CausationID:evt.EventID})
   else:
      adapter.TransitionState(ctx, {lc.ID, EDGE_DETECTED→REJECTED, output.RejectReason})
9. adapter.MarkEventProcessed(ctx, evt.EventID)
```

### Worker Flow (Validation, Updated)

```
1. ClaimNextEvent("validation_worker", ["edge_event"])
2. Fan-out:
   a. probDTO     := probabilityModel.Predict(features)            → InsertEvent("probability_event")
   b. slipDTO     := slippageModel.Estimate(liq, size, chain)      → InsertEvent("slippage_event")
   c. latDTO      := latencyModel.Profile(chain)                   (cached, periodic)
3. Compute EV using model outputs:
   EV = P × ExpectedGainBps - (1-P) × ExpectedLossBps - FixedCostsBps - SlippageP95Bps
4. Emit ValidatedEdgeDTO with ProbabilityUsed / SlippageP95BpsUsed populated from model outputs (not config)
```

### Adapter Calls (Complete)

**Event bus:**

```
adapter.ClaimNextEvent(ctx, group, eventTypes)    // dequeue
adapter.InsertEvent(ctx, probability_event)       // fan-out 1 — probability worker
adapter.InsertEvent(ctx, slippage_event)          // fan-out 2 — slippage worker
adapter.InsertEvent(ctx, latency_event)           // periodic chain profile — latency worker
adapter.MarkEventProcessed(ctx, eventID)          // after every consume
```

**DTO persistence:**

```
adapter.InsertProbabilityEstimate(ctx, dto)
adapter.InsertSlippageEstimate(ctx, dto)
adapter.InsertLatencyProfile(ctx, dto)
```

**DTO reads (validation join — new in Phase 4):**

```
adapter.GetProbabilityEstimateByTrace(ctx, traceID)  // validation worker joins model outputs
adapter.GetSlippageEstimateByTrace(ctx, traceID)     // validation worker joins model outputs
adapter.GetLatestLatencyProfile(ctx, chain)          // validation worker reads current chain profile
```

**Lifecycle:**

```
adapter.TransitionState(ctx, request)             // validation enforced (Phase 3+)
```

**Version:**

```
adapter.GetActiveStrategyVersion(ctx)             // model version ID derived from strategy version
```

### Failure Handling

| Condition                                    | Action                                                              |
| -------------------------------------------- | ------------------------------------------------------------------- |
| Probability model returns NaN / out-of-range | Reject in validator; lifecycle → REJECTED with reason `model_error` |
| Slippage model lacks data for bucket         | Fall back to conservative priors from `config.slippage.fallback_*`  |
| Drift detected (§ 3.2.11)                    | Freeze affected features (set Confidence=0); emit alert event       |
| Private RPC submission rejected              | Fall back to public mempool; mark route="public_fallback"           |

### Exit Criteria

- [ ] Brier score of probability model `< 0.25` on held-out Phase 2 data
- [ ] Realized-vs-predicted slippage p95 error `< 30%` on 100+ executed trades
- [ ] `pass_rate` falls in `[0.5%, 5%]` target zone on live replay
- [ ] Private RPC routing activates when `AllocationDTO.SizeUsd >= config.execution.private_route_threshold_usd`
- [ ] Feature drift detector surfaces via metrics; freeze behavior verified in integration test
- [ ] **Section I — Standard Execution Quality Gate:**
  - [ ] DTO integrity: `ProbabilityEstimateDTO`, `SlippageEstimateDTO`, `LatencyProfileDTO` all carry non-empty `TraceID`, `CorrelationID`, `VersionID`, `CausationID`
  - [ ] Events emitted: every `feature_event` produces exactly one each of `probability_event`, `slippage_event`; `latency_event` emitted on each periodic tick
  - [ ] Adapter calls: zero direct SQL in `internal/modules/models/`; all model outputs stored via adapter `InsertEvent`
  - [ ] Trace propagation: `TraceID` from `FeatureDTO` is identical in all three fan-out model DTOs for the same token
  - [ ] Deterministic replay: same `feature_event` inputs + same model coefficients → same model output DTOs bit-for-bit

---

## Phase 5 — Learning Engine (P2)

**Architecture:** Layer 10 § 3.10, § 4.1 Strategy Versioning, § 4.3 Opportunity Monitor

### Objective

Close the feedback loop: track FP/FN, record shadow trades, compute bounded versioned parameter updates, manage StrategyVersion A/B promotion. Includes safe-learning with shadow version staging, rollback watchdog, and shadow execution (paper trading) mode.

### BLOCKERS

**Phase 4 exit criteria must all be checked before starting Phase 5.**

Specifically: probability calibration Brier score `< 0.25`, slippage p95 error `< 30%`, `pass_rate` in `[0.5%, 5%]` on live replay, private RPC routing functional, `go test ./internal/modules/...` passes.

### Scope

**In scope:** `LearningRecordDTO` per exit, shadow trade recording on every rejection, FP/FN classification, cohort analysis, bounded single-family-per-cycle updates (`Δ ≤ 10%`, `N ≥ 30`), StrategyVersion creation + A/B promotion, opportunity monitor (starvation/overtrading), safe-learning with shadow staging (`draft → shadow → active → rolled_back`), shadow execution / paper trading mode (`config.execution.mode=shadow`), rollback watchdog.

**Explicitly excluded:** Kill switch / halt modes (Phase 6), wallet sharding (Phase 6), event bus partitioning (Phase 6). Learning in Phase 5 uses production events; Phase 6 handles operational safety.

### Event Types Emitted

| Event Type                 | Emitter              | DTO                                                   |
| -------------------------- | -------------------- | ----------------------------------------------------- |
| `learning_record_event`    | learning recorder    | `LearningRecordDTO`                                   |
| `evaluation_event`         | evaluator (periodic) | `EvaluationDTO`                                       |
| `strategy_promotion_event` | A/B promoter         | metadata                                              |
| `mode_transition_event`    | opportunity monitor  | mode change payload                                   |
| `adjustment_event`         | adjustment worker    | metadata (new version candidate created, priority=50) |

### DTO Pipeline

| Worker / Stage        | Input DTO           | Input Event             | Output DTO                        | Output Event               |
| --------------------- | ------------------- | ----------------------- | --------------------------------- | -------------------------- |
| Learning recorder     | `PositionStateDTO`  | `position_event` (exit) | `LearningRecordDTO`               | `learning_record_event`    |
| Shadow recorder       | any rejected DTO    | rejection events        | `LearningRecordDTO` (shadow=true) | `learning_record_event`    |
| Evaluator (periodic)  | `LearningRecordDTO` | `learning_record_event` | `EvaluationDTO`                   | `evaluation_event`         |
| **Adjustment Worker** | `EvaluationDTO`     | `evaluation_event`      | `StrategyVersion` (DB)            | `strategy_promotion_event` |

### Lifecycle Transitions

Phase 5 does not drive token lifecycle state. It observes `position_event` (exited) as input, which arrives at `POSITION_CLOSED` state (set by Phase 2/3 position worker). No `TransitionState` calls in Phase 5 workers.

Phase 5 **does** manage `StrategyVersion.Status` state machine:

| Worker            | Version From | Version To    | Condition                                         | Adapter Call               |
| ----------------- | ------------ | ------------- | ------------------------------------------------- | -------------------------- |
| Adjustment        | (new)        | `draft`       | always on create                                  | `CreateStrategyVersion`    |
| Adjustment        | `draft`      | `shadow`      | ready for A/B window                              | `SetStrategyVersionStatus` |
| A/B promoter      | `shadow`     | `active`      | expectancy > 1.05× AND drawdown ≤ base AND N ≥ 30 | `SetStrategyVersionStatus` |
| Rollback watchdog | `active`     | `rolled_back` | expectancy drops > rollback threshold             | `SetStrategyVersionStatus` |

### Traceability

Learning and evaluation DTOs carry:

| Field           | Rule                                                                                        |
| --------------- | ------------------------------------------------------------------------------------------- |
| `TraceID`       | Copy from `PositionStateDTO.TraceID` — tracing the token journey end-to-end                 |
| `CorrelationID` | Copy from `PositionStateDTO.CorrelationID`                                                  |
| `CausationID`   | Set to `positionEvent.EventID` (the exit event triggering evaluation)                       |
| `VersionID`     | Set to the `StrategyVersion.StrategyVersionID` that was **active when the trade was taken** |

### Safe Learning — Shadow Version + Rollback (from § 7.6)

Extend `StrategyVersion.Status` with additional states:

```
Status ∈ {draft, shadow, active, deactivated, rolled_back}
```

**New flow:**

1. `Updater` writes new version as **`shadow`** (not `active`) with observation window `cfg.learning.shadow_window_minutes`.
2. During window, shadow version receives **mirrored decisions only** — execution worker routes to paper executor when `cfg.execution.shadow_strategy_id` is set (no real capital, no on-chain tx).
3. A/B promoter moves `shadow → active` when promotion conditions pass (existing § 5.5 rules).
4. **Rollback watchdog** (new `run_rollback_watchdog.go`): if active version's realized expectancy drops below baseline by more than `cfg.learning.rollback_threshold_pct` within `cfg.learning.post_promotion_watch_minutes`, auto-rollback — `active → rolled_back`, previous active reinstated.

**New adapter methods** (declared in `docs/db_adapter_spec.md` § 11.3):

```go
adapter.SetStrategyVersionStatus(ctx, versionID, status string) error
adapter.GetShadowVersion(ctx) (*StrategyVersion, error)
```

### Shadow Execution Mode (Paper Trading) (from § 7.7)

`config/execution.yaml` gains `mode: shadow | live`. When `mode=shadow`:

```go
// internal/modules/execution/paper.go
func (m *Module) ProcessShadow(ctx context.Context, in contracts.AllocationDTO) (contracts.ExecutionResultDTO, error)
// - No tx signing, no RPC submission.
// - Realized price = on-chain spot at decision time + SlippageEstimateDTO.ExpectedP95Bps.
// - Gas/latency drawn from LatencyProfileDTO samples (not real gas paid).
// - ExecutionResultDTO.Simulated = true.
```

All downstream modules (Position, Learning) treat `Simulated=true` identically — they record real market prices, no capital moved. DB: `executions.simulated = TRUE`. Shadow results feed `LearningRecordDTO` with `shadow=true` and `simulated=true`.

### File Structure

```
contracts/
├── evaluation.go
└── learning_record.go

internal/modules/learning/
├── recorder.go                            // Emit LearningRecordDTO on every position exit
├── shadow_recorder.go                     // Emit shadow LearningRecordDTO on rejections
├── shadow_observer.go                     // Periodic: observe rejected tokens' price trajectory
├── fp_fn_classifier.go                    // TP|FP|TN|FN per record
├── evaluator.go                           // Emit EvaluationDTO per window
├── cohort.go                              // liquidity_bucket:age_bucket:source
├── updater.go                             // Bounded parameter updates (§ 3.10.12)
├── ab_promoter.go                         // § 4.1.6
├── opportunity_monitor.go                 // § 4.3 starvation / overtrading
└── learning_test.go

internal/workers/
├── run_learning_record.go                 // Triggered by position_event (Status=exited); shadow=false records
├── run_shadow_recorder.go                 // Triggered by rejection events; emits shadow LearningRecordDTO
├── run_shadow_observer.go                 // Periodic — observes rejected tokens' price trajectory
├── run_evaluator.go                       // Periodic (every eval_window_minutes); emits evaluation_event
├── run_rollback_watchdog.go               // Periodic — post-promotion degradation watchdog; may rollback active version
└── run_updater.go                         // Triggered by evaluation_event; creates candidate StrategyVersion

database/migrations/
└── 20260101000005_learning_tables.sql     // learning_records, evaluations, shadow_trades
```

### Function Contracts

```go
func (r *Recorder) RecordExecuted(ctx context.Context, pos contracts.PositionStateDTO) (contracts.LearningRecordDTO, error)
func (s *ShadowRecorder) RecordRejection(ctx context.Context, stage string, rejected any) (shadowID string, err error)
func (o *ShadowObserver) Observe(ctx context.Context, shadowID string) (observedReturnPct float64, complete bool, err error)
func (c *Classifier) Classify(outcome string, pnlPct float64) string    // → TP|FP|TN|FN
func (e *Evaluator) EvaluateWindow(ctx context.Context, versionID string, start, end time.Time) (contracts.EvaluationDTO, error)
func (u *Updater) Update(ctx context.Context, eval contracts.EvaluationDTO) (newVersionID string, err error)
func (p *ABPromoter) ConsiderPromotion(ctx context.Context, candidateVersionID string) (promoted bool, err error)
func (m *OpportunityMonitor) Check(ctx context.Context, window time.Duration) (newMode string, err error)
```

### DTO Flow

```
position_event (exited)  ──> learning_record_event (executed, LearningRecordDTO shadow=false)
*rejection events*       ──> learning_record_event (shadow, LearningRecordDTO shadow=true)
                              + shadow_trades row pending observation_complete
periodic                 ──> evaluation_event (EvaluationDTO per StrategyVersion)
eval → update            ──> new StrategyVersion row in strategy_versions (promotion = activation toggle)
```

### Worker Flows (Phase 5 — Individual Workers)

**Learning Recorder Worker**

```
Input event:  "position_event" (Status=exited)
Output event: "learning_record_event" (shadow=false)

1. ClaimNextEvent("learning_recorder_worker", ["position_event"])
2. posDTO := DecodePosition(evt.Payload); if Status != "exited" → MarkEventProcessed & skip
3. lrDTO := recorder.RecordExecuted(ctx, posDTO)
   // lrDTO.Shadow = false, lrDTO.SimulatedExecution = posDTO.Simulated
   // lrDTO.Classification := classifier.Classify(outcome, posDTO.PnlPct)   → TP|FP|TN|FN
   // Trace propagation: TraceID/CorrelationID ← posDTO; CausationID ← evt.EventID
   // VersionID = posDTO.VersionID (version active when trade was taken)
4. adapter.InsertLearningRecord(ctx, lrDTO)
5. adapter.InsertEvent(ctx, {Type:"learning_record_event", Payload:lrDTO, CausationID:evt.EventID})
6. adapter.MarkEventProcessed(ctx, evt.EventID)
```

**Shadow Recorder Worker**

```
Input events: any rejection event (data_quality_event/edge_event/validated_edge_event with Decision=REJECT)
Output event: "learning_record_event" (shadow=true)

1. ClaimNextEvent("shadow_recorder_worker",
       ["data_quality_event","edge_event","validated_edge_event","selection_event"])
2. Parse rejection from payload; if not a rejection → MarkEventProcessed & skip
3. shadowID, _ := shadowRecorder.RecordRejection(ctx, stage, rejectedDTO)
   // Inserts shadow_trades row: observation_complete=false, rejected_at=now
   // lrDTO.Shadow = true, lrDTO.Classification = "TN" initially (updated by observer)
4. adapter.InsertLearningRecord(ctx, lrDTO)
5. adapter.InsertEvent(ctx, {Type:"learning_record_event", Payload:lrDTO, CausationID:evt.EventID})
6. adapter.MarkEventProcessed(ctx, evt.EventID)
```

**Shadow Observer Worker (periodic)**

```
Runs every config.learning.shadow_poll_interval_seconds

1. shadows := adapter.GetShadowTradesByWindow(ctx, observation_start, observation_end)
   // WHERE observation_complete = FALSE AND rejected_at < now - observation_window_s
2. for each shadow:
      observedReturn, complete, _ := observer.Observe(ctx, shadow.ShadowID)
      // Fetches current token price from RPC; computes return since rejection
      if complete:
          shadow.ObservedReturnPct     = observedReturn
          shadow.ObservationComplete   = true
          classification := "TN"
          if observedReturn > cfg.evaluation.fn_gain_threshold_pct: classification = "FN"
          adapter.UpdateShadowTradeObservation(ctx, shadow.ShadowID, observedReturn, classification)
          // Also update corresponding LearningRecordDTO classification in learning_records
```

**Evaluator Worker (periodic)**

```
Runs every config.learning.eval_window_minutes

1. activeVersion := adapter.GetActiveStrategyVersion(ctx)
2. window := [now - cfg.learning.eval_window_seconds, now]
3. evalDTO := evaluator.EvaluateWindow(ctx, activeVersion.VersionID, window.Start, window.End)
   // Aggregates LearningRecordDTO rows in window:
   //   SampleSize, TP/FP/TN/FN counts
   //   Expectancy = P × avgWin − (1−P) × avgLoss  per cohort
   //   MaxDrawdownPct, SharpeRatio, AvgExecutionError, AvgPredictionError
   // evalDTO.TraceID = SHA256("eval"||versionID||window_id)[:16]
   // evalDTO.CausationID = "" (periodic root event)
   // evalDTO.VersionID = activeVersion.VersionID
4. adapter.InsertEvaluation(ctx, evalDTO)
5. adapter.InsertEvent(ctx, {Type:"evaluation_event", Payload:evalDTO, CausationID:""})
```

**Rollback Watchdog Worker (periodic)**

```
Runs every config.learning.rollback_check_interval_seconds

1. promotedVersion := adapter.GetActiveStrategyVersion(ctx)
   // if promotedVersion.PromotedAt is nil or > post_promotion_watch_minutes → skip
2. baselineVersion := adapter.GetStrategyVersion(ctx, promotedVersion.ParentVersionID)
3. promotedEval  := evaluator.EvaluateWindow(ctx, promotedVersion.VersionID, ...)
4. baselineEval  := evaluator.EvaluateWindow(ctx, baselineVersion.VersionID, ...)
5. if promotedEval.Expectancy < baselineEval.Expectancy * (1 - cfg.learning.rollback_threshold_pct):
      adapter.SetStrategyVersionStatus(ctx, promotedVersion.VersionID, "rolled_back")
      adapter.SetStrategyVersionStatus(ctx, baselineVersion.VersionID, "active")
      adapter.InsertEvent(ctx, {Type:"strategy_promotion_event",
          Payload:{Action:"rollback", FromVersionID:promotedVersion.VersionID,
                   ToVersionID:baselineVersion.VersionID}})
```

### Worker Flow (Updater)

```
1. EvaluateWindow(versionID=activeVersion, now - eval_window, now)
2. If SampleSize < config.learning.min_sample_size → skip (log starvation)
3. Choose ONE parameter family for this cycle (round-robin from config.learning.families):
     - thresholds   (edge min, ev threshold, liquidity min)
     - weights      (feature weights in momentum)
     - cohort_mults (selection bonuses per cohort)
4. newConfig := applyBoundedDelta(activeConfig, family, eval, maxDelta=config.learning.max_delta_pct)
5. newVersionID := SHA256(canonical_json(newConfig))[:16]
6. adapter.CreateStrategyVersion(StrategyVersion{VersionID: newVersionID, Snapshot: newConfig})
7. Do NOT activate yet — wait for ABPromoter
```

### Worker Flow (A/B Promoter)

```
1. candidateEval := EvaluateWindow(candidateVersionID)
2. baselineEval  := EvaluateWindow(activeVersionID)
3. Promote iff ALL:
     candidateEval.SampleSize >= 30
     candidateEval.Expectancy > baselineEval.Expectancy * 1.05
     candidateEval.MaxDrawdownPct <= baselineEval.MaxDrawdownPct
4. If promote: UPDATE strategy_versions SET activated_at = CURRENT_TIMESTAMP WHERE ...
               Deactivate old version
5. Log promotion decision to events bus (strategy_promotion_event)
```

### Worker Flow (Adjustment Worker)

The adjustment worker creates a new candidate `StrategyVersion` (shadow), validates all constraints, and — only if all gates pass — promotes it to active.

```
Trigger: ClaimNextEvent("adjustment_worker", ["evaluation_event"])

1. evalDTO := DecodeEvaluation(evt.Payload)
2. activeVersion := adapter.GetActiveStrategyVersion(ctx)

// Gate 1: sample-size gate
3. if evalDTO.SampleSize < cfg.learning.min_samples:
      // Insufficient data — do not adjust; log skip
      adapter.MarkEventProcessed(ctx, evt.EventID); return

// Gate 2: single-family rule (only one parameter family tuned per cycle)
4. paramFamily := adjuster.SelectFamily(evalDTO)      // selects highest-variance family
5. if len(paramFamily.Fields) > 1:
      return error("adjustment must tune one family at a time")

// Gate 3: bounded update rule
6. proposal := adjuster.Propose(ctx, activeVersion, evalDTO, paramFamily)
   // Δ ≤ cfg.learning.max_param_delta_pct (10%) per field per cycle
   // If any Δ > bound → clamp to bound; do not abort
7. for each field in proposal.ChangedFields:
      if abs(proposal.NewValue[field] - activeVersion.Params[field]) /
             activeVersion.Params[field] > cfg.learning.max_param_delta_pct:
          return error("bounded update violated: field %s", field)

// Gate 4: expectancy improvement gate (A/B dry-run, shadow simulation)
8. shadowEval := evaluator.SimulateWindow(ctx, proposal, evalDTO)
   // Re-runs evaluation window with proposed params against stored LearningRecords
   // Does NOT execute live trades
9. if shadowEval.Expectancy <= evalDTO.Expectancy:
      adapter.MarkEventProcessed(ctx, evt.EventID); return  // no improvement

// Create & promote
10. newVersion := adapter.CreateStrategyVersion(ctx, {
        ParentVersionID : activeVersion.VersionID,
        Params          : proposal.Params,
        CreatedAt       : now,
        Status          : "candidate",
    })
11. adapter.SetStrategyVersionStatus(ctx, newVersion.VersionID, "active")
12. adapter.SetStrategyVersionStatus(ctx, activeVersion.VersionID, "superseded")
13. adapter.InsertEvent(ctx, {Type:"adjustment_event",
        Payload: {
            Action          : "adjust",
            FromVersionID   : activeVersion.VersionID,
            ToVersionID     : newVersion.VersionID,
            ChangedFamily   : paramFamily.Name,
            DeltaSummary    : proposal.DeltaSummary,
        },
        CausationID: evt.EventID,
    })
14. adapter.MarkEventProcessed(ctx, evt.EventID)
```

### 5.1 Adjustment Worker

The Adjustment Worker is the **bounded update engine**. It is the only component permitted to create new `StrategyVersion` rows that affect live parameters.

**File:** `internal/workers/run_updater.go`

**Input:** `evaluation_event` (payload = `EvaluationDTO`)
**Output:** New `StrategyVersion` row at status `draft`

**Bounded update algorithm:**

```
1. eval := decode(evaluation_event) → EvaluationDTO
2. assert eval.SampleSize >= config.learning.min_sample_size (default 30)
      → if not: skip, emit mode_transition_event{reason:"insufficient_samples"}
3. choose ONE parameter family for this cycle (round-robin per config.learning.families):
      families: [thresholds, weights, cohort_mults]
      family := families[cycle_counter % len(families)]
4. currentParams := adapter.GetActiveStrategyVersion().Snapshot
5. delta := computeDelta(eval, family)
      // delta must satisfy: |delta[k] / currentParams[k]| <= config.learning.max_delta_pct (default 0.10)
      // if any single parameter exceeds bound → clamp to bound
6. newParams := applyDelta(currentParams, family, delta)
7. newVersionID := SHA256(canonical_json(newParams))[:16]
8. adapter.CreateStrategyVersion(StrategyVersion{
       VersionID: newVersionID, Status: "draft",
       ParentVersionID: activeVersionID, Snapshot: newParams,
       ParameterFamilyUpdated: family, SampleSize: eval.SampleSize,
       TraceID: eval.TraceID, CausationID: evaluationEvent.EventID,
   })
9. adapter.SetStrategyVersionStatus(ctx, newVersionID, "shadow")
10. adapter.InsertEvent(ctx, strategy_promotion_event{CandidateVersionID: newVersionID})
```

**Invariants (must never be violated):**

- Exactly 1 parameter family updated per cycle
- `|Δparam| ≤ 10%` per cycle (all parameters, enforced by adapter or worker)
- `N ≥ 30` samples required before any update
- Each update bumps version — no in-place mutation of `strategy_versions` rows

### Adapter Calls (Complete)

**Event bus:**

```
adapter.ClaimNextEvent(ctx, group, eventTypes)          // dequeue (position_event, learning_record_event, evaluation_event)
adapter.InsertEvent(ctx, learning_record_event)         // per exit / rejection
adapter.InsertEvent(ctx, evaluation_event)              // periodic from evaluator
adapter.InsertEvent(ctx, strategy_promotion_event)      // from updater / A/B promoter
adapter.InsertEvent(ctx, mode_transition_event)         // from opportunity monitor
adapter.MarkEventProcessed(ctx, eventID)                // after every consume
```

**DTO persistence:**

```
adapter.InsertLearningRecord(ctx, dto)
adapter.InsertShadowTrade(ctx, shadowID, tokenAddress, rejectedAt, stage)
adapter.InsertEvaluation(ctx, dto)                     // also called by evaluation engine in Phase 3
```

**Strategy version lifecycle:**

```
adapter.CreateStrategyVersion(ctx, sv)
adapter.SetStrategyVersionStatus(ctx, versionID, status)   // draft→shadow, shadow→active, active→rolled_back
adapter.GetShadowVersion(ctx)
adapter.GetActiveStrategyVersion(ctx)
adapter.GetStrategyVersion(ctx, versionID)
```

**Learning reads:**

```
adapter.GetLearningRecordsByWindow(ctx, versionID, start, end)
adapter.GetShadowTradesByWindow(ctx, start, end)
adapter.GetEvaluationsByVersion(ctx, versionID)
```

### Failure Handling

| Condition                                   | Action                                                                     |
| ------------------------------------------- | -------------------------------------------------------------------------- |
| Insufficient sample size                    | Skip update; log starvation; opportunity monitor may shift mode            |
| Candidate version degrades (drawdown worse) | Rollback: deactivate candidate, reactivate prior version                   |
| Shadow observation window incomplete        | Keep `observation_complete = false`; observer retries until window elapsed |
| Multiple families updated in one cycle      | **Forbidden** — updater rejects; alert                                     |

### Exit Criteria

- [ ] Every exited position produces exactly 1 `learning_record_event` with `shadow = false`
- [ ] Every rejection produces exactly 1 shadow record with `shadow = true` awaiting observation
- [ ] Evaluator emits `evaluation_event` every `config.learning.eval_window_minutes`
- [ ] Updater touches exactly 1 parameter family per cycle (verified by snapshot diff count)
- [ ] A/B promotion deterministic: same samples → same decision
- [ ] Operational mode auto-transitions observable (starvation → EXPLORATION; rug spike → STRICT)
- [ ] Replay on historical data: same data + same initial version → same final version
- [ ] **Section I — Standard Execution Quality Gate:**
  - [ ] DTO integrity: `LearningRecordDTO` and `EvaluationDTO` carry non-empty `TraceID`, `CorrelationID`, `VersionID`, `CausationID`
  - [ ] Lifecycle transitions: `POSITION_CLOSED→EVALUATED` observable in `token_state_transition` for every processed exit
  - [ ] Events emitted: every `evaluation_event` triggers exactly one `adjustment_event` when sample gate passed
  - [ ] Adapter calls: zero direct SQL in `internal/modules/learning/`; all writes via adapter
  - [ ] Trace propagation: `TraceID` from `PositionStateDTO` carried through to `LearningRecordDTO` and `EvaluationDTO`

---

## Phase 6 — Resource Control, Wallet Sharding, Scaling (P2)

**Architecture:** § 4.9 Resource Control, § 3.8.6 Wallet Sharding, § 3.8.8 Bounded Parallelism

### Objective

Scale throughput, reduce latency, enforce cost budgets, guarantee exit-path priority under backpressure. Add global kill switch (risk halt), full capital safety envelope, event bus partitioning, data retention/archival, and MEV-aware execution routing.

### BLOCKERS

**Phase 5 exit criteria must all be checked before starting Phase 6.** Phases 5 and 6 may run in parallel if Phase 4 is complete and they own disjoint files.

Specifically: every exited position produces a `LearningRecordDTO`, bounded updates touch exactly 1 family per cycle, A/B promotion deterministic, shadow execution verified (`Simulated=true` records present), rollback watchdog fires on synthetic degradation.

### Scope

**In scope:** RPC token bucket, gas daily cap per wallet + system, compute queue backpressure, backpressure shed policy (exits never dropped), wallet sharding (`hash(token) % n`), global execution semaphore [5, 20], prebuilt calldata, priority-ordered `ClaimNextEvent` (now full version with `ComputePriority()` from Phase 3), **global kill switch** (`HALTED`/`DEGRADED`/`BALANCED` modes), **full capital safety envelope** (per-token + per-cohort caps), **event bus partitioning** (`PARTITION BY LIST (chain)`), **data retention/archival** (hot/warm/cold), **MEV-aware routing** (Flashbots, Beaverbuild, Eden), cost observability.

**Explicitly excluded:** New DTO types, new pipeline stages. Phase 6 is purely operational hardening of the existing pipeline.

### Event Types Emitted

| Event Type      | Emitter          | DTO / Notes                    |
| --------------- | ---------------- | ------------------------------ |
| `halted_event`  | risk controller  | `SystemStateDTO` (mode=HALTED) |
| `expired_event` | any worker (TTL) | `ExpiredEventDTO`              |
| `system_event`  | orchestrator     | metadata (halt/resume/mode)    |
| `archive_event` | archive worker   | metadata (archival complete)   |

### DTO Pipeline

Phase 6 adds no new DTO transformations. It **wraps** all existing pipeline workers with operational controls.

| Control Layer         | Applies To       | Input Event        | Side Effect / Output                                 |
| --------------------- | ---------------- | ------------------ | ---------------------------------------------------- |
| Kill switch           | all workers      | any event          | Drop selection events if HALTED; exits continue      |
| Capital envelope      | capital worker   | `selection_event`  | `AllocationDTO.Rejected=true` if envelope exceeded   |
| Wallet sharding       | execution worker | `allocation_event` | Route to shard wallet via `hash(tokenAddr) % n`      |
| Execution semaphore   | execution worker | `allocation_event` | Acquire semaphore before submit; adaptive cap [5,20] |
| TTL expiry (enforced) | all workers      | any event          | `expired_event` on TTL breach                        |
| Archive               | archive worker   | _(periodic)_       | Move events to archive partition                     |

### Lifecycle Transitions

Phase 6 adds no new token lifecycle states. All existing `DETECTED → ... → POSITION_CLOSED` transitions remain unchanged.

Phase 6 **manages a system-level state machine** (not token-level):

| Controller      | From Mode  | To Mode    | Condition                               | Adapter Call        |
| --------------- | ---------- | ---------- | --------------------------------------- | ------------------- |
| risk controller | `BALANCED` | `DEGRADED` | drawdown ≥ `risk.degraded_drawdown_pct` | `UpsertSystemState` |
| risk controller | `DEGRADED` | `HALTED`   | drawdown ≥ `risk.halt_drawdown_pct`     | `UpsertSystemState` |
| risk controller | `HALTED`   | `BALANCED` | drawdown ≤ `risk.resume_drawdown_pct`   | `UpsertSystemState` |
| Telegram /mode  | any        | any        | operator override (logged, reversible)  | `UpsertSystemState` |

### Traceability

Phase 6 requires that ALL events — including `halted_event`, `system_event`, `archive_event` — carry:

| Field           | Rule                                                                                 |
| --------------- | ------------------------------------------------------------------------------------ |
| `TraceID`       | Copy from triggering event; system events may use `systemTraceID = SHA256("system")` |
| `CorrelationID` | Copy from triggering event                                                           |
| `CausationID`   | Set to triggering `EventID` (e.g., the last processed trade event triggering halt)   |
| `VersionID`     | Active `StrategyVersion.StrategyVersionID` at time of emission                       |

### Global Kill Switch — Risk Halt (from § 7.3)

New background worker `internal/workers/run_risk_controller.go`:

```go
// Runs every config.risk.check_interval_seconds
func RunRiskController(ctx context.Context, adapter database.Adapter, cfg Config) error {
    state := adapter.GetSystemState(ctx)
    dd    := computeDrawdown(ctx, adapter, cfg.risk.drawdown_window_hours)
    mode  := state.Mode
    switch {
    case dd >= cfg.risk.halt_drawdown_pct:
        mode = "HALTED"
    case dd >= cfg.risk.degraded_drawdown_pct:
        mode = "DEGRADED"
    default:
        if state.Mode == "HALTED" && dd <= cfg.risk.resume_drawdown_pct {
            mode = "BALANCED"
        }
    }
    return adapter.UpsertSystemState(ctx, contracts.SystemStateDTO{Mode: mode, DrawdownPct: dd})
}
```

Execution, Selection, Capital workers add a pre-check:

```go
state, _ := adapter.GetSystemState(ctx)
switch state.Mode {
case "HALTED":
    // Allow EXIT events only. All new-entry events → emit "halted_event", skip.
case "DEGRADED":
    // Entry allowed; SizeUsd *= cfg.risk.degraded_size_multiplier
case "BALANCED", "EXPLORATION", "STRICT":
    // Normal operation
}
```

**New adapter methods:** `GetSystemState`, `UpsertSystemState` (see `docs/db_adapter_spec.md` § 11.1).

### Full Capital Safety Envelope (from § 7.4)

Extends Phase 2's minimal `CheckEnvelope()` with per-token and per-cohort caps:

```go
// internal/modules/capital/envelope.go (update)
func (m *Module) CheckEnvelope(ctx context.Context, adapter database.Adapter, proposed contracts.AllocationDTO) (ok bool, rejectReason string)
// Rejects when ANY holds:
//   total_exposure_usd + proposed.SizeUsd > cfg.capital.max_total_exposure_usd
//   per_token_exposure_usd(token) + proposed.SizeUsd > cfg.capital.per_token_cap_usd     (NEW)
//   per_cohort_exposure_usd(cohort) + proposed.SizeUsd > cfg.capital.per_cohort_cap_usd  (NEW)
//   open_positions_count >= cfg.capital.max_concurrent_positions
```

Requires `adapter.GetExposureSummary(ctx, token, cohort string)` — O(1) query (see spec § 11.2).

### Event Bus Partitioning (from § 7.8)

The `events` table gains `PARTITION BY LIST (chain)`. Migration adds one partition child per configured chain. `ClaimNextEvent` gains optional `chain` parameter so a worker pool can be bound to a single partition (one set of workers per market, horizontal scale).

Migration: `database/migrations/20260101000006_event_partitioning.sql` (see spec § 11.4).

Adding a new chain = adding a partition child — no application code change required.

### Data Retention & Archival (from § 7.9)

New worker `internal/workers/run_archive.go` running every `cfg.retention.interval_hours`:

```
Hot  (events table):    keep last cfg.retention.hot_days  (default 7)
Warm (events table):    keep cfg.retention.warm_days (default 30), processed=TRUE only
Cold (events_archive):  older rows → INSERT ... SELECT + DELETE
```

`events_archive` is a partition child that can be detached and dumped. `token_lifecycle`, `executions`, `positions`, `strategy_versions`, `learning_records` are **never archived** (retained forever for auditability).

### MEV-Aware Execution (from § 7.10)

New file `internal/modules/execution/mev.go`:

```go
func (m *Module) PickRoute(alloc contracts.AllocationDTO, lat contracts.LatencyProfileDTO) string
// Returns "public" | "flashbots" | "beaverbuild" | "eden"
// Rule:
//   if alloc.SizeUsd >= cfg.mev.private_size_threshold_usd → cfg.mev.preferred_private
//   if detected front-run pattern → private
//   else → "public"
```

Gas escalation:

```
first_attempt:    basefee + cfg.gas.priority_fee_bps_of_base * basefee
replacement k:    prev_max_fee * cfg.execution.fee_bump_multiplier  (Phase 3)
hard_cap:         cfg.gas.max_priority_fee_gwei  (reject tx at sign-time if exceeded)
```

Slippage guard: `amountOutMin = expected_out * (1 - cfg.execution.slippage_guard_bps / 10_000)`. If `SlippageEstimateDTO.ExpectedP95Bps > slippage_guard_bps` at sign-time → `Status=rejected`, `RejectReason=slippage_guard`. `ExecutionResultDTO` gains `MEVProtected bool` and `ExecutionPath string` fields.

### File Structure

```
internal/resource_control/
├── rpc_budget.go                          // Token bucket per endpoint
├── gas_budget.go                          // Per-wallet daily cap, system cap
├── compute_budget.go                      // Worker counts + queue depth
├── priority.go                            // PRIORITY_BASE table (exits > entries)
├── backpressure.go                        // Shed rules + forbidden-drop list
├── halt.go                                // Halt conditions with auto-resume
└── resource_control_test.go

internal/modules/execution/
├── wallet_shard.go                        // hash(TokenAddress) % n
└── concurrency.go                         // Global execution semaphore [5, 20]

config/
├── priority.yaml                          // PRIORITY_BASE weights per event type
└── budgets.yaml                           // RPC, gas, compute caps

internal/workers/
├── run_risk_controller.go                 // Periodic — computes drawdown, transitions BALANCED/DEGRADED/HALTED
└── run_archive.go                         // Periodic — moves processed events to events_archive partition
```

### Function Contracts

```go
// internal/resource_control/rpc_budget.go
type RPCBudget interface {
    Acquire(ctx context.Context, endpoint string) error   // blocks or returns ErrBudgetExhausted
    Release(endpoint string)
}

// internal/resource_control/priority.go
func ComputePriority(eventType string, age time.Duration) int   // higher = processed first

// internal/resource_control/backpressure.go
type BackpressurePolicy interface {
    ShouldDrop(ctx context.Context, evt *database.Event) (drop bool, reason string)
    // Exits / confirmations / quarantine events are NEVER dropped
}

// internal/modules/execution/wallet_shard.go
func PickWallet(tokenAddress string, shards []WalletConfig) WalletConfig

// internal/modules/execution/concurrency.go
type ExecutionSemaphore interface {
    Acquire(ctx context.Context) error
    Release()
    AdjustLimit(newLimit int)  // adaptive on failure rate
}
```

### DTO Flow

No new DTOs. Modifies the orchestrator's event selection policy (priority ordering):

```
SELECT ... FROM events
 WHERE processed = FALSE AND event_type = ANY($1)
 ORDER BY
   (CASE event_type
      WHEN 'position_event_exit'      THEN 1000
      WHEN 'execution_replacement'    THEN 900
      WHEN 'position_event_open'      THEN 500
      WHEN 'allocation_event'         THEN 400
      WHEN 'validated_edge_event'     THEN 300
      WHEN 'edge_event'               THEN 200
      WHEN 'feature_event'            THEN 150
      WHEN 'data_quality_event'       THEN 120
      WHEN 'market_data_event'        THEN 100
      WHEN 'adjustment_event'         THEN 50
      ELSE 10
    END) DESC,
   created_at ASC
 FOR UPDATE SKIP LOCKED LIMIT 1
```

### Worker Flow (All Workers, Updated)

```
Before stage.Process(evt):
  1. rpcBudget.Acquire(chain_endpoint) or drop with reason "rpc_budget_exhausted"
  2. gasBudget.CheckWallet(wallet) or skip with reason "wallet_gas_cap"
  3. backpressure.ShouldDrop(evt) — but exits are NEVER dropped

On stage.Process() success:
  4. Emit next event
  5. Release RPC budget
  6. MarkEventProcessed
```

### Worker Flow (Risk Controller — periodic)

The risk controller runs on a separate ticker goroutine (not event-driven). It reads the current drawdown window, computes the system mode, and upserts `SystemStateDTO` via the adapter. All pipeline workers query this before processing entry-path events.

```
Runs every config.risk.check_interval_seconds

1. state := adapter.GetSystemState(ctx)
   dd    := computeDrawdown(ctx, adapter, cfg.risk.drawdown_window_hours)
   // computeDrawdown: SELECT SUM(pnl_usd) FROM positions
   //                  WHERE status='exited' AND closed_at > now()-interval
   //                  / SELECT SUM(size_usd) FROM positions WHERE status='open'
2. newMode := state.Mode
   switch:
   case dd >= cfg.risk.halt_drawdown_pct:
       newMode = "HALTED"
   case dd >= cfg.risk.degraded_drawdown_pct:
       newMode = "DEGRADED"
   default:
       if state.Mode == "HALTED" && dd <= cfg.risk.resume_drawdown_pct:
           newMode = "BALANCED"   // auto-resume

3. if newMode != state.Mode:
       adapter.UpsertSystemState(ctx, SystemStateDTO{Mode:newMode, DrawdownPct:dd})
       adapter.InsertEvent(ctx, {Type:"system_event",
           Payload:{
               PrevMode : state.Mode,
               NewMode  : newMode,
               DrawdownPct: dd,
               Trigger  : "risk_controller",
           },
           TraceID      : systemTraceID,      // SHA256("system")[:16]
           CausationID  : "",                 // root system event
           VersionID    : activeVersionID,
       })
```

**Pre-check gate (injected into all entry-path workers)**

All workers that process entry-creating events (`data_quality_event`, `edge_event`, `validated_edge_event`, `selection_event`, `allocation_event`) add this block before `stage.Process()`:

```
state, err := adapter.GetSystemState(ctx)
if err != nil { return err }

switch state.Mode {
case "HALTED":
    // Emit halted_event for observability, then skip without error (return nil)
    adapter.InsertEvent(ctx, {Type:"halted_event",
        Payload:{EventID:evt.EventID, Reason:"system_halted"},
        CausationID: evt.EventID, VersionID: activeVersionID})
    adapter.MarkEventProcessed(ctx, evt.EventID)
    return nil

case "DEGRADED":
    // Allow through — capital worker will apply degraded_size_multiplier
    // No action here; capital module reads config.risk.degraded_size_multiplier
}
// EXIT events (position_event with Status=exited) bypass this check unconditionally
```

### Worker Flow (Archive Worker — periodic)

```
Runs every config.retention.interval_hours

1. cutoff := now - config.retention.hot_days * 24 * time.Hour
2. archived, err := adapter.ArchiveEvents(ctx, cutoff)
   // Archive: INSERT INTO events_archive SELECT * FROM events
   //          WHERE created_at < cutoff AND processed = TRUE
   //          ON CONFLICT DO NOTHING
   //          DELETE FROM events WHERE created_at < cutoff AND processed = TRUE
   // Partition tables: token_lifecycle, executions, positions, strategy_versions,
   //                   learning_records are NEVER archived (retained forever)
3. if archived > 0:
      adapter.InsertEvent(ctx, {Type:"archive_event",
          Payload:{RowsArchived:archived, CutoffAt:cutoff},
          TraceID     : systemTraceID,
          CausationID : "",
          VersionID   : activeVersionID,
      })
4. log.Info("archive complete", "rows_archived", archived, "cutoff", cutoff)
```

### Adapter Calls (Complete)

**Event bus (updated `ClaimNextEvent`):**

```
adapter.ClaimNextEvent(ctx, group, eventTypes, opts)   // opts.ChainFilter (new: partition-aware)
adapter.InsertEvent(ctx, event)                        // halted_event, system_event, archive_event
adapter.MarkEventProcessed(ctx, eventID)
adapter.GetEventsByTraceIncludeArchive(ctx, traceID)   // full trace including archived events
```

**System state:**

```
adapter.GetSystemState(ctx)                            // risk controller reads current mode
adapter.UpsertSystemState(ctx, SystemStateDTO)         // risk controller + Telegram /mode writes
```

**Capital envelope:**

```
adapter.GetExposureSummary(ctx, chain)                 // per-chain + per-token open exposure
adapter.GetOpenPositions(ctx)                          // for capital gate pre-check
```

**Nonce / execution (enhanced for wallet sharding):**

```
adapter.AllocateNonce(ctx, wallet, chain)              // per-shard wallet now
adapter.ReconcileNonce(ctx, wallet, chain, actual)
```

**Archival:**

```
adapter.ArchiveEvents(ctx, olderThan time.Time)        // moves events to events_archive partition
```

### Failure Handling (Budget / Halt)

| Condition                      | Action                                                                      |
| ------------------------------ | --------------------------------------------------------------------------- |
| RPC budget exhausted           | Wait `config.budgets.rpc_wait_ms`; if still exhausted, shed low-priority    |
| Gas daily cap reached (wallet) | Rotate to next wallet shard with remaining budget                           |
| System daily gas cap reached   | Halt NEW entries; ALLOW exits unconditionally                               |
| Compute queue depth > max      | Shed entries (forbidden to shed exits)                                      |
| Halt triggered                 | Emit `system_halt_event`; Telegram alert; auto-resume when condition clears |

### Exit Criteria

- [ ] System handles 10× Phase 2 baseline (market_data_events/sec) without unbounded queue growth
- [ ] End-to-end `executed_trade_latency_p95 < 1500ms` (ingestion → execution_event)
- [ ] `system_daily_gas_cap` enforcement verified in simulation
- [ ] Exit event always processes even when entry queue has 10k+ pending events (synthetic test)
- [ ] Wallet sharding: `hash(token) % n` deterministic; same token always routes to same wallet
- [ ] Global execution semaphore adjusts (5 → 20) based on failure rate
- [ ] Cost dashboards surface `cost_per_trade_usd`, `rpc_usage_rps`, `gas_spend_daily_usd`
- [ ] **Section I — Standard Execution Quality Gate:**
  - [ ] DTO integrity: all DTOs emitted by Phase 6 workers carry non-empty `TraceID`, `CorrelationID`, `VersionID`, `CausationID`
  - [ ] Lifecycle transitions: kill-switch halt (`SELECTED→REJECTED`) observable in `token_state_transition` on `/kill` command
  - [ ] Events emitted: `system_event` and `halted_event` emitted on mode transitions; `archive_event` emitted on archival batch
  - [ ] Adapter calls: zero direct SQL in `internal/modules/`; all resource control writes via adapter
  - [ ] Trace propagation: archived events retain original `trace_id`; `GetEventsByTraceIncludeArchive` returns complete chain
  - [ ] Deterministic replay: same event stream + same sharding config → same wallet assignments and execution order bit-for-bit

---

# DB Adapter Mapping

Concrete adapter method usage per phase. Full interface in `docs/db_adapter_spec.md` § 2.

| Phase | Adapter Methods Used                                                                                                                                                                                                           | Tables Touched                                                                               |
| ----- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | -------------------------------------------------------------------------------------------- |
| 0     | `Initialize`, `RunMigrations`, `Close`, `CreateStrategyVersion`, `GetActiveStrategyVersion`, `CreateRun`, `UpdateRunStage`, `UpdateRunStatus`, `GetRun`, `InsertEvent`, `ClaimNextEvent`, `MarkEventProcessed`, `GetEventByID` | `events`, `consumer_offsets`, `pipeline_runs`, `strategy_versions`, `_migrations`            |
| 1     | `InsertEvent`, `InsertMarketData`, `UpsertIngestionWatermark`, `GetIngestionWatermark` (new)                                                                                                                                   | `events`, `ingestion_state`, `rpc_endpoint_state`, `tokens`                                  |
| 2.1   | `InsertEvent`, `InsertDataQuality`, `StartLifecycle`, `TransitionState`                                                                                                                                                        | `events`, `data_quality_*` (implicit in events), `token_lifecycle`, `token_state_transition` |
| 2.2   | `InsertEvent`, `InsertFeature`, `TransitionState`                                                                                                                                                                              | `events`, `token_lifecycle`, `token_state_transition`                                        |
| 2.3   | `InsertEvent`, `InsertEdge`, `TransitionState`                                                                                                                                                                                 | Same                                                                                         |
| 2.4   | `InsertEvent`, `InsertValidatedEdge`, `TransitionState`                                                                                                                                                                        | Same                                                                                         |
| 2.5   | `InsertEvent`, `InsertSelection`, `TransitionState`, `GetOpenPositions`                                                                                                                                                        | Same + `positions`                                                                           |
| 2.6   | `InsertEvent`, `InsertAllocation`, `TransitionState`                                                                                                                                                                           | Same                                                                                         |
| 2.7   | `InsertEvent`, `InsertExecutionResult`, `AllocateNonce`, `ReconcileNonce`, `TransitionState`                                                                                                                                   | `events`, `executions`, `wallet_nonce_state`, `token_lifecycle`                              |
| 2.8   | `InsertEvent`, `InsertPositionState`, `GetOpenPositions`, `GetPosition`, `TransitionState`                                                                                                                                     | `events`, `positions`                                                                        |
| 3     | All Phase 2 methods + `QuarantineToken`, `GetEventsByTrace`, `GetEventsByCorrelation`, `GetFailureChain`, `InsertEvaluation`, `GetExecutionByLifecycle`, `GetShadowTradesByWindow`                                             | `token_state_violation`, `evaluations`                                                       |
| 4     | `InsertEvent` (probability/slippage/latency), `GetActiveStrategyVersion`                                                                                                                                                       | `events` only (model outputs are event-sourced)                                              |
| 5     | `InsertLearningRecord`, `InsertEvaluation`, `CreateStrategyVersion`, `GetStrategyVersion`, `SetStrategyVersionStatus`, `GetShadowVersion`                                                                                      | `learning_records`, `evaluations`, `shadow_trades`, `strategy_versions`                      |
| 6     | `GetSystemState`, `UpsertSystemState`, `GetExposureSummary`, `GetEventsByTraceIncludeArchive` — also: `ClaimNextEvent` gains optional `chain` partition filter                                                                 | `system_states`, `events_archive`                                                            |

**Rule:** No phase introduces an adapter method not declared in `docs/db_adapter_spec.md` § 2 without updating that spec first.

---

# DTO Pipeline Map

Full DTO flow — matches `docs/architecture.md` End-to-End Pipeline diagram exactly.

```
                  Layer 0 (Phase 1)
                        │
                        ▼
                 MarketDataDTO
                 [market_data_event]
                        │
                        ▼
                 Layer 1 (Phase 2.1 / expanded Phase 4)
                 DataQualityDTO
                 [data_quality_event]
                        │  (PASS | RISKY_PASS continues)
                        ▼
                 Layer 2 (Phase 2.2 / expanded Phase 4)
                 FeatureDTO + FeatureConfidence
                 [feature_event]
                        │
          ┌─────────────┼─────────────┐
          ▼             ▼             ▼
    Layer 3 (Ph 2.3)  Layer 4 (Ph 4)  Layer 4 (Ph 4)
    EdgeDTO           ProbabilityDTO  SlippageDTO
    [edge_event]      [prob_event]    [slippage_event]
          │             │             │
          └─────────────┼─────────────┘
                        ▼
                  Layer 5 (Phase 2.4 / priors replaced in Phase 4)
                  ValidatedEdgeDTO
                  [validated_edge_event]
                        │  (ACCEPT continues)
                        ▼
                  Layer 6 (Phase 2.5)
                  SelectionOutputDTO
                  [selection_event]
                        │  (Selected == true)
                        ▼
                  Layer 7 (Phase 2.6)
                  AllocationDTO
                  [allocation_event]
                        │
                        ▼
                  Layer 8 (Phase 2.7 / realism in Phase 3)
                  ExecutionResultDTO                     ← Token state: SELECTED → EXECUTED
                  [execution_event]
                        │  (Success / Status=confirmed)
                        ▼
                  Layer 9 (Phase 2.8)
                  PositionStateDTO (open)                ← Token state: EXECUTED → POSITION_OPEN
                  [position_event (Status=open)]
                        │
                  PositionStateDTO (exited)              ← Token state: POSITION_OPEN → POSITION_CLOSED
                  [position_event (Status=exited)]
                        │  (Status=exited → TransitionState(POSITION_CLOSED→EVALUATED))
                        │
                  Layer 10a (Phase 3 — Evaluation Engine)
                        ▼
                  EvaluationDTO                          ← Token state: POSITION_CLOSED → EVALUATED (terminal)
                  [evaluation_event]  ← mandatory pre-learning
                        │
                  Layer 10b (Phase 5 — Learning Engine)
                        ▼
                  LearningRecordDTO (shadow=false)
                  [learning_record_event]
                        │
                        ▼
                  StrategyVersion update (versioned, bounded, family-isolated)
                  → feeds back into all consumer workers as new VersionID
```

**Shadow Trade Loop (Phase 5):**

```
<any rejection>  ──> LearningRecordDTO (shadow=true)
                 ──> shadow_trades row (observation_complete=false)
                 ──> [Phase 5 observer waits observation_window_s]
                 ──> observed_return_pct populated; shadow classified TN|FN
```

---

# Phase Dependency Graph

```
                  ┌──────────┐
                  │ Phase 0  │  (P0, blocker)
                  │ Infra    │
                  └────┬─────┘
                       │
                       ▼
                  ┌──────────┐
                  │ Phase 1  │  (P1)
                  │ Ingest.  │
                  └────┬─────┘
                       │
                       ▼
                  ┌──────────┐
                  │ Phase 2  │  (P1) ★ FIRST TRADE GATE
                  │ MVP Slice│
                  │ 2.1→2.8  │
                  └────┬─────┘
                       │
                       ▼
                  ┌──────────┐
                  │ Phase 3  │  (P1.5)
                  │ State m. │
                  │ + Trace  │
                  │ + Retry  │
                  └────┬─────┘
                       │
                       ▼
                  ┌──────────┐
                  │ Phase 4  │  (P1.5)
                  │ Models + │
                  │ Full DQ  │
                  └────┬─────┘
                       │
               ┌───────┴───────┐
               ▼               ▼
          ┌──────────┐    ┌──────────┐
          │ Phase 5  │    │ Phase 6  │  (both P2, can run in parallel)
          │ Learning │    │ Resource │
          └──────────┘    └──────────┘
```

**Sub-phase parallelism within Phase 2** (2.1–2.8): each sub-phase owns a disjoint directory + contract file, so 2.1 / 2.2 / 2.3 can proceed in parallel once their DTO dependency is finalized. Recommended order:

```
2.1 (DQ)   ──┐
2.2 (Feat) ──┤── serial (DQ → Feat → Edge)
2.3 (Edge) ──┘

2.4 (Valid) ──> 2.5 (Select) ──> 2.6 (Capital) ──> 2.7 (Exec) ──> 2.8 (Position)
```

**Merge gates (mandatory before starting next phase):**

- Phase 0 merged to `main` with all tables present + worker loop tested
- Phase 1 merged with live ingestion verified + replay deterministic
- Phase 2 merged with **at least 1 testnet trade confirmed**
- Phase 3 merged with state machine + traceability enforcement active
- Phase 4 merged with model outputs replacing priors

---

## Go-Live Checklist

> All items must be checked before routing real capital on mainnet. Each item references the phase that introduces the requirement and has a deterministic pass/fail test.

### Infrastructure (Phase 0)

- [ ] `sniper migrate` on empty DB creates all tables (idempotent: re-run produces zero DDL errors, zero schema changes)
- [ ] `strategy_versions` table has exactly 1 row with `status='active'` at boot
- [ ] `SELECT ... FOR UPDATE SKIP LOCKED` worker loop: verified under 2 concurrent workers, no double-processing observed in `events` table
- [ ] All config YAML files load without error at startup; missing required key → panic with clear message

### Ingestion (Phase 1)

- [ ] Live `market_data_event` flowing from at least 2 RPC endpoints per chain
- [ ] Gap recovery: kill one RPC endpoint, reconnect — events resume without gap; no duplicate `market_data_event` for same block
- [ ] `ingestion_latency_p95 < 500ms` on Ethereum, `< 200ms` on BSC (measured over 1h window)
- [ ] Replay fixture block range twice → zero duplicate events in `events` table

### First Trade Path (Phase 2)

- [ ] At least 10 successful testnet trades (status=confirmed) with full causal chain in `events` table
- [ ] TTL expiry: inject 1 artificially delayed event; observe `expired_event` in DB and position `lifecycle=REJECTED`
- [ ] Capital envelope: attempt trade violating `max_total_exposure_usd` → `allocation_event.Rejected=true`, `RejectReason` non-empty
- [ ] Latency check: inject profile with `P95Ms > opportunity_window_ms` → `validated_edge_event.Decision=REJECT`, `RejectReason=latency_exceeds_window`
- [ ] Shadow mode: set `config.execution.mode=shadow`, execute 20 decisions → zero on-chain txs, all `executions.simulated=TRUE`

### Evaluation & Correctness (Phase 3)

- [ ] Invalid state transition (`lifecycle DETECTED → POSITION_OPEN` skip) → `token_state_violations` row created, event quarantined
- [ ] Stuck tx: synthetic low-gas submission → replacement tx submitted within `cfg.execution.replacement_threshold_ms`, same nonce, higher fee
- [ ] Telegram `/kill` → no new `allocation_event` emitted within 2 event-bus ticks; open positions continue to `position_event (exited)` normally
- [ ] Telegram `/resume` → new entries resume on next valid `validated_edge_event`
- [ ] Priority ordering: under queue depth of 100+ mixed events, `position_event (exit)` always claims before `allocation_event` (verified by log timestamps)

### Signal Quality (Phase 4)

- [ ] Probability model Brier score `< 0.25` on held-out testnet data (N ≥ 200 trades)
- [ ] Slippage p95 realized vs predicted error `< 30%` across 50+ testnet trades
- [ ] `pass_rate` (fraction of tokens passing full pipeline) in `[0.5%, 5%]` on 24h live replay
- [ ] Private RPC route selected for all trades above `cfg.mev.private_size_threshold_usd` (observable via `execution_event.ExecutionPath`)

### Learning Safety (Phase 5)

- [ ] Shadow version: new update stays in `status='shadow'` for `cfg.learning.shadow_window_minutes` before promotion
- [ ] Paper trading: 100 simulated decisions via `mode=shadow` → zero on-chain txs, all `LearningRecordDTO.Shadow=true`
- [ ] Rollback: inject 30 degraded shadow samples → rollback fires within `cfg.learning.post_promotion_watch_minutes`, previous `status='active'` version reinstated
- [ ] Bounded update: diff config snapshot before/after one cycle → exactly 1 parameter family changed, `Δ ≤ 10%`
- [ ] N-gate: cycle with N < 30 samples → no update emitted, `skipped_update_reason=insufficient_samples`

### Operational Safety (Phase 6)

- [ ] Kill switch: inject drawdown ≥ `cfg.risk.halt_drawdown_pct` → `SystemStateDTO.Mode=HALTED`, new `allocation_event` skipped within `check_interval_seconds`; open positions continue exiting
- [ ] Auto-resume: drawdown recedes below `cfg.risk.resume_drawdown_pct` → mode returns to `BALANCED` without manual intervention
- [ ] Capital envelope (full 4 caps): all 4 rejection reasons observable via injection test (total, per-token, per-cohort, max-positions)
- [ ] Gas hard cap: synthetic tx whose computed fee exceeds `cfg.gas.max_priority_fee_gwei` → rejected at sign-time, `Status=rejected`, `RejectReason=gas_hard_cap`
- [ ] MEV routing: all trades above threshold have `ExecutionPath != "public"` in `executions` table
- [ ] Archival: after `retention.hot_days` in staging, `events` table row count is bounded; `events_archive` contains older rows
- [ ] `executed_trade_latency_p95 < 1500ms` end-to-end over 1h production window

### Final Gate (All Phases)

- [ ] `grep -r 'import.*database' internal/modules/` → **zero matches**
- [ ] `grep -rnE 'INSERT|SELECT|UPDATE|DELETE' internal/modules/` → **zero matches**
- [ ] `go test ./...` passes without skips (unit + integration)
- [ ] `go vet ./...` passes without warnings
- [ ] All numeric thresholds verified in `config/*.yaml`; `grep -rn '[0-9]\{4,\}' internal/` returns only port numbers and SHA lengths
- [ ] Security: no private keys in logs (grep `0x[0-9a-fA-F]{64}` in captured stdout/stderr); no credentials in `config/*.yaml`
- [ ] Parameterized queries only: `grep -rn 'fmt.Sprintf.*INSERT\|fmt.Sprintf.*SELECT' database/` → **zero matches**
- [ ] `sniper migrate` on empty production DB → re-run → no errors both times

---

# Cross-Cutting Invariants

These invariants apply to **every phase** and are verified in every phase's exit criteria:

| Invariant                   | Verification                                                                                                                                                                                                                                                                               |
| --------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------ | ------ | ---------------------------------------- |
| Determinism                 | Replay fixture events → bit-for-bit identical outputs                                                                                                                                                                                                                                      |
| Idempotency                 | Insert same event twice → 1 row (`ON CONFLICT DO NOTHING`)                                                                                                                                                                                                                                 |
| DTO-only module boundaries  | `grep -r 'import.*database' internal/modules/` returns empty                                                                                                                                                                                                                               |
| No SQL in modules           | `grep -rnE 'INSERT                                                                                                                                                                                                                                                                         | UPDATE | DELETE | SELECT' internal/modules/` returns empty |
| Content-addressable IDs     | `EventID == SHA256(canonical_json(payload))[:16]`                                                                                                                                                                                                                                          |
| Traceability enforced       | `orphan_event_count` metric stays at 0                                                                                                                                                                                                                                                     |
| State machine enforced      | `token_state_violation_count` visible + quarantine observable                                                                                                                                                                                                                              |
| Config-driven parameters    | No magic numbers in module code; all tunables in `config/*.yaml`                                                                                                                                                                                                                           |
| Strategy versioning         | Every DTO has non-empty `VersionID`; changing config creates new `StrategyVersion` row                                                                                                                                                                                                     |
| Event-sourced state         | Dropping all projection tables and replaying events rebuilds state correctly                                                                                                                                                                                                               |
| Exit-path priority          | Under load test, exit events process before any new entry events                                                                                                                                                                                                                           |
| Wallet-nonce monotonicity   | `SELECT next_nonce FROM wallet_nonce_state` is strictly increasing per `(wallet, chain)`                                                                                                                                                                                                   |
| TTL enforced                | No DTO processed after `ExpiresAt`; observe `expired_event` in DB                                                                                                                                                                                                                          |
| Priority ordering correct   | Under load, exit events always claimed before entry events (`PRIORITY_EXIT ≥ 900`)                                                                                                                                                                                                         |
| Kill-switch responsive      | Mode transition from BALANCED → HALTED propagates within `check_interval_seconds`                                                                                                                                                                                                          |
| Capital envelope inviolable | No executed trade violates any of the 4 caps; verified via injection tests                                                                                                                                                                                                                 |
| Shadow isolation            | Shadow-mode executions (`Simulated=true`) leave on-chain state unchanged                                                                                                                                                                                                                   |
| Rollback deterministic      | Given same degraded shadow samples, rollback decision is reproducible across replays                                                                                                                                                                                                       |
| Archive lossless            | `events ∪ events_archive` bit-equals original write stream for any `TraceID`                                                                                                                                                                                                               |
| Lifecycle completeness      | Every token reaching `POSITION_CLOSED` eventually reaches `EVALUATED`; verified by querying `token_lifecycle WHERE state='POSITION_CLOSED' AND NOT EXISTS (SELECT 1 FROM token_state_transitions WHERE lifecycle_id=id AND to_state='EVALUATED')` returning 0 rows after evaluation window |
| Adjustment bounded          | Every `adjustment_event` has `Δparameter ≤ cfg.learning.max_param_delta_pct` (10%) and `SampleSize ≥ cfg.learning.min_samples` (30); verified via `SELECT * FROM strategy_versions WHERE delta_pct > 0.10 OR sample_size < 30` returning 0 rows                                            |
| Single-family adjustment    | No `adjustment_event` changes more than one parameter family per cycle; verified by `len(ChangedFields per family) ≤ 1` in event payload                                                                                                                                                   |

Every PR that introduces a new module or modifies a pipeline stage MUST attach a checklist confirming each of these invariants holds post-merge.
