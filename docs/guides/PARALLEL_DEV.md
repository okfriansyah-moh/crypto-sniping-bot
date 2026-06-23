# Parallel Development Guide — `run_parallel.sh`

> Operator guide for running multiple implementation phases simultaneously using
> autonomous AI agents. Supports 3 execution modes that balance speed, cost, and
> merge complexity.

---

## Table of Contents

1. [Overview](#1-overview)
2. [Model Routing Strategy](#2-model-routing-strategy)
3. [Mode Definitions](#3-mode-definitions)
4. [Mode Selection Strategy](#4-mode-selection-strategy)
5. [Phase Grouping Rules](#5-phase-grouping-rules)
6. [Token Cost Optimization Strategy](#6-token-cost-optimization-strategy)
7. [Resilience Framework](#7-resilience-framework)
8. [Status Display](#8-status-display)
9. [Requirements](#9-requirements)
10. [Fully Autonomous Pipeline](#10-fully-autonomous-pipeline)
11. [Hook System](#11-hook-system)
12. [Parallel Safety Invariants](#12-parallel-safety-invariants)
13. [Parallel Failure Modes](#13-parallel-failure-modes)

---

## 1. Overview

This framework supports parallel development of pipeline phases. Most phases own isolated
modules under `internal/modules/` and communicate only through immutable DTOs in `shared/contracts/`.
This isolation enables parallel development — multiple phases implemented at the same time
by independent AI agents.

However, parallelism has tradeoffs:

| Dimension      | More Parallelism              | Less Parallelism        |
| -------------- | ----------------------------- | ----------------------- |
| **Speed**      | Faster wall-clock time        | Slower (sequential)     |
| **Token cost** | Higher (each agent re-reads)  | Lower (shared context)  |
| **Merge risk** | More conflicts at integration | Fewer conflicts         |
| **Debugging**  | Harder (concurrent sessions)  | Easier (single session) |

Three execution modes let the operator choose the right balance for the situation.

---

## 2. Model Routing Strategy

Each mode uses a different **heavy model** for its most complex phase/group, plus a shared
round-robin **rotation pool** for all other agents.

| Mode                     | Heavy Model         | Used For                               |
| ------------------------ | ------------------- | -------------------------------------- |
| Mode 1 (Full Parallel)   | `claude-opus-4.7`   | Heaviest phase (most complex by score) |
| Mode 2 (Token-Optimized) | `claude-sonnet-4.6` | Single session (all phases)            |
| Mode 3 (Hybrid)          | `claude-sonnet-4.6` | Heaviest group (by complexity score)   |

**Rotation pool** (round-robin, used for all other phases and remediation agents):

```
claude-sonnet-4.6 → claude-sonnet-4.5 → gpt-5.3-codex → gpt-5.4
```

Used for: non-heavy phases, conflict-resolver, post-merge review, docs sync, quality gate
remediation, and integration remediation.

**Environment overrides:**

```bash
MODEL_HEAVY="claude-opus-4.7"         # Override Mode 1 heavy model
MODEL_HEAVY_LITE="claude-sonnet-4.6"   # Override Modes 2 & 3 heavy model
```

---

## 3. Mode Definitions

### Mode 1 — Full Parallel (Maximum Speed)

Each phase runs in a **separate Git worktree** with a **dedicated Copilot CLI agent**.
All phases execute simultaneously. The heaviest phase (highest complexity score) gets
`claude-opus-4.7`; all others rotate through the pool.

**How it works:**

```text
main
 ├─ track/phase-2   ← worktree 1, checkpoint + agent pipeline (bounded retries)
 ├─ track/phase-3   ← worktree 2, checkpoint + agent pipeline (bounded retries)
 └─ track/phase-4   ← worktree 3, checkpoint + agent pipeline (bounded retries)
```

1. Creates a branch per phase from `main`
2. Creates a Git worktree per branch (sibling directories)
3. Generates a `PHASE_TASK.md` instruction file in each worktree
4. Creates **checkpoint** (`git tag checkpoint-phase-N-pre`) in each worktree
5. Runs `scripts/hooks/setup-env.sh` in each worktree (non-fatal if absent)
6. Runs the **agent pipeline** per worktree with **bounded retries**:
   - `scripts/hooks/activate-env.sh` — activates runtime env before agent
   - `phase-builder` — implements the phase (up to 5 retries)
   - `dto-guardian` — validates DTO contracts (up to 5 retries)
   - `integration` — validates module wiring (up to 5 retries)
   - `security-auditor` — OWASP security review (up to 3 retries)
   - `test-builder` — generates unit + integration tests (up to 3 retries)
   - `refactor` — fixes quality gate failures (up to 3 retries)
   - If any stage exceeds retry limit → rollback to checkpoint
7. Tracks per-phase status in `.parallel-dev/phase-status.json`
8. Resource control: max `MAX_PARALLEL_AGENTS` (default 3) concurrent pipelines
9. Waits for all agent pipelines to finish
10. **Auto-merges** all branches into an integration branch (union strategy, bounded retries)
    - Conflicts resolved automatically by `conflict-resolver` agent (up to 5 retries)
11. **Post-merge review** via `merge-reviewer` agent — validates DTO flow, module boundaries, orchestrator authority
12. **Documentation sync** via `merge-reviewer` agent — detects implementation drift from `docs/` specs (advisory)
13. Global validation + orchestrator authority check
14. **Creates PR automatically** via `gh pr create` (pushed to `origin`)

**When to use:**

- Deadline pressure — need maximum throughput
- All phases in the batch are independent (no shared file ownership)

---

### Mode 2 — Token-Optimized (Serial Grouping)

Multiple phases run **sequentially in a single Copilot CLI session**. No worktrees.
Context is shared across phases. Always uses `claude-sonnet-4.6` as the model.

**How it works:**

```text
main
 └─ track/group-2-3-4  ← single branch, checkpoint + agent pipeline (bounded retries)
     Phase 2 → commit → Phase 3 → commit → Phase 4 → commit
     dto-guardian → integration → security-auditor → test-builder → refactor (bounded retries) → global validation
```

1. Creates a single branch from `main`
2. Generates a single `PHASE_TASK.md` with all phases listed in order
3. Creates **checkpoint** (`git tag checkpoint-group-X-pre`)
4. Runs `scripts/hooks/activate-env.sh` (non-fatal if absent)
5. Runs the **agent pipeline** with **bounded retries**
6. Each phase is committed before starting the next
7. **Post-merge review** via `merge-reviewer` agent — validates DTO flow, module boundaries, orchestrator authority
8. **Documentation sync** via `merge-reviewer` agent (advisory)
9. Global validation + orchestrator authority check
10. **Creates PR automatically** via `gh pr create` (pushed to `origin`)

**When to use:**

- Cost-sensitive development (limited premium requests)
- Phases have sequential dependencies
- Debugging a specific pipeline section end-to-end

**Grouping rules:**

- Maximum **3 phases per session** (beyond this, context window saturates)
- Phases must be in dependency order (earlier phases first)
- DTO-producing phases go before DTO-consuming phases

---

### Mode 3 — Hybrid (Balanced) — DEFAULT

Groups of phases run **in parallel across groups**, but **sequentially within each group**.
Combines the isolation of Mode 1 with the context sharing of Mode 2. The heaviest group
gets `claude-sonnet-4.6`; other groups rotate through the pool.

**How it works:**

```text
main
 ├─ track/group-a  ← worktree 1, checkpoint + agent pipeline (bounded retries)
 └─ track/group-b  ← worktree 2, checkpoint + agent pipeline (bounded retries)
```

1. Groups phases by dependency and file ownership
2. Creates a branch + worktree per group
3. Creates **checkpoint** per group
4. Runs `scripts/hooks/setup-env.sh` in each worktree (non-fatal if absent)
5. Each group runs the **agent pipeline** with **bounded retries**:
   - `scripts/hooks/activate-env.sh` — activates runtime env before agent
6. Tracks per-group status in `.parallel-dev/phase-status.json`
7. Groups execute in parallel (independent worktrees)
8. **Auto-merges** all group branches into integration branch (union strategy, bounded retries)
   - Conflicts resolved automatically by `conflict-resolver` agent (up to 5 retries)
9. **Post-merge review** via `merge-reviewer` agent — validates DTO flow, module boundaries, orchestrator authority
10. **Documentation sync** via `merge-reviewer` agent (advisory)
11. Global validation + orchestrator authority check
12. **Creates PR automatically** via `gh pr create` (pushed to `origin`)

**When to use:**

- Default choice for most development sessions
- Balance between speed and cost
- Phases have natural groupings by pipeline section

---

## 4. Mode Selection Strategy

```text
                        ┌─────────────────────┐
                        │  How many phases?    │
                        └─────────┬───────────┘
                                  │
                    ┌─────────────┼─────────────┐
                    ▼             ▼              ▼
               1 phase      2–3 phases      4+ phases
                    │             │              │
                    ▼             ▼              ▼
              Mode 2         Mode 2          ┌──────┐
           (single session)  (single session)│ Are   │
                                             │ they  │
                                             │ indep?│
                                             └──┬───┘
                                           yes  │  no
                                            ┌───┘───┐
                                            ▼       ▼
                                         Mode 1   Mode 3
                                       (full par) (hybrid)
```

| Scenario                                    | Recommended Mode |
| ------------------------------------------- | ---------------- |
| Single phase implementation                 | Mode 2           |
| 2–3 phases with sequential dependency       | Mode 2           |
| 2–3 fully independent phases                | Mode 1           |
| 4+ phases, mix of dependent and independent | Mode 3           |
| Cost-constrained (limited premium requests) | Mode 2           |
| Deadline pressure, all phases independent   | Mode 1           |
| Default / unsure                            | Mode 3           |

---

## 5. Phase Grouping Rules

### Safe Parallel Combinations

Phases can run simultaneously when they own **different files**:

```text
✅ Phase A ‖ Phase C  — different modules, no shared files
✅ Phase B ‖ Phase D  — independent inputs
```

### Unsafe Combinations

```text
❌ Phase 0 ‖ anything   — Phase 0 creates shared infrastructure
❌ DTO changes ‖ module changes — Module depends on DTO definition
❌ orchestrator ‖ any module  — Concurrent changes conflict
```

### Canonical Phase Groups (Crypto-Sniping-Bot)

Quick reference — all 8 phases at a glance. Expand the GROUP sections below for per-subsection detail.

| Phase | Name                                        | Group             | Priority | Parallel?    | Blocks / Requires                 | Key DTOs Produced                                                                                                                           |
| ----- | ------------------------------------------- | ----------------- | -------- | ------------ | --------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| **0** | Core Infrastructure                         | A — Sequential    | P0       | No           | Blocks everything                 | `EventEnvelope`, `StrategyVersion`                                                                                                          |
| **1** | Detection & Ingestion                       | A — Sequential    | P1       | No           | Requires Phase 0                  | `MarketDataDTO`                                                                                                                             |
| **2** | Minimal Trading Pipeline (FIRST TRADE)      | A — Sequential    | P1       | No           | Requires Phase 1                  | `DataQualityDTO`, `FeatureDTO`, `EdgeDTO`, `ValidatedEdgeDTO`, `SelectionOutput`, `AllocationDTO`, `ExecutionResultDTO`, `PositionStateDTO` |
| **3** | Evaluation & Correctness                    | B — Parallel-safe | P1.5     | ✅ with 4, 5 | Requires Phase 2                  | `EvaluationDTO`                                                                                                                             |
| **4** | Signal Quality                              | B — Parallel-safe | P1.5     | ✅ with 3, 5 | Requires Phase 3                  | `ProbabilityEstimateDTO`, `SlippageEstimateDTO`, `LatencyProfileDTO`                                                                        |
| **5** | Learning Engine                             | B — Parallel-safe | P2       | ✅ with 3, 4 | Requires Phase 4                  | `LearningRecordDTO`                                                                                                                         |
| **6** | Resource Control, Wallet Sharding & Scaling | C — Final         | P2       | No           | Requires Phase 5 + Group B merged | `SystemStateDTO`                                                                                                                            |
| **7** | Solana Market Extension                     | D — Market addon  | P2       | No           | Requires Phase 6 merged           | — (chain-agnostic; reuses existing DTOs)                                                                                                    |

---

#### GROUP A — SEQUENTIAL ONLY (no parallelism)

Phases MUST run one at a time in order. Each phase is a strict prerequisite for the next.

---

##### Phase 0 — Core Infrastructure

**Priority:** P0 | **Blocker for:** all other phases

| #   | Subsection                      | Purpose                                                                       | Key File(s)                              |
| --- | ------------------------------- | ----------------------------------------------------------------------------- | ---------------------------------------- |
| 0.1 | Priority Layers                 | P0/P1/P1.5/P2 tier definitions                                                | `shared/config/priority.yaml`                   |
| 0.2 | Module Layout Pattern           | `internal/modules/<name>/` skeleton; `run_<name>.go` worker convention        | `internal/modules/`, `internal/workers/` |
| 0.3 | Traceability Contract           | TraceID/CorrelationID/CausationID/VersionID enforced at adapter boundary      | `shared/contracts/trace.go`                     |
| 0.4 | Idempotency Contract            | `EventID = SHA256(payload)[:16]`; all inserts `ON CONFLICT DO NOTHING`        | `shared/database/engines/postgres/events.go`    |
| 0.5 | Config-Driven Thresholds        | All numeric constants in `shared/config/*.yaml`; hardcoding forbidden in modules     | `shared/config/*.yaml`                          |
| 0.6 | Global Event→Worker Routing     | Authoritative event-type → consumer-group mapping table                       | `internal/orchestrator/registry.go`      |
| 0.7 | Canonical Lifecycle CAS Pattern | READ → VALIDATE → CAS write-skew guard; `TransitionState` with version column | `shared/database/adapter.go`                    |

Sub-sections:

- **0.1 Priority Layers** — `shared/config/priority.yaml`; P0 / P1 / P1.5 / P2 tier definitions
- **0.2 Module Layout Pattern** — `internal/modules/<name>/` skeleton; `internal/workers/run_<name>.go` convention
- **0.3 Traceability Contract** — TraceID / CorrelationID / CausationID / VersionID propagation rules enforced at adapter boundary
- **0.4 Idempotency Contract** — `EventID = SHA256(payload)[:16]`; all inserts use `ON CONFLICT DO NOTHING`
- **0.5 Config-Driven Thresholds** — All numeric constants in `shared/config/*.yaml`; hardcoded values forbidden in modules
- **0.6 Global Event→Worker Routing** — Authoritative event-type → consumer-group mapping table
- **0.7 Canonical Lifecycle CAS Pattern** — READ → VALIDATE → CAS write-skew guard; `TransitionState` with version column

Files introduced: `shared/database/adapter.go`, `shared/database/errors.go`, `shared/database/migrations.go`, `shared/database/engines/postgres/` (postgres.go, events.go, runs.go, versions.go), `shared/database/migrations/20260101000001_initial_schema.sql`, `internal/orchestrator/` (orchestrator.go, worker.go, registry.go, checkpoint.go), `internal/app/config/config.go`, `internal/app/logging/logger.go`, `shared/contracts/trace.go`, `shared/contracts/event_envelope.go`, `cmd/root.go`, `cmd/server.go`, `cmd/migrate.go`, `shared/config/pipeline.yaml`, `shared/config/chains.yaml`, `shared/config/execution.yaml`, `shared/config/gas.yaml`, `shared/config/priority.yaml`

Exit gate: `sniper migrate` idempotent, `SELECT … FOR UPDATE SKIP LOCKED` verified, `StrategyVersion` pin at startup, `go test ./database/... ./internal/orchestrator/...` passes.

---

##### Phase 1 — Detection & Ingestion

**Priority:** P1 | **Requires:** Phase 0 exit gate

| Subsection       | Purpose                                                                                    | Output                    | Key File(s)                               |
| ---------------- | ------------------------------------------------------------------------------------------ | ------------------------- | ----------------------------------------- |
| Ingestion module | Primary WebSocket `eth_subscribe`; HTTP `eth_getLogs` fallback                             | `market_data_event`       | `internal/modules/ingestion/ingestion.go` |
| Normalize        | `rawLog → MarketDataDTO`; decodes PairCreated/Mint/Swap/Burn                               | `MarketDataDTO`           | `normalize.go`                            |
| Subscribe        | WebSocket `eth_subscribe` connection lifecycle                                             | —                         | `subscribe.go`                            |
| Poll             | HTTP `eth_getLogs` fallback for non-WS chains                                              | —                         | `poll.go`                                 |
| Heartbeat        | WS ping/pong keepalive; detects silent disconnects                                         | —                         | `heartbeat.go`                            |
| Reconnect        | Exponential backoff + endpoint failover on WS failure                                      | —                         | `reconnect.go`                            |
| Gap Recovery     | Fill `[last_block+1, current_block]` on reconnect; `UpsertIngestionWatermark`              | —                         | `gap_recovery.go`                         |
| Reorg Detection  | Confirmation-depth check; `MarketDataDTO.Reorged = true` flag; re-emits on confirmed block | `MarketDataDTO` (updated) | `reorg.go`                                |
| Topics Registry  | PairCreated/Mint/Swap/Burn canonical topic-hash registry per DEX family                    | —                         | `topics.go`                               |

Sub-sections:

- **Ingestion module** — Primary WebSocket `eth_subscribe`; HTTP `eth_getLogs` fallback
- **Normalize** — `rawLog → MarketDataDTO` per chain; decodes PairCreated, Mint, Swap, Burn
- **Subscribe** — WebSocket `eth_subscribe` logs; connection lifecycle
- **Poll** — HTTP `eth_getLogs` fallback for chains without WS support
- **Heartbeat** — WS ping/pong keepalive; detects silent disconnects
- **Reconnect** — Exponential backoff + endpoint failover on WS failure
- **Gap Recovery** — Fill `[last_processed_block+1, current_block]` on reconnect; `UpsertIngestionWatermark`
- **Reorg Detection** — Confirmation-depth check; `MarketDataDTO.Reorged = true` flag; re-emits on confirmed block
- **Topics Registry** — `(PairCreated, Mint, Swap, Burn)` canonical topic-hash registry per DEX family

Files introduced: `shared/contracts/market_data.go` (MarketDataDTO), `internal/modules/ingestion/` (ingestion.go, normalize.go, subscribe.go, poll.go, heartbeat.go, reconnect.go, gap_recovery.go, reorg.go, topics.go), `internal/workers/run_ingestion.go`, `shared/database/migrations/20260101000002_ingestion_tables.sql`

Exit gate: live WebSocket on Uniswap V2/V3 + PancakeSwap, deterministic `EventID` per block+logIndex, gap recovery verified on synthetic gap, zero SQL in `internal/modules/ingestion/`.

---

##### Phase 2 — Minimal Trading Pipeline (FIRST TRADE)

**Priority:** P1 | **Requires:** Phase 1 exit gate

| #    | Subsection                      | Layer | Output DTO                         | Worker File                                    |
| ---- | ------------------------------- | ----- | ---------------------------------- | ---------------------------------------------- |
| 2.1  | Data Quality (minimal)          | L1    | `DataQualityDTO`                   | `run_data_quality.go`                          |
| 2.2  | Feature Extraction (5 features) | L2    | `FeatureDTO` + `FeatureConfidence` | `run_features.go`                              |
| 2.3  | Edge Discovery (single rule)    | L3    | `EdgeDTO`                          | `run_edge.go`                                  |
| 2.4  | Edge Validation (fixed priors)  | L5    | `ValidatedEdgeDTO`                 | `run_validation.go`                            |
| 2.5  | Selection (top-1)               | L6    | `SelectionOutput`                  | `run_selection.go`                             |
| 2.6  | Capital (fixed size)            | L7    | `AllocationDTO`                    | `run_capital.go`                               |
| 2.7  | Execution (real tx)             | L8    | `ExecutionResultDTO`               | `run_execution.go`                             |
| 2.8  | Position (TP/SL/TIME)           | L9    | `PositionStateDTO`                 | `run_position_open.go`, `run_position_poll.go` |
| 2.9  | Orchestrator Wiring             | —     | —                                  | `cmd/server.go`                                |
| 2.10 | Migration                       | —     | —                                  | `20260101000003_trading_tables.sql`            |
| 2.11 | Exit Criteria / Testing         | —     | —                                  | FIRST TRADE GATE                               |

Sub-sections:

- **2.1 Data Quality (Layer 1, minimal)** — Honeypot check via static-call simulation; fake-liquidity check (mint ratio); tax reject (buy/sell fee > threshold); `DataQualityDTO.Decision = PASS | REJECT`
- **2.2 Feature Extraction (Layer 2, 5 features)** — LiquidityScore, TxVelocityScore, ContractSafety, TokenAge, VolumeMomentum; Z-score normalisation; `FeatureDTO` + `FeatureConfidence`
- **2.3 Edge Discovery (Layer 3, single rule)** — `NEW_LAUNCH_RULE`: age × velocity × liquidity composite gate; `EdgeStrength` score; `EdgeConfidence`; TTL = `config.edge.ttl_seconds` (default 8 s)
- **2.4 Edge Validation (Layer 5, fixed priors)** — EV gate: `P × avgWin − (1−P) × avgLoss > config.validation.min_ev`; fixed priors from config; latency budget check; TTL = `config.validated_edge.ttl_seconds` (default 5 s)
- **2.5 Selection (Layer 6, top-1)** — Concurrency gate; `max_open_positions = 1` (config); `SelectionOutput` with single candidate; expired candidates dropped
- **2.6 Capital (Layer 7, fixed size)** — Fixed `config.capital.fixed_entry_size_usd`; `CheckEnvelope` (total exposure only, Phase 2 version); `ExecutionID = SHA256(CorrelationID)[:16]`; TTL = `config.allocation.ttl_seconds` (default 3 s)
- **2.7 Execution (Layer 8, real tx)** — DB-backed nonce allocation; EIP-1559 gas pricing; Uniswap V2 calldata build; sign + submit; wait receipt (1 confirmation); single attempt only; `ExecutionResultDTO`
- **2.8 Position (Layer 9, TP/SL/TIME)** — `OpenPosition` worker; `PollExit` worker; TP1 fixed ratio; SL fixed ratio; TIME exit after `config.position.max_hold_seconds`; sell tx submission on trigger; `PositionStateDTO`
- **2.9 Orchestrator Wiring** — Register all 10 workers in `cmd/server.go`; worker registration order matches pipeline stage order
- **2.10 Migration** — `20260101000003_trading_tables.sql`: wallet_nonce_state, executions, positions, tokens tables
- **2.11 Exit Criteria / Testing (FIRST TRADE GATE)** — At least 1 real on-chain swap on testnet; full causal chain observable in events table; replay determinism; full trace IDs on every DTO; zero SQL in `internal/modules/`

Files introduced: `shared/contracts/` (data_quality.go, feature.go, edge.go, validated_edge.go, selection.go, allocation.go, execution.go, position.go), `internal/modules/data_quality/` (data_quality.go, honeypot.go, fake_liquidity.go, tax_reject.go, simulation.go), `internal/modules/features/` (features.go, normalize.go, liquidity.go, tx_velocity.go, holder_count.go, contract_flags.go), `internal/modules/edge/` (edge.go, new_launch_rule.go), `internal/modules/validation/` (validation.go, ev_gate.go), `internal/modules/selection/` (selection.go, concurrency_gate.go), `internal/modules/capital/` (capital.go, envelope.go), `internal/modules/execution/` (execution.go, nonce.go, gas.go, build_calldata.go, submit.go, wait_receipt.go, abi.go), `internal/modules/position/` (position.go, tp_sl.go, time_exit.go, sell_tx.go), `internal/workers/` (run_data_quality.go, run_features.go, run_edge.go, run_validation.go, run_selection.go, run_capital.go, run_execution.go, run_position_open.go, run_position_poll.go)

Exit gate: **FIRST TRADE GATE** — live swap confirmed on testnet, complete event chain in DB, replay deterministic, `go test ./internal/modules/...` passes.

---

#### GROUP B — SAFE PARALLEL (independent modules)

These phases MAY run simultaneously once Group A is complete. They own separate `internal/modules/` subdirectories with no shared files.

---

##### Phase 3 — Evaluation & Correctness

**Priority:** P1.5 | **Requires:** Phase 2 exit gate | **Safe parallel with:** Phase 4, Phase 5

| Subsection                      | Purpose                                                                                                        | Key File(s)                                      |
| ------------------------------- | -------------------------------------------------------------------------------------------------------------- | ------------------------------------------------ |
| State Machine                   | CAS-enforced lifecycle transitions; `token_state_violation` on illegal transition; quarantine on Nth violation | `internal/modules/state_machine/`                |
| Traceability Enforcement        | `ErrMissingTraceField` / `ErrOrphanEvent` enforced at adapter write time; CorrelationID chain validated        | `internal/modules/traceability/`                 |
| Transaction Retry / Replacement | Same-nonce gas bump (δ ≈ 10–20%, max 2–3 retries); RETRIABLE vs FATAL classification                           | `execution/replacement.go`, `execution/retry.go` |
| Circuit Breaker                 | Per-RPC-endpoint open/half-open/closed state machine                                                           | `execution/circuit_breaker.go`                   |
| Priority-Aware Event Processing | `ComputePriority(eventType, age)`; `ORDER BY priority DESC, created_at ASC` in `ClaimNextEvent`                | `internal/orchestrator/`                         |
| Improved Latency Window         | Dynamic `OpportunityWindowMs` subtracts measured RPC overhead per chain                                        | `internal/modules/validation/`                   |
| Telegram Dispatcher             | Event-bus-only Telegram; operator commands `/status` `/pnl` `/positions` `/kill` `/resume` `/version`          | `internal/telegram/`                             |
| 3.1 Evaluation Engine           | `PredictionError`/`FalsePositive`/`FalseNegative`/`Expectancy` → `evaluation_event`; `EvaluationDTO`           | `internal/modules/evaluation/evaluation.go`      |

Sub-sections:

- **State Machine** — `AllowedTransitions` matrix; CAS-enforced `TransitionState`; `token_state_violation` insert on illegal transition; `QuarantineToken` on Nth violation
- **Traceability Enforcement** — `ErrMissingTraceField` and `ErrOrphanEvent` enforced at adapter write time; CorrelationID chain validated on every insert
- **Transaction Retry / Replacement** — Same-nonce gas bump (δ ≈ 10–20%, max 2–3 retries); RETRIABLE vs FATAL error classification; `execution/replacement.go`, `execution/retry.go`
- **Circuit Breaker** — Per-RPC-endpoint open/half-open/closed state machine; `execution/circuit_breaker.go`
- **Priority-Aware Event Processing** — `ComputePriority(eventType, age)`; `Event.Priority` int field; `ORDER BY priority DESC, created_at ASC` injected into `ClaimNextEvent`
- **Improved Latency Window** — Dynamic `OpportunityWindowMs` subtracts measured RPC overhead per chain
- **Telegram Dispatcher** — All user-facing events routed via `telegram_event` on the bus; `internal/telegram/dispatcher.go`; operator commands: `/status`, `/pnl`, `/positions`, `/kill`, `/resume`, `/version`
- **3.1 Evaluation Engine (Layer 10 pre-learning gate)** — Consumes `position_event (Status=exited)`; computes `PredictionError`, `FalsePositive`, `FalseNegative`, `ExecutionError`, `Expectancy`; emits `evaluation_event`; `EvaluationDTO`; worker `run_evaluation.go`

Files introduced: `internal/modules/state_machine/` (state_machine.go, transitions.go, quarantine.go, state_machine_test.go), `internal/modules/traceability/` (validator.go, validator_test.go), `internal/modules/execution/` additions (replacement.go, retry.go, circuit_breaker.go), `internal/telegram/` (dispatcher.go, commands.go, bot.go), `shared/contracts/evaluation.go` (EvaluationDTO), `internal/modules/evaluation/` (evaluation.go, evaluation_test.go), `internal/workers/run_evaluation.go`, `shared/database/migrations/20260101000004_state_machine.sql`

Exit gate: CAS rejection observable in `token_state_violation`, `orphan_event_count = 0`, tx replacement tested on testnet, Telegram `/kill` functional, replay determinism with evaluation events.

---

##### Phase 4 — Signal Quality (Models + Full DQ / Features)

**Priority:** P1.5 | **Requires:** Phase 3 exit gate | **Safe parallel with:** Phase 3 (separate modules), Phase 5

| Subsection                | Layer | Output DTO                       | Worker File                     |
| ------------------------- | ----- | -------------------------------- | ------------------------------- |
| Data Quality — full suite | L1    | `DataQualityDTO` (extended)      | `run_data_quality.go` additions |
| Feature Extraction — full | L2    | `FeatureDTO` (extended)          | `run_features.go` additions     |
| Edge — full               | L3    | `EdgeDTO` (extended)             | `run_edge.go` additions         |
| Probability Model         | L4    | `ProbabilityEstimateDTO`         | `run_probability.go`            |
| Slippage Model            | L4    | `SlippageEstimateDTO`            | `run_slippage.go`               |
| Latency Model             | L4    | `LatencyProfileDTO`              | `run_latency.go`                |
| Validation Worker Update  | L5    | `ValidatedEdgeDTO` (real priors) | `run_validation.go` (updated)   |
| Private RPC Routing       | L8    | —                                | `execution/private_rpc.go`      |

Sub-sections:

- **Data Quality — full suite** — Wash-trading detection (`wash_trading.go`); rug risk score (`rug_risk.go`); tax anomaly detection (`tax_anomaly.go`); weighted risk aggregation (`risk_score.go`); all extending Phase 2 DQ skeleton
- **Feature Extraction — full** — Holder distribution Gini index (`holder_distribution.go`); wallet entropy score (`wallet_entropy.go`); feature drift detector (`drift_detector.go`)
- **Edge — full** — Momentum composite score (`momentum.go`); adaptive threshold updater (`adaptive_threshold.go`); new-pool gate (`new_pool_gate.go`)
- **Probability Model** — Logistic regression fit on `LearningRecord` history; emits `ProbabilityEstimateDTO`; worker `run_probability.go`; fan-out from `feature_event`
- **Slippage Model** — Empirical bucket curve (liquidity depth → p50/p95 slippage); emits `SlippageEstimateDTO`; worker `run_slippage.go`
- **Latency Model** — Rolling percentiles per chain from past receipts; emits `LatencyProfileDTO`; periodic worker `run_latency.go`
- **Validation Worker Update** — Joins probability + slippage + latency outputs before EV gate; replaces Phase 2 fixed priors; `model_join_timeout` fallback to priors
- **Private RPC Routing** — Flashbots + Beaverbuild client pool (`internal/modules/execution/private_rpc.go`); route selection by size threshold

Files introduced: `shared/contracts/` (probability.go, slippage.go, latency.go), `internal/modules/data_quality/` additions (wash_trading.go, rug_risk.go, tax_anomaly.go, risk_score.go), `internal/modules/features/` additions (holder_distribution.go, wallet_entropy.go, drift_detector.go), `internal/modules/edge/` additions (momentum.go, adaptive_threshold.go, new_pool_gate.go), `internal/modules/models/` (probability.go, probability_fit.go, slippage.go, latency.go, models_test.go), `internal/workers/` (run_probability.go, run_slippage.go, run_latency.go), `internal/modules/execution/private_rpc.go`

Exit gate: Brier score `< 0.25`, slippage p95 error `< 30%`, `pass_rate ∈ [0.5%, 5%]`, private RPC routing active, feature drift detector fires on synthetic drift.

---

##### Phase 5 — Learning Engine

**Priority:** P2 | **Requires:** Phase 4 exit gate | **Safe parallel with:** Phase 3, Phase 4 (separate modules)

| Subsection                       | Trigger                                         | Output                                       | Worker File                |
| -------------------------------- | ----------------------------------------------- | -------------------------------------------- | -------------------------- |
| Learning Recorder                | `position_event` (exited)                       | `LearningRecordDTO` (shadow=false)           | `run_learning_record.go`   |
| Shadow Recorder                  | rejection events (DQ/edge/validation/selection) | `LearningRecordDTO` (shadow=true)            | `run_shadow_recorder.go`   |
| Shadow Observer                  | periodic                                        | `shadow_trades` observation update           | `run_shadow_observer.go`   |
| Evaluator                        | periodic per `eval_window_minutes`              | `EvaluationDTO` → `evaluation_event`         | `run_evaluator.go`         |
| Rollback Watchdog                | periodic post-promotion watch                   | `strategy_promotion_event` (rollback action) | `run_rollback_watchdog.go` |
| 5.1 Adjustment Worker            | `evaluation_event`                              | new `StrategyVersion` (draft → shadow)       | `run_updater.go`           |
| A/B Promoter                     | candidate `StrategyVersion` ready               | `StrategyVersion` (shadow → active)          | `run_updater.go`           |
| Opportunity Monitor              | periodic                                        | `mode_transition_event`                      | `run_evaluator.go`         |
| Safe Learning / Shadow Staging   | —                                               | `StrategyVersion.Status` lifecycle extension | —                          |
| Shadow Execution (Paper Trading) | `allocation_event` (shadow mode)                | `ExecutionResultDTO` (Simulated=true)        | `execution/paper.go`       |

Sub-sections:

- **Learning Recorder** — Emits `LearningRecordDTO` (shadow=false) per exited position; trace propagated from `PositionStateDTO`; worker `run_learning_record.go`
- **Shadow Recorder** — Emits `LearningRecordDTO` (shadow=true) per rejected event (DQ/edge/validation/selection); inserts `shadow_trades` row with `observation_complete=false`; worker `run_shadow_recorder.go`
- **Shadow Observer** — Periodic; fetches current token price for each pending shadow trade; computes return since rejection; reclassifies `TN → FN` if `observedReturn > config.evaluation.fn_gain_threshold_pct`; worker `run_shadow_observer.go`
- **Evaluator** — Periodic per `config.learning.eval_window_minutes`; aggregates TP/FP/TN/FN counts, Expectancy, MaxDrawdownPct, SharpeRatio, AvgExecutionError; emits `evaluation_event`; worker `run_evaluator.go`
- **Rollback Watchdog** — Periodic post-promotion watch; if active version expectancy drops below baseline × `(1 − rollback_threshold_pct)` within watch window → auto-rollback `active → rolled_back`, prior version reinstated; worker `run_rollback_watchdog.go`
- **5.1 Adjustment Worker (Bounded Update Engine)** — Triggered by `evaluation_event`; enforces: `N ≥ 30` sample gate, single-family-per-cycle rule (thresholds | weights | cohort_mults), `|Δparam| ≤ 10%` per cycle; creates new `StrategyVersion` at status `draft → shadow`; worker `run_updater.go`
- **A/B Promoter** — Promotes `shadow → active` when ALL: `SampleSize ≥ 30`, `Expectancy(candidate) > Expectancy(baseline) × 1.05`, `Drawdown(candidate) ≤ Drawdown(baseline)`; deactivates prior version; emits `strategy_promotion_event`
- **Opportunity Monitor** — Detects starvation (pass_rate → 0) and overtrading (pass_rate too high); emits `mode_transition_event` (→ EXPLORATION or → STRICT); feeds operational mode system
- **Safe Learning / Shadow Staging** — Extended `StrategyVersion.Status`: `draft → shadow → active → deactivated | rolled_back`; new version runs paper-trades only until A/B gate passes
- **Shadow Execution Mode (Paper Trading)** — `config.execution.mode = shadow | live`; `ProcessShadow()` in `internal/modules/execution/paper.go`; no RPC submission; `ExecutionResultDTO.Simulated = true`; shadow results feed `LearningRecordDTO` with `shadow=true, simulated=true`

Files introduced: `shared/contracts/learning_record.go` (LearningRecordDTO), `internal/modules/learning/` (recorder.go, shadow_recorder.go, shadow_observer.go, fp_fn_classifier.go, evaluator.go, cohort.go, updater.go, ab_promoter.go, opportunity_monitor.go, learning_test.go), `internal/modules/execution/paper.go`, `internal/workers/` (run_learning_record.go, run_shadow_recorder.go, run_shadow_observer.go, run_evaluator.go, run_rollback_watchdog.go, run_updater.go), `shared/database/migrations/20260101000005_learning_tables.sql`

Exit gate: every exit → exactly 1 `learning_record_event` (shadow=false), every rejection → exactly 1 shadow record, evaluator fires on schedule, updater touches exactly 1 parameter family per cycle, A/B promotion deterministic, rollback watchdog fires on synthetic degradation.

---

#### GROUP C — CONTROL LAYER (runs after all others)

Must run after all Group B phases are merged, validated, and their exit criteria met.

---

##### Phase 6 — Resource Control, Wallet Sharding & Production Hardening

**Priority:** P2 | **Requires:** Phase 5 exit gate | **Must run after:** Group B fully merged and validated

| Subsection                   | Scope                                                                                    | Key File(s)                                          |
| ---------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------- |
| RPC Budget                   | Token-bucket rate limiter per endpoint; sheds low-priority on exhaustion                 | `resource_control/rpc_budget.go`                     |
| Gas Budget                   | Per-wallet daily cap + system-wide cap; wallet rotation on cap hit                       | `resource_control/gas_budget.go`                     |
| Compute Budget               | Worker concurrency counts + queue depth limits                                           | `resource_control/compute_budget.go`                 |
| Backpressure                 | Shed policy under queue pressure; exits and confirmations NEVER dropped                  | `resource_control/backpressure.go`                   |
| Global Kill Switch           | BALANCED → DEGRADED → HALTED state machine; auto-resume on recovery                      | `resource_control/halt.go`, `run_risk_controller.go` |
| Full Capital Safety Envelope | Per-token + per-cohort caps extending Phase 2 `CheckEnvelope`; O(1) `GetExposureSummary` | `modules/capital/envelope.go`                        |
| Wallet Sharding              | `hash(TokenAddress) % n` deterministic routing; strict nonce per shard                   | `modules/execution/wallet_shard.go`                  |
| Execution Semaphore          | Global concurrency [5, 20]; adaptive on failure rate                                     | `modules/execution/concurrency.go`                   |
| Priority Ordering (full)     | Exit ≥ 900, replacement ≥ 900, entries ≤ 500; exits NEVER dropped                        | `internal/orchestrator/`                             |
| Event Bus Partitioning       | `PARTITION BY LIST (chain)`; per-chain worker pools; horizontal scale                    | migration `000006`, `shared/database/engines/postgres/`     |
| Data Retention & Archival    | Hot 7d / Warm 30d / Cold archive partition; audit tables retained forever                | `run_archive.go`                                     |
| MEV-Aware Routing            | Flashbots/Beaverbuild/Eden selection; slippage guard at sign-time                        | `modules/execution/mev.go`                           |

Sub-sections:

- **RPC Budget** — Token-bucket rate limiter per endpoint; `Acquire / Release`; sheds low-priority on exhaustion; `internal/resource_control/rpc_budget.go`
- **Gas Budget** — Per-wallet daily cap + system-wide daily cap; wallet rotation on cap hit; `internal/resource_control/gas_budget.go`
- **Compute Budget** — Worker concurrency counts + queue depth limits; `internal/resource_control/compute_budget.go`
- **Backpressure** — Shed policy for low-priority events under queue pressure; exits and confirmations are NEVER dropped; `internal/resource_control/backpressure.go`
- **Global Kill Switch (Risk Halt)** — System mode state machine: `BALANCED → DEGRADED → HALTED`; auto-resume on drawdown recovery; `run_risk_controller.go` (periodic); all entry-path workers add pre-check gate; `internal/resource_control/halt.go`
- **Full Capital Safety Envelope** — Extends Phase 2 minimal `CheckEnvelope` with per-token cap + per-cohort cap; `adapter.GetExposureSummary(ctx, token, cohort)`; O(1) query; `internal/modules/capital/envelope.go` (updated)
- **Wallet Sharding** — `hash(TokenAddress) % n` deterministic routing; one in-flight tx per shard wallet; strictly increasing nonce per shard; `internal/modules/execution/wallet_shard.go`
- **Execution Semaphore** — Global concurrency limit [5, 20]; adaptive: reduce on high failure rate, restore on recovery; `internal/modules/execution/concurrency.go`
- **Priority Ordering (full version)** — `ORDER BY priority DESC, created_at ASC` in `ClaimNextEvent`; exit events ≥ 900, replacement ≥ 900, entries ≤ 500; NEVER drop exit events regardless of queue depth
- **Event Bus Partitioning** — `events` table gains `PARTITION BY LIST (chain)`; one partition child per configured chain; `ClaimNextEvent` gains optional `chain` filter; horizontal scale = add partition + worker pool
- **Data Retention & Archival** — Hot (7 d) / Warm (30 d) / Cold (archive partition); `run_archive.go` periodic; `token_lifecycle`, `executions`, `positions`, `strategy_versions`, `learning_records` retained forever (never archived)
- **MEV-Aware Routing** — Flashbots / Beaverbuild / Eden route selection by size + front-run detection; gas escalation formula; slippage guard at sign-time (`amountOutMin`); `ExecutionResultDTO.MEVProtected`, `.ExecutionPath`; `internal/modules/execution/mev.go`

Files introduced: `internal/resource_control/` (rpc_budget.go, gas_budget.go, compute_budget.go, priority.go, backpressure.go, halt.go, resource_control_test.go), `internal/modules/execution/` additions (wallet_shard.go, concurrency.go, mev.go), `internal/workers/` (run_risk_controller.go, run_archive.go), `shared/config/priority.yaml`, `shared/config/budgets.yaml`, `shared/database/migrations/20260101000006_event_partitioning.sql`

Exit gate: 10× Phase 2 baseline throughput without unbounded queue growth, p95 latency `< 1500 ms`, exit always processed under full queue (10k+ pending entries synthetic test), wallet sharding deterministic (same token → same wallet), kill switch fires on drawdown threshold, cost dashboards show `cost_per_trade_usd` / `rpc_usage_rps` / `gas_spend_daily_usd`.

---

#### GROUP D — MARKET EXTENSION (runs after Group C)

Adds an additional `(ingestion + execution)` market without touching shared layers. Must run after Phase 6 is merged, validated, and exit criteria met. See [docs/reference/architecture.md § 3.11](architecture.md#311-multi-market-architecture-evm--solana) for the full multi-market contract.

---

##### Phase 7 — Solana Market Extension

**Priority:** P2 | **Requires:** Phase 6 exit gate | **Must run after:** Group C fully merged and validated

| Subsection                | Scope                                                                                           | Key File(s)                                                                   |
| ------------------------- | ----------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------- |
| Solana Ingestion          | `logsSubscribe` / `programSubscribe` for Pump.fun + Raydium; Borsh decode; emit `MarketDataDTO` | `internal/modules/ingestion_solana/`                                          |
| Solana Execution          | ed25519 keypair, instruction builder, `sendTransaction`, signature confirmation                 | `internal/modules/execution_solana/`                                          |
| Execution Router          | Switch by `AllocationDTO.Chain` → EVM or Solana; only chain-aware component in Layer 8          | `internal/modules/execution/router.go` (NEW)                                  |
| Solana Workers            | Source ingestion worker + execution router rewire                                               | `internal/workers/run_ingestion_solana.go`, `run_execution.go` (rewired only) |
| Solana RPC Endpoint State | Per-endpoint circuit breaker for Solana RPCs; isolated from EVM endpoint state                  | migration `000007`, adapter additions                                         |
| Solana Signature Tracking | Idempotent recording of submitted signatures + confirmation status                              | migration `000007`, adapter additions                                         |
| Config                    | Additive `chains.yaml::solana` and `execution.yaml::solana` blocks                              | `shared/config/chains.yaml`, `shared/config/execution.yaml`                                 |

**Files introduced:** `internal/modules/ingestion_solana/` (full module), `internal/modules/execution_solana/` (full module), `internal/modules/execution/router.go` (new), `internal/workers/run_ingestion_solana.go` (new), `shared/database/migrations/20260101000007_solana_tables.sql` (new), additive blocks in `shared/config/chains.yaml` and `shared/config/execution.yaml`.

**Files MODIFIED (minimal, scoped):** `internal/workers/run_execution.go` (rewired to call router only — no logic change to EVM path).

**Files NEVER touched by Phase 7:** `shared/contracts/` (DTO schemas), `internal/modules/ingestion/` (EVM ingestion), `internal/modules/data_quality/`, `internal/modules/feature/`, `internal/modules/edge/`, `internal/modules/probability/`, `internal/modules/slippage/`, `internal/modules/selection/`, `internal/modules/capital/`, `internal/modules/position/`, `internal/modules/evaluation/`, `internal/modules/learning/`, EVM execution code paths in `internal/modules/execution/` (Phase 2/6 files), the `database.Adapter` interface (additive methods only).

**Exit gate:** ≥ 10 Solana **devnet** swaps confirmed end-to-end, replay determinism for Solana fixtures, zero EVM regression on full Phase 2 testnet replay, zero cross-market imports (verified by import-graph guard), `AllocateNonce` never invoked when `chain="solana"`. Full criteria in [docs/reference/implementation_roadmap.md § Phase 7 Exit Criteria](implementation_roadmap.md#phase-7--solana-market-extension-p2).

---

**Rules:**

- Group A (Phase 0–2) MUST complete sequentially before any Group B phase starts
- Group B (Phase 3–5) MAY run in parallel — they own separate `internal/modules/` subdirs
- Group C (Phase 6) MUST run after all Group B phases are merged and validated
- Group D (Phase 7) MUST run after Phase 6 is merged and validated; MUST NOT touch EVM ingestion (`internal/modules/ingestion/`), EVM execution code paths (Phase 2/6 files in `internal/modules/execution/`), `shared/contracts/*.go` schemas, or the `database.Adapter` interface (additive methods only)
- Group D (Phase 7) production-grade hardening (per [docs/reference/architecture.md § 3.11.10](architecture.md#31110-solana-ingestion--execution-guarantees-production-grade)) is mandatory: deterministic replay, `EventID` PK dedup, monotonic ingestion watermark, bounded retries (≤ 5), multi-RPC failover with health scoring, latency-aware execution gate, and the full `FailureCategory` enum on every non-confirmed Solana `ExecutionResultDTO` — these are exit criteria, not optional polish
- No phase modifies `shared/contracts/` or `shared/database/` without going through the DTO guardian
- `internal/orchestrator/` is a SHARED file — requires merge validation before any edit

### File Ownership Matrix

| Path                                 | Owner                  | Rule                                                                                                                    |
| ------------------------------------ | ---------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `shared/contracts/`                         | LOCKED — additive only | No field removal, no rename                                                                                             |
| `shared/database/`                          | LOCKED — Phase 0 only  | Schema changes via migrations only                                                                                      |
| `docs/`                              | READ-ONLY              | No agent may modify                                                                                                     |
| `shared/config/`                            | ADDITIVE only          | New keys allowed; existing keys never removed                                                                           |
| `internal/modules/dex_ingest/`       | Phase 1                | Exclusive ownership                                                                                                     |
| `internal/modules/data_quality/`     | Phase 2+4              | Phase 2 skeleton, Phase 4 expands                                                                                       |
| `internal/modules/feature/`          | Phase 2+4              | Phase 2 basic, Phase 4 full                                                                                             |
| `internal/modules/edge/`             | Phase 2+4              | Phase 2 single rule, Phase 4 full                                                                                       |
| `internal/modules/probability/`      | Phase 4                | Exclusive ownership                                                                                                     |
| `internal/modules/slippage/`         | Phase 4                | Exclusive ownership                                                                                                     |
| `internal/modules/selection/`        | Phase 2+4              | Phase 2 top-1, Phase 4 top-K                                                                                            |
| `internal/modules/capital/`          | Phase 2+6              | Phase 2 fixed, Phase 6 full sharding                                                                                    |
| `internal/modules/execution/`        | Phase 2+6+7            | Phase 2 basic, Phase 6 sharding, Phase 7 router only (NEW file `router.go`); EVM execution paths are LOCKED for Phase 7 |
| `internal/modules/execution_solana/` | Phase 7                | Exclusive ownership — Solana execution                                                                                  |
| `internal/modules/ingestion/`        | Phase 1                | LOCKED for Phase 7                                                                                                      |
| `internal/modules/ingestion_solana/` | Phase 7                | Exclusive ownership — Solana ingestion                                                                                  |
| `internal/modules/position/`         | Phase 2+5              | Phase 2 TP/SL, Phase 5 adaptive                                                                                         |
| `internal/modules/evaluation/`       | Phase 3                | Exclusive ownership                                                                                                     |
| `internal/modules/learning/`         | Phase 5                | Exclusive ownership                                                                                                     |
| `internal/orchestrator/`             | SHARED                 | Merge validation required before any edit                                                                               |
| `internal/workers/`                  | Per-phase              | Each phase owns its own worker file                                                                                     |

---

## 6. Token Cost Optimization Strategy

### Skill-First Loading

All agents use the skills system from `.github/skills/` instead of re-reading full documentation:

| Full Doc                    | Tokens | Equivalent Skill              | Tokens | Savings |
| --------------------------- | ------ | ----------------------------- | ------ | ------- |
| `docs/reference/architecture.md`      | ~5000  | pipeline + modularity skills  | ~600   | 88%     |
| `docs/reference/dto_contracts.md`     | ~6000  | dto skill                     | ~400   | 93%     |
| `docs/reference/orchestrator_spec.md` | ~4000  | pipeline + idempotency skills | ~500   | 88%     |

### Token Optimization Rules

1. **Reuse context within session** — Never re-read a document already loaded
2. **Skills first, docs second** — Load skills before falling back to raw documentation
3. **Prefer grouped execution** — Use Mode 2 or Mode 3 to share context
4. **No full-doc reads** — Load the relevant skill, then deep-dive into specific doc sections only if needed
5. **Progressive loading** — Skill discovery → skill body → doc section (only if needed)
6. **Skill injection is automatic** — `run_parallel.sh` injects skill references into every Copilot call

---

## 7. Resilience Framework

### Universal Retry Pattern

ALL stages follow the same deterministic execution pattern:

```text
execute → validate → fix → re-validate → bounded retry → success OR rollback
```

Every code path terminates in a defined state — **no infinite loops, no undefined state**.

### Retry Configuration

```bash
MAX_RETRIES_PHASE_BUILDER=5
MAX_RETRIES_DTO=5
MAX_RETRIES_INTEGRATION=5
MAX_RETRIES_MERGE=5
MAX_RETRIES_GLOBAL_VALIDATION=5
MAX_REMEDIATION_RETRIES=3          # quality gate remediation within pipeline
MAX_PARALLEL_AGENTS=3              # resource control
MODEL_HEAVY=claude-opus-4.7        # Mode 1 heavy model
MODEL_HEAVY_LITE=claude-sonnet-4.6 # Modes 2 & 3 heavy model
```

All retry limits are bounded. The system is **guaranteed to terminate**.

### Workspace Confinement

All agent prompts include a `_WORKSPACE_CONSTRAINT` clause that is injected automatically:

```
WORKSPACE CONSTRAINT: NEVER write any files, scripts, summaries, or reports to
/tmp, /var, /private, or any path outside this project directory. Write ALL output
files inside the project — use .parallel-dev/ for temporary artifacts and output/
for generated files.
```

This prevents `Permission denied` errors that occur when agents attempt to create
verification scripts or summary files in `/tmp` outside the allowed workspace.

### Checkpoint & Rollback

Before each phase/group, a Git tag checkpoint is created:

```bash
git tag checkpoint-${phase_label}-pre
```

If any stage exceeds its retry limit:

```bash
git reset --hard checkpoint-${phase_label}-pre
```

On success, the checkpoint is cleaned up:

```bash
git tag -d checkpoint-${phase_label}-pre
```

### Agent Pipeline (per phase/group)

```text
phase-builder (up to 5 retries)
  → dto-guardian (up to 5 retries)
    → integration (up to 5 retries)
      → security-auditor (up to 3 retries)
        → test-builder (up to 3 retries)
          → refactor/quality gates (up to 3 retries)
            → success OR rollback
```

### Stage-Specific Validation

| Stage             | Agent                   | Validates                                                               | Fix Agent            | On Failure                               |
| ----------------- | ----------------------- | ----------------------------------------------------------------------- | -------------------- | ---------------------------------------- |
| Phase Builder     | `phase-builder`         | Module compiles; no syntax errors; imports valid                        | `refactor`           | Rollback to checkpoint                   |
| DTO Guardian      | `dto-guardian` (STRICT) | All DTOs immutable; no missing/extra fields; no mutable defaults        | `dto-guardian`       | Rollback to checkpoint                   |
| Integration       | `integration`           | No cross-module imports; no DB calls in modules; deterministic ordering | `refactor`           | Rollback to checkpoint                   |
| Security Audit    | `security-auditor`      | OWASP Top 10; no secrets in code; no injection vectors                  | `refactor`           | Rollback to checkpoint                   |
| Test Builder      | `test-builder`          | Test suite passes; no network/DB in unit tests                          | `refactor`           | Rollback to checkpoint                   |
| Global Validation | orchestrator            | All quality gates + DTO flow + orchestrator authority                   | `refactor` (up to 5) | `remediation_failed` — operator required |

#### Phase Builder

- **Execute:** Copilot `phase-builder` agent implements phase
- **Validate:** Module compiles, no syntax errors, imports valid
- **Fix:** `refactor` agent fixes compilation issues
- **On failure:** Rollback to checkpoint

#### DTO Guardian (STRICT)

- **Execute:** Copilot `dto-guardian` agent validates `shared/contracts/`
- **Validate:** All DTOs immutable, no missing/extra fields, no mutable defaults
- **Fix:** `dto-guardian` agent fixes DTO issues
- **On failure:** Rollback to checkpoint

#### Integration Agent

- **Execute:** Copilot `integration` agent validates cross-module wiring
- **Validate:** No cross-module imports, no DB in modules, deterministic ordering
- **Fix:** `refactor` agent removes violations
- **On failure:** Rollback to checkpoint

#### Global Validation (CRITICAL)

- **Checks:** All quality gates + DTO flow + orchestrator authority
- **Fix:** `refactor` agent (up to 5 remediation attempts)
- **On failure:** System enters `remediation_failed` state — operator intervention required

### Post-Phase Pipeline Stages

After all phase/group agents complete, the following pipeline stages run in sequence.
Each stage is tracked in `.parallel-dev/phase-status.json` with its own state, model,
exit code, and timestamp.

| Stage               | Agent            | Function                                            | Fatal?        |
| ------------------- | ---------------- | --------------------------------------------------- | ------------- |
| `post-merge-review` | `merge-reviewer` | DTO flow, module boundaries, orchestrator authority | Yes           |
| `docs-sync`         | `merge-reviewer` | Implementation drift from `docs/` specs             | No (advisory) |
| `global-validation` | orchestrator     | Quality gates + orchestrator authority check        | Yes           |
| `remediation`       | `refactor`       | Fix global-validation failures (up to 5 retries)    | Yes           |

### Per-Mode Recovery

| Mode   | Failure Scope          | Recovery Action                                                  |
| ------ | ---------------------- | ---------------------------------------------------------------- |
| Mode 1 | Single agent fails     | Rollback that phase. Other agents continue.                      |
| Mode 1 | Merge conflict         | `conflict-resolver` agent with bounded retry (up to 5).          |
| Mode 2 | Agent fails mid-group  | Rollback to checkpoint. Earlier commits preserved.               |
| Mode 2 | Context window full    | Split remaining phases into new session.                         |
| Mode 3 | Single group fails     | Rollback that group. Other groups continue.                      |
| Mode 3 | Merge conflict         | `conflict-resolver` agent with bounded retry (up to 5).          |
| All    | Post-merge review fail | `merge-reviewer` agent retries (up to 5). Then: `review_failed`. |
| All    | Global validation fail | `refactor` agent (up to 5). Then: defined `remediation_failed`.  |

### Quality Gate Checks (All Modes)

Quality gates are fully delegated to `scripts/hooks/quality-gates.sh`. The orchestrator
calls this hook and treats a non-zero exit code as a gate failure. Each project provides
its own gate implementation (Python/Node/Go/etc.)

Recommended checks to implement in the hook:

1. **Compile/import check** — Project compiles/imports successfully
2. **Lint check** — No lint errors in modified files
3. **Test check** — Test suite passes
4. **SQL check** — No database driver imports in `internal/modules/`
5. **Cross-module check** — No cross-module imports between `internal/modules/` packages
6. **Console check** — No unstructured console output in `internal/modules/`
7. **DTO validation** — All DTOs in `shared/contracts/` are immutable
8. **Orchestrator integrity** — No database imports in `internal/modules/`
9. **Protected files** — Warns if `shared/contracts/`, `shared/database/`, or `docs/` were modified
10. **Deterministic ordering** — No unordered iteration of collections without explicit sorting

Gates 1–8 are **blocking** (cause failure). Gates 9–10 are **advisory**.

---

## 8. Status Display

Run `./scripts/run_parallel.sh status` at any time to see the live session state.

### Full Status Layout

```
═══ Parallel Development Status ═══

  Mode:               3 (Hybrid)
  Phases:             2 3 4
  Integration branch: integration/parallel-20260324-100000
  Status:             running
  Started:            2026-03-24T10:00:00Z
  Branches:           track/phase-2, track/phase-3, track/phase-4

  Model (heavy):      claude-sonnet-4.6
  Rotation pool:      claude-sonnet-4.6 → claude-sonnet-4.5 → gpt-5.3-codex → gpt-5.4

  Branch Progress:
    phase-2 (ingestion-scene-splitter)           2 commits        — feat(phase-2): implement ingestion
    phase-3 (processing)                         0 commits        — (no commits yet)

  Agent Status:
    Phase/Group                    State            Model                        Exit   Updated
    ────────────────────────────── ──────────────── ──────────────────────────── ────── ────────────────────
    phase-2 (ingestion-scene-spl.) complete         claude-opus-4.7              0      2026-03-24T10:30:00Z
    phase-3 (processing)           running          claude-sonnet-4.5            —      2026-03-24T10:15:00Z
    ──────────── Post-Phase Pipeline ────────────────────────────────────────────────────────
    post-merge-review              complete         claude-sonnet-4.6            0      2026-03-24T10:35:00Z
    docs-sync                      advisory_failed  claude-sonnet-4.5            1      2026-03-24T10:36:00Z
    global-validation              complete         N/A                          0      2026-03-24T10:37:00Z

  Log files:
    phase-2-phase-builder-1.log -> /path/to/log (12,345 bytes)
    phase-2-dto-guardian-1.log  -> /path/to/log (4,210 bytes)
    post-merge-review-1.log     -> /path/to/log (8,901 bytes)
    docs-sync.log               -> /path/to/log (3,102 bytes)
```

### Phase/Group State Values

| State             | Meaning                                              |
| ----------------- | ---------------------------------------------------- |
| `running`         | Agent pipeline actively executing                    |
| `complete`        | All stages passed; exit code 0                       |
| `failed`          | Exceeded retry limit; rolled back to checkpoint      |
| `timed_out`       | Per-phase timeout expired (exit code 124)            |
| `advisory_failed` | Docs-sync advisory check reported issues (non-fatal) |

### State File Location

Phase status is persisted atomically to `.parallel-dev/phase-status.json`.
The session state (mode, branches, status) is persisted to `.parallel-dev/state.json`.

---

## 9. Requirements

- **Bash 4+** — Required for associative arrays. macOS ships with bash 3.2; install via `brew install bash`
- **Git 2.5+** — Worktree support
- **Python 3** — Used by the YAML config parser and `update_phase_status()` (stdlib only, no packages required)
- **Copilot CLI** — For automated agent execution
- **GitHub CLI (`gh`)** — For PR creation. Auto-installed if absent (Homebrew/apt/dnf). Run `gh auth login` once
- **`COPILOT_GITHUB_TOKEN`, `GH_TOKEN`, or `GITHUB_TOKEN`** — Copilot auth token (checked at startup)
- **(Optional)** `timeout` or `gtimeout` — Per-phase timeout enforcement (falls back to shell watchdog)
- **Language-specific tools** — Belong in `scripts/hooks/`, not in `run_parallel.sh` itself

---

## 10. Fully Autonomous Pipeline

The `start` command runs the **entire pipeline** from implementation to PR without human
intervention:

```text
./scripts/run_parallel.sh start [--mode=1|2|3] <phases...>
        │
        ▼
[1] Per phase/group: phase-builder → dto-guardian → integration → security-auditor → test-builder → refactor
        │  (bounded retries per stage; rollback to checkpoint on exceed)
        ▼
[2] Auto-merge all branches (union strategy)
         └─ conflict-resolver agent resolves conflicts (bounded retries)
        │
        ▼
[3] Post-merge review — merge-reviewer agent
         └─ DTO flow integrity + module boundaries + orchestrator authority
        │
        ▼
[4] Documentation sync — merge-reviewer agent (advisory, non-blocking)
        │
        ▼
[5] Global validation — quality gates + orchestrator authority
         └─ refactor agent remediates failures (bounded retries)
        │
        ▼
[6] git push + gh pr create  ───────────────────────────►  PR ready for review
```

The only step requiring a human is reviewing and merging the PR.

### Opting Out of Auto-Merge

To run agents without auto-proceeding to merge and PR:

```bash
./scripts/run_parallel.sh start --no-auto-merge [--mode=1|2|3] <phases...>
# Agents run, then stop. You can inspect before:
./scripts/run_parallel.sh merge
```

### Partial Failure Handling

If some (but not all) agents fail in Modes 1/3, auto-merge is **skipped**. You see:

```
[WARN] Some agent(s) failed — skipping auto-merge.
[INFO] Fix failures then run: run_parallel.sh merge
```

Failed phases are rolled back to checkpoint; successful phases remain on their branches.

---

## 11. Hook System

All language-specific operations are delegated to hook scripts under `scripts/hooks/`.
The orchestrator (`run_parallel.sh`) **never needs project-specific modification**.

### Required Hook Files

| Hook file                        | When called                       | Purpose                                              |
| -------------------------------- | --------------------------------- | ---------------------------------------------------- |
| `scripts/hooks/setup-env.sh`     | Worktree creation (Modes 1 & 3)   | Install deps (pip install, npm ci, go mod download…) |
| `scripts/hooks/activate-env.sh`  | Before each agent run (all modes) | Activate runtime env (.venv, nvm, …)                 |
| `scripts/hooks/validate.sh`      | After phase-builder + after merge | Syntax/compile/import checks                         |
| `scripts/hooks/quality-gates.sh` | Quality gates check               | Lint + tests + arch checks                           |

### Hook Contract

- **Missing hook** → warning logged, execution continues (non-blocking)
- **Hook exits 0** → success
- **Hook exits non-zero** → failure (triggers retry or rollback per stage)
- Hooks run with `cwd` set to the worktree/project root
- Hooks receive no arguments by default (add project-specific logic inside)

### Session State

The orchestrator persists session-level state to `.parallel-dev/state.json`, including
model routing information for visibility:

```json
{
  "mode": 1,
  "phases": "2 3 4",
  "integration_branch": "integration/parallel-20260324-100000",
  "branches": ["track/phase-2", "track/phase-3", "track/phase-4"],
  "started_at": "2026-03-24T10:00:00Z",
  "status": "running",
  "model_heavy": "claude-opus-4.7",
  "model_rotation_pool": [
    "claude-sonnet-4.6",
    "claude-sonnet-4.5",
    "gpt-5.3-codex",
    "gpt-5.4"
  ]
}
```

### Phase Status Tracking

Each phase/group writes structured status to `.parallel-dev/phase-status.json`,
including which model the agent is using:

```json
{
  "phases": {
    "phase-2": {
      "phase": "phase-2",
      "state": "complete",
      "model": "claude-opus-4.7",
      "started_at": "2026-03-24T10:00:00Z",
      "exit_code": 0,
      "updated_at": "2026-03-24T10:30:00Z"
    },
    "phase-3": {
      "phase": "phase-3",
      "state": "running",
      "model": "claude-sonnet-4.5",
      "started_at": "2026-03-24T10:01:00Z",
      "updated_at": "2026-03-24T10:15:00Z"
    }
  }
}
```

States: `running` → `complete` | `failed` | `timed_out`

### Per-Phase Timeout

All agent subshells are wrapped with `run_with_timeout 1800` (30 minutes default).
A timed-out phase is recorded as `timed_out` in phase-status.json and treated as a
failure (triggers rollback). Override by setting `AGENT_TIMEOUT_SECONDS` before calling
the function directly.

---

## 12. Parallel Safety Invariants

These invariants MUST hold across all phases, all modes, and all merge combinations.
Any violation corrupts pipeline state and cannot be recovered by retry alone.

| #   | Invariant                                                                                         | Allowed                                                               | Forbidden                                                                                | Enforcement                                                                    |
| --- | ------------------------------------------------------------------------------------------------- | --------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ |
| A   | **DTO Immutability** — `shared/contracts/` is additive-only                                              | Add new fields; add new DTO types                                     | Remove / rename fields; change field type; make immutable field mutable                  | `dto-guardian` after every `phase-builder`; violation aborts phase immediately |
| B   | **Adapter Interface Immutability** — `shared/database/adapter.go` does not change during parallel phases | Add a new adapter method                                              | Rename / change signature of existing method; remove a method                            | Compile error detected by `integration` agent                                  |
| C   | **Module Isolation** — each module is a pure function                                             | Accept DTO in, return DTO out; import from `shared/contracts/` only          | Import another module; import from `shared/database/`; manage own state; call Telegram directly | `integration` agent cross-module import check                                  |
| D   | **Event Contract Immutability** — event type strings are immutable once published                 | Add a new event type                                                  | Rename existing event type; change payload schema without versioning                     | `integration` agent event-type registry check                                  |
| E   | **Lifecycle Consistency** — all state transitions use READ → VALIDATE → CAS                       | `TransitionState` with `ExpectedStateVersion`; `SKIP LOCKED` claiming | Optimistic update without CAS; two workers reading same event without SKIP LOCKED        | `quality-gates.sh` CAS pattern check                                           |
| F   | **Event Bus Safety** — one primary consumer per event type                                        | Fan-out by publishing new events from consumer                        | Two workers both reading the same event type; consumer mutating past events              | `integration` agent consumer-group registry                                    |

### A. DTO Immutability

```text
INVARIANT: contracts/ is additive-only.
  ✅ Adding new fields to a DTO
  ✅ Adding new DTO types
  ❌ Removing a field
  ❌ Renaming a field
  ❌ Changing a field type
  ❌ Making an immutable field mutable
```

Enforcement: `dto-guardian` runs after every `phase-builder`. Any violation aborts the
phase immediately — no retry, rollback to checkpoint.

### B. Adapter Interface Immutability

```text
INVARIANT: database/adapter.go interface does not change during parallel phases.
  ✅ Adding a new adapter method (new phase needs it)
  ❌ Renaming an existing method (breaks all callers simultaneously)
  ❌ Changing an existing method signature
  ❌ Removing a method
```

Adapter changes require sequential coordination — schedule as Phase 0 extension or
between phase groups.

### C. Module Isolation

```text
INVARIANT: Each module in internal/modules/ is a pure function.
  ✅ Module accepts DTO input, returns DTO output
  ✅ Module imports from contracts/ only
  ❌ Module imports another module (internal/modules/<other>)
  ❌ Module imports from database/ or calls the adapter
  ❌ Module manages its own state
  ❌ Module calls Telegram or any external service directly
```

### D. Event Contract Immutability

```text
INVARIANT: Event type strings are immutable once published.
  ✅ Adding a new event type
  ❌ Renaming an existing event type (breaks all consumers)
  ❌ Changing an event payload schema without versioning
```

Consumer workers use string-matched event types — renames silently break consumers with
no compile error. New event types require new consumer registration in orchestrator.

### E. Lifecycle Consistency (Token State Machine)

All token state transitions MUST follow the READ → VALIDATE → CAS TRANSITION pattern:

```go
// Safe lifecycle transition (compare-and-swap):
affected, err := adapter.TransitionTokenState(ctx, tokenID, "queued", "processing")
if err != nil || affected == 0 {
    // Another worker already transitioned — skip, do not retry
    return nil
}
```

`SELECT ... FOR UPDATE SKIP LOCKED` guarantees only one worker claims a token at a time.
**Never use** optimistic updates without CAS — race conditions corrupt the lifecycle audit trail.

### F. Event Bus Safety

```text
INVARIANT: One primary consumer per event type.
  ✅ One worker reads "token_detected" events
  ✅ Fan-out via publishing new events from the consumer
  ❌ Two workers both reading "token_detected" (duplicate processing)
  ❌ Consumer mutating past events
```

All events are append-only. Workers consume via `SELECT ... FOR UPDATE SKIP LOCKED`
and track offset via `consumer_offsets`. Full state is reconstructible from event log alone.

---

## 13. Parallel Failure Modes

The following failure modes can occur during parallel development. Each has a defined
detection method and resolution path.

| Failure Mode                         | Description                                                                                                                                                                                                                   | Detection                                                                                                                                                               | Resolution                                                                                                          |
| ------------------------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------- | --- | --------- | --- | ------------------------------------------------------------------------------------------------- |
| **DTO Drift**                        | A module uses fields not in `shared/contracts/` or uses stale types                                                                                                                                                                  | `dto-guardian` reports mismatches                                                                                                                                       | Agent fixes contract or module; additive only                                                                       |
| **Module Collision**                 | Two parallel phases write to the same `internal/modules/<name>/` file                                                                                                                                                         | Git merge conflict on merge                                                                                                                                             | `conflict-resolver` agent + union merge strategy                                                                    |
| **Lifecycle Race**                   | Two workers transition a token simultaneously without CAS                                                                                                                                                                     | Duplicate processing in logs or corrupted position state                                                                                                                | Fix transition to use CAS + SKIP LOCKED; replay from event log                                                      |
| **Merge Corruption**                 | A phase's module silently breaks an earlier phase after union merge                                                                                                                                                           | Global validation quality gates fail post-merge                                                                                                                         | `refactor` agent with full quality gate context; re-run gates                                                       |
| **Adapter Signature Break**          | A phase changes `shared/database/adapter.go` interface during parallel work                                                                                                                                                          | Compile error in other phases                                                                                                                                           | Sequential fix: finish and merge the changing phase before others                                                   |
| **Event Type Rename**                | An event type string is renamed in one branch                                                                                                                                                                                 | Consumer worker silently stops processing in merged code                                                                                                                | `integration` agent detects; revert rename, use additive versioning                                                 |
| **Orchestrator Conflict**            | Two phases both modify `internal/orchestrator/orchestrator.go`                                                                                                                                                                | Git merge conflict                                                                                                                                                      | `conflict-resolver` agent; union merge preserves all stage registrations                                            |
| **Cross-Market Contamination**       | Solana logic leaks into EVM code path (or vice versa) — e.g. Solana module imports `internal/modules/ingestion/`, or EVM execution invokes Solana RPC                                                                         | Import-graph guard via `dependency-analysis` skill; integration test counter for `AllocateNonce` calls when `chain="solana"`                                            | Phase 7 rollback; refactor to relocate logic into the chain-specific module under `internal/modules/*_solana/`      |
| **Solana Ingestion Non-Determinism** | Solana worker writes events with mismatched `EventID` across two replay runs of the same fixture (e.g. `time.Now()` leaked into `IngestedAt`, RPC endpoint URL leaked into the ID, or non-deterministic instruction ordering) | Replay test diff: `SELECT event_id FROM events WHERE event_id LIKE 'replay:%' ORDER BY ...` differs run-to-run; or duplicate rows missing from `ON CONFLICT DO NOTHING` | Revert `internal/modules/ingestion_solana/` to last-known-good commit; re-verify `EventID = SHA256("solana"         |     | signature |     | instruction_index)[:16]`is exact; ensure`IngestedAt` derives from event timestamp, not wall clock |
| **Solana Watermark Regression**      | Crash between event INSERT and watermark UPDATE leaves `solana_ingestion_watermark.slot` behind the highest `events.slot`; or a buggy adapter caller passes a smaller slot                                                    | `UpsertIngestionWatermark` rejects regression; integration test asserts monotonicity post-restart                                                                       | Run gap recovery from persisted watermark; `EventID` PK absorbs duplicate emission; never manually rewind watermark |

### Merge Validation Steps (run after every branch merge)

Run these in sequence before proceeding to global validation:

1. **DTO compatibility** — `dto-guardian` re-validates all `shared/contracts/`; additive check only
2. **Adapter interface** — Verify `shared/database/adapter.go` matches all call sites in orchestrator
3. **Event type registry** — Verify all event type strings in workers match emissions in modules
4. **Worker registration** — Verify all workers are registered in `internal/orchestrator/orchestrator.go`
5. **Orchestrator wiring** — Verify stage sequence matches canonical pipeline order (Layer 0–10)
6. **Quality gates** — `scripts/hooks/quality-gates.sh` exits 0
