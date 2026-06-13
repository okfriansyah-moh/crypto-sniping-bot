# PLAN.md — Post-Filter Profit Restoration & Pipeline Proof Hardening

> **Version:** 1.2
> **Date:** 2026-06-10
> **Author:** Plan Management (from production gate review + prior codebase review)
> **Status:** Phase 1 complete (Tasks 1–12 ✅ 2026-06-10) · Phase 2 complete (Tasks 13–19 ✅ 2026-06-10)
> **Source of Truth:** Phase 1 — `docs/reference/architecture.md` §3.6–3.10 + `docs/analysis/profitability-gaps.md` · Phase 2 — production gate review `output/logs/gate_*_20260612_174145.*` + `docs/reference/architecture.md` §3.1 (L0/L1)
> **Pipeline Layers Affected:** L0, L1, L10, Platform (ingestion, probes workers, DB adapter, gate scripts)
> **Profit Factors Affected:** DataQuality, AdaptationQuality (Phase 2 restores pipeline proof → enables all downstream factors)

---

## 1. Goal

Restore live profit capture **without relaxing intentional product constraints**: 15-minute token age floor, $70k market-cap floor, and fixed $5 entry sizing remain unchanged. Profit instead comes from (a) making the **post-filter pipeline** actually work — mode-aware EV/edge gates, competitive selection among tokens that already passed DQ, reliable probability joins, on-chain execution pricing, and a closed learning loop; and (b) leaning into the **rescan strategy** (15m→48h bands) as the primary alpha path for graduated, de-risked tokens.

Helius credit savings are pursued **orthogonally** — eliminate redundant RPC after `transactionSubscribe`, batch probe calls, gate serial launchers at L0 before probes, and tighten rescan re-probe policy — so credit headroom supports more probe budget on tokens that can actually trade.

**Why:** Current wiring leaves Execution ≈ 0 (shadow default), AdaptationQuality ≈ 0 (FN path unwired), and mode transitions ineffective (EV gate static at 100 bps). Credits are ~75% optimized but still leak ~1 credit/event on Raydium ingestion.

**Profit factor(s) affected:** Edge, Probability, Execution, AdaptationQuality (Capital held constant at fixed $5 by design).

### Phase 2 Goal (Tasks 13–19)

Restore **PIPELINE_PROOF** viability: fix L0 mint extraction defects that poison L1, unblock the shadow FN observer SQL path, uplift Raydium-v4 pool-init yield, and add measurable throughput baselines so operators can distinguish **code defects** from **genuinely quiet markets**.

**Why:** The 30-minute gate run (`gate_20260612_174145`) produced **0 L0→L10 traces** despite 602k+ pumpfun-amm WS notifications. Production decision: `NOT_READY`. Root cause is predominantly **code**, not market scarcity (see §1.1).

**Profit factor(s) affected:** DataQuality (correct tokens enter DQ), AdaptationQuality (shadow observer unblocked).

---

## 1.1 Throughput Root Cause Analysis — Gate Run `20260612_174145`

**Question:** Is low ingestion / DQ / detection over 30 minutes because Solana had few launches, or because the codebase is wrong?

**Answer: predominantly codebase defects, not low market activity.**

| Signal                                        | Observed (30 min)   | Healthy expectation                                                | Verdict                                            |
| --------------------------------------------- | ------------------- | ------------------------------------------------------------------ | -------------------------------------------------- |
| pumpfun-amm WS notifications                  | **602,758**         | High volume = active market                                        | Market is **not** quiet                            |
| `solana_ingestion_emitted` (all programs)     | **48**              | Tens–hundreds of valid pool creates possible                       | **Code yield too low**                             |
| Emitted with `TokenAddress = WSOL` (`So111…`) | **44 / 48 (92%)**   | **0**                                                              | **Decoder bug** — not real tokens                  |
| Emitted with valid meme mint (`*pump` etc.)   | **4**               | Dozens/hour typical for pump graduations                           | **Severely under-detected**                        |
| raydium-v4 notifications                      | **34,706**          | Many swaps + some inits                                            | Active feed                                        |
| raydium-v4 `events_emitted`                   | **1** (0.003%)      | >0.1% init yield on filtered stream                                | **Decoder / tx-parse defect**                      |
| `market_probes_completed`                     | **30**              | Should track `ingestion_emitted` within lag                        | **18 events backlog** (37.5% stuck)                |
| L1 `dq_decision`                              | **30**, all `SKIP`  | At least 1 `PASS`/`RISKY_PASS` in EXPLORATION during active market | **Funnel dead** — WSOL spam + serial_launcher_skip |
| L2–L10 `stage_completed`                      | **0**               | ≥1 for pipeline proof                                              | **Blocked at L1**                                  |
| `shadow_observer_failed`                      | **30×** (every 60s) | 0                                                                  | **SQL encode bug** — AdaptationQuality path dead   |

**Causal chain (simplified):**

```
pumpfun-amm decoder emits WSOL as TokenAddress (92%)
    → market_probes waste ~3.5s/token on system mint (holder_dist timeouts)
    → creator_profiles inflated (creator_count≈49 on unrelated wallets)
    → DQ serial_launcher_skip on every probed token (social/holder gates fail)
    → emitted=0 at L1 → L2–L10 never run → traces_completed=0
```

**What is NOT the primary cause:**

- **Quiet Solana launch market** — 602k pumpfun-amm notifications disprove this.
- **Intentional DQ floors** (15m age, $70k mcap) — these apply at L5+ / rescan; they do not explain L0 emitting WSOL or L1 emitting zero downstream events.
- **EXPLORATION serial-launcher SKIP policy** — correct by design for unqualified serial launchers, but currently triggered on **corrupted inputs** (WSOL + inflated creator counts).

**Phase 2 success criteria (measurable after Tasks 13–19):**

1. `wsol_token_address_emitted = 0` over any 30-minute gate run.
2. `ingestion_valid_token_ratio ≥ 0.80` (valid mint / total emitted) on pumpfun-amm.
3. `market_probes_completed / ingestion_emitted ≥ 0.95` over 30 minutes (no sustained backlog).
4. `dq_pass_or_risky_pass ≥ 1` per 30-minute run during active market hours (EXPLORATION or VERY_EXPLORATION).
5. `traces_completed ≥ 1` (full L0→L10) in shadow mode — PIPELINE_PROOF exit.
6. `shadow_observer_failed = 0` (SQL fix verified).

---

## 2. Architecture Impact

### Affected Pipeline Layers

| Layer    | Module path                                                                              | Change type                                          |
| -------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------- |
| L0       | `internal/modules/ingestion_solana/`                                                     | Parse WS full tx; optional pre-filter enable         |
| L3       | `internal/modules/edge/`                                                                 | Mode-aware `edge_strength_min`                       |
| L5       | `internal/modules/validation/`                                                           | Mode-aware `ev_threshold_bps`                        |
| L6       | `internal/modules/selection/`                                                            | Top-K ranking + exploration band + per-creator dedup |
| L8       | `internal/modules/execution_solana/`                                                     | On-chain pool price for fill simulation              |
| L9       | `internal/modules/position/`                                                             | On-chain price client (replace DEXScreener hot path) |
| L10      | `internal/modules/learning/`, `internal/workers/`                                        | Shadow FN recorder, observer, A/B promoter wired     |
| Platform | `internal/workers/run_validation.go`, `run_edge.go`, `run_selection.go`, `cmd/server.go` | Worker wiring                                        |
| Config   | `config/priority.yaml`, `config/chains.yaml`, `config/pipeline.yaml`                     | Mode lookup, pre_filter, rescan probe policy         |
| RPC      | `internal/rpc/solana_rpc.go`, `internal/modules/probes/`                                 | Batch accounts; txSubscribe payload reuse            |

### Phase 2 Additional Impact (Tasks 13–19)

| Layer    | Module path                                                            | Change type                                             |
| -------- | ---------------------------------------------------------------------- | ------------------------------------------------------- |
| L0       | `internal/modules/ingestion_solana/`                                   | Mint-pair resolution; system-mint reject; Raydium yield |
| L0       | `internal/workers/creator_profile_aggregator.go`                       | Skip system-mint token launches                         |
| L10      | `database/engines/postgres/learning.go`                                | Shadow observer SQL fix                                 |
| Platform | `scripts/gate_review_collect.sh`, `scripts/validate_pipeline_proof.sh` | Throughput verdict automation                           |
| Config   | `config/chains.yaml`                                                   | `ingestion.system_mint_reject` list                     |

### DTO Flow (before → after)

```
Before:
  validated_edge_event → selection (first-ACCEPT turnstile) → capital (fixed $5) → shadow execution

After:
  validated_edge_event → selection (Top-K rank among ACCEPT, fixed $5, exploration slot) → capital (fixed $5, unchanged)
  rejection events → shadow_recorder → shadow_observer → learning_record (FN path)
  strategy_version (shadow) → ab_promoter → active promotion (bounded)
  position_poll → on-chain pool reserves (Solana) instead of DEXScreener
```

### Key Decisions

| Decision                                            | Rationale                                                                               |
| --------------------------------------------------- | --------------------------------------------------------------------------------------- |
| **Keep 15m age + $70k mcap + $5 fixed**             | Operator-intentional; alpha = post-graduation momentum via rescan, not raw mint sniping |
| **Mode-aware EV/edge from `priority.yaml`**         | EXPLORATION starvation recovery must relax gates on _qualified_ tokens, not DQ floors   |
| **Top-K selection without Kelly**                   | Rank by `P × EV`; allocate fixed $5 per slot up to `max_positions` per mode             |
| **Helius savings at ingestion + probes**            | Credit optimization must not weaken DQ; drop waste before probes run                    |
| **On-chain pool price for L8/L9**                   | Execution factor improves without changing entry size or DQ thresholds                  |
| **Shadow → live is config flip after gate metrics** | No auto-live; wire metrics first, operator promotes after ≥14d / ≥30 trades             |

---

## 3. Invariants Preserved

This plan maintains the following architecture invariants:

- [x] **Profit invariant**: `Profit = Edge × Probability × Execution × Capital × DataQuality × AdaptationQuality` — improves E, P, X, AQ; C and DQ floors held constant by design
- [x] **Determinism**: same input + same config = identical output — mode lookup from DB system state + event timestamps; no `math/rand`
- [x] **Idempotency**: content-addressable IDs, `ON CONFLICT DO NOTHING`
- [x] **Module isolation**: no cross-module imports; all comms via `contracts/` DTOs
- [x] **No direct DB access from modules**: adapter-only pattern preserved
- [x] **DTO additive-only**: no existing fields renamed, removed, or type-changed
- [x] **Config-driven**: all new thresholds in `config/` YAML, not hardcoded in Go
- [x] **Event bus backbone**: all state transitions flow through PostgreSQL `events` table
- [x] **Security invariants**: HTTPS-only URLs, API keys via `os.Getenv`, bounded HTTP bodies
- [x] **Layer-1 hard rejects intact**: serial launcher (STRICT/BALANCED), no-social-links, high-supply never bypassed
- [ ] **Migrations append-only**: no migrations required (worker wiring + config only)

**Factors NOT changed by this plan:** Capital sizing formula (fixed $5), DataQuality age/mcap floors.

**Phase 2 additional invariant notes:**

- [x] **Layer-1 hard rejects intact** — Phase 2 fixes L0 inputs; does not bypass serial launcher / social / supply rejects
- [x] **EXPLORATION SKIP policy preserved** — unqualified serial launchers still SKIP; goal is valid tokens reaching PASS/RISKY_PASS
- [ ] **Migrations append-only** — Phase 2 has no schema migrations (SQL query fix only)

---

## 4. Implementation Tasks

### Dependency Graph

```
Task 1 (Config: ModeThresholdResolver — priority.yaml → runtime lookup)
    │
    ├──────────────────────────────┐
    ▼                              ▼
Task 2 (L5: mode-aware EV gate)   Task 3 (L3: mode-aware edge_strength_min)
    │                              │
    └──────────────┬───────────────┘
                   ▼
Task 4 (L6: Top-K selection + exploration band + per_creator_dedup)
                   │
                   ▼
Task 5 (L10: wire ShadowRecorder + ShadowObserver + ABPromoter workers)
                   │
                   ▼
Task 6 (Evaluation: fix winProbability from validated-edge chain)
                   │
    ┌──────────────┴──────────────┐
    ▼                             ▼
Task 7 (Helius: parse transactionSubscribe in-process)   Task 8 (Helius: probe getMultipleAccounts batch + rescan re-probe policy)
    │                             │
    └──────────────┬──────────────┘
                   ▼
Task 9 (L0: enable pre_filter for serial launchers — credits before probes)
                   │
                   ▼
Task 10 (L8/L9: on-chain Solana pool price client for execution + exits)
                   │
                   ▼
Task 11 (Config: shadow-gate metrics dashboard + live-flip runbook fields)
                   │
                   ▼
Task 12 (Tests + build validation + PROGRESS_REPORT.md) ✅

── Phase 2: Pipeline Proof Hardening (gate run 20260612_174145) ──

Task 13 (L0: PumpFun AMM mint-pair resolution — fix WSOL-as-token bug)
    │
    ├──────────────────────────────┐
    ▼                              ▼
Task 14 (L0: system-mint reject guard + creator-profile hygiene)   Task 15 (DB: GetShadowTradesByWindow SQL fix)
    │                              │
    └──────────────┬───────────────┘
                   ▼
Task 16 (L0: Raydium-v4 Initialize2 yield uplift) ✅
                   │
                   ▼
Task 17 (Platform: gate throughput metrics + heartbeat counters) ✅
                   │
                   ▼
Task 18 (Platform: pipeline-proof acceptance harness) ✅
                   │
                   ▼
Task 19 (Tests + build validation + PROGRESS_REPORT.md) ✅
```

### Task Completion Protocol (required for every task)

A task is **not complete** until all validation commands pass **and** `docs/ops/PROGRESS_REPORT.md` is updated in the same session. Do not defer the progress report to Task 12.

**After completing Tasks 1–11**, append a row to the progress log in `docs/ops/PROGRESS_REPORT.md` with:

| Field  | What to record                                                                       |
| ------ | ------------------------------------------------------------------------------------ |
| Date   | `YYYY-MM-DD` (session date)                                                          |
| Task   | `PLAN Task N — {task name}`                                                          |
| Status | `completed`                                                                          |
| Notes  | Files changed, key behavior delivered, `go build` / `go vet` / `go test` exit status |

Use the existing table format in `docs/ops/PROGRESS_REPORT.md` (see prior `PG-*` and `Task *` entries). Update the **Last Updated** date in the Summary section.

**Task 12** additionally records plan-level completion (all 12 tasks done, link to `docs/plans/2026-06-10-profit-restoration-plan.md`).

---

### Task 1 — Config: Mode Threshold Resolver ✅

**Goal:** Single runtime helper that maps `STRICT|BALANCED|EXPLORATION|VERY_EXPLORATION` → `ev_threshold_bps`, `edge_strength_min`, `max_positions`, `explore_budget_pct` from `config/priority.yaml`.

**Layer(s) affected:** Config, Platform

**Files to create/modify:**

- `internal/app/config/priority.go` (create) — `ModeThresholds` struct + `ResolveModeThresholds(mode string) ModeThresholds`
- `internal/app/config/config.go` (modify) — expose `Priority` on root `Config` if not already wired
- `config/priority.yaml` (modify) — add comment cross-ref to workers; verify `max_positions` values align with selection

**Invariant check:**

- [x] Config from YAML only
- [x] Unknown mode → fail-closed to STRICT thresholds

**Validation:**

- `go build ./...`: zero build errors
- `go vet ./...`: zero vet issues
- `docs/ops/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
- `go test ./internal/app/config/...`: all packages green

**Prompt context needed:** §7.1 mode table

---

### Task 2 — L5: Mode-Aware EV Gate ✅

**Goal:** Validation worker uses per-mode `ev_threshold_bps` instead of static `pipeline.yaml` value.

**Layer(s) affected:** L5, Platform

**Files to create/modify:**

- `internal/workers/run_validation.go` (modify) — read system operational mode; pass resolved threshold to module
- `internal/modules/validation/process_with_estimates.go` (modify) — accept `evThresholdBps int32` parameter (or inject via module config snapshot per call)
- `internal/modules/validation/process_with_estimates_test.go` (modify) — mode threshold cases

**Invariant check:**

- [x] No hardcoded thresholds in module
- [x] STRICT mode never below configured STRICT floor
- [x] No cross-module imports (only `contracts/`)
- [x] Config values from YAML, not hardcoded

**Validation:**

- `go build ./...`: zero build errors
- `go vet ./...`: zero vet issues
- `docs/ops/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
- `go test ./internal/modules/validation/... ./internal/workers/...`: all packages green

**Prompt context needed:** §7.1, §7.2 EV formula

---

### Task 3 — L3: Mode-Aware Edge Strength Floor ✅

**Goal:** Edge module rejects edges below mode-specific `edge_strength_min` from `priority.yaml`.

**Layer(s) affected:** L3, Platform

**Files to create/modify:**

- `internal/workers/run_edge.go` (modify) — resolve mode; pass `edgeStrengthMin` to module
- `internal/modules/edge/edge.go` (modify) — apply floor after strength computation, before emit
- `internal/modules/edge/edge_test.go` (modify) — EXPLORATION accepts weaker edges that BALANCED rejects

**Invariant check:**

- [x] Does not bypass L1 hard rejects (edge runs after DQ)
- [x] No cross-module imports (only `contracts/`)
- [x] Config values from YAML, not hardcoded

**Validation:**

- `go build ./...`: zero build errors
- `go vet ./...`: zero vet issues
- `docs/ops/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
- `go test ./internal/modules/edge/...`: all packages green

---

### Task 4 — L6: Top-K Selection + Exploration Band + Per-Creator Dedup ✅

**Goal:** Replace first-ACCEPT turnstile with micro-batch Top-K greedy selection; fixed $5 per slot; one exploration slot per `explore_budget_pct`; wire existing `per_creator_dedup.go`.

**Layer(s) affected:** L6, Platform, Config

**Files to create/modify:**

- `internal/modules/selection/selection.go` (modify) — `ProcessBatch(ctx, []ValidatedEdgeDTO, openCount, modeThresholds)` → ranked outputs
- `internal/modules/selection/per_creator_dedup.go` (modify if needed) — integrate into batch path
- `internal/modules/selection/top_k.go` (create) — greedy Top-K + diversity bucket (reuse `DiversityBucket` field)
- `internal/workers/run_selection.go` (modify) — accumulate validated edges in 2–5s window OR process with adapter batch query; call `ProcessBatch`
- `config/pipeline.yaml` (modify) — `selection.batch_window_ms`, `selection.top_k` (default = `priority.modes.*.max_positions`)
- `internal/app/config/config.go` (modify) — `SelectionConfig` fields

**Invariant check:**

- [x] Fixed sizing unchanged — selection only sets `Selected=true` + rank
- [x] `IsExploration` set deterministically from rank + explore_budget_pct
- [x] No cross-module imports
- [x] Config values from YAML, not hardcoded

**Validation:**

- `go build ./...`: zero build errors
- `go vet ./...`: zero vet issues
- `docs/ops/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
- `go test ./internal/modules/selection/...`: all packages green

**Prompt context needed:** §7.3 selection algorithm

---

### Task 5 — L10: Wire Shadow FN Path + A/B Promoter ✅

**Goal:** Close AdaptationQuality loop — capture false negatives from rejected tokens, observe price paths, promote shadow strategy versions when gate passes.

**Layer(s) affected:** L10, Platform

**Files to create/modify:**

- `cmd/server.go` (modify) — register `RunShadowRecorder`, `RunShadowObserver`, `RunABPromoter` goroutines (mirror existing worker registration pattern)
- `internal/workers/run_shadow_recorder.go` (modify if needed) — consume `data_quality_event` REJECT + `validated_edge_event` REJECT
- `internal/workers/run_shadow_observer.go` (modify if needed) — poll DEXScreener or on-chain price from Task 10
- `internal/workers/run_ab_promoter.go` (create) — periodic `ShouldPromote` → `adapter.PromoteStrategyVersion`
- `internal/modules/learning/ab_promoter.go` (no logic change — already tested)

**Invariant check:**

- [x] Bounded promotion (expectancy × 1.05, drawdown ≤ baseline, N ≥ 30)
- [x] Telegram via event bus only
- [x] No direct DB access from modules

**Validation:**

- `go build ./...`: zero build errors
- `go vet ./...`: zero vet issues
- `docs/ops/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
- `go test ./internal/modules/learning/...`: all packages green

---

### Task 6 — Evaluation: Probability Feedback Fix ✅

**Goal:** `evaluation.go` uses `ProbabilityUsed` from validated-edge / allocation chain instead of hardcoded `0.0` — enables Brier score and learning updater signal.

**Layer(s) affected:** Evaluation (pre-L10), Platform

**Files to create/modify:**

- `internal/modules/evaluation/evaluation.go` (modify) — load prob from correlation event chain via adapter helper
- `database/adapter.go` + `database/engines/postgres/` (modify) — `GetProbabilityForLifecycle(ctx, lifecycleID) (float64, bool)` if missing
- `internal/modules/evaluation/evaluation_test.go` (modify)

**Invariant check:**

- [x] Adapter-only DB access from worker context
- [x] No cross-module imports

**Validation:**

- `go build ./...`: zero build errors
- `go vet ./...`: zero vet issues
- `docs/ops/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
- `go test ./internal/modules/evaluation/...`: all packages green

---

### Task 7 — Helius: Eliminate Redundant `getTransaction` After `transactionSubscribe` ✅

**Goal:** Parse full transaction from WS notification payload; skip HTTP `getTransaction` for Raydium `transactionSubscribe` path (~1 credit/pool event saved).

**Layer(s) affected:** L0, RPC

**Files to create/modify:**

- `internal/rpc/solana_rpc.go` (modify) — expose parsed tx from `LogsNotification` / txSubscribe payload; stop discarding full tx body
- `internal/modules/ingestion_solana/ingestion_solana.go` (modify) — branch: if `prog.SubscriptionMethod == transactionSubscribe` && tx in payload → normalize without `GetTransaction`
- `internal/modules/ingestion_solana/ingestion_solana_test.go` (modify) — fixture from captured WS payload

**Invariant check:**

- [x] `get_transaction_rps` circuit breaker still applies to logsSubscribe paths
- [x] Rate-limit backoff unchanged
- [x] RPC error messages truncated to 200 chars

**Validation:**

- `go build ./...`: zero build errors
- `go vet ./...`: zero vet issues
- `docs/ops/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
- `go test ./internal/modules/ingestion_solana/...`: all packages green

**Prompt context needed:** §7.4 Helius credit rules

---

### Task 8 — Helius: Probe Batching + Rescan Re-Probe Policy ✅

**Goal:** Reduce per-token probe credits without touching DQ floors — batch `getAccountInfo` for authorities + pumpfun_lp; skip `pumpfun_lp` on Phase 2 rescan bands (12h–48h) where reserves change slowly.

**Layer(s) affected:** Probes, Platform, Config

**Files to create/modify:**

- `internal/rpc/solana_rpc.go` (modify) — `GetMultipleAccounts(ctx, []pubkey)`
- `internal/modules/probes/solana_batch.go` (create) — batch fetch helper used by probe worker
- `internal/workers/run_market_probes.go` (modify) — batch path for new tokens; rescan band-aware skip for `solana_pumpfun_lp` on Phase 2
- `config/pipeline.yaml` (modify) — `probes.rescan_skip_pumpfun_lp_phase2: true`, `probes.batch_accounts: true`
- `internal/app/config/probes_config.go` (modify)

**Invariant check:**

- [x] Fail-closed on partial batch failure (`*Known=false`)
- [x] `max_probes_per_hour` cap preserved
- [x] Config values from YAML, not hardcoded

**Validation:**

- `go build ./...`: zero build errors
- `go vet ./...`: zero vet issues
- `docs/ops/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
- `go test ./internal/modules/probes/...`: all packages green

---

### Task 9 — L0: Enable Pre-Cohort Filter (Credits Before Probes) ✅

**Goal:** Turn on `chains.yaml` `pre_filter` to drop serial launchers at ingestion — saves 3–4 Helius credits per doomed token without relaxing DQ age/mcap floors.

**Layer(s) affected:** L0, Config

**Files to create/modify:**

- `config/chains.yaml` (modify) — `pre_filter.enabled: true`, `max_creator_prev_token_count: 25` (or align with VERY_EXPLORATION DQ ceiling)
- `cmd/server.go` (modify) — verify `CreatorProfileReader` wired to ingestion module (exists from Task 25; confirm not nil)
- `internal/modules/ingestion_solana/ingestion_solana.go` (modify only if reader unwired)

**Invariant check:**

- [x] Fail-open on reader error (pass through)
- [x] Does not bypass L1 hard rejects — additive earlier filter
- [x] Config values from YAML, not hardcoded

**Validation:**

- `go build ./...`: zero build errors
- `go vet ./...`: zero vet issues
- `docs/ops/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
- `go test ./internal/modules/ingestion_solana/...`: all packages green

---

### Task 10 — L8/L9: On-Chain Solana Pool Price Client ✅

**Goal:** Replace DEXScreener as primary price oracle for position TP/SL/trailing and shadow fill simulation — improves Execution factor without changing $5 size or DQ floors.

**Layer(s) affected:** L8, L9, RPC

**Files to create/modify:**

- `internal/rpc/pool_price_solana.go` (create) — read bonding-curve / AMM vault reserves via cached `getAccountInfo` (respect TTL from config)
- `internal/modules/position/position.go` (no change — consumes `PriceClient` interface)
- `cmd/server.go` (modify) — inject `PoolPriceClient` for Solana instead of `DEXScreenerPriceClient` when `price_oracle.mode: on_chain`
- `config/pipeline.yaml` (modify) — `price_oracle.mode: on_chain`, `price_oracle.cache_ttl_seconds: 5`
- `internal/workers/run_execution.go` (modify) — shadow fill uses same oracle

**Invariant check:**

- [x] Bounded RPC — cache TTL from YAML
- [x] Fail-open to last-known price with staleness flag (no panic)
- [x] HTTPS-only RPC URLs
- [x] Config values from YAML, not hardcoded

**Validation:**

- `go build ./...`: zero build errors
- `go vet ./...`: zero vet issues
- `docs/ops/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
- `go test ./internal/rpc/... ./internal/modules/position/...`: all packages green

---

### Task 11 — Shadow Gate Metrics + Live-Flip Readiness Config ✅

**Goal:** Expose shadow PnL gate metrics (≥30 trades, 14-day positive `realized_pnl_bps`) via health endpoint / Telegram `/status`; document config flip `execution.mode: live` as operator action — no auto-promotion.

**Layer(s) affected:** Platform, Config

**Files to create/modify:**

- `internal/modules/health/shadow_gate.go` (create) — query adapter for shadow trade stats
- `internal/telegram/commands.go` (modify) — `/status` shows gate pass/fail
- `config/pipeline.yaml` (modify) — `execution.shadow_gate.min_trades: 30`, `min_window_days: 14`, `min_aggregate_pnl_bps: 0`

**Invariant check:**

- [x] Telegram via event bus only
- [x] No auto-live
- [x] Config values from YAML, not hardcoded

**Validation:**

- `go build ./...`: zero build errors
- `go vet ./...`: zero vet issues
- `docs/ops/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
- `go test ./internal/modules/health/...`: all packages green

---

### Task 12 — Tests, Build Validation, PROGRESS_REPORT ✅

**Goal:** Full green build; record plan-level completion in `docs/ops/PROGRESS_REPORT.md` (per-task entries for Tasks 1–11 must already exist).

**Layer(s) affected:** Platform

**Files to create/modify:**

- `docs/ops/PROGRESS_REPORT.md` (modify) — append final plan completion entry (`docs/plans/2026-06-10-profit-restoration-plan.md` — all 12 tasks done); verify Tasks 1–11 each have their own row

**Invariant check:**

- [x] All prior tasks validated

**Validation:**

- `go build ./...`: zero build errors
- `go vet ./...`: zero vet issues
- `docs/ops/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
- `go test ./...`: all packages green

---

### Task 13 — L0: PumpFun AMM Mint-Pair Resolution (WSOL Bug) ✅

**Goal:** Stop emitting wrapped SOL (`So11111111111111111111111111111111111111112`) as `MarketDataDTO.TokenAddress` for pumpfun-amm CreatePool events — the #1 blocker from gate run `20260612_174145` (44/48 emissions wrong).

**Layer(s) affected:** L0

**Files to create/modify:**

- `internal/modules/ingestion_solana/mint_pair.go` (create) — shared deterministic helper:
  - `const WrappedSOLMint = "So11111111111111111111111111111111111111112"`
  - `func ResolveTradableMint(baseMint, quoteMint string) (tokenMint, baseAddr string, ok bool)` — when one side is WSOL/native SOL, return the **other** as `tokenMint`; when both are system/stable mints, return `ok=false`
  - `func IsSystemMint(addr string) bool` — WSOL + USDC/USDT mints from config (see Task 14; for Task 13 hardcode only WSOL constant, Task 14 moves list to YAML)
- `internal/modules/ingestion_solana/pumpfun_amm.go` (modify) — after `DecodePumpFunAMMCreatePool`, call `ResolveTradableMint(event.BaseMint, event.QuoteMint)`; set `TokenAddress` / `BaseAddress` / `Token0Address` / `Token1Address` from result; return `(nil, nil)` when `ok=false` (count as `dto_nil_skip`, not emit)
- `internal/modules/ingestion_solana/pumpfun_amm_test.go` (modify) — add regression cases:
  - BaseMint=WSOL, QuoteMint=`*pump` → TokenAddress=`*pump`
  - BaseMint=`*pump`, QuoteMint=WSOL → TokenAddress=`*pump`
  - Both WSOL → nil DTO
  - Fixture from gate log tx `6T87isHQTc6YZNCpHcm29xuDME9BtoK1psuyH4K4oxceF8c4FmpZGouvrevBiTmSJVrRexbNoyiRATV4zXcFByJ` (token `K93mdxq…pump`) once wire bytes captured
- `internal/modules/ingestion_solana/mint_pair_test.go` (create) — table-driven tests for `ResolveTradableMint`

**Root-cause hypothesis to validate during implementation:**

- On-chain PumpFun AMM CreatePool may place WSOL at account index 3 (`baseMint` slot in IDL) for some pool orientations; blind use of `instr.Accounts[3]` as graduated token is wrong ~92% of the time.
- Alternative: discriminator matches non-create instructions — verify with 1-in-100 `solana_tx_sample` logs; if discriminator false-positive, tighten `IsPumpFunAMMCreatePool` guard.

**Invariant check:**

- [x] Deterministic: same accounts → same mint resolution
- [x] No cross-module imports
- [x] No DB access
- [x] DTO fields populated consistently (`TokenAddress` = tradable mint, `BaseAddress` = quote/stable side)

**Validation:**

- `go build ./...`: zero build errors
- `go vet ./...`: zero vet issues
- `go test ./internal/modules/ingestion_solana/...`: all packages green (new WSOL regression tests pass)
- `docs/ops/PROGRESS_REPORT.md`: append completion entry — **required**

**Prompt context needed:** §7.10 MarketDataDTO mint fields, §7.11 Gate run evidence

---

### Task 14 — L0: System-Mint Reject Guard + Creator-Profile Hygiene ✅

**Goal:** Fail-closed at L0 before event-bus emit for system/stable mints; prevent `creator_profile_aggregator` from counting WSOL launches — stops creator_count inflation (`creator_count≈49`) that triggers spurious `serial_launcher_skip` at L1.

**Layer(s) affected:** L0, Platform, Config

**Files to create/modify:**

- `config/chains.yaml` (modify) — add `ingestion.system_mint_reject:` block:
  ```yaml
  ingestion:
    system_mint_reject:
      enabled: true
      mints:
        - "So11111111111111111111111111111111111111112" # WSOL / native SOL placeholder
        - "EPjFWdd5AufqSSqeM2qN1xzybapC8G4wEGGkZwyTDt1v" # USDC
        - "Es9vMFrzaCERmJfrF4H2FYD4KCoNkY11McCe8BenwNYB" # USDT
  ```
- `internal/app/config/chains_config.go` (modify) — `SystemMintRejectConfig` struct + loader; default `enabled: true` with WSOL only if YAML omits list
- `internal/modules/ingestion_solana/mint_pair.go` (modify) — `IsSystemMint` reads from injected config slice (fallback to WSOL constant when config nil)
- `internal/modules/ingestion_solana/ingestion_solana.go` (modify) — after normalizer + creator guard, before `applyPreFilter` / emit:
  - if `IsSystemMint(dto.TokenAddress)` → increment new heartbeat counter `system_mint_rejected`; `continue` (no emit)
- `internal/workers/creator_profile_aggregator.go` (modify) — in `handleMarketDataEvent`, skip upsert when `TokenAddress` is a configured system mint (same list from config injected at worker start)
- `internal/modules/ingestion_solana/ingestion_solana_test.go` (modify) — system mint rejected, not emitted

**Invariant check:**

- [x] Config-driven mint list (not hardcoded in module after config struct exists)
- [x] Fail-closed: unknown mint passes through
- [x] Does not bypass L1 hard rejects — additive L0 hygiene
- [x] No cross-module imports in ingestion (config injected at construction)

**Validation:**

- `go build ./...`: zero build errors
- `go vet ./...`: zero vet issues
- `go test ./internal/modules/ingestion_solana/... ./internal/workers/...`: all packages green
- `docs/ops/PROGRESS_REPORT.md`: append completion entry — **required**

**Prompt context needed:** §7.10, §7.12 Creator profile aggregator rules

---

### Task 15 — DB: Fix `GetShadowTradesByWindow` SQL Encode Error ✅

**Goal:** Eliminate `shadow_observer_failed: unable to encode 3600 into text format for text (OID 25)` — shadow FN observer was dead for entire 30-minute run (30 errors, 1/minute).

**Layer(s) affected:** DB (adapter engine)

**Files to create/modify:**

- `database/engines/postgres/learning.go` (modify) — replace interval expression:
  ```sql
  -- BEFORE (broken with pgx int arg):
  AND rejected_at < NOW() - ($1 || ' seconds')::interval
  -- AFTER (pgx-safe):
  AND rejected_at < NOW() - make_interval(secs => $1::double precision)
  ```
- `database/engines/postgres/learning_test.go` (modify or create) — integration-style test with `windowSeconds=3600` against test DB or sqlmock verifying query compiles and args bind
- `internal/workers/run_shadow_observer.go` (modify only if observer needs regression log) — optional: add `shadow_observer_tick` debug on success path for gate verification

**Invariant check:**

- [x] Portable SQL (`make_interval` supported on PostgreSQL target)
- [x] Adapter-only boundary preserved
- [x] No module SQL

**Validation:**

- `go build ./...`: zero build errors
- `go vet ./...`: zero vet issues
- `go test ./database/engines/postgres/...`: all packages green
- Manual: 5-minute run shows **zero** `shadow_observer_failed` lines
- `docs/ops/PROGRESS_REPORT.md`: append completion entry — **required**

**Prompt context needed:** §7.13 Shadow trade schema

---

### Task 16 — L0: Raydium-v4 Initialize2 Yield Uplift ✅

**Goal:** Raise raydium-v4 `events_emitted` yield from **0.003%** (1/34,706 notifications) — second-largest detection gap after pumpfun-amm WSOL bug.

**Layer(s) affected:** L0, RPC

**Files to create/modify:**

- `internal/modules/ingestion_solana/ingestion_solana.go` (modify) — diagnose and fix `no_instr_match` / `failed_tx` paths for `transactionSubscribe`:
  - Ensure embedded `TransactionResult` from WS payload is used (Task 7 path) — verify `failed_tx` counter not incrementing on valid embedded txs
  - When `instrMatched==0` but logs contain `Initialize2` / pool-init signature, fetch via `getTransaction` as fallback (rate-limited, circuit-breaker guarded) — **only** for raydium-v4 family
  - Add heartbeat sub-counter `raydium_init_fallback_fetch` for observability
- `internal/modules/ingestion_solana/raydium.go` (modify if needed) — verify `NormalizeRaydiumV4Instruction` routes Initialize2 tag correctly; add `TokenAddress` mint-pair resolution via `ResolveTradableMint` when coin/pc ordering ambiguous
- `internal/modules/ingestion_solana/transaction_subscribe_no_rpc_test.go` (modify) — extend fixtures for CPI-nested Initialize2 (common pattern)
- `internal/modules/ingestion_solana/raydium_yield_test.go` (create) — table tests: swap → no DTO; Initialize2 → DTO; truncated tx → fallback path mocked

**Investigation checklist (do during task, log in PROGRESS_REPORT):**

1. Sample 10 `failed_tx` signatures from gate log — classify: nil embedded tx vs commitment lag vs parse error.
2. Sample 10 `no_instr_match` — classify: program not in top-level instructions (CPI-only) vs wrong program ID filter.
3. Target post-fix: `events_emitted / notifications_received ≥ 0.001` over 30-minute active session.

**Invariant check:**

- [x] Rate-limited fallback fetch — respect `get_transaction_rps` circuit breaker
- [x] RPC errors truncated to 200 chars
- [x] Deterministic EventID unchanged for same tx+instr index
- [x] Swap instructions still produce no DTO (no DLQ spam)

**Validation:**

- `go build ./...`: zero build errors
- `go vet ./...`: zero vet issues
- `go test ./internal/modules/ingestion_solana/...`: all packages green
- `docs/ops/PROGRESS_REPORT.md`: append completion entry with yield metrics — **required**

**Prompt context needed:** §7.14 Raydium-v4 account layout, §7.11 gate evidence

---

### Task 17 — Platform: Gate Throughput Metrics + Heartbeat Counters ✅

**Goal:** Make the "low detection = code vs market" question **automatically answerable** in every gate run — extend collector script and ingestion heartbeats with the §1.1 success criteria.

**Layer(s) affected:** Platform

**Files to create/modify:**

- `internal/modules/ingestion_solana/ingestion_solana.go` (modify) — add atomic counters emitted in heartbeat:
  - `system_mint_rejected` (from Task 14)
  - `valid_token_emitted` (emitted where TokenAddress is not system mint)
  - `mint_pair_swapped` (ResolveTradableMint flipped base/quote)
- `scripts/gate_review_collect.sh` (modify) — compute and print in brief + JSON evidence:
  - `wsol_token_address_emitted` (count `solana_ingestion_emitted` where token=WSOL)
  - `ingestion_valid_token_ratio`
  - `market_probes_completed / ingestion_emitted` (backlog ratio)
  - `dq_pass_or_risky_pass` count (`dq_decision` where decision in PASS,RISKY_PASS)
  - `shadow_observer_failed` count
  - Per-program heartbeat final row (pumpfun-amm, raydium-v4)
  - **Verdict line:** `THROUGHPUT_VERDICT: CODE_DEFECT | MARKET_QUIET | HEALTHY` using thresholds from §1.1
- `output/logs/` (runtime) — no file committed; document expected output format in script header comment

**Invariant check:**

- [x] Script remains read-only on DB (log parsing only)
- [x] No new dependencies

**Validation:**

- Run `scripts/gate_review_collect.sh` against existing `gate_clean_20260612_174145.log` — must classify `THROUGHPUT_VERDICT: CODE_DEFECT`
- After Tasks 13–16 deployed, 30-minute run must show `wsol_token_address_emitted=0`
- `docs/ops/PROGRESS_REPORT.md`: append completion entry — **required**

**Prompt context needed:** §1.1 success criteria table

---

### Task 18 — Platform: Pipeline-Proof Acceptance Harness ✅

**Goal:** Scripted PIPELINE_PROOF exit check — operator runs one command after a gate session to know if `SHADOW_READY` criteria are met (≥1 full trace, no duplicate execution).

**Layer(s) affected:** Platform

**Files to create/modify:**

- `scripts/validate_pipeline_proof.sh` (create) — reads latest `gate_evidence_*.json` + clean log:
  - **PASS** iff `traces_completed >= 1` AND `duplicate_execution == 0` AND `wsol_token_address_emitted == 0`
  - Exit code 0 = PASS, 1 = FAIL with single-line reason
  - Print `PRODUCTION_DECISION: SHADOW_READY | NOT_READY`
- `docs/plans/2026-06-10-profit-restoration-plan.md` (modify) — cross-link script in §6 (this file)
- `Makefile` or `scripts/README` (modify if exists) — add `make gate-proof` target wrapping collect + validate

**Invariant check:**

- [x] Read-only validation — no code path changes
- [x] Deterministic thresholds from §1.1

**Validation:**

- Against `gate_20260612_174145` artifacts: exit code **1**, reason `traces_completed=0`
- Against synthetic fixture log with 1 full trace: exit code **0**
- `docs/ops/PROGRESS_REPORT.md`: append completion entry — **required**

**Prompt context needed:** §1.1, §7.15 Pipeline proof trace sequence

---

### Task 19 — Phase 2 Tests, Build Validation, PROGRESS_REPORT ✅

**Goal:** Full green build; record Phase 2 plan completion; verify §1.1 success criteria on a **live 30-minute gate run** after Tasks 13–18.

**Layer(s) affected:** Platform

**Files to create/modify:**

- `docs/ops/PROGRESS_REPORT.md` (modify) — append Tasks 13–18 rows + Phase 2 completion entry with gate run metrics

**Acceptance gate (all must pass on fresh 30-minute run):**

| Criterion                     | Threshold |
| ----------------------------- | --------- |
| `wsol_token_address_emitted`  | 0         |
| `ingestion_valid_token_ratio` | ≥ 0.80    |
| `market_probes_backlog_ratio` | ≤ 0.05    |
| `dq_pass_or_risky_pass`       | ≥ 1       |
| `traces_completed`            | ≥ 1       |
| `shadow_observer_failed`      | 0         |

**Invariant check:**

- [x] All prior Phase 2 tasks validated

**Validation:**

- `go build ./...`: zero build errors
- `go vet ./...`: zero vet issues
- `go test ./...`: all packages green
- `scripts/gate_review_collect.sh` (30m live run) + `scripts/validate_pipeline_proof.sh`: PASS
- `docs/ops/PROGRESS_REPORT.md`: append completion entry — **required**

---

## 5. Task Summary

| Task | Name                         | Primary files                                                         | Depends on | Complexity |
| ---- | ---------------------------- | --------------------------------------------------------------------- | ---------- | ---------- |
| 1    | Mode threshold resolver      | `priority.go`, `config.go`                                            | —          | Low        |
| 2    | Mode-aware EV gate           | `run_validation.go`, `process_with_estimates.go`                      | 1          | Medium     |
| 3    | Mode-aware edge floor        | `run_edge.go`, `edge.go`                                              | 1          | Medium     |
| 4    | Top-K selection + dedup      | `selection/`, `run_selection.go`                                      | 2, 3       | High       |
| 5    | Wire learning FN + A/B       | `server.go`, `run_ab_promoter.go`                                     | —          | Medium     |
| 6    | Fix evaluation probability   | `evaluation.go`, adapter                                              | —          | Medium     |
| 7    | txSubscribe in-process parse | `solana_rpc.go`, `ingestion_solana.go`                                | —          | High       |
| 8    | Probe batch + rescan policy  | `run_market_probes.go`, `solana_batch.go`                             | —          | Medium     |
| 9    | Enable L0 pre_filter         | `chains.yaml`, `server.go`                                            | —          | Low        |
| 10   | On-chain pool price          | `pool_price_solana.go`, `server.go`                                   | —          | High       |
| 11   | Shadow gate metrics          | `shadow_gate.go`, telegram                                            | —          | Low        |
| 12   | Phase 1 final validation     | all                                                                   | 1–11       | Low        |
| 13   | PumpFun AMM mint-pair fix ✅ | `mint_pair.go`, `pumpfun_amm.go`                                      | —          | High       |
| 14   | System-mint reject guard ✅  | `chains.yaml`, `ingestion_solana.go`, `creator_profile_aggregator.go` | 13         | Medium     |
| 15   | Shadow SQL encode fix ✅     | `postgres/learning.go`                                                | —          | Low        |
| 16   | Raydium-v4 yield uplift ✅   | `ingestion_solana.go`, `raydium.go`                                   | 13         | High       |
| 17   | Gate throughput metrics ✅   | `gate_review_collect.sh`, heartbeats                                  | 13, 14     | Medium     |
| 18   | Pipeline-proof harness ✅    | `validate_pipeline_proof.sh`                                          | 17         | Low        |
| 19   | Phase 2 final validation ✅  | PROGRESS_REPORT + live gate run                                       | 13–18      | Medium     |

> **Progress tracking:** Every task row above requires a matching `docs/ops/PROGRESS_REPORT.md` entry on completion (see §4 Task Completion Protocol).

---

## 6. How to Use This Plan

### Phase 1 (Tasks 1–12) — complete ✅

1. Implement Tasks 1–3 first — mode-aware gates unlock EXPLORATION without touching age/mcap floors.
2. Tasks 7–9 can run **in parallel** with 1–4 (Helius track independent).
3. Task 10 before flipping shadow→live (Task 11 confirms readiness).
4. **After every task:** run validation commands, then update `docs/ops/PROGRESS_REPORT.md`.

### Phase 2 (Tasks 13–19) — pipeline proof hardening ✅

1. **Start with Task 13** — WSOL-as-token is the highest-impact blocker; everything downstream depends on valid mints.
2. **Task 15 can run in parallel with Task 13** (independent SQL fix) — unblocks AdaptationQuality observer immediately.
3. **Task 14 after Task 13** — reuses `mint_pair.go` helpers; do not skip (prevents creator_count pollution).
4. **Task 16 after Task 13** — reuses `ResolveTradableMint` for Raydium coin/pc ordering.
5. **Task 17 after 13+14** — metrics will show false `CODE_DEFECT` until mint fixes land.
6. **Task 18 after 17** — acceptance harness reads gate evidence JSON.
7. **Task 19** — mandatory **live 30-minute gate run**:
   ```bash
   # terminal 1: run bot in shadow mode for 30 minutes
   # terminal 2: after stop:
   scripts/gate_review_collect.sh
   scripts/validate_pipeline_proof.sh
   scripts/validate_phase2_acceptance.sh   # full §1.1 six-criteria gate (Task 19)
   # or: make phase2-proof MINS=30
   ```
8. **After every task (13–19):** run validation commands, then update `docs/ops/PROGRESS_REPORT.md`.

**Operator workflow to answer "code vs market":**

```bash
scripts/gate_review_collect.sh   # produces brief + evidence JSON
# Read THROUGHPUT_VERDICT line in gate_brief_*.txt
# CODE_DEFECT → implement/fix Phase 2 tasks
# MARKET_QUIET  → only when notifications_received is also low (rare on Solana)
# HEALTHY       → proceed to extended shadow trading (MODE 2)
```

**Operator workflow for PIPELINE_PROOF exit (Task 18):**

```bash
scripts/gate_review_collect.sh              # or: make gate-collect MINS=30
scripts/validate_pipeline_proof.sh          # or: make gate-validate
# PASS → PRODUCTION_DECISION: SHADOW_READY (exit 0)
# FAIL → PRODUCTION_DECISION: NOT_READY + single-line reason (exit 1)
make gate-proof MINS=30                     # collect + validate in one step
```

**Explicit non-goals (intentional — both phases):**

- Do not lower `min_token_age_seconds` (900)
- Do not lower `min_market_cap_usd` (70000)
- Do not bypass L1 hard rejects (serial launcher in STRICT/BALANCED, no-social-links, high-supply)
- Do not relax EXPLORATION `serial_launcher_skip` policy to force trades — fix **inputs** (valid mints, accurate creator counts) instead
- Do not wire Kelly / `ProcessWithEstimates` for capital — fixed $5 stays

**Parallel development note (Phase 2):** Tasks 13 and 15 are independent. Task 16 can start after Task 13 mint helper exists. Tasks 17–18 are sequential after core L0 fixes.

---

## 7. Deep Knowledge Reference

### §7.1 Operational Mode Thresholds (`config/priority.yaml`)

| Mode             | `ev_threshold_bps` | `edge_strength_min` | `max_positions` | `explore_budget_pct` |
| ---------------- | ------------------ | ------------------- | --------------- | -------------------- |
| STRICT           | 150                | 0.75                | 5               | 1.0%                 |
| BALANCED         | 100                | 0.60                | 15              | 2.0%                 |
| EXPLORATION      | 60                 | 0.45                | 20              | 5.0%                 |
| VERY_EXPLORATION | 30                 | 0.30                | 25              | 8.0%                 |

Lookup source: `adapter.GetSystemState().OperationalMode` (adaptive controller) with fallback to `priority.active_mode`.

Unknown mode → fail-closed to STRICT thresholds.

### §7.2 EV Gate Formula (L5)

```
EV = P × prior_gain_bps − (1−P) × prior_loss_bps − fixed_costs_bps − slippage_p95_bps
ACCEPT iff EV ≥ ev_threshold_bps(mode) AND latency_p95 ≤ opportunity_window_ms
```

Priors from `config/pipeline.yaml` `validation:` block. Model P used when `use_model_output: true` and join succeeds.

### §7.3 Top-K Selection (L6) — Fixed $5 Variant

```
1. Filter: Decision == ACCEPT, per_creator_dedup pass, chain open_count < max_positions(mode)
2. Score: combinedScore = P × EV_bps / 1000
3. Sort descending (deterministic tie-break: TokenAddress lexicographic)
4. Greedy pick Top-K; mark lowest explore_budget_pct fraction as IsExploration=true
5. Emit selection_event per picked edge; defer/reject rest with below_top_k
```

Capital worker unchanged: `Process()` → fixed $5 per `Selected=true`.

### §7.4 Helius Credit Budget (post-plan targets)

| Source                   | Before          | After (est.)                           |
| ------------------------ | --------------- | -------------------------------------- |
| Raydium `getTransaction` | ~1 cr/event     | ~0 (WS parse)                          |
| New-token probes         | ~3–4 cr/token   | ~2 cr (batch)                          |
| Rescan `pumpfun_lp`      | 1 cr/band/token | 0 on Phase 2 bands                     |
| Pre-filter drops         | 0               | saves ~3–4 cr per dropped launcher     |
| Position price           | 0 (DEXScreener) | ~0.2 cr/position/5s (cached pool read) |

Net: probe budget freed → can raise effective throughput on **qualified** tokens without raising `max_probes_per_hour`.

**Helius billing reference:** `getTransaction` = 1 credit; DAS `getAsset` = 10 credits; `getProgramAccounts` = 10 credits. See `internal/app/config/probes_config.go` for canonical credit annotations.

### §7.5 Rescan as Primary Alpha Path

With 15m age + $70k mcap enforced, profitable entries concentrate in:

- **Phase 1 bands** (15m–8h): organic momentum re-entries
- **Phase 2 bands** (12h–48h): reversal / catalyst

Plan optimizes **selection + execution + learning** on rescan-emitted `market_data_event` (`transport: rescan_<band>`), not raw mint ingestion.

Rescan worker (`internal/workers/run_rescan.go`) is DB-only — no RPC. Downstream probe policy changes (Task 8) affect credits only on re-emitted events.

### §7.6 Layer-1 Hard Rejects (never bypass)

1. Serial launcher (STRICT/BALANCED hard-reject; EXPLORATION/VERY_EXPLORATION → RISKY_PASS or SKIP per mode profile)
2. No social / unknown social (`reject_no_social_links`, `reject_unknown_social_links`)
3. Excessive total supply / unknown supply (`max_total_supply: 1B`, `reject_unknown_total_supply`)

Canonical implementation: `internal/modules/data_quality/data_quality.go` (`ProcessForMode`).

### §7.7 A/B Promotion Gate (L10)

```
Promote shadow → active iff:
  expectancy(V2) > expectancy(V1) × 1.05
  AND drawdown(V2) ≤ drawdown(V1)
  AND N ≥ 30 samples
```

Implementation: `internal/modules/learning/ab_promoter.go` (`ShouldPromote`). Wired in Task 5.

### §7.8 Security Rules Relevant to This Plan

- Pool price RPC: truncate errors to 200 chars before surfacing
- No API keys in YAML — RPC URLs from `SOLANA_RPC_HTTP_*` env vars only
- On-chain price client: HTTPS RPC only (same invariant as Jito/Groq)
- Telegram: event bus only — no direct API calls from modules
- Bounded HTTP bodies on any external price fallback

### §7.9 Strategic Framing

| Constraint      | Alternative profit lever                                                                                              |
| --------------- | --------------------------------------------------------------------------------------------------------------------- |
| 15m age floor   | Rescan bands 15m→48h **are** the entry window — optimize ranking + execution there                                    |
| $70k mcap floor | Trade graduated tokens with real liquidity — on-chain pricing improves exit capture                                   |
| Fixed $5 sizing | Win via **hit rate × exit quality × more concurrent slots** (Top-K + mode-aware EV), not bet sizing                   |
| Helius budget   | Stop paying for doomed tokens (pre_filter) and duplicate fetches (txSubscribe parse) — redeploy credits on qualifiers |

### §7.10 MarketDataDTO Mint Fields (L0 — Phase 2)

Relevant fields on `contracts.MarketDataDTO` for ingestion decoders:

| Field                             | Semantics                  | Phase 2 rule                             |
| --------------------------------- | -------------------------- | ---------------------------------------- |
| `TokenAddress`                    | Tradable meme/project mint | **Must never** be WSOL/USDC/USDT         |
| `BaseAddress`                     | Quote/stable side of pair  | Usually WSOL for Solana AMMs             |
| `Token0Address` / `Token1Address` | Raw pool ordering          | May differ from TokenAddress/BaseAddress |
| `CreatorAddress`                  | Human creator wallet       | Must not be factory program ID           |
| `PoolAddress`                     | AMM pool state account     | Used by L8/L9 price oracle               |
| `Market`                          | e.g. `solana-pumpfun-amm`  | Drives probe + DQ cohort routing         |

`EventID` / `TraceID` = `SHA256("solana|" + txSignature + "|" + instrIndex)[:16]` — unchanged by mint-pair fix.

### §7.11 Gate Run Evidence — `20260612_174145` (canonical)

| Metric                    | Value   | Log pattern                                         |
| ------------------------- | ------- | --------------------------------------------------- |
| Run duration              | 30 min  | `gate_brief_20260612_174145.txt`                    |
| pumpfun-amm notifications | 602,758 | `solana_ingestion_heartbeat` final row              |
| ingestion emitted         | 48      | `solana_ingestion_emitted`                          |
| WSOL token emissions      | 44      | token=`So11111111111111111111111111111111111111112` |
| Valid pump mints          | 4       | `*pump` suffix addresses                            |
| raydium-v4 emitted        | 1       | `solana-raydium-v4` market                          |
| market_probes completed   | 30      | `market_probes_completed`                           |
| DQ decisions              | 30 SKIP | `dq_decision` decision=SKIP                         |
| shadow_observer errors    | 30      | `shadow_observer_failed`                            |
| L2–L10 stage_completed    | 0       | `stage_completed` worker_group                      |

Artifacts: `output/logs/gate_clean_20260612_174145.log`, `gate_evidence_20260612_174145.json`.

### §7.12 Creator Profile Aggregator Rules

Consumer: `creator_profile_aggregator` on `market_data_event`.

- Skips empty `CreatorAddress`
- Skips factory programs: `6EF8rrecthR5Dkzon8Nwu78hRvfCKubJ14M5uBEwF6P`, `pAMMBay6oceH9fJKBRHGP5D4bD4sWpmSwMn52FMfXEA`
- **Phase 2 addition:** skip when `TokenAddress` ∈ `ingestion.system_mint_reject.mints`
- Upsert increments `total_tokens` → feeds `CreatorPrevTokenCount` probe → L1 serial launcher check

WSOL events before Task 14 inflate counts → `creator_count≈49` → `serial_launcher_skip` in EXPLORATION.

### §7.13 Shadow Trades Schema + Observer

Table: `shadow_trades` — FN observation for rejected tokens.

`GetShadowTradesByWindow(ctx, windowSeconds int)` — broken query uses string concat on int param.

Fix: `make_interval(secs => $1::double precision)`.

Observer worker: `internal/workers/run_shadow_observer.go` — polls every 60s; gate run showed 30 consecutive failures.

### §7.14 Raydium-v4 Initialize2 Layout

Program: `675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8`

Instruction tag: `RaydiumV4OpInitialize2 = 1`

Account layout (0-based):

```
4 = amm (pool)
8 = coinMintAddress (token0)
9 = pcMintAddress   (token1 / quote)
```

`NormalizeRaydiumPoolInit` sets `TokenAddress=coinMint`, `BaseAddress=pcMint` — apply `ResolveTradableMint` when `coinMint` is WSOL.

Heartbeat counters to watch: `no_instr_match`, `failed_tx`, `skipped_unknown_instruction`, `events_emitted`.

### §7.15 Pipeline Proof Trace Sequence

Minimum events for `traces_completed >= 1`:

```
market_data_event (L0 ingestion)
  → market_data_enriched (L0 probes)
  → data_quality_event PASS|RISKY_PASS (L1)
  → feature_event (L2)
  → edge_event (L3)
  → probability_estimate + slippage_estimate (L4)
  → validated_edge_event ACCEPT (L5)
  → selection_event (L6)
  → allocation_event (L7)
  → execution_event (L8 shadow)
  → position_opened → position_closed (L9)
  → learning_record_event (L10)
```

Gate script counts via `stage_completed` worker_group + `dq_decision` + execution/position log lines.

Phase 2 does **not** require positive PnL — only lifecycle completion in shadow mode.
