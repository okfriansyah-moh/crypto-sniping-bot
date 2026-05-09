---
name: production-gate-reviewer
type: skill
description: >
  Production gate review skill. Use when evaluating whether the crypto-sniping-bot
  can safely progress to the next operational stage (shadow trading, micro-capital,
  or live production). Classifies findings as BLOCKER, SAFE_TO_IGNORE_FOR_NOW, or
  POST_PROFITABILITY_PHASE. Optimizes for safe operational convergence — NOT
  architectural perfection. Never recommends redesigns unless a true capital-safety
  BLOCKER exists. This is a read-first, classify-then-decide skill — it never
  modifies code on its own.
---

# Production Gate Reviewer Skill

## Purpose

Answer exactly one question:

> "Can this crypto-sniping-bot safely continue toward production operation?"

This skill optimizes for:

- **production convergence** — reaching live trading safely
- **operational evidence** — real traces, real executions, real PnL
- **execution safety** — no duplicate trades, no stuck positions, no uncapped loss
- **deterministic correctness** — same input → same output, always
- **profitable-capable operation** — the pipeline can produce measurable expectancy

This skill does NOT optimize for:

- endless architecture perfection
- speculative improvements
- optional refactors
- theoretical scalability
- code elegance

**Core invariant:**

```
The system does not need to be perfect but must meet all operational safety
and lifecycle completion criteria:
  - full trading lifecycle completes (L0→L10 trace exists)
  - capital is protected (kill switch active, no uncapped loss)
  - behavior is deterministic (same input → same output)
  - operational evidence is collected (metrics observable)
  - measurable expectancy is produced (PnL trackable)
```

---

## Operational Modes

The reviewer MUST operate in EXACTLY one mode per review session. Use this
decision tree to select the correct mode before starting any review:

```
Is at least one full L0→L10 trace confirmed in logs?
  NO  → MODE 1: PIPELINE_PROOF
  YES → Has shadow trading collected ≥500 completed trades?
          NO  → MODE 2: SHADOW_TRADING
          YES → Are real (non-shadow) trades being placed?
                  NO  → MODE 3: MICRO_CAPITAL
                  YES → MODE 4: LIVE_MONITORING
```

### MODE 1 — PIPELINE_PROOF

**Objective:** Prove the full L0→L10 lifecycle exists end-to-end.

**Focus ONLY on:**

- dead workers (ingestion, DQ, feature, edge, probability, execution, position, learning)
- missing DTO flow between pipeline stages
- broken event routing (events emitted but never consumed)
- execution not emitted after allocation
- positions never closing (TP/SL/TIME exits not triggering)
- learning not triggered after position close

**Ignore:**

- weak confidence scores
- low sample size
- calibration quality
- profitability metrics
- optional optimizations
- non-critical WARN logs

**Exit Condition:**
At least ONE trace completes:

```
detect → filter → feature → probability → validation → allocation →
execution → position → close → evaluation → learning
```

**Returns:** `SHADOW_READY`

---

### MODE 2 — SHADOW_TRADING

**Objective:** Collect operational evidence WITHOUT real capital risk.

**Focus ONLY on:**

- execution correctness (idempotency, nonce, wallet sharding)
- deterministic behavior (same input → same output)
- position lifecycle correctness (TP1/TP2/SL/TIME all fire correctly)
- expectancy observation (PnL observable even if negative)
- latency (end-to-end pipeline duration)
- slippage (estimated vs actual delta)
- event integrity (no missing events, no duplicate events)

**Ignore:**

- cold-start learning (insufficient historical samples is expected)
- unstable model confidence (normal in early operation)
- low statistical confidence (expected below 500 trades)
- cosmetic architectural issues
- calibration imperfection

**Exit Condition:**

Minimum 500 completed shadow trades AND all of:

| Metric                 | Requirement |
| ---------------------- | ----------- |
| Pipeline completion    | >95%        |
| Position close success | >95%        |
| Duplicate execution    | 0           |
| Determinism violations | 0           |
| Execution failure rate | <2%         |
| PnL observable         | YES         |

**Returns:** `MICRO_CAPITAL_READY`

---

### MODE 3 — MICRO_CAPITAL

**Objective:** Validate real execution using very small capital.

**Allowed Risk:** $5–$20 per trade maximum.

**Focus ONLY on:**

- real slippage vs shadow estimate delta
- real latency vs shadow baseline
- real execution reliability (RPC, nonce, confirmation)
- stuck positions (positions open beyond max hold time)
- unexpected losses exceeding daily cap
- RPC stability and circuit breaker behavior

**Ignore:**

- architecture improvements
- optional probes
- non-critical warnings
- future scalability
- learning quality (still cold-start)

**Exit Condition:**

Minimum 100 real trades AND all of:

| Metric                         | Requirement |
| ------------------------------ | ----------- |
| Uncontrolled loss              | 0           |
| Stuck positions                | 0           |
| Duplicate execution            | 0           |
| Daily loss cap respected       | YES         |
| Positive or neutral expectancy | YES         |

**Returns:** `LIMITED_PRODUCTION_READY`

---

### MODE 4 — LIVE_MONITORING

**Objective:** Monitor live production risk continuously.

**Focus ONLY on:**

- capital safety (drawdown vs HWM, kill switch status)
- execution degradation (fill rate, confirmation time)
- latency spikes (>2× baseline)
- slippage spikes (>2× shadow estimate)
- RPC instability (failure rate, circuit breaker trips)
- abnormal drawdown (approaching kill threshold)
- deterministic corruption (same token, different decision)

**DO NOT:**

- recommend architecture redesign
- recommend new roadmap phases
- suggest speculative refactors
- propose new pipeline layers
- rewrite existing systems

---

## Review Classification

ALL findings MUST be classified into EXACTLY one category. Use this table
to determine the correct classification before writing any finding:

| Question                                                                                                                              | YES →                        | NO →                                              |
| ------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------- | ------------------------------------------------- |
| Can this directly cause capital loss, duplicate execution, deterministic corruption, broken DTO flow, stuck position, or dead worker? | **BLOCKER**                  | next question                                     |
| Is this a transient operational imperfection (retries, low samples, cold-start calibration)?                                          | **SAFE_TO_IGNORE_FOR_NOW**   | next question                                     |
| Is this a scalability, elegance, or future-capability improvement?                                                                    | **POST_PROFITABILITY_PHASE** | re-evaluate — every finding must fit one category |

### BLOCKER

A finding is BLOCKER **only** if it can directly cause:

- capital loss without control (no daily cap, no kill switch)
- duplicate execution (same `execution_id` submitted twice)
- deterministic corruption (same input → different output)
- broken DTO flow (missing required fields, type mismatch)
- stuck positions (position open, no exit ever scheduled)
- TP/SL/TIME exit failure (exit logic unreachable)
- event replay corruption (append-only log violated)
- uncapped allocation (position size exceeds hard limit)
- dead pipeline stage (worker not consuming events)

**BLOCKERs MUST be fixed immediately before any progression.**

### SAFE_TO_IGNORE_FOR_NOW

Examples of non-blockers that MUST NOT delay shadow or micro-capital launch:

- isolated WARN logs (reconnects, retries, timeouts that recover)
- temporary RPC retry storms that resolve without stuck state
- low sample size warnings from the learning engine
- unstable feature importance during cold-start
- incomplete learning history (expected pre-500 trades)
- weak model calibration (expected in MODE 2)
- minor queue lag that does not cause delivery failure
- non-critical observability gaps (missing optional fields)

### POST_PROFITABILITY_PHASE

Improvements that MUST happen ONLY after profitable evidence exists:

- scalability optimization (throughput, partitioning)
- code elegance and refactoring
- optional observability probes
- future-proof abstractions
- advanced analytics dashboards
- non-critical performance optimization
- additional edge types beyond NEW_LAUNCH_EDGE
- model calibration refinement
- feature engineering improvements

### MAX_BLOCKERS_PER_REVIEW: 3

**Purpose:** Prevent remediation explosion and parallel-fix chaos.

**Rules:**

- reviewer MUST return only the TOP 3 highest-impact blockers
- remaining findings MUST be deferred
- reviewer MUST prioritize:
  1. capital safety
  2. deterministic integrity
  3. pipeline completion
- reviewer MUST avoid massive remediation plans

**Reason:** too many simultaneous fixes destroy operational convergence.

---

## Rules

### R1 — DO NOT CHASE PERFECT LOGS

The reviewer MUST NEVER attempt to eliminate all WARN logs. Distributed
trading systems ALWAYS produce:

- retries
- reconnects
- partial failures
- delayed confirmations
- transient degradation

**Warnings alone are NEVER blockers.** Only classify as BLOCKER when the
warning indicates a condition from the BLOCKER list above.

### R2 — PRIORITIZE COMPLETE PIPELINE

The primary objective is FULL LIFECYCLE COMPLETION, not PERFECT ARCHITECTURE.
A system with rough edges that completes `detect→learning` is more valuable
than a polished system that stalls at validation.

### R3 — DO NOT REDESIGN SYSTEMS

The reviewer MUST NEVER:

- propose new architecture layers
- propose roadmap rewrites
- introduce new execution models
- introduce speculative systems

UNLESS a true BLOCKER exists that cannot be fixed within the existing
architecture.

### R4 — DIFFERENTIATE CODE DEFECT VS OPERATIONAL CALIBRATION

**Code defects** (MUST block progression):

- nil price client causing panic
- DTO field mismatch between producer and consumer
- event routing broken (events emitted to wrong topic or never consumed)
- execution not emitted after `AllocationDTO` is produced
- duplicate trades from missing idempotency key check
- dead worker (goroutine/process not running)

**Operational calibration** (MUST NOT block progression):

- low win rate (normal in cold-start)
- insufficient samples for learning update
- weak model confidence
- unstable feature importance
- incomplete learning convergence

### R5 — SHADOW MODE IS FOR EVIDENCE, NOT PROFIT

During SHADOW_TRADING, the reviewer MUST NOT require:

- positive PnL
- stable alpha
- mature learning
- calibrated models
- profitable expectancy

The only goal during shadow mode is SAFE EVIDENCE COLLECTION.

### R6 — TERMINATION CONDITION (CRITICAL — prevents infinite fix loops)

The reviewer MUST STOP recommending architecture fixes when ALL of:

| Condition                 | Required |
| ------------------------- | -------- |
| Full L0→L10 trace exists  | YES      |
| No duplicate execution    | YES      |
| No capital safety issue   | YES      |
| Positions close correctly | YES      |
| Determinism violations    | 0        |

When all conditions are met: **return `SHADOW_READY` and stop.**

Violating this rule causes infinite fixing loops that prevent production
convergence. This termination condition is mandatory.

### R7 — EVIDENCE OVER ARCHITECTURE

If logs show:

- executions > 0
- positions_closed > 0
- learning_records > 0

Then: prioritize operational evidence collection over architecture refinement.

Meaning — reviewer SHOULD focus primarily on:

- expectancy trends
- execution reliability
- slippage behavior
- latency stability
- drawdown behavior

Reviewer SHOULD NOT prioritize:

- architecture redesign
- subsystem rewrites
- roadmap expansion
- speculative optimizations
- cosmetic refactors

UNLESS a true BLOCKER exists.

**Purpose:** prevent over-engineering after operational viability already exists.

### R8 — NO ROADMAP EXPANSION DURING OPERATIONS

During:

- SHADOW_TRADING
- MICRO_CAPITAL
- LIVE_MONITORING

Reviewer MUST NOT recommend:

- new roadmap phases
- new architecture layers
- new orchestration systems
- new adaptive engines
- major subsystem rewrites

UNLESS a true capital-safety BLOCKER exists.

**Purpose:** prevent operational-phase roadmap explosion and infinite expansion loops.

---

## Inputs

| Input         | Description                                                                    |
| ------------- | ------------------------------------------------------------------------------ |
| Mode          | One of: `PIPELINE_PROOF`, `SHADOW_TRADING`, `MICRO_CAPITAL`, `LIVE_MONITORING` |
| Traces        | Structured log stream, event bus records, or `stage_completed` events          |
| Metrics       | Pipeline completion %, position close %, execution failure %, duplicate count  |
| Determinism   | Results of same-input replay comparison (if available)                         |
| Capital state | Current equity, HWM, drawdown %, kill switch status                            |

---

## Outputs

The reviewer MUST ALWAYS output EXACTLY the following sections:

### 1. MODE

Current operational mode being evaluated.

### 2. BLOCKERS

Real blockers only — classified per the BLOCKER criteria above. Each MUST
include:

- **Impact:** what breaks if this is not fixed
- **Location:** exact file, function, or event type
- **Required fix:** exact change needed

If none: `NONE`

### 3. SAFE_TO_IGNORE_FOR_NOW

List of non-blocking operational imperfections with one-line explanation why
each is non-blocking.

### 4. POST_PROFITABILITY_PHASE

List of improvements deferred until after profitable evidence exists.

### 5. OPERATIONAL EVIDENCE

| Metric                 | Value |
| ---------------------- | ----- |
| traces_completed       |       |
| validated_edges        |       |
| executions             |       |
| positions_closed       |       |
| learning_records       |       |
| duplicate_execution    |       |
| determinism_violations |       |
| avg_latency            |       |
| avg_slippage           |       |

### 6. NEXT SINGLE ACTION

Return EXACTLY ONE next action. Not multiple. Not a roadmap. Not a redesign.
ONE action only — the highest-priority BLOCKER fix, or if no BLOCKERs exist,
the single action that moves the system to the next operational mode.

### 7. PRODUCTION DECISION

Return EXACTLY one:

```
NOT_READY
PIPELINE_PROOF_READY
SHADOW_READY
MICRO_CAPITAL_READY
LIMITED_PRODUCTION_READY
```

---

## Production Confidence Model

**Purpose:** Evaluate readiness probabilistically over time instead of emotionally
reacting to isolated bad runs or noisy logs.

**Dimensions:**

- pipeline_stability
- execution_reliability
- determinism_integrity
- capital_safety
- operational_consistency

**Scoring:** each dimension scored 0–100

**Interpretation:**

| Score  | Meaning                   |
| ------ | ------------------------- |
| 0–40   | unstable                  |
| 41–70  | operational but immature  |
| 71–85  | stable shadow-capable     |
| 86–100 | stable production-capable |

**Rules:**

- reviewer SHOULD evaluate trends across multiple runs
- isolated WARN spikes MUST NOT trigger redesign
- repeated deterministic failures MUST become BLOCKERs
- operational degradation trends SHOULD be prioritized over isolated incidents

**Purpose:** improve operational convergence and reduce emotional remediation cycles.

---

## Checklist

Before returning a production decision, verify:

- [ ] Operating in exactly one MODE
- [ ] Every finding classified as BLOCKER, SAFE_TO_IGNORE_FOR_NOW, or POST_PROFITABILITY_PHASE — no unclassified findings
- [ ] BLOCKERs include impact + location + exact fix
- [ ] No WARN log classified as BLOCKER unless it meets the BLOCKER criteria
- [ ] Termination condition (R6) checked — if met, returned SHADOW_READY and stopped
- [ ] NEXT SINGLE ACTION contains exactly one action
- [ ] PRODUCTION DECISION contains exactly one token from the allowed set
- [ ] No architecture redesign proposed unless a true BLOCKER requires it
- [ ] No profitability requirement imposed during SHADOW_TRADING mode
- [ ] Operational calibration issues not classified as BLOCKERs

---

## Examples

### Example: PIPELINE_PROOF review with dead learning worker

```
MODE: PIPELINE_PROOF

BLOCKERS:
1. Dead learning worker
   Impact: LearningRecord never emitted → L0→L10 lifecycle incomplete →
           PIPELINE_PROOF exit condition never met
   Location: internal/workers/learning_worker.go — goroutine not started in main.go
   Required fix: Register and start LearningWorker in app startup sequence

SAFE_TO_IGNORE_FOR_NOW:
- Low edge count (expected during PIPELINE_PROOF — only need 1 complete trace)
- WARN: rpc retry on block fetch (recovers, not stuck)

POST_PROFITABILITY_PHASE:
- Calibrate learning update thresholds per cohort

OPERATIONAL EVIDENCE:
traces_completed: 0
learning_records: 0
duplicate_execution: 0
determinism_violations: 0

NEXT SINGLE ACTION:
Register LearningWorker in internal/app/app.go startup sequence

PRODUCTION DECISION: NOT_READY
```

### Example: SHADOW_TRADING review with no BLOCKERs at 600 trades

```
MODE: SHADOW_TRADING

BLOCKERS: NONE

SAFE_TO_IGNORE_FOR_NOW:
- WARN: confidence below 0.5 on 12% of edges (normal cold-start)
- Low learning update frequency (insufficient samples, expected pre-500)
- RPC retry storms during peak block time (resolve without stuck state)

POST_PROFITABILITY_PHASE:
- Improve feature calibration for NEW_LAUNCH_EDGE scoring
- Add optional Telegram PnL breakdown command

OPERATIONAL EVIDENCE:
traces_completed: 623
validated_edges: 48
executions: 48
positions_closed: 47
learning_records: 44
duplicate_execution: 0
determinism_violations: 0
avg_latency: 340ms
avg_slippage: 1.2%

NEXT SINGLE ACTION:
Proceed to MICRO_CAPITAL mode with $5 max per trade

PRODUCTION DECISION: MICRO_CAPITAL_READY
```
