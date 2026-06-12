# PLAN.md — Post-Filter Profit Restoration & Helius Credit Efficiency

> **Version:** 1.1
> **Date:** 2026-06-10
> **Author:** Plan Management (from codebase review)
> **Status:** Complete (all 12 tasks done — 2026-06-10)
> **Source of Truth:** Prior review findings + `docs/architecture.md` §3.6–3.10, §7 + `docs/PROFITABILITY_GAPS.md` (GAP-04, GAP-14, GAP-15) + `config/pipeline.yaml` shadow gate comments
> **Pipeline Layers Affected:** L0, L3, L5, L6, L8, L9, L10, Platform (workers, config, rpc)
> **Profit Factors Affected:** Edge, Probability, Execution, AdaptationQuality (+ Helius efficiency as infra enabling sustained DataQuality)

---

## 1. Goal

Restore live profit capture **without relaxing intentional product constraints**: 15-minute token age floor, $70k market-cap floor, and fixed $5 entry sizing remain unchanged. Profit instead comes from (a) making the **post-filter pipeline** actually work — mode-aware EV/edge gates, competitive selection among tokens that already passed DQ, reliable probability joins, on-chain execution pricing, and a closed learning loop; and (b) leaning into the **rescan strategy** (15m→48h bands) as the primary alpha path for graduated, de-risked tokens.

Helius credit savings are pursued **orthogonally** — eliminate redundant RPC after `transactionSubscribe`, batch probe calls, gate serial launchers at L0 before probes, and tighten rescan re-probe policy — so credit headroom supports more probe budget on tokens that can actually trade.

**Why:** Current wiring leaves Execution ≈ 0 (shadow default), AdaptationQuality ≈ 0 (FN path unwired), and mode transitions ineffective (EV gate static at 100 bps). Credits are ~75% optimized but still leak ~1 credit/event on Raydium ingestion.

**Profit factor(s) affected:** Edge, Probability, Execution, AdaptationQuality (Capital held constant at fixed $5 by design).

---

## 2. Architecture Impact

### Affected Pipeline Layers

| Layer | Module path | Change type |
|-------|-------------|-------------|
| L0 | `internal/modules/ingestion_solana/` | Parse WS full tx; optional pre-filter enable |
| L3 | `internal/modules/edge/` | Mode-aware `edge_strength_min` |
| L5 | `internal/modules/validation/` | Mode-aware `ev_threshold_bps` |
| L6 | `internal/modules/selection/` | Top-K ranking + exploration band + per-creator dedup |
| L8 | `internal/modules/execution_solana/` | On-chain pool price for fill simulation |
| L9 | `internal/modules/position/` | On-chain price client (replace DEXScreener hot path) |
| L10 | `internal/modules/learning/`, `internal/workers/` | Shadow FN recorder, observer, A/B promoter wired |
| Platform | `internal/workers/run_validation.go`, `run_edge.go`, `run_selection.go`, `cmd/server.go` | Worker wiring |
| Config | `config/priority.yaml`, `config/chains.yaml`, `config/pipeline.yaml` | Mode lookup, pre_filter, rescan probe policy |
| RPC | `internal/rpc/solana_rpc.go`, `internal/modules/probes/` | Batch accounts; txSubscribe payload reuse |

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

| Decision | Rationale |
|----------|-----------|
| **Keep 15m age + $70k mcap + $5 fixed** | Operator-intentional; alpha = post-graduation momentum via rescan, not raw mint sniping |
| **Mode-aware EV/edge from `priority.yaml`** | EXPLORATION starvation recovery must relax gates on *qualified* tokens, not DQ floors |
| **Top-K selection without Kelly** | Rank by `P × EV`; allocate fixed $5 per slot up to `max_positions` per mode |
| **Helius savings at ingestion + probes** | Credit optimization must not weaken DQ; drop waste before probes run |
| **On-chain pool price for L8/L9** | Execution factor improves without changing entry size or DQ thresholds |
| **Shadow → live is config flip after gate metrics** | No auto-live; wire metrics first, operator promotes after ≥14d / ≥30 trades |

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
```

### Task Completion Protocol (required for every task)

A task is **not complete** until all validation commands pass **and** `docs/PROGRESS_REPORT.md` is updated in the same session. Do not defer the progress report to Task 12.

**After completing Tasks 1–11**, append a row to the progress log in `docs/PROGRESS_REPORT.md` with:

| Field | What to record |
|-------|----------------|
| Date | `YYYY-MM-DD` (session date) |
| Task | `PLAN Task N — {task name}` |
| Status | `completed` |
| Notes | Files changed, key behavior delivered, `go build` / `go vet` / `go test` exit status |

Use the existing table format in `docs/PROGRESS_REPORT.md` (see prior `PG-*` and `Task *` entries). Update the **Last Updated** date in the Summary section.

**Task 12** additionally records plan-level completion (all 12 tasks done, link to `docs/PLAN.md`).

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
- `docs/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
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
- `docs/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
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
- `docs/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
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
- `docs/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
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
- `docs/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
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
- `docs/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
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
- `docs/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
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
- `docs/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
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
- `docs/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
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
- `docs/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
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
- `docs/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
- `go test ./internal/modules/health/...`: all packages green

---

### Task 12 — Tests, Build Validation, PROGRESS_REPORT ✅

**Goal:** Full green build; record plan-level completion in `docs/PROGRESS_REPORT.md` (per-task entries for Tasks 1–11 must already exist).

**Layer(s) affected:** Platform

**Files to create/modify:**

- `docs/PROGRESS_REPORT.md` (modify) — append final plan completion entry (`docs/PLAN.md` — all 12 tasks done); verify Tasks 1–11 each have their own row

**Invariant check:**

- [x] All prior tasks validated

**Validation:**

- `go build ./...`: zero build errors
- `go vet ./...`: zero vet issues
- `docs/PROGRESS_REPORT.md`: append completion entry for this task (see §4 Task Completion Protocol) — **required**
- `go test ./...`: all packages green

---

## 5. Task Summary

| Task | Name | Primary files | Depends on | Complexity |
|------|------|---------------|------------|------------|
| 1 | Mode threshold resolver | `priority.go`, `config.go` | — | Low |
| 2 | Mode-aware EV gate | `run_validation.go`, `process_with_estimates.go` | 1 | Medium |
| 3 | Mode-aware edge floor | `run_edge.go`, `edge.go` | 1 | Medium |
| 4 | Top-K selection + dedup | `selection/`, `run_selection.go` | 2, 3 | High |
| 5 | Wire learning FN + A/B | `server.go`, `run_ab_promoter.go` | — | Medium |
| 6 | Fix evaluation probability | `evaluation.go`, adapter | — | Medium |
| 7 | txSubscribe in-process parse | `solana_rpc.go`, `ingestion_solana.go` | — | High |
| 8 | Probe batch + rescan policy | `run_market_probes.go`, `solana_batch.go` | — | Medium |
| 9 | Enable L0 pre_filter | `chains.yaml`, `server.go` | — | Low |
| 10 | On-chain pool price | `pool_price_solana.go`, `server.go` | — | High |
| 11 | Shadow gate metrics | `shadow_gate.go`, telegram | — | Low |
| 12 | Final validation | all | 1–11 | Low |

> **Progress tracking:** Every task row above requires a matching `docs/PROGRESS_REPORT.md` entry on completion (see §4 Task Completion Protocol).

---

## 6. How to Use This Plan

1. Implement Tasks 1–3 first — mode-aware gates unlock EXPLORATION without touching age/mcap floors.
2. Tasks 7–9 can run **in parallel** with 1–4 (Helius track independent).
3. Task 10 before flipping shadow→live (Task 11 confirms readiness).
4. Task 4 is the largest — split across two sessions if needed (batch worker first, dedup second).
5. **After every task (1–12):** run validation commands, then update `docs/PROGRESS_REPORT.md` before starting the next task. A task without a progress entry is considered incomplete.
6. After Task 12, operator manually sets `execution.mode: live` only when shadow gate passes.

**Explicit non-goals (intentional):**

- Do not lower `min_token_age_seconds` (900)
- Do not lower `min_market_cap_usd` (70000)
- Do not wire Kelly / `ProcessWithEstimates` for capital — fixed $5 stays

**Parallel development note:** Tasks 5–6 and 7–9 are independent of Tasks 1–4 and can be assigned to separate agents. Task 4 must not start until Tasks 2 and 3 complete.

---

## 7. Deep Knowledge Reference

### §7.1 Operational Mode Thresholds (`config/priority.yaml`)

| Mode | `ev_threshold_bps` | `edge_strength_min` | `max_positions` | `explore_budget_pct` |
|------|-------------------|---------------------|-----------------|----------------------|
| STRICT | 150 | 0.75 | 5 | 1.0% |
| BALANCED | 100 | 0.60 | 15 | 2.0% |
| EXPLORATION | 60 | 0.45 | 20 | 5.0% |
| VERY_EXPLORATION | 30 | 0.30 | 25 | 8.0% |

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

| Source | Before | After (est.) |
|--------|--------|--------------|
| Raydium `getTransaction` | ~1 cr/event | ~0 (WS parse) |
| New-token probes | ~3–4 cr/token | ~2 cr (batch) |
| Rescan `pumpfun_lp` | 1 cr/band/token | 0 on Phase 2 bands |
| Pre-filter drops | 0 | saves ~3–4 cr per dropped launcher |
| Position price | 0 (DEXScreener) | ~0.2 cr/position/5s (cached pool read) |

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

| Constraint | Alternative profit lever |
|------------|--------------------------|
| 15m age floor | Rescan bands 15m→48h **are** the entry window — optimize ranking + execution there |
| $70k mcap floor | Trade graduated tokens with real liquidity — on-chain pricing improves exit capture |
| Fixed $5 sizing | Win via **hit rate × exit quality × more concurrent slots** (Top-K + mode-aware EV), not bet sizing |
| Helius budget | Stop paying for doomed tokens (pre_filter) and duplicate fetches (txSubscribe parse) — redeploy credits on qualifiers |
