# PLAN.md — Time-Banded Rescan Layer (14-Band, 0–48h Coverage)

> **Version:** 2.0
> **Date:** May 10, 2026 (v1.0: May 3, 2026 — 4 bands; v2.0: May 10, 2026 — extended to 14 bands, 48h coverage)
> **Status:** Implemented (Phase 10 — Group G, fully extended)
> **Source of Truth:** `docs/reference/architecture.md`, `docs/analysis/profitability-gaps.md`, `.github/copilot-instructions.md`
> **Related design notes:** `docs/specs/2026-05-03-time-banded-rescan-design.md` (this file is the executable spec)
> **Runnable via:** `./scripts/run_parallel.sh start --mode=2 10`

---

## 1. Goal

Add a **second scanning track** to the pipeline that re-emits eligible tokens at **14 fixed age bands spanning 0–48h** after first detection, so the existing `MOMENTUM_EDGE` path can capture alpha that the `NEW_LAUNCH_EDGE` path missed across three distinct alpha windows:

- **Goal A** — Organic momentum buildup (0–8h): early-phase community growth, volume accumulation, and `MOMENTUM_EDGE` ignition on pump.fun and Raydium tokens that were illiquid at t=0.
- **Goal B** — Stalled position reversal (8–24h): tokens already held that show recovery from a temporary dip; rescan provides a second evaluation point for the DQ → Edge → Validation stack.
- **Goal C** — Post-dip recovery and CEX catalyst window (24–48h): macro reversal rescans, exchange listing rumor detection, and narrative-driven second-wave momentum (data: PNUT=11d, GOAT=37d, WIF=81d to $1B — second-wave alpha is real).

The capability is implemented as **one new periodic worker** that re-emits `market_data_event` for tokens whose first scan was _temporally_ unfavourable but not _structurally_ malicious. The downstream pipeline (DQ → Features → Edge → Validation → Selection → Capital → Execution → Position → Learning) is **completely unchanged** and processes rescanned tokens identically to fresh ones.

**Why** (priority order — explicit per operator):

1. **Profit first.** Today the pipeline only sees a token once. Tokens that build organic momentum over 2–8h are invisible after first detection. 14 bands across 48h unlocks `MOMENTUM_EDGE` opportunities at every alpha cluster identified in memecoin lifecycle data. See § 1.1 for data-driven band design rationale.
2. **Security second.** No new attack surface. The rescan worker is a pure DB reader + event emitter — it makes no RPC calls, holds no keys, executes no transactions.
3. **Capital protection third.** Filters out tokens with structural reject signatures (honeypot, high tax, rug score) so capital is never re-exposed to known-bad contracts. Skips tokens already in open positions to prevent double-entry.

**Not goals (explicit):**

- Not adding new edge taxonomies, new DTOs, or new event types.
- Not changing DQ, Features, Edge, Validation, Selection, Capital, Execution, Position, or Learning modules.
- Not adding probe/RPC re-fetch on rescan (separate future work — see § 11 Future Work).
- Not introducing new database engines or breaking the modular-monolith invariant.

---

## 1.1 Band Design Rationale (Data-Driven)

Band density is calibrated to historical memecoin alpha windows derived from Solana memecoin lifecycle data (100 tokens, Tier 1–10, May 2026 research):

| Alpha Window | Tokens                                  | Pattern                                     | Band Coverage                    |
| ------------ | --------------------------------------- | ------------------------------------------- | -------------------------------- |
| 0–6h         | Tier 9–10 (CHILLGUY, WOJAK, FAP)        | Explode fast, die fast                      | 15m / 30m / 45m / 1h / 1.5h / 2h |
| 30m–8h       | Tier 3–6 (MICHI, GIGA, FWOG, CHAD)      | pump.fun organic momentum buildup           | 2h / 3h / 4h / 6h / 8h           |
| 2h–48h       | Tier 4–5 (GOAT 37d, MEW 214d)           | Narrative builds over days                  | 8h / 12h / 24h                   |
| 6h–48h+      | Tier 1–2 (WIF 81d, PNUT 11d, BONK 356d) | CEX listing catalysts, second-wave momentum | 12h / 24h / 36h / 48h            |

**Key finding:** Uniform 15-minute intervals from 0–48h (192 events/token) have ~90% noise in the 8h–24h dead zone. Sparse bands with variable density concentrate rescan compute where historical data shows alpha clusters. The 14-band design reduces event volume by **93%** vs uniform 15m while maintaining full coverage of all alpha windows.

**Band structure (14 bands total):**

```
Phase 1 — Early dense (Goal A: organic momentum, 0–8h):
  15m → 30m → 45m → 1h → 1.5h → 2h → 3h → 4h → 6h → 8h

Phase 2 — Recovery checkpoints (Goals B+C: reversal + CEX catalyst, 12–48h):
  12h → 24h → 36h → 48h
```

**Age windows (seconds):**

| Band | MinAge (s) | MaxAge (s) | Width | Priority | Goal |
| ---- | ---------- | ---------- | ----- | -------- | ---- |
| 15m  | 900        | 1800       | 15m   | 80       | A    |
| 30m  | 1800       | 2700       | 15m   | 60       | A    |
| 45m  | 2700       | 3600       | 15m   | 40       | A    |
| 1h   | 3600       | 5400       | 30m   | 30       | A    |
| 1.5h | 5400       | 7200       | 30m   | 28       | A    |
| 2h   | 7200       | 10800      | 1h    | 26       | A    |
| 3h   | 10800      | 14400      | 1h    | 24       | A    |
| 4h   | 14400      | 21600      | 2h    | 22       | A    |
| 6h   | 21600      | 28800      | 2h    | 20       | A    |
| 8h   | 28800      | 43200      | 4h    | 18       | A+B  |
| 12h  | 43200      | 86400      | 12h   | 16       | B    |
| 24h  | 86400      | 129600     | 12h   | 14       | B+C  |
| 36h  | 129600     | 172800     | 12h   | 12       | C    |
| 48h  | 172800     | 201600     | 8h    | 10       | C    |

---

## 2. Architecture Overview

```
                         ┌────────────────────────────────────────┐
                         │  RescanWorker  (NEW, Layer 0.5)         │
                         │  • periodic ticker (interval_seconds)   │
                         │  • per band: GetTokensForRescan()       │
                         │  • emit market_data_event with new      │
                         │    EventID (band-namespaced, idempotent)│
                         │  • mode-adaptive eligibility filters    │
                         └──────────────┬──────────────────────────┘
                                        │  InsertEvent + InsertMarketData
                                        ▼
                                  market_data_event       ◀── existing event type
                                        │
        (unchanged) DQ → Features → Edge → Validation → Selection
                        Capital → Execution → Position → Learning
                                        │
                              EdgeType = MOMENTUM_EDGE
                              (fires naturally for age ≥ 5min)
```

**Key architectural decisions (non-negotiable):**

| Decision                                                                            | Rationale                                                                              |
| ----------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| Re-emit `market_data_event` (existing type)                                         | Zero pipeline changes. New event types would require touching DQ/features/edge wiring. |
| Content-addressable `EventID = SHA256(chain‖token‖band_name‖rescan_bucket_ts)[:16]` | Idempotent: two ticker cycles in the same bucket cannot duplicate. Replay-safe.        |
| Per-band SQL filter on `data_quality.honeypot_score / rug_score`                    | No DQ module changes; reuses already-stored sub-scores. Saves probe budget.            |
| Mode-adaptive thresholds (STRICT / BALANCED / EXPLORATION)                          | Extends existing `operational-modes` system. Aligns rescan aggressiveness with mode.   |
| `skip_open_positions = true`                                                        | Capital protection: never double-enter a token already held.                           |
| Worker is **DB-read-only** plus event emit                                          | No RPC, no keys, no on-chain calls — minimal attack surface.                           |
| One new adapter method `GetTokensForRescan`                                         | All other DB access reuses existing adapter methods.                                   |
| New migration **only** if helper indexes are needed (see § 5 Task 2)                | Zero schema changes to existing tables.                                                |
| Disabled by default in `pipeline.yaml`                                              | Safe rollout; operators flip `rescan.enabled: true` when ready.                        |

**Layer placement:**
The rescan worker sits at **Layer 0.5** — between Layer 0 (raw ingestion) and Layer 1 (DQ). It is documented as a Layer 0 satellite in `docs/reference/architecture.md` § 3.0.x (additive update — see § 8 Documentation).

---

## 3. Profit Hypothesis (Profit-First Justification)

Per `docs/analysis/profitability-gaps.md`, current `Edge` factor estimate is ~0.60 and `AdaptationQuality` ~0.20.

The rescan layer raises Edge × AdaptationQuality through three concrete mechanisms:

| Mechanism                                                                                                       | Profit factor affected | Quantification                                                                                                                           |
| --------------------------------------------------------------------------------------------------------------- | ---------------------- | ---------------------------------------------------------------------------------------------------------------------------------------- |
| Capture `MOMENTUM_EDGE` opportunities currently invisible (no second event arrives)                             | Edge                   | Estimated 30–50 % more candidate edges per day in BALANCED mode (from `data_quality` reject distribution)                                |
| Re-evaluate previously-passed-but-unselected tokens whose features matured                                      | Edge × Probability     | Tokens that scored `edge_strength < threshold` at t=0 may exceed threshold at t=30m as `VolumeMomentum` and `TxVelocityScore` accumulate |
| Provide more signal samples to Layer 10 Learning Engine (rescan trades count as additional cohort observations) | AdaptationQuality      | More `LearningRecord`s per cohort → faster convergence of bounded threshold updates                                                      |

The **profit-first skill gate** (see `.github/skills/profit-first/SKILL.md`) requires every new feature to demonstrate which factor it raises. This plan raises Edge directly and AdaptationQuality indirectly, with no negative impact on DataQuality (filters preserve scam rejection) or Capital (open-position skip preserves exposure caps).

---

## 4. Tech Stack & Scope

**No new dependencies.** Pure additive Go code following the existing skeleton-parallel pattern.

| Layer                        | What's added                                                                                                                 |
| ---------------------------- | ---------------------------------------------------------------------------------------------------------------------------- |
| `contracts/`                 | **Nothing.** `MarketDataDTO` is reused as-is. (Strict additive-only enforcement per copilot-instructions § Protected Files.) |
| `database/adapter.go`        | One new interface method: `GetTokensForRescan`                                                                               |
| `database/engines/postgres/` | One new file `rescan.go` implementing `GetTokensForRescan`                                                                   |
| `database/migrations/`       | One new migration `20260503000022_rescan_indexes.sql` (helper indexes only, no schema change)                                |
| `internal/app/config/`       | Extend `Config` with `RescanConfig` struct                                                                                   |
| `internal/workers/`          | New file `run_rescan.go` (ticker loop)                                                                                       |
| `cmd/server.go`              | One new `go func()` to start the worker                                                                                      |
| `config/pipeline.yaml`       | Additive `rescan:` block (disabled by default)                                                                               |
| Tests                        | Unit + integration tests under `internal/workers/` and `tests/integration/`                                                  |

---

## 5. File Layout (After Implementation)

```
crypto-sniping-bot/
├── config/
│   └── pipeline.yaml                                          # ADDITIVE: rescan: block
├── contracts/                                                 # UNCHANGED (protected, additive-only)
├── database/
│   ├── adapter.go                                             # ADDITIVE: GetTokensForRescan method
│   ├── engines/postgres/
│   │   └── rescan.go                                          # NEW
│   └── migrations/
│       └── 20260503000022_rescan_indexes.sql                  # NEW (idx-only)
├── internal/
│   ├── app/config/
│   │   └── rescan_config.go                                   # NEW (RescanConfig struct + defaults)
│   └── workers/
│       ├── run_rescan.go                                      # NEW (ticker + emit loop)
│       └── run_rescan_test.go                                 # NEW (worker unit tests)
├── tests/integration/
│   └── rescan_pipeline_test.go                                # NEW (end-to-end integration)
├── cmd/
│   └── server.go                                              # MODIFIED: wire RunRescan goroutine
└── docs/
    ├── PLAN.md                                                # this file
    └── specs/
        └── 2026-05-03-time-banded-rescan-design.md            # design doc (brainstorming output)
```

---

## 6. DTO & Event Flow (Reused — Zero New Contracts)

The rescan worker emits exactly one event type:

```
Event{
  EventID:        SHA256(chain‖token_address‖band_name‖bucket_ts)[:16],   // content-addressable
  EventType:      "market_data_event",                                     // existing
  TraceID:        new UUID (rescan starts a fresh trace)
  CorrelationID:  same as TraceID (root of new pipeline run)
  CausationID:    "" (root event — Layer 0 convention)
  VersionID:      pinned strategy version at worker startup
  Payload:        contracts.MarketDataDTO   // re-loaded from DB, with PoolAgeSeconds bumped
  Priority:       configurable per band (later bands = lower priority)
}
```

**Idempotency proof:** `bucket_ts = floor(now_unix / interval_seconds) * interval_seconds`. Two ticker iterations within the same bucket compute the same `EventID`. The adapter's `InsertEvent` uses `ON CONFLICT (event_id) DO NOTHING` — safe.

**Trace continuity:** Each rescan emits a **new TraceID**. Operators correlating original-vs-rescan traces use `token_address` (not trace_id). This matches the `traceability` skill rule R2 (one trace = one pipeline run).

---

## 7. Implementation Tasks

The plan is broken into **8 self-contained tasks**. Each task is implementable in a single focused phase-builder session. Dependency graph:

```
Task 1 (Config struct + YAML) ─────────────────────────┐
    │                                                   │
    ▼                                                   │
Task 2 (Migration: indexes only) ──────────┐           │
    │                                       │           │
    ▼                                       ▼           │
Task 3 (Adapter method GetTokensForRescan) ◀───────────┤
    │                                                   │
    ▼                                                   │
Task 4 (Postgres engine impl + adapter test)            │
    │                                                   │
    ▼                                                   │
Task 5 (Worker run_rescan.go) ◀─────────────────────────┘
    │
    ▼
Task 6 (server.go wire-up)
    │
    ▼
Task 7 (Integration tests)
    │
    ▼
Task 8 (Docs sync — architecture.md § 3.0.x, log-reviewer PRS)
```

---

### Task 1 — Configuration

**Goal:** Add `RescanConfig` so operators can enable, tune bands, and override per mode without code changes.

**Files (create):**

- `internal/app/config/rescan_config.go`

**Files (modify, additive only):**

- `internal/app/config/config.go` — embed `Rescan RescanConfig` field
- `config/pipeline.yaml` — append `rescan:` block

**Schema (Go):**

```go
// rescan_config.go
package config

type RescanConfig struct {
    Enabled           bool          `yaml:"enabled"`
    IntervalSeconds   int           `yaml:"interval_seconds"`         // 60
    SkipOpenPositions bool          `yaml:"skip_open_positions"`      // true
    Eligibility       RescanEligibility `yaml:"eligibility"`
    Bands             []RescanBand  `yaml:"bands"`
    ModeOverrides     map[string]RescanEligibility `yaml:"mode_overrides"`
}

type RescanEligibility struct {
    MaxHoneypotScore float64 `yaml:"max_honeypot_score"` // tokens above this are NEVER rescanned
    MaxRugScore      float64 `yaml:"max_rug_score"`
    MaxBuyTaxBps     int32   `yaml:"max_buy_tax_bps"`
    IncludePassed    bool    `yaml:"include_passed"`     // also rescan PASS/RISKY_PASS tokens
}

type RescanBand struct {
    Name           string `yaml:"name"`             // "15m", "30m", "45m", "1h"
    MinAgeSeconds  int    `yaml:"min_age_seconds"`
    MaxAgeSeconds  int    `yaml:"max_age_seconds"`
    Priority       int32  `yaml:"priority"`         // event priority; later bands lower
}
```

**Defaults (in `applyRescanDefaults`) — 14-band design (v2.0):**

```go
// Disabled by default — operators must opt in.
Enabled:           false,
IntervalSeconds:   60,
SkipOpenPositions: true,
Eligibility:       { MaxHoneypotScore: 0.5, MaxRugScore: 0.65, MaxBuyTaxBps: 3000, IncludePassed: true },
// Phase 1 — Early dense (Goal A: organic momentum, 0-8h)
// Phase 2 — Recovery checkpoints (Goals B+C: reversal + CEX catalyst, 12-48h)
Bands: [
  { Name: "15m",  MinAgeSeconds: 900,    MaxAgeSeconds: 1800,   Priority: 80 },
  { Name: "30m",  MinAgeSeconds: 1800,   MaxAgeSeconds: 2700,   Priority: 60 },
  { Name: "45m",  MinAgeSeconds: 2700,   MaxAgeSeconds: 3600,   Priority: 40 },
  { Name: "1h",   MinAgeSeconds: 3600,   MaxAgeSeconds: 5400,   Priority: 30 },
  { Name: "1.5h", MinAgeSeconds: 5400,   MaxAgeSeconds: 7200,   Priority: 28 },
  { Name: "2h",   MinAgeSeconds: 7200,   MaxAgeSeconds: 10800,  Priority: 26 },
  { Name: "3h",   MinAgeSeconds: 10800,  MaxAgeSeconds: 14400,  Priority: 24 },
  { Name: "4h",   MinAgeSeconds: 14400,  MaxAgeSeconds: 21600,  Priority: 22 },
  { Name: "6h",   MinAgeSeconds: 21600,  MaxAgeSeconds: 28800,  Priority: 20 },
  { Name: "8h",   MinAgeSeconds: 28800,  MaxAgeSeconds: 43200,  Priority: 18 },
  { Name: "12h",  MinAgeSeconds: 43200,  MaxAgeSeconds: 86400,  Priority: 16 },
  { Name: "24h",  MinAgeSeconds: 86400,  MaxAgeSeconds: 129600, Priority: 14 },
  { Name: "36h",  MinAgeSeconds: 129600, MaxAgeSeconds: 172800, Priority: 12 },
  { Name: "48h",  MinAgeSeconds: 172800, MaxAgeSeconds: 201600, Priority: 10 },
],
ModeOverrides: {
  "STRICT":           { MaxHoneypotScore: 0.30, MaxRugScore: 0.50, MaxBuyTaxBps: 1500, IncludePassed: false },
  "BALANCED":         {} /* uses defaults */,
  "EXPLORATION":      { MaxHoneypotScore: 0.60, MaxRugScore: 0.75, MaxBuyTaxBps: 4500, IncludePassed: true  },
  "VERY_EXPLORATION": { MaxHoneypotScore: 0.75, MaxRugScore: 0.85, MaxBuyTaxBps: 6000, IncludePassed: true  },
},
```

**Validation rules (must enforce in `Validate()`):**

1. `interval_seconds >= 10` (prevent ticker storm)
2. Each band's `min_age < max_age`
3. Bands sorted by `min_age` ascending (deterministic order)
4. `0.0 ≤ max_honeypot_score ≤ 1.0`
5. `0 ≤ max_buy_tax_bps ≤ 10000`
6. Mode override keys ∈ `{STRICT, BALANCED, EXPLORATION}`

**Skills:** `config-validation`, `coding-standards`, `code-quality`
**Agent:** `phase-builder` → `dto-guardian` (validates additive-only)
**Exit criteria:** `go build ./...` passes; config unit tests cover validation rules; defaults match this spec.

---

### Task 2 — Database Migration (Indexes Only)

**Goal:** Add helper indexes for the rescan query. **No schema changes** — all required columns already exist.

**File:** `database/migrations/20260503000022_rescan_indexes.sql`

```sql
-- Phase 10 — rescan layer support indexes.
-- Pure additive: CREATE INDEX IF NOT EXISTS only. No table or column changes.
-- See docs/plans/2026-06-10-profit-restoration-plan.md § Task 2.

BEGIN;

-- Composite index covering (chain, ingested_at) for age-band scans.
CREATE INDEX IF NOT EXISTS idx_market_data_chain_ingested_at
    ON market_data (chain, ingested_at DESC);

-- Latest data_quality row per token (rescan eligibility join).
CREATE INDEX IF NOT EXISTS idx_data_quality_token_evaluated
    ON data_quality (token_address, evaluated_at DESC);

-- Lifecycle current_state lookup for skip_open_positions filter.
CREATE INDEX IF NOT EXISTS idx_lifecycle_token_state
    ON token_lifecycle (token_address, current_state);

COMMIT;
```

**Rules per `migration-management` skill:**

- File name follows `YYYYMMDD000NNN_description.sql` exactly.
- All `CREATE INDEX IF NOT EXISTS` — idempotent, re-runnable.
- No `ALTER TABLE`, no `DROP`, no `RENAME`.
- Portable SQL only (no PG-specific extensions in this migration).

**Skills:** `migration-management`, `database-portability`, `idempotency`
**Agent:** `phase-builder` → `dto-guardian`
**Exit criteria:** `make migrate` applies cleanly on a fresh DB AND on a DB already at the previous migration; `EXPLAIN` on the rescan query confirms index usage (validated in Task 4).

---

### Task 3 — Adapter Interface Extension

**Goal:** Add the `GetTokensForRescan` method to `database.Adapter`.

**File (modify, additive only):** `database/adapter.go`

**Method signature:**

```go
// GetTokensForRescan returns up to `limit` MarketDataDTOs whose
// (current_time - market_data.ingested_at) falls in [minAge, maxAge],
// filtered by the latest data_quality row's eligibility (sub-scores ≤
// thresholds), excluding tokens whose token_lifecycle.current_state is
// in the in-flight set (POSITION_OPEN, EXECUTION_PENDING, etc.) when
// skipOpenPositions is true. Results are deterministic: ordered by
// ingested_at DESC, token_address ASC; one row per token (latest).
//
// Read-only. Idempotent. No side effects.
GetTokensForRescan(ctx context.Context, q RescanQuery) ([]contracts.MarketDataDTO, error)
```

**`RescanQuery` struct (define in `database/adapter.go`):**

```go
type RescanQuery struct {
    Chain              string  // optional filter; "" = all chains
    MinAgeSeconds      int
    MaxAgeSeconds      int
    MaxHoneypotScore   float64
    MaxRugScore        float64
    MaxBuyTaxBps       int32
    IncludePassed      bool    // include decision IN ('PASS','RISKY_PASS') alongside REJECT
    SkipOpenPositions  bool
    Limit              int
}
```

**Skills:** `dto`, `modularity`, `database-portability`
**Agent:** `phase-builder` → `dto-guardian` (verifies adapter signature; no DTO changes)
**Exit criteria:** Interface compiles; method documented with the contract above; `database/adapter_test.go` extends with a no-op stub if needed.

---

### Task 4 — Postgres Engine Implementation

**Goal:** Implement `GetTokensForRescan` on the postgres engine.

**File:** `database/engines/postgres/rescan.go`

**SQL (parameterized, portable):**

```sql
WITH latest_dq AS (
    SELECT DISTINCT ON (token_address)
        token_address, decision, honeypot_score, rug_score, buy_tax_bps
    FROM data_quality
    ORDER BY token_address, evaluated_at DESC
),
latest_lifecycle AS (
    SELECT DISTINCT ON (token_address) token_address, current_state
    FROM token_lifecycle
    ORDER BY token_address, updated_at DESC
)
SELECT DISTINCT ON (md.token_address)
    md.event_id, md.trace_id, md.correlation_id, md.causation_id, md.version_id,
    md.chain, md.market, md.block_number, md.block_hash, md.tx_hash, md.log_index,
    md.event_topic, md.pool_address, md.token_address, md.base_address,
    md.token0_address, md.token1_address,
    md.amount0_raw, md.amount1_raw, md.reserve_base_raw, md.reserve_token_raw,
    md.block_timestamp, md.ingested_at,
    md.rpc_endpoint, md.transport, md.confirmation_depth, md.reorged,
    md.expires_at, md.priority, md.symbol, md.name,
    md.bonding_curve_progress_bps
    -- (additive: select all dq detector input columns to preserve replay parity)
FROM market_data md
JOIN latest_dq dq ON dq.token_address = md.token_address
LEFT JOIN latest_lifecycle ll ON ll.token_address = md.token_address
WHERE
    md.ingested_at <= CURRENT_TIMESTAMP - ($1 || ' seconds')::interval
    AND md.ingested_at >= CURRENT_TIMESTAMP - ($2 || ' seconds')::interval
    AND ($3 = '' OR md.chain = $3)
    AND dq.honeypot_score <= $4
    AND dq.rug_score      <= $5
    AND dq.buy_tax_bps    <= $6
    AND (
        dq.decision = 'REJECT'
        OR ($7 AND dq.decision IN ('PASS', 'RISKY_PASS'))
    )
    AND (
        NOT $8
        OR ll.current_state IS NULL
        OR ll.current_state NOT IN (
            'POSITION_OPEN', 'EXECUTION_PENDING', 'CAPITAL_ALLOCATED', 'SELECTED'
        )
    )
ORDER BY md.token_address, md.ingested_at DESC
LIMIT $9
```

**Implementation rules:**

- Use `pgx`/`database/sql` parameterized queries — **never** string interpolation (`security-audit` skill, OWASP A03).
- Map rows into `contracts.MarketDataDTO` — direct field assignment, no reflection.
- On query error: wrap with `fmt.Errorf("postgres.GetTokensForRescan: %w", err)`.
- Empty result is **not** an error — return `([]MarketDataDTO{}, nil)`.

**Determinism:** `ORDER BY md.token_address, md.ingested_at DESC` produces identical results across runs given identical input. No `LIMIT` without `ORDER BY`.

**Skills:** `database-portability`, `idempotency`, `determinism`, `security-audit`, `coding-standards`
**Agent:** `phase-builder` → `security-auditor` (SQL injection scan)
**Exit criteria:** Unit test against test DB returns expected rows for all eligibility combinations; `EXPLAIN ANALYZE` shows index hits on `idx_market_data_chain_ingested_at` and `idx_data_quality_token_evaluated`.

---

### Task 5 — Worker (`run_rescan.go`)

**Goal:** Implement the periodic rescan ticker that re-emits `market_data_event`.

**File:** `internal/workers/run_rescan.go` (new)

**Skeleton (mirrors `run_ingestion_solana.go`):**

```go
package workers

// run_rescan.go — periodic worker that re-emits market_data_event for
// tokens in configured age bands. Pure DB reader + event emitter; no
// RPC, no keys, no on-chain calls. See docs/plans/2026-06-10-profit-restoration-plan.md § Task 5.

func RunRescan(
    ctx context.Context,
    adapter database.Adapter,
    cfg *config.Config,
    logger *slog.Logger,
) error {
    if !cfg.Rescan.Enabled {
        logger.Info("rescan_worker_disabled")
        <-ctx.Done()
        return ctx.Err()
    }

    sv, err := adapter.GetActiveStrategyVersion(ctx)
    if err != nil { return fmt.Errorf("run_rescan: pin version: %w", err) }
    versionID := sv.StrategyVersionID

    interval := time.Duration(cfg.Rescan.IntervalSeconds) * time.Second
    ticker := time.NewTicker(interval)
    defer ticker.Stop()

    logger.Info("rescan_worker_started",
        "version_id", versionID,
        "interval_seconds", cfg.Rescan.IntervalSeconds,
        "bands", len(cfg.Rescan.Bands))

    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case t := <-ticker.C:
            if err := runRescanTick(ctx, adapter, cfg, versionID, t, logger); err != nil {
                logger.Warn("rescan_tick_error", "error", err)
                // Never abort the worker — bounded failure per failure skill.
            }
        }
    }
}

func runRescanTick(
    ctx context.Context, adapter database.Adapter, cfg *config.Config,
    versionID string, tickTime time.Time, logger *slog.Logger,
) error {
    // 1. Read active operational mode for eligibility override.
    mode := "BALANCED"
    if state, _ := adapter.GetSystemState(ctx); state != nil && state.Mode != "" {
        mode = state.Mode
    }
    eligibility := resolveEligibility(cfg.Rescan, mode)

    // 2. Compute bucket timestamp (idempotency anchor).
    bucketTs := tickTime.Truncate(time.Duration(cfg.Rescan.IntervalSeconds) * time.Second).Unix()

    // 3. For each band: query and emit.
    for _, band := range cfg.Rescan.Bands {
        rows, err := adapter.GetTokensForRescan(ctx, database.RescanQuery{
            MinAgeSeconds:     band.MinAgeSeconds,
            MaxAgeSeconds:     band.MaxAgeSeconds,
            MaxHoneypotScore:  eligibility.MaxHoneypotScore,
            MaxRugScore:       eligibility.MaxRugScore,
            MaxBuyTaxBps:      eligibility.MaxBuyTaxBps,
            IncludePassed:     eligibility.IncludePassed,
            SkipOpenPositions: cfg.Rescan.SkipOpenPositions,
            Limit:             cfg.Rescan.MaxPerBandPerTick, // safety cap
        })
        if err != nil { return fmt.Errorf("band %s: %w", band.Name, err) }

        for _, dto := range rows {
            if err := emitRescanEvent(ctx, adapter, dto, band, bucketTs, versionID, logger); err != nil {
                logger.Warn("rescan_emit_failed", "token", dto.TokenAddress, "band", band.Name, "error", err)
                continue // Per-token failure must not abort the band (per monitoring-loop-engine skill).
            }
        }
        logger.Info("rescan_band_completed",
            "band", band.Name, "candidates", len(rows), "mode", mode)
    }
    return nil
}

func emitRescanEvent(
    ctx context.Context, adapter database.Adapter,
    dto contracts.MarketDataDTO, band config.RescanBand,
    bucketTs int64, versionID string, logger *slog.Logger,
) error {
    // Content-addressable EventID — idempotent per (token, band, bucket).
    h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%s|%d",
        dto.Chain, dto.TokenAddress, band.Name, bucketTs)))
    newEventID := hex.EncodeToString(h[:])[:16]

    // Fresh trace_id (rescan = new pipeline run).
    traceID := uuid.NewString()

    // Build new MarketDataDTO with bumped ingested_at and band-tagged metadata.
    rescanned := dto
    rescanned.EventID       = newEventID
    rescanned.TraceID       = traceID
    rescanned.CorrelationID = traceID
    rescanned.CausationID   = ""           // Layer 0 root convention
    rescanned.VersionID     = versionID
    rescanned.IngestedAt    = time.Now().UTC().Format(time.RFC3339Nano)
    rescanned.Transport     = "rescan_" + band.Name   // diagnostic tag
    rescanned.Priority      = band.Priority

    // Insert market_data row (idempotent on event_id).
    if err := adapter.InsertMarketData(ctx, rescanned); err != nil {
        return fmt.Errorf("insert_market_data: %w", err)
    }

    // Emit event.
    payload, _ := json.Marshal(rescanned)
    evt := database.Event{
        EventID:       newEventID,
        EventType:     "market_data_event",
        TraceID:       traceID,
        CorrelationID: traceID,
        CausationID:   "",
        VersionID:     versionID,
        Payload:       payload,
        Priority:      band.Priority,
    }
    return adapter.InsertEvent(ctx, evt)
}
```

**Rules:**

- **Determinism:** `EventID` is content-hashed; same band+token+bucket → same ID.
- **Idempotency:** `InsertEvent` and `InsertMarketData` already use `ON CONFLICT DO NOTHING`.
- **Per-token failure isolation:** one failure does not abort the band (per `monitoring-loop-engine` skill).
- **No RPC, no keys, no on-chain calls.**
- **Heartbeat:** emit `rescan_worker_heartbeat` every N ticks with counts (per `observability` skill).

**Skills:** `idempotency`, `determinism`, `failure`, `traceability`, `event-bus`, `observability`, `coding-standards`, `monitoring-loop-engine`
**Agent:** `phase-builder` → `module-builder` (writes worker) → `test-builder` (RED-GREEN-REFACTOR)
**Exit criteria:** Worker compiles; unit tests cover (a) disabled mode early-exits, (b) ticker emits N events per band, (c) duplicate bucket emits zero new events, (d) per-token error doesn't abort band, (e) mode override applied.

---

### Task 6 — Server Wire-Up

**Goal:** Start the rescan worker as a goroutine alongside other workers.

**File (modify):** `cmd/server.go`

**Change (additive `go func` block, follows existing patterns):**

```go
// Rescan worker — Phase 10 (Layer 0.5). Disabled unless cfg.Rescan.Enabled.
go func() {
    if err := workers.RunRescan(ctx, db, cfg, logger); err != nil && err != ctx.Err() {
        logger.Error("rescan_worker_exited", "error", err)
    }
}()
```

**Placement:** After `RunPositionPoll`, before `RunRiskController` — keep grouped with periodic background workers.

**Skills:** `modularity`, `coding-standards`
**Agent:** `phase-builder` → `integration` (verifies wiring)
**Exit criteria:** `go build ./...` passes; `make run` boots without panics; structured log line `rescan_worker_started` appears (or `rescan_worker_disabled` when flag is false).

---

### Task 7 — Integration Tests

**Goal:** Prove end-to-end pipeline behaviour with rescan enabled.

**File:** `tests/integration/rescan_pipeline_test.go`

**Scenarios (each as a top-level test function):**

1. **`TestRescan_DisabledByDefault`** — confirm worker does not emit when `enabled: false`.
2. **`TestRescan_15mBand_ReEmitsTemporalReject`** — seed a `market_data` row with `ingested_at = NOW() - 16min` and a `data_quality` row with `decision='REJECT', honeypot_score=0.2, rug_score=0.4`. Run one tick. Assert one new `market_data_event` exists with `transport='rescan_15m'`.
3. **`TestRescan_StructuralRejectExcluded`** — seed `honeypot_score=0.9`. Assert zero events emitted.
4. **`TestRescan_OpenPositionExcluded`** — seed lifecycle `current_state='POSITION_OPEN'`. Assert zero events emitted.
5. **`TestRescan_IdempotentOnSecondTick`** — run two ticks within the same bucket. Assert exactly one event in the bus.
6. **`TestRescan_ModeOverride_STRICT`** — set `system_state.mode='STRICT'`, seed `honeypot_score=0.4`. Assert zero events (STRICT cap is 0.30).
7. **`TestRescan_DownstreamPipeline_FiresMomentumEdge`** — full pipeline: seed an aged token with growing volume features → run tick → assert that a downstream `edge_event` with `EdgeType=MOMENTUM_EDGE` is produced.

**Skills:** `test-driven-development`, `test-generation`, `determinism`
**Agent:** `phase-builder` → `test-builder`
**Exit criteria:** All seven tests pass under `go test ./tests/integration/... -run Rescan`.

---

### Task 8 — Documentation Sync

**Goal:** Update `docs/reference/architecture.md` and the `log-reviewer` skill to recognise the rescan layer.

> Per copilot-instructions § Protected Files, `docs/` is read-only. **Exception:** this PLAN's Task 8 must update `docs/reference/architecture.md` because the rescan worker introduces a new layer — this is the same exception that applies to `PROGRESS_REPORT.md`. Operator must approve before this single edit.

**Files:**

- `docs/reference/architecture.md` — add § 3.0.5 "Rescan Layer (0.5)" describing the worker, its profit hypothesis, and its position in the pipeline. **One section, additive.**
- `.github/skills/log-reviewer/SKILL.md` — extend R4 detectors with rescan patterns and add PRS dimension #11 _or_ fold into existing dimensions (see § 9 below).
- `.github/skills/rescan-orchestration/SKILL.md` — **NEW** skill capturing the rescan band pattern for future reuse.
- `config/phases.yaml` — add Phase 10 entry under Group G (this is config, not docs — additive).

**Skills:** `docs-sync`, `parallel-dev-docs`
**Agent:** `phase-builder` → `merge-reviewer`
**Exit criteria:** All cross-references resolve; `log-reviewer` PRS rubric still totals 100; `phases.yaml` parses cleanly.

---

## 8. Phase Mapping for `run_parallel.sh`

This work registers as **Phase 10 — `rescan-layer`** in `config/phases.yaml`. Add the following block:

```yaml
10:
  name: "rescan-layer"
  complexity: 5
  group: "G" # NEW group: "Time-banded rescan, runs after Phase 9"
  skills: "rescan-orchestration, event-bus, idempotency, determinism, traceability, failure, config-validation, database-portability, migration-management, modularity, dto, code-quality, coding-standards, test-generation, test-driven-development, observability, operational-modes, monitoring-loop-engine, profit-first"
```

**Why complexity = 5?** No new DTOs, no new event types, no module changes. Single worker + one adapter method + one migration of indexes. Roughly half the size of Phase 1 (`dex-ingestion`).

**Group G rationale:** Must run after Phase 9 (profitability-restoration) so that DQ sub-scores are populated correctly — rescan eligibility queries `data_quality.honeypot_score`/`rug_score` which Phase 9 makes non-stub. Running Phase 10 before Phase 9 would filter on constant-zero scores and re-emit every reject indiscriminately.

**Update the comment block in `config/phases.yaml`:**

```
#   Group G — RESCAN LAYER:     Phase 10 (time-banded rescan) — after Phase 9
```

**Run command:**

```bash
./scripts/run_parallel.sh start --mode=2 10
```

Mode 2 (token-optimized, single session) is the recommended mode for this phase because:

1. Sequential dependency chain (Tasks 1 → 8 cannot parallelize meaningfully).
2. Total LoC is small (~600 lines incl. tests).
3. No coordination with other parallel branches.

Mode 3 also works but provides no speedup for an isolated single-phase run.

---

## 9. Skill & Agent Updates

### 9.1 NEW skill — `rescan-orchestration`

Create `.github/skills/rescan-orchestration/SKILL.md` capturing the time-banded rescan pattern. Outline (full content created in Task 8):

- **Purpose:** Re-emit pipeline events at fixed age bands without altering downstream stages.
- **Rules:** content-addressable EventID; mode-adaptive eligibility; structural-reject exclusion; open-position skip; per-token failure isolation; bounded ticker rate.
- **Inputs:** `MarketDataDTO` from `market_data` table + `DataQualityDTO` sub-scores + lifecycle state.
- **Outputs:** `market_data_event` re-emissions tagged with `transport='rescan_<band>'`.
- **Anti-patterns:** new event types (forbidden), per-band new DTOs (forbidden), RPC calls inside the worker (forbidden).
- **Checklist:** 7 items mirroring Task 5's exit criteria.

### 9.2 UPDATE skill — `log-reviewer` (PRS additions)

Add to **§ R4 invariant detectors** (in priority order):

| Pattern                                                                                                | Class    | Layer | Route to                                                          |
| ------------------------------------------------------------------------------------------------------ | -------- | ----- | ----------------------------------------------------------------- |
| `rescan_worker_started` absent for ≥10 min while `cfg.rescan.enabled=true`                             | STUCK    | 0.5   | rescan-orchestration, event-bus                                   |
| `rescan_band_completed.candidates=0` for **all** bands sustained ≥30 min                               | DEGRADED | 0.5   | rescan-orchestration, data-quality-engine (eligibility too tight) |
| `rescan_emit_failed` rate > 5 % of candidates per band                                                 | DEGRADED | 0.5   | rescan-orchestration, event-bus                                   |
| Same `(token_address, band)` re-emitted within one bucket window                                       | BAD      | 0.5   | idempotency, rescan-orchestration                                 |
| `transport='rescan_*'` market_data_event with no downstream `dq_decision` within 60 s                  | STUCK    | 0.5→1 | event-bus, data-quality-engine                                    |
| `MOMENTUM_EDGE` count from rescanned traces > NEW_LAUNCH_EDGE count from fresh traces (sustained 24 h) | DEGRADED | 3     | edge-detection (likely NEW_LAUNCH window mis-tuned)               |

**PRS rubric extension** — extend Dimension #1 (Pipeline completeness) wording **without changing weights**:

```
Dimension #1 — Pipeline completeness — all stages L0–L10 observed ≥1×
  10 pts: ≥1 end-to-end trace AND (rescan disabled OR ≥1 rescanned trace
          reaches at least edge_decision in the window)
  5 pts:  L0–L5 observed, L6–L10 absent
  0 pts:  L0–L5 incomplete
```

This preserves the 100-point ceiling. Rescan does not warrant its own dimension because it is a _capability_, not a _correctness gate_ — the existing "pipeline completeness" dimension correctly absorbs it.

**Pipeline-stage completeness** (R3) — add a parenthetical note that rescanned traces start at Layer 0.5 and follow the same canonical sequence from Layer 1 onward:

```
solana_ingestion_emitted | dex_pool_detected | rescan_worker_emit   # Layer 0 / 0.5
  → dq_decision                                                      # Layer 1
  ...
```

### 9.3 NO new agent required

The existing agent roster (`phase-builder`, `dto-guardian`, `module-builder`, `integration`, `security-auditor`, `test-builder`, `merge-reviewer`) covers every task. Rationale:

- Worker creation → `module-builder`
- Adapter signature → `dto-guardian` validates additive-only
- SQL → `security-auditor` (injection scan)
- Tests → `test-builder`
- Wire-up + docs → `integration` + `merge-reviewer`

---

## 10. Architectural Compliance Checklist

Per `.github/copilot-instructions.md`:

| Invariant                                      | How this plan complies                                                                                                                                                                                      |
| ---------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Modular monolith — single process              | Worker is a goroutine in `cmd/server.go`. No new processes.                                                                                                                                                 |
| Module communication via DTOs only             | Worker reads `MarketDataDTO` from adapter and emits `MarketDataDTO` payloads. No raw maps cross boundaries.                                                                                                 |
| Pipeline strict sequential order               | Rescan does NOT alter ordering. It re-injects at Layer 0; downstream sequence is unchanged.                                                                                                                 |
| Determinism (same input → same output)         | EventID is content-addressable; SQL has total ordering; no `random`, no wall-clock in payload computation beyond `IngestedAt` (which is the _cause_ of the new event, not output of a function under test). |
| Idempotency                                    | `InsertEvent` ON CONFLICT DO NOTHING; bucket-truncated timestamp ensures intra-bucket re-runs are no-ops.                                                                                                   |
| State authority — DB is single source of truth | All rescan input state is read from `market_data`, `data_quality`, `token_lifecycle`. No worker-local cache.                                                                                                |
| DB adapter sole entry point                    | `GetTokensForRescan` lives on the adapter. Worker has zero SQL strings.                                                                                                                                     |
| Orchestrator authority                         | The rescan worker is a Layer-0 satellite emitter (same authority as `ingestion` worker). It does NOT call other modules.                                                                                    |
| Event-sourced backbone                         | All emissions go through `events` table via `InsertEvent`. No side channels.                                                                                                                                |
| Per-market isolation                           | Optional `chain` filter in `RescanQuery` preserves per-market boundaries.                                                                                                                                   |
| Telegram via event bus only                    | Worker does NOT call Telegram. Status surfaces via heartbeat events that the existing dispatcher consumes.                                                                                                  |
| Strategy versioning & replay                   | `versionID` is pinned at worker start; emitted events carry it; replay-deterministic given the same DB state.                                                                                               |
| Operational modes                              | Eligibility thresholds are mode-overridden at every tick.                                                                                                                                                   |
| Learning safety                                | No bounded-update logic in this worker (it doesn't tune anything). It _feeds_ Layer 10, which has its own bounded-update guards.                                                                            |
| Forbidden technologies                         | None introduced. No microservices, no Kafka, no Redis, no LLM, no cloud APIs.                                                                                                                               |
| Protected files policy                         | `contracts/` not modified. Existing migrations not modified. New migration uses post-dated prefix `20260503000022_`. New skill, new worker, new adapter method, new test files — all additive.              |
| File naming standards                          | `run_rescan.go`, `rescan_config.go`, `rescan.go`, `rescan_pipeline_test.go` — all functional names.                                                                                                         |

---

## 11. Risks, Mitigations, Future Work

### Risks

| Risk                                                                    | Severity                   | Mitigation                                                                                                               |
| ----------------------------------------------------------------------- | -------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| Eligibility filters too tight → no rescan candidates → wasted infra     | LOW                        | `log-reviewer` R4 detector flags it as DEGRADED within 30 min; operator widens thresholds via YAML reload (no redeploy). |
| Eligibility filters too loose → flood pipeline with structural rejects  | MEDIUM                     | Defaults are conservative (`max_honeypot_score: 0.5`). `MaxPerBandPerTick` cap. `STRICT` mode auto-tightens.             |
| Rescan event collides with original event ID                            | NIL                        | EventID hash includes band name + bucket_ts — distinct from the original (which hashes tx_hash + log_index).             |
| `data_quality.honeypot_score` is stub (always 0.0) before Phase 9 lands | HIGH if run before Phase 9 | **Hard sequencing rule in `phases.yaml` Group G.** Phase 10 cannot run until Phase 9 is merged.                          |
| Wallet sharding contention on rescan-driven executions                  | LOW                        | Existing `execution-engine` shard logic handles bursts; rescan emissions are rate-limited by `interval_seconds`.         |
| Increased Postgres load from band scans every 60 s                      | LOW                        | New indexes (Task 2) make scans index-only; query is bounded by `LIMIT`; load tested in Task 7.                          |

### Future Work (NOT in this plan)

1. **Probe re-fetch on rescan** — currently rescan re-uses the original probe data. A future iteration could re-run `run_market_probes` for rescanned tokens to refresh tax/honeypot/LP signals. Adds latency + RPC budget cost; requires its own profit-vs-cost analysis.
2. **Adaptive band schedule** — bands could shrink/expand based on observed `MOMENTUM_EDGE` win rate per band (Layer 10 feedback).
3. **Cross-chain rescan budgets** — currently shared across chains; could be per-chain-per-band.
4. **Rescan-only operational mode** — `RESCAN_ONLY` mode that disables fresh ingestion and processes only banded re-emissions for replay testing.

---

## 12. Exit Criteria for Phase 10

The phase is complete when **all** of the following are true:

1. `go build ./...` passes with zero warnings.
2. `go test ./...` passes with zero failures.
3. All seven integration tests in Task 7 pass.
4. `make migrate` applies the new migration cleanly on a fresh DB.
5. `cfg.Rescan.Enabled = false` is the shipped default and `make run` produces zero rescan emissions.
6. With `cfg.Rescan.Enabled = true` and seeded DB fixtures, the structured log shows ≥1 `rescan_band_completed` line per configured band.
7. With seeded data designed to produce a `MOMENTUM_EDGE` (aged token + growing volume), the end-to-end pipeline produces an `edge_event` whose `EdgeType=MOMENTUM_EDGE` and whose causation trail leads back to a `transport='rescan_*'` `market_data_event`.
8. `log-reviewer` skill PRS run on the test logs scores ≥ Dimension #1 = 10 pts (rescan trace observed at edge layer).
9. `docs/reference/architecture.md` § 3.0.5 exists and cross-references this plan.
10. `phases.yaml` Phase 10 entry parses; `./scripts/run_parallel.sh status` lists Phase 10 in Group G.
11. `.github/skills/rescan-orchestration/SKILL.md` exists with the seven-item checklist.
12. `docs/ops/PROGRESS_REPORT.md` updated with Phase 10 completion entry (this is the standard exit step for any phase).

---

## 13. Operator Runbook (Post-Merge)

**Enable the rescan layer** (manual operator step):

```bash
# 1. Edit config/pipeline.yaml — set rescan.enabled: true
# 2. Restart the bot:
make restart
# 3. Verify in logs:
make logs | grep rescan_worker_started
# 4. After 30 minutes, check candidate counts:
make logs | grep rescan_band_completed | tail -20
# 5. Run log-reviewer skill to confirm PRS dimension #1 still scores 10:
/log-reviewer
```

**Disable on incident** (kill switch):

```bash
# Set rescan.enabled: false and restart, OR send Telegram /mode STRICT
# (STRICT mode tightens rescan eligibility but does not fully disable;
#  fully disabling requires the YAML flag).
```

**Tuning checklist (operator decision tree):**

| Symptom                                                                          | Action                                                                                                                   |
| -------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------ |
| Zero candidates per band for >30 min in BALANCED                                 | Loosen `eligibility.max_honeypot_score` by 0.1 or set `include_passed: true`                                             |
| Pipeline overload (DQ queue depth rising)                                        | Lower `interval_seconds` or `MaxPerBandPerTick`; or temporarily set `enabled: false`                                     |
| `MOMENTUM_EDGE` win rate < `NEW_LAUNCH_EDGE` win rate after 200 closed positions | Tighten eligibility (raise `max_rug_score` floor); or shrink the longest band (`1h` → `45m` cap)                         |
| `EXPLORATION` mode triggered by starvation                                       | Verify rescan emissions reach edge layer; if not, eligibility is filtering too aggressively even in EXPLORATION override |

---

## 14. References

- `docs/reference/architecture.md` § 1 (pipeline), § 2.4 (per-market isolation), § 3.0 (Layer 0 ingestion), § 3.3 (edge taxonomy), § 7 (operational modes)
- `docs/analysis/profitability-gaps.md` (current factor estimates, target uplift)
- `docs/reference/dto_contracts.md` (`MarketDataDTO`, `DataQualityDTO`)
- `docs/reference/db_adapter_spec.md` (adapter interface conventions)
- `docs/reference/orchestrator_spec.md` (worker lifecycle conventions)
- `.github/copilot-instructions.md` (architectural invariants)
- `.github/skills/profit-first/SKILL.md` (profit-factor evaluation gate)
- `.github/skills/event-bus/SKILL.md` (PostgreSQL append-only event bus rules)
- `.github/skills/idempotency/SKILL.md` (content-addressable IDs)
- `.github/skills/operational-modes/SKILL.md` (STRICT/BALANCED/EXPLORATION)
- `.github/skills/edge-detection/SKILL.md` (`MOMENTUM_EDGE` taxonomy)
- `.github/skills/log-reviewer/SKILL.md` (PRS, R4 detectors — updated in Task 8)
- `.github/skills/rescan-orchestration/SKILL.md` (NEW — created in Task 8)
- `.github/agents/phase-builder.agent.md` (autonomous phase implementor)
- `config/phases.yaml` (Phase 10 entry — added in Task 8)
- `scripts/run_parallel.sh` (runner: `start --mode=2 10`)
