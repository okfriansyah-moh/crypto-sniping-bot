# Database Adapter Specification — Project-Specific Extension

> **Canonical source of database contract.** Extends the skeleton adapter pattern with the concrete schema, DTOs, and invariants required by the deterministic event-driven sniper system (`docs/reference/architecture.md`).

---

## 1. Design Principles

- **Single entry point:** `shared/database/adapter.go` is the ONLY database interface
- **DTO-based I/O:** Adapter accepts and returns immutable DTOs from `shared/contracts/` — no raw rows, no maps, no primitives
- **Engine-agnostic interface:** Adapter API independent of Postgres; engine details isolated under `shared/database/engines/`
- **Portable SQL:** All SQL uses syntax compatible across Postgres-family engines (`ON CONFLICT`, `CURRENT_TIMESTAMP`, parameterized queries)
- **Modules are DB-free:** No module under `internal/modules/` may import `shared/database/`, import a DB driver, or contain SQL strings
- **Event-sourced:** The `events` table is the authoritative log. All DTO transitions append to it. State tables are derived projections.
- **Traceability enforced at write time:** Every event write validates `trace_id`, `correlation_id`, `causation_id` (except Layer 0), `version_id` — orphan events are rejected.
- **Multi-chain ready (additive):** The adapter is chain-agnostic at the DTO layer. The `Chain` field on every DTO (`eth | bsc | solana`) is a free-form string; chain-specific interpretation lives in the consuming module. Adding a new chain (Phase 7: Solana) introduces only **additive** adapter methods (e.g. `UpsertSolanaEndpointState`, `InsertSolanaSignature`) — never modifies the existing interface. Chain-restricted methods (e.g. `AllocateNonce`, `ReconcileNonce`) document the restriction in their contract; callers are responsible for gating on `chain`. `InsertExecutionResult` accepts `ExecutionResultDTO` for **all** chains; chain-specific fields (Solana signature, EVM tx hash) map to the same `TxHash` slot, and `Nonce` is unused (`0`) for Solana.
- **Solana ingestion state model (additive):** Solana ingestion uses two persistent state slices not present in the EVM model: a **monotonic watermark** (`solana_ingestion_watermark.slot`) and an **optional signature ledger** (`solana_signatures`) for idempotent execution. The adapter exposes `UpsertIngestionWatermark(ctx, chain, slot)` (rejects regressions with `ErrWatermarkRegression`), `GetIngestionWatermark`, `InsertSolanaSignature` (`ON CONFLICT DO NOTHING`), `UpdateSolanaSignatureStatus`, plus endpoint health/circuit-breaker methods (`UpsertSolanaEndpointState`, `GetSolanaEndpointState`, `UpsertSolanaEndpointHealth`, `ListSolanaEndpointsRanked`). All are additive; no existing call site is altered. The nonce manager (`AllocateNonce`, `ReconcileNonce`) remains EVM-only — Solana callers MUST NOT invoke it. See `docs/reference/architecture.md` § 3.11.10 and `docs/reference/implementation_roadmap.md` § 7.1.6 / § 7.7 for the production-grade hardening invariants. Migration: `shared/database/migrations/20260101000007_solana_tables.sql`.
- **Production hardening contract (architecture § 4.10):** The adapter is the **single** authoritative boundary for the determinism + exactly-once + failure-safety guarantees. Specifically: (a) event reads are ordered by `logical_order_key` (never `created_at`); (b) `ClaimExecution` is the only path that reserves an `execution_id` — in-memory locks are advisory only; (c) `MoveToDLQ` is the only path that records terminal failure of an event; (d) `UpsertPositionFromExecution` enforces the single-position-per-execution invariant via a UNIQUE constraint on `source_execution_id`; (e) `SetSystemHalt` / `IsSystemHalted` are the only legitimate read/write of the global kill switch; (f) `PromoteStrategyVersion` is the only path that activates a new strategy version. Direct SQL bypassing these methods is FORBIDDEN. Migration: `shared/database/migrations/20260101000012_production_hardening.sql`.

---

## 2. Adapter Interface (Go)

```go
package database

import (
    "context"

    "cryptobot/contracts"
)

// Adapter is the single entry point for all database operations.
// Every method accepts and returns immutable DTOs from contracts/.
// Every method is idempotent where writes are involved.
type Adapter interface {
    // ── Lifecycle ───────────────────────────────────────────────

    Initialize(ctx context.Context, cfg Config) error
    RunMigrations(ctx context.Context) error
    Close(ctx context.Context) error

    // ── Event Bus (append-only) ─────────────────────────────────

    // InsertEvent appends a DTO transition to the event bus.
    // Idempotent: INSERT ... ON CONFLICT (event_id) DO NOTHING.
    // Validates trace_id, correlation_id, causation_id, version_id.
    // Rejects if causation_id refers to a non-existent event (orphan prevention).
    InsertEvent(ctx context.Context, evt Event) error

    // ClaimNextEvent atomically claims the next unprocessed event
    // for a worker group using SELECT ... FOR UPDATE SKIP LOCKED.
    // Returns nil if the queue is empty.
    ClaimNextEvent(ctx context.Context, group string, eventTypes []string) (*Event, error)

    // MarkEventProcessed marks an event as handled. Called only after
    // successful stage execution and any resulting event writes.
    MarkEventProcessed(ctx context.Context, eventID string) error

    // GetEventByID fetches a specific event (used for trace traversal).
    GetEventByID(ctx context.Context, eventID string) (*Event, error)

    // ── Ingestion (Layer 0) ─────────────────────────────────────

    // UpsertIngestionWatermark records the last processed block per chain.
    // Used for gap recovery on reconnect.
    UpsertIngestionWatermark(ctx context.Context, chain string, blockNumber uint64) error
    GetIngestionWatermark(ctx context.Context, chain string) (uint64, error)

    InsertMarketData(ctx context.Context, dto contracts.MarketDataDTO) error
    GetMarketData(ctx context.Context, eventID string) (*contracts.MarketDataDTO, error)

    // ── Token Lifecycle State Machine (§ 4.7) ───────────────────

    // StartLifecycle creates a new lifecycle entry at state DETECTED.
    // Unique active lifecycle per token enforced by partial unique index.
    StartLifecycle(ctx context.Context, dto contracts.MarketDataDTO) (lifecycleID string, err error)

    // TransitionState applies a forward-only transition using CAS:
    //   UPDATE token_lifecycle
    //      SET current_state = $new,
    //          state_version = state_version + 1,
    //          updated_at = CURRENT_TIMESTAMP
    //    WHERE token_lifecycle_id = $id
    //      AND current_state = $expected_from
    //      AND state_version = $expected_version
    // Returns ErrInvalidTransition if no row is updated.
    // Also inserts an audit row into token_state_transition.
    TransitionState(ctx context.Context, req TransitionRequest) error

    GetLifecycle(ctx context.Context, lifecycleID string) (*Lifecycle, error)
    QuarantineToken(ctx context.Context, tokenAddress string, reason string) error

    // ── DTO persistence (one method per DTO type) ───────────────

    InsertDataQuality(ctx context.Context, dto contracts.DataQualityDTO) error
    InsertFeature(ctx context.Context, dto contracts.FeatureDTO) error
    InsertEdge(ctx context.Context, dto contracts.EdgeDTO) error
    InsertValidatedEdge(ctx context.Context, dto contracts.ValidatedEdgeDTO) error
    InsertSelection(ctx context.Context, dto contracts.SelectionOutputDTO) error
    InsertAllocation(ctx context.Context, dto contracts.AllocationDTO) error
    InsertExecutionResult(ctx context.Context, dto contracts.ExecutionResultDTO) error
    InsertPositionState(ctx context.Context, dto contracts.PositionStateDTO) error
    InsertEvaluation(ctx context.Context, dto contracts.EvaluationDTO) error
    InsertLearningRecord(ctx context.Context, dto contracts.LearningRecordDTO) error

    // ── Execution: Nonce Manager (§ 3.8.19) ─────────────────────
    //
    // EVM-ONLY. Nonce management is part of the EVM execution model.
    // Solana (Phase 7) does NOT use nonces — it uses a recent-blockhash +
    // signature-uniqueness ordering model. Solana workers MUST NOT call
    // AllocateNonce or ReconcileNonce. Callers MUST gate on
    //   chain ∈ {"eth", "bsc"}
    // before invoking these methods. See docs/reference/architecture.md § 3.11.6.

    // AllocateNonce atomically reserves the next nonce for a wallet.
    // Uses UPDATE ... RETURNING with row-level lock.
    // EVM-ONLY — see note above.
    AllocateNonce(ctx context.Context, walletAddress string, chain string) (nonce uint64, err error)

    // ReconcileNonce updates local state from on-chain eth_getTransactionCount.
    // EVM-ONLY — see note above.
    ReconcileNonce(ctx context.Context, walletAddress string, chain string, onchainNonce uint64) error

    // ── Positions ───────────────────────────────────────────────

    GetOpenPositions(ctx context.Context) ([]contracts.PositionStateDTO, error)
    GetPosition(ctx context.Context, positionID string) (*contracts.PositionStateDTO, error)

    // ── Strategy Versions (§ 4.1) ───────────────────────────────

    CreateStrategyVersion(ctx context.Context, sv StrategyVersion) error
    GetActiveStrategyVersion(ctx context.Context) (*StrategyVersion, error)
    GetStrategyVersion(ctx context.Context, versionID string) (*StrategyVersion, error)

    // ── Solana Adapter Methods (Phase 7 — additive, non-breaking) ─
    //
    // These methods support Solana ingestion + execution per
    // docs/reference/architecture.md § 3.11.10 (production-grade hardening).
    // EVM callers MUST NOT invoke them; Solana callers MUST NOT
    // invoke nonce methods (AllocateNonce / ReconcileNonce).
    //
    // Migration: 20260101000007_solana_tables.sql defines the
    // backing tables solana_rpc_endpoint_state, solana_signatures,
    // solana_ingestion_watermark, solana_endpoint_health.

    // Endpoint circuit breaker state (CLOSED|OPEN|HALF_OPEN)
    UpsertSolanaEndpointState(ctx context.Context, url, state string, failures int) error
    GetSolanaEndpointState(ctx context.Context, url string) (state string, failures int, err error)

    // Signature ledger — idempotent INSERT (ON CONFLICT DO NOTHING)
    InsertSolanaSignature(ctx context.Context, sig, executionID, walletAddr string, slot uint64) error
    UpdateSolanaSignatureStatus(ctx context.Context, sig, commitment, status string) error

    // Ingestion watermark — MONOTONIC; rejects regression with ErrWatermarkRegression
    UpsertIngestionWatermark(ctx context.Context, chain string, slot uint64) error
    GetIngestionWatermark(ctx context.Context, chain string) (slot uint64, err error)

    // Endpoint health for dynamic routing (§ 3.11.10 / roadmap § 7.7)
    UpsertSolanaEndpointHealth(ctx context.Context, h SolanaEndpointHealth) error
    ListSolanaEndpointsRanked(ctx context.Context) ([]SolanaEndpointHealth, error)

    // ── Production Hardening (architecture § 4.10) ──────────────
    //
    // These methods enforce the determinism + exactly-once + failure
    // safety contract. They are additive; existing call sites are
    // unaffected. Every method on this block uses ON CONFLICT DO NOTHING
    // semantics where applicable — "0 rows" is the SUCCESS path for
    // idempotent retries, never an error.

    // Event ordering (§ 4.10.A) — claim a batch of unprocessed events for
    // a given consumer + chain in strict ascending logical_order_key,
    // restricted to the worker's partition shard.
    //   ORDER BY logical_order_key ASC  (NEVER created_at)
    ClaimNextEvents(ctx context.Context, q EventClaimQuery) ([]contracts.EventEnvelope, error)

    // EventClaimQuery {Chain, Consumer, WorkerID, NumWorkers, Limit, MinOrderingKey}

    // DLQ (§ 4.10.C)
    IncrementEventRetry(ctx context.Context, eventID, consumer string) (retryCount int, err error)
    MoveToDLQ(ctx context.Context, e DLQEntry) error
    RequeueFromDLQ(ctx context.Context, eventID string) error
    ListDLQ(ctx context.Context, filter DLQFilter) ([]DLQEntry, error)

    // Exactly-once execution lock (§ 4.10.D)
    //   INSERT INTO executions (execution_id, ...) VALUES (...)
    //   ON CONFLICT (execution_id) DO NOTHING RETURNING execution_id;
    // Returns claimed=true if this caller is the first to claim the ID,
    // false if another worker already claimed it (caller MUST NOT submit).
    ClaimExecution(ctx context.Context, dto contracts.AllocationDTO) (claimed bool, err error)

    // Position consistency (§ 4.10.E)
    UpsertPositionFromExecution(ctx context.Context, p contracts.PositionStateDTO) (created bool, err error)
    ListOpenPositionsForReconciliation(ctx context.Context) ([]contracts.PositionStateDTO, error)
    AdjustPositionAmount(ctx context.Context, positionID string, onchainAmount string, reason string) error
    ClosePositionForced(ctx context.Context, positionID string, reason string) error

    // Latency feedback loop (§ 4.10.F)
    InsertLatencyEvent(ctx context.Context, le LatencyEvent) error
    GetLatencyProfile(ctx context.Context, chain, endpoint, opKind string, windowSec int) (contracts.LatencyProfileDTO, error)

    // Config consistency (§ 4.10.G)
    PromoteStrategyVersion(ctx context.Context, newVersionID string, drainTimeoutSec int) error
    DrainAndCheckPipelineIdle(ctx context.Context, timeoutSec int) (idle bool, err error)

    // Circuit breaker / kill switch (§ 4.10.H)
    SetSystemHalt(ctx context.Context, halt bool, reason, operator string) error
    IsSystemHalted(ctx context.Context) (halted bool, reason string, err error)

    // Replay validation (§ 4.10.I)
    ComputeStateHash(ctx context.Context) (hexDigest string, err error)

    // ── Production Hardening — Stage 4 (§ 4.11) ─────────────────

    // Partition leases (§ 4.11.B)
    ClaimPartitions(ctx context.Context, chain, consumer, workerID string, n, ttlSec int) ([]int, error)
    RenewPartitions(ctx context.Context, chain, consumer, workerID string) error
    ReleasePartitions(ctx context.Context, chain, consumer, workerID string) error

    // Crash-safe recovery (§ 4.11.C)
    ListInFlightExecutions(ctx context.Context) ([]InFlightExecution, error)
    FinalizeExecution(ctx context.Context, executionID string, receipt ExecutionReceipt) error
    AbortReservedExecution(ctx context.Context, executionID, reason string) error
    MarkExecutionLost(ctx context.Context, executionID, reason string) error

    // Reorg handling (§ 4.11.D)
    RecordReorg(ctx context.Context, chain string, oldBlock, newBlock int64, depth int) error
    InvalidateBlockRange(ctx context.Context, chain string, fromBlock, toBlock int64) (affected int, err error)
    MarkPositionsUncertain(ctx context.Context, chain string, fromBlock int64, reason string) error
    ListReorgPendingExecutions(ctx context.Context, chain string) ([]Execution, error)
    ReResolveExecutionAfterReorg(ctx context.Context, executionID, txHash string, outcome ReorgOutcome) error

    // Evaluation invariant (§ 4.11.E)
    RecordExecutionForEvaluation(ctx context.Context, executionID string, deadlineSec int) error
    MarkEvaluationDone(ctx context.Context, executionID string) error
    ListMissingEvaluations(ctx context.Context) ([]MissingEvaluation, error)

    // Backpressure (§ 4.11.F)
    GetUnprocessedCount(ctx context.Context, chain, consumer string) (int64, error)
    RecordDrop(ctx context.Context, chain, reason, tokenAddress, score string) error

    // ── Trace Queries (§ 4.8) ───────────────────────────────────

    GetEventsByTrace(ctx context.Context, traceID string) ([]Event, error)
    GetEventsByCorrelation(ctx context.Context, correlationID string) ([]Event, error)
    GetFailureChain(ctx context.Context, failedEventID string) ([]Event, error)

    // ── Pipeline Runs ───────────────────────────────────────────

    CreateRun(ctx context.Context, run PipelineRun) error
    UpdateRunStage(ctx context.Context, runID string, stage string) error
    UpdateRunStatus(ctx context.Context, runID string, status string) error
    GetRun(ctx context.Context, runID string) (*PipelineRun, error)
}

// Event is the event bus row representation.
type Event struct {
    EventID       string
    EventType     string      // e.g., "market_data_event", "feature_event"
    Payload       []byte      // canonical JSON of the DTO
    TraceID       string
    CorrelationID string
    CausationID   *string     // nil only for Layer 0 events
    VersionID     string
    CreatedAt     string      // ISO 8601
    Processed     bool
}

// TransitionRequest carries the CAS parameters for a state transition.
type TransitionRequest struct {
    LifecycleID       string
    ExpectedFromState string  // current_state value at time of read
    ExpectedVersion   int64   // state_version value at time of read
    NewState          string  // target state (must be valid forward transition)
    TraceID           string
    CorrelationID     string
    Reason            string
}

// Lifecycle is the current state of a token's journey through the pipeline.
type Lifecycle struct {
    TokenLifecycleID string
    TokenAddress     string
    CurrentState     string
    StateVersion     int64
    TerminalReason   *string
    CreatedAt        string
    UpdatedAt        string
}

// StrategyVersion is an immutable snapshot of all tunable config.
type StrategyVersion struct {
    StrategyVersionID string          // SHA256(config_snapshot)[:16]
    ConfigSnapshot    []byte          // canonical JSON
    CreatedAt         string
    ActivatedAt       *string
    DeactivatedAt     *string
}

// PipelineRun tracks a per-market pipeline execution.
type PipelineRun struct {
    RunID              string
    TraceID            string
    Status             string         // started|processing|completed|partial|failed
    LastCompletedStage *string
    StrategyVersionID  string
    CreatedAt          string
    UpdatedAt          string
}
```

---

## 3. SQL Compatibility Rules

### Allowed

| Pattern                             | Example                                                                   |
| ----------------------------------- | ------------------------------------------------------------------------- |
| `ON CONFLICT DO NOTHING`            | `INSERT INTO events (...) VALUES (...) ON CONFLICT (event_id) DO NOTHING` |
| `ON CONFLICT DO UPDATE`             | `... ON CONFLICT (key) DO UPDATE SET col = EXCLUDED.col`                  |
| Parameterized queries               | `db.QueryContext(ctx, "SELECT ... WHERE id = $1", id)`                    |
| `CURRENT_TIMESTAMP`                 | `DEFAULT CURRENT_TIMESTAMP`                                               |
| `SELECT ... FOR UPDATE SKIP LOCKED` | Worker dequeue pattern                                                    |
| `RETURNING`                         | Atomic allocation with read-after-write                                   |
| Standard types                      | `TEXT`, `BIGINT`, `INTEGER`, `NUMERIC`, `BOOLEAN`, `JSONB`, `TIMESTAMP`   |

### Forbidden

| Pattern                        | Why                                     |
| ------------------------------ | --------------------------------------- |
| `INSERT OR IGNORE`             | SQLite-specific; not portable           |
| `INSERT OR REPLACE`            | SQLite-specific; not portable           |
| String interpolation in SQL    | SQL injection risk                      |
| `AUTOINCREMENT` / `SERIAL`     | IDs must be content-addressable `TEXT`  |
| Engine-specific functions      | Not portable                            |
| `NOW()` vs `CURRENT_TIMESTAMP` | Standardize on `CURRENT_TIMESTAMP`      |
| Stored procedures / triggers   | Business logic must live in Go, not SQL |

---

## 4. Engine Selection

```yaml
# shared/config/pipeline.yaml
database:
  engine: postgres
  host: localhost
  port: 5432
  database: sniper
  user: sniper
  password_env: SNIPER_DB_PASSWORD
  pool:
    max_open_conns: 20
    max_idle_conns: 5
    conn_max_lifetime_seconds: 3600
```

Directory layout:

```
database/
├── adapter.go                  # Adapter interface + types
├── migrations.go               # Migration runner
├── errors.go                   # Sentinel errors (ErrInvalidTransition, ErrOrphanEvent, ...)
├── engines/
│   └── postgres/
│       ├── postgres.go         # pgx/lib-pq implementation of Adapter
│       ├── events.go           # Event bus methods
│       ├── lifecycle.go        # State machine methods
│       ├── dtos.go             # DTO persistence methods
│       ├── nonce.go            # Nonce manager methods
│       └── trace.go            # Trace query helpers
└── migrations/
    ├── 20260101000001_initial_schema.sql
    ├── 20260101000002_ingestion_tables.sql
    ├── 20260101000003_trading_tables.sql
    ├── 20260101000004_state_machine.sql
    └── 20260101000005_learning_tables.sql
```

---

## 5. Migration Strategy

### Naming

`YYYYMMDDNNNNNN_description.sql` — strict ISO date + 6-digit sequence + snake-case description.

### Rules

1. Migrations are **append-only** — never modify existing files
2. All schema changes go through migrations — no ad-hoc `ALTER TABLE` in application code
3. `adapter.RunMigrations()` applies all pending migrations in order
4. Migration state tracked in `_migrations` table
5. All SQL in migrations uses portable syntax (§ 3 above)
6. Each migration is transactional (wrapped in `BEGIN; ... COMMIT;`)

---

## 6. Project Schema (Full)

### 6.1 Event Bus

```sql
CREATE TABLE events (
    event_id       TEXT        PRIMARY KEY,                   -- SHA256(payload_signature)[:16]
    event_type     TEXT        NOT NULL,
    payload        JSONB       NOT NULL,
    trace_id       TEXT        NOT NULL,
    correlation_id TEXT        NOT NULL,
    causation_id   TEXT,                                      -- FK to events.event_id; NULL only for Layer 0
    version_id     TEXT        NOT NULL,
    created_at     TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP,
    processed      BOOLEAN     NOT NULL DEFAULT FALSE,
    CONSTRAINT fk_causation FOREIGN KEY (causation_id) REFERENCES events(event_id)
);
CREATE INDEX idx_events_unprocessed  ON events (processed, created_at) WHERE processed = FALSE;
CREATE INDEX idx_events_trace        ON events (trace_id);
CREATE INDEX idx_events_correlation  ON events (correlation_id);
CREATE INDEX idx_events_causation    ON events (causation_id);
CREATE INDEX idx_events_type         ON events (event_type, processed, created_at);

CREATE TABLE consumer_offsets (
    consumer_group  TEXT        PRIMARY KEY,
    last_event_id   TEXT        NOT NULL,
    updated_at      TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

### 6.2 Ingestion (Layer 0)

```sql
CREATE TABLE ingestion_state (
    chain                TEXT       PRIMARY KEY,
    last_processed_block BIGINT     NOT NULL,
    last_synced_at       TIMESTAMP  NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE rpc_endpoint_state (
    endpoint_url      TEXT       PRIMARY KEY,
    chain             TEXT       NOT NULL,
    failure_count     INTEGER    NOT NULL DEFAULT 0,
    last_failure_at   TIMESTAMP,
    circuit_open      BOOLEAN    NOT NULL DEFAULT FALSE,
    circuit_opened_at TIMESTAMP
);
```

### 6.3 Tokens (Layer 1 rollup — queryable projection)

```sql
CREATE TABLE tokens (
    token_address TEXT       PRIMARY KEY,                    -- checksummed address
    chain         TEXT       NOT NULL,
    first_seen_at TIMESTAMP  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_seen_at  TIMESTAMP  NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_tokens_chain ON tokens (chain);
```

### 6.4 Token Lifecycle State Machine (§ 4.7)

```sql
CREATE TABLE token_lifecycle (
    token_lifecycle_id  TEXT        PRIMARY KEY,             -- SHA256(token_address||first_detect_trace_id)[:16]
    token_address       TEXT        NOT NULL REFERENCES tokens(token_address),
    current_state       TEXT        NOT NULL,                -- enum: DETECTED|FILTERED|FEATURED|EDGE_DETECTED|VALIDATED|SELECTED|EXECUTED|POSITION_OPEN|EXITED|EVALUATED|REJECTED|FAILED
    state_version       BIGINT      NOT NULL DEFAULT 1,      -- optimistic-concurrency counter
    terminal_reason     TEXT,                                -- set when current_state ∈ {REJECTED, FAILED, EVALUATED}
    created_at          TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP
);
-- One active (non-terminal) lifecycle per token.
CREATE UNIQUE INDEX uq_active_lifecycle
    ON token_lifecycle (token_address)
    WHERE current_state NOT IN ('REJECTED', 'FAILED', 'EVALUATED');
CREATE INDEX idx_lifecycle_state ON token_lifecycle (current_state, updated_at);

CREATE TABLE token_state_transition (
    transition_id      TEXT        PRIMARY KEY,              -- SHA256(lifecycle_id||from||to||trace_id)[:16]
    token_lifecycle_id TEXT        NOT NULL REFERENCES token_lifecycle(token_lifecycle_id),
    from_state         TEXT        NOT NULL,
    to_state           TEXT        NOT NULL,
    trace_id           TEXT        NOT NULL,
    correlation_id     TEXT        NOT NULL,
    reason             TEXT,
    transitioned_at    TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_transition_lifecycle ON token_state_transition (token_lifecycle_id, transitioned_at);

CREATE TABLE token_state_violation (
    violation_id       TEXT        PRIMARY KEY,
    token_lifecycle_id TEXT        NOT NULL REFERENCES token_lifecycle(token_lifecycle_id),
    attempted_from     TEXT        NOT NULL,
    attempted_to       TEXT        NOT NULL,
    trace_id           TEXT        NOT NULL,
    violated_at        TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP
);
```

### 6.5 Executions (Layer 8)

```sql
CREATE TABLE wallet_nonce_state (
    wallet_address TEXT       NOT NULL,
    chain          TEXT       NOT NULL,
    next_nonce     BIGINT     NOT NULL,
    last_onchain   BIGINT     NOT NULL,
    reconciled_at  TIMESTAMP  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (wallet_address, chain)
);

CREATE TABLE executions (
    execution_id         TEXT        PRIMARY KEY,            -- SHA256(allocation_id)[:16]
    token_lifecycle_id   TEXT        NOT NULL REFERENCES token_lifecycle(token_lifecycle_id),
    allocation_id        TEXT        NOT NULL,
    wallet_address       TEXT        NOT NULL,
    chain                TEXT        NOT NULL,
    nonce_used           BIGINT      NOT NULL,
    tx_hash              TEXT,                               -- NULL until submitted
    status               TEXT        NOT NULL DEFAULT 'pending',  -- pending|submitted|confirmed|reverted|dropped|replaced|failed
    attempts             INTEGER     NOT NULL DEFAULT 0,
    replaced             BOOLEAN     NOT NULL DEFAULT FALSE,
    replacement_count    INTEGER     NOT NULL DEFAULT 0,
    mempool_route        TEXT        NOT NULL,               -- public|private_flashbots|private_beaverbuild
    latency_ms           INTEGER,
    slippage_realized_bps INTEGER,
    final_gas_used       BIGINT,
    final_max_fee_wei    NUMERIC(78, 0),
    error_code           TEXT,
    created_at           TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_executions_status   ON executions (status, created_at);
CREATE INDEX idx_executions_lifecycle ON executions (token_lifecycle_id);
CREATE INDEX idx_executions_wallet   ON executions (wallet_address, chain, nonce_used);
```

### 6.6 Positions (Layer 9)

```sql
CREATE TABLE positions (
    position_id          TEXT        PRIMARY KEY,            -- SHA256(execution_id)[:16]
    token_lifecycle_id   TEXT        NOT NULL REFERENCES token_lifecycle(token_lifecycle_id),
    execution_id         TEXT        NOT NULL REFERENCES executions(execution_id),
    token_address        TEXT        NOT NULL,
    entry_price          NUMERIC(38, 18) NOT NULL,
    entry_size_usd       NUMERIC(20, 8)  NOT NULL,
    exit_price           NUMERIC(38, 18),
    exit_reason          TEXT,                               -- TP1|TP2|SL|TIME|MANUAL
    pnl_usd              NUMERIC(20, 8),
    pnl_pct              NUMERIC(10, 6),
    status               TEXT        NOT NULL DEFAULT 'open',  -- open|exited|failed
    opened_at            TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP,
    exited_at            TIMESTAMP,
    strategy_version_id  TEXT        NOT NULL REFERENCES strategy_versions(strategy_version_id)
);
CREATE INDEX idx_positions_status    ON positions (status, opened_at);
CREATE INDEX idx_positions_lifecycle ON positions (token_lifecycle_id);
```

### 6.7 Learning (Layer 10)

```sql
CREATE TABLE learning_records (
    record_id            TEXT        PRIMARY KEY,            -- SHA256(position_id||shadow_flag)[:16]
    token_lifecycle_id   TEXT        NOT NULL REFERENCES token_lifecycle(token_lifecycle_id),
    shadow               BOOLEAN     NOT NULL DEFAULT FALSE, -- TRUE for rejected opportunities
    outcome              TEXT        NOT NULL,               -- TP|SL|TIME|RUG|MISSED_PUMP|CORRECT_REJECT
    pnl_usd              NUMERIC(20, 8),
    pnl_pct              NUMERIC(10, 6),
    features_snapshot    JSONB       NOT NULL,
    edge_snapshot        JSONB       NOT NULL,
    prediction_error     NUMERIC(10, 6),
    classification       TEXT        NOT NULL,               -- TP|FP|TN|FN
    cohort               TEXT        NOT NULL,               -- liquidity_bucket:age_bucket:source
    strategy_version_id  TEXT        NOT NULL REFERENCES strategy_versions(strategy_version_id),
    recorded_at          TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_learning_cohort  ON learning_records (cohort, recorded_at);
CREATE INDEX idx_learning_version ON learning_records (strategy_version_id);
CREATE INDEX idx_learning_class   ON learning_records (classification, recorded_at);

CREATE TABLE evaluations (
    evaluation_id        TEXT        PRIMARY KEY,
    strategy_version_id  TEXT        NOT NULL REFERENCES strategy_versions(strategy_version_id),
    window_start         TIMESTAMP   NOT NULL,
    window_end           TIMESTAMP   NOT NULL,
    sample_size          INTEGER     NOT NULL,
    true_positive_count  INTEGER     NOT NULL,
    false_positive_count INTEGER     NOT NULL,
    true_negative_count  INTEGER     NOT NULL,
    false_negative_count INTEGER     NOT NULL,
    expectancy           NUMERIC(20, 8) NOT NULL,
    max_drawdown_pct     NUMERIC(10, 6) NOT NULL,
    brier_score          NUMERIC(10, 6),
    created_at           TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE shadow_trades (
    shadow_id            TEXT        PRIMARY KEY,
    token_lifecycle_id   TEXT        NOT NULL REFERENCES token_lifecycle(token_lifecycle_id),
    rejected_at_stage    TEXT        NOT NULL,               -- FILTER|VALIDATE|SELECT
    rejected_at          TIMESTAMP   NOT NULL DEFAULT CURRENT_TIMESTAMP,
    observation_window_s INTEGER     NOT NULL,
    observed_return_pct  NUMERIC(10, 6),                     -- populated after observation window
    observation_complete BOOLEAN     NOT NULL DEFAULT FALSE
);
CREATE INDEX idx_shadow_incomplete ON shadow_trades (observation_complete, rejected_at);
```

### 6.8 Strategy Versions & Pipeline Runs

(Defined in Phase 0 — § Phase 0 of `docs/reference/implementation_roadmap.md`.)

---

## 7. Transaction Model

- **Event writes:** single-statement transactions (atomic)
- **Lifecycle CAS transitions:** single-statement `UPDATE ... WHERE ... AND state_version = $v` followed by audit INSERT, inside the same transaction
- **Nonce allocation:** `UPDATE wallet_nonce_state SET next_nonce = next_nonce + 1 WHERE ... RETURNING next_nonce - 1` — atomic
- **Worker dequeue + stage execution:** dequeue and mark-processed are in separate transactions; stage work in between must be idempotent

```go
// Example: atomic CAS state transition
const sqlTransition = `
    UPDATE token_lifecycle
       SET current_state = $1,
           state_version = state_version + 1,
           updated_at    = CURRENT_TIMESTAMP
     WHERE token_lifecycle_id = $2
       AND current_state      = $3
       AND state_version      = $4
`
result, err := tx.ExecContext(ctx, sqlTransition, req.NewState, req.LifecycleID, req.ExpectedFromState, req.ExpectedVersion)
if rowsAffected, _ := result.RowsAffected(); rowsAffected == 0 {
    return ErrInvalidTransition
}
```

---

## 8. Idempotency Enforcement

1. **All INSERTs** use `ON CONFLICT (<pk>) DO NOTHING` (or `DO UPDATE` where progress updates are valid)
2. **Primary keys are content-addressable** — `SHA256(content_signature)[:16]`
3. **Duplicate inserts** are silently ignored (no errors bubbled up)
4. **State transitions** use CAS on `state_version` — only forward, only once
5. **Nonce allocation** uses atomic `UPDATE ... RETURNING` — no double-allocation possible
6. **Event processing** uses `SELECT ... FOR UPDATE SKIP LOCKED` — only one worker claims each event
7. **Replay invariant:** feeding the same event sequence twice produces the same database state

```sql
-- Idempotent event insert
INSERT INTO events (event_id, event_type, payload, trace_id, correlation_id, causation_id, version_id)
VALUES ($1, $2, $3, $4, $5, $6, $7)
ON CONFLICT (event_id) DO NOTHING;

-- Idempotent execution insert
INSERT INTO executions (execution_id, allocation_id, ...)
VALUES ($1, $2, ...)
ON CONFLICT (execution_id) DO NOTHING;
```

---

## 9. Worker Loop Pattern

```go
// Canonical worker loop — referenced by orchestrator for every stage.
func (w *Worker) Run(ctx context.Context) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }

        evt, err := w.adapter.ClaimNextEvent(ctx, w.group, w.eventTypes)
        if err != nil {
            return err
        }
        if evt == nil {
            time.Sleep(w.idleBackoff)  // deterministic config value, NOT random
            continue
        }

        output, stageErr := w.stage.Process(ctx, evt)
        if stageErr != nil {
            w.logFailure(evt, stageErr)
            // event stays unprocessed; retry per failure policy
            continue
        }

        // Write output event(s), then mark input processed — same transaction ideally.
        if err := w.adapter.InsertEvent(ctx, output); err != nil {
            return err
        }
        if err := w.adapter.MarkEventProcessed(ctx, evt.EventID); err != nil {
            return err
        }
    }
}
```

---

## 10. Rejected-Write Conditions

The adapter rejects writes in these cases (returns typed error, never silently drops):

| Condition                                                  | Error                    |
| ---------------------------------------------------------- | ------------------------ |
| Event missing `trace_id` / `correlation_id` / `version_id` | `ErrMissingTraceField`   |
| Event has `causation_id` referencing non-existent event    | `ErrOrphanEvent`         |
| Layer 0 event has non-nil `causation_id`                   | `ErrInvalidRootEvent`    |
| State transition fails CAS                                 | `ErrInvalidTransition`   |
| Transition target not in allowed forward set               | `ErrForbiddenTransition` |
| Nonce allocation encounters stale on-chain state           | `ErrNonceGap`            |
| Strategy version not registered                            | `ErrUnknownVersion`      |

---

## 11. Invariants Audited by Adapter

At every write:

1. `event_id` matches `SHA256(canonical_json(payload))[:16]` — rejects mismatch
2. `causation_id` (when non-null) exists in `events`
3. `version_id` exists in `strategy_versions`
4. DTO timestamps are ISO 8601 strings
5. All `TokenAddress` fields are checksummed (EIP-55) or reject
6. Enum fields (status, state, decision, classification) match their declared enum sets

---

# 11. Production Gap Extensions (Additive — No Breaking Changes)

> All schema changes are delivered via **new migration files** (`20260101000006_production_gaps.sql` and later). Existing tables gain columns with defaults so prior inserts remain valid. New tables do not replace any existing structure.

---

## 11.1 Schema Additions

### Migration `shared/database/migrations/20260101000006_production_gaps.sql`

```sql
-- ── events: priority + ttl ──────────────────────────────────────────────────
ALTER TABLE events ADD COLUMN IF NOT EXISTS priority   INTEGER NOT NULL DEFAULT 0;
ALTER TABLE events ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

-- Partial indexes — only unprocessed rows matter on the hot path.
CREATE INDEX IF NOT EXISTS idx_events_priority
    ON events (priority DESC, created_at ASC)
    WHERE processed = FALSE;

CREATE INDEX IF NOT EXISTS idx_events_expires
    ON events (expires_at)
    WHERE processed = FALSE AND expires_at IS NOT NULL;

-- ── system_state: singleton risk posture ────────────────────────────────────
CREATE TABLE IF NOT EXISTS system_state (
    singleton_key          TEXT         PRIMARY KEY DEFAULT 'global'
                                        CHECK (singleton_key = 'global'),
    mode                   TEXT         NOT NULL
                                        CHECK (mode IN ('BALANCED','STRICT','EXPLORATION','DEGRADED','HALTED')),
    drawdown_pct           NUMERIC(6,4) NOT NULL DEFAULT 0,
    drawdown_window_hours  INTEGER      NOT NULL DEFAULT 24,
    open_positions         INTEGER      NOT NULL DEFAULT 0,
    total_exposure_usd     NUMERIC(20,6) NOT NULL DEFAULT 0,
    active_strategy_id     TEXT         NOT NULL,
    shadow_strategy_id     TEXT         NOT NULL DEFAULT '',
    last_transition_reason TEXT         NOT NULL DEFAULT '',
    updated_at             TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    version_id             TEXT         NOT NULL,
    state_version          BIGINT       NOT NULL DEFAULT 0
);

-- ── strategy_versions: shadow/rollback lifecycle ────────────────────────────
ALTER TABLE strategy_versions
    ADD COLUMN IF NOT EXISTS status              TEXT        NOT NULL DEFAULT 'draft'
        CHECK (status IN ('draft','shadow','active','deactivated','rolled_back')),
    ADD COLUMN IF NOT EXISTS shadow_started_at   TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS promoted_at         TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS rolled_back_at      TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS parent_version_id   TEXT;

-- At most one ACTIVE strategy at any time (enforced in DB).
CREATE UNIQUE INDEX IF NOT EXISTS idx_strategy_versions_active
    ON strategy_versions ((1))
    WHERE status = 'active';

-- ── allocations/executions/learning: new additive columns ──────────────────
ALTER TABLE allocations
    ADD COLUMN IF NOT EXISTS rejected       BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS reject_reason  TEXT    NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS cohort_id      TEXT    NOT NULL DEFAULT '';

ALTER TABLE executions
    ADD COLUMN IF NOT EXISTS mev_protected      BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS execution_path     TEXT    NOT NULL DEFAULT 'public'
        CHECK (execution_path IN ('public','flashbots','beaverbuild','eden')),
    ADD COLUMN IF NOT EXISTS slippage_guard_bps INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS rejection_reason   TEXT    NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS simulated          BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE learning_records
    ADD COLUMN IF NOT EXISTS simulated       BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS expired_source  BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN IF NOT EXISTS strategy_status TEXT    NOT NULL DEFAULT 'active';
```

### Migration `shared/database/migrations/20260101000007_events_partitioning.sql`

(Applied only when enabling horizontal scale. Postgres 13+.)

```sql
-- Rename legacy table, create partitioned parent, copy data.
ALTER TABLE events RENAME TO events_legacy;

CREATE TABLE events (
    LIKE events_legacy INCLUDING ALL
) PARTITION BY LIST (chain);

-- Child per configured chain (created by migration script or at chain onboarding).
CREATE TABLE events_ethereum  PARTITION OF events FOR VALUES IN ('ethereum');
CREATE TABLE events_bsc       PARTITION OF events FOR VALUES IN ('bsc');
CREATE TABLE events_base      PARTITION OF events FOR VALUES IN ('base');
CREATE TABLE events_default   PARTITION OF events DEFAULT;

INSERT INTO events SELECT * FROM events_legacy;
DROP TABLE events_legacy;
```

### Migration `shared/database/migrations/20260101000008_events_archive.sql`

```sql
CREATE TABLE IF NOT EXISTS events_archive (
    LIKE events INCLUDING DEFAULTS INCLUDING CONSTRAINTS
);

CREATE INDEX IF NOT EXISTS idx_events_archive_trace
    ON events_archive (trace_id);

CREATE INDEX IF NOT EXISTS idx_events_archive_created
    ON events_archive (created_at);
```

---

## 11.2 Adapter Interface Additions

Append to the `Adapter` interface (additive methods only — no existing signature changes):

```go
// --- Priority + TTL aware dequeue ---
// Behavior change for ClaimNextEvent: ordering becomes
//   ORDER BY priority DESC, created_at ASC
// and excludes rows where expires_at IS NOT NULL AND expires_at < NOW().
// Signature unchanged — callers get the new behavior automatically.
//
// NEW: explicit expiry marker for rows the worker observed as expired.
MarkEventExpired(ctx context.Context, eventID string, reason string) error

// --- System state (singleton) ---
GetSystemState(ctx context.Context) (*contracts.SystemStateDTO, error)
UpsertSystemState(ctx context.Context, state contracts.SystemStateDTO, expectedVersion int64) (newVersion int64, err error)
// CAS on state_version. Returns ErrStaleState if expectedVersion mismatches.

// --- Exposure summary for capital envelope (O(1) via maintained aggregates) ---
type ExposureSummary struct {
    TotalUsd            float64
    PerToken            map[string]float64   // tokenAddress -> usd
    PerCohort           map[string]float64   // cohortID     -> usd
    OpenPositions       int32
}
GetExposureSummary(ctx context.Context) (*ExposureSummary, error)

// --- Strategy version lifecycle ---
SetStrategyVersionStatus(ctx context.Context, versionID string, newStatus string, reason string) error
// Enforces legal transitions:
//   draft       -> shadow | deactivated
//   shadow      -> active | deactivated
//   active      -> rolled_back | deactivated
//   deactivated -> (terminal)
//   rolled_back -> (terminal)
// Promotion to 'active' atomically demotes existing active to 'deactivated'.
GetActiveStrategy(ctx context.Context) (*contracts.StrategyVersion, error)
GetShadowStrategy(ctx context.Context) (*contracts.StrategyVersion, error)

// --- Archival ---
ArchiveEvents(ctx context.Context, olderThan time.Time, batchSize int) (archivedCount int, err error)
// Moves rows where processed=TRUE AND created_at < olderThan from events to events_archive.
// NEVER archives events linked to open positions (FK-safe: checks positions.status).
// Runs in single transaction per batch; idempotent.

GetEventsByTraceIncludeArchive(ctx context.Context, traceID string) ([]contracts.EventEnvelope, error)
// Union of events + events_archive. Used by replay and audit.

// --- Execution log (Telegram /executions command) ---
// ExecutionLogRow is a read-only projection joining token_lifecycle,
// market_data, and execution_results for operator visibility.
type ExecutionLogRow struct {
    TokenAddress   string
    Symbol         string
    Chain          string
    LifecycleState string
    Status         string
    ErrorCode      string
    TxHash         string
    UpdatedAt      string // ISO 8601
}
GetExecutionLog(ctx context.Context, limit int) ([]ExecutionLogRow, error)
// Returns up to `limit` rows for lifecycle states:
//   SELECTED, EXECUTED, POSITION_OPEN, POSITION_CLOSED, EVALUATED, FAILED
// Ordered by updated_at DESC.
// Implemented via LATERAL joins in postgres/lifecycle.go.
```

---

## 11.3 ClaimNextEvent Query (Updated)

Exact SQL used by `ClaimNextEvent`:

```sql
UPDATE events
   SET processed    = TRUE,
       processed_at = NOW(),
       processed_by = $1      -- worker_id
 WHERE event_id = (
       SELECT event_id FROM events
        WHERE processed = FALSE
          AND event_type = ANY($2)                          -- consumed types
          AND (chain = $3 OR $3 = '')                       -- optional partition filter
          AND (expires_at IS NULL OR expires_at > NOW())    -- TTL gate
        ORDER BY priority DESC, created_at ASC
          FOR UPDATE SKIP LOCKED
        LIMIT 1
 )
 RETURNING event_id, event_type, payload, trace_id, correlation_id,
           causation_id, priority, expires_at, created_at;
```

Rows whose `expires_at <= NOW()` are left unclaimed; the sweeper (§ 11.5) emits `expired_event` for them and then marks processed.

---

## 11.4 TTL Sweeper (Background Job)

Runs every `cfg.events.sweeper_interval_seconds` (default 1s):

```sql
WITH expired AS (
  SELECT event_id, event_type, trace_id, correlation_id, chain
    FROM events
   WHERE processed = FALSE
     AND expires_at IS NOT NULL
     AND expires_at <= NOW()
   LIMIT $1                                                  -- batch_size
     FOR UPDATE SKIP LOCKED
)
UPDATE events e
   SET processed    = TRUE,
       processed_at = NOW(),
       processed_by = 'ttl_sweeper'
  FROM expired
 WHERE e.event_id = expired.event_id
 RETURNING e.event_id, e.event_type, e.trace_id, e.correlation_id, e.chain;
```

For each returned row the sweeper emits an `expired_event` in the same transaction using `InsertEvent`.

---

## 11.5 Error Code Additions

| Error                  | Condition                                                      |
| ---------------------- | -------------------------------------------------------------- |
| `ErrEventExpired`      | Attempt to claim/process a row where `expires_at <= NOW()`     |
| `ErrStaleState`        | `UpsertSystemState` CAS mismatch                               |
| `ErrInvalidEnum`       | Write attempted with an enum value outside the allowed set     |
| `ErrIllegalTransition` | `SetStrategyVersionStatus` called with an illegal status move  |
| `ErrEnvelopeBreach`    | Adapter-level guard if write would violate exposure invariants |

---

## 11.6 Retention Policy (Operational)

| Tier | Location         | Retention                                                       | Query Access                     |
| ---- | ---------------- | --------------------------------------------------------------- | -------------------------------- |
| Hot  | `events`         | ≤ `cfg.retention.hot_days` (default 7)                          | Direct, fast                     |
| Warm | `events`         | ≤ `cfg.retention.warm_days` (default 30), `processed=TRUE` only | Direct, fast                     |
| Cold | `events_archive` | Indefinite (or detach partition for dump)                       | `GetEventsByTraceIncludeArchive` |

Never archived (forever in primary tables): `token_lifecycle`, `executions`, `positions`, `strategy_versions`, `learning_records`, `system_state`.

---

## 11.7 Idempotency & Determinism Invariants (Additions to § 4)

- New columns do NOT participate in `EventID` computation _except_ `priority` and `expires_at`, which ARE canonical (they affect claim order / lifetime and must be reproducible on replay).
- `SystemStateDTO` writes are CAS-guarded via `state_version`; concurrent updaters serialize deterministically.
- `SetStrategyVersionStatus` transitions are transactional; the `idx_strategy_versions_active` partial unique index guarantees the single-active invariant at the DB level (not only in application code).
