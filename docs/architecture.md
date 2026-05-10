# Architecture — Deterministic Event-Driven Microstructure Sniper System

> **Single source of truth.** This document is the only reference for system design, the engineering contract for implementation, and the operational blueprint for production.

**System Identity (Precise)**

> Deterministic, event-driven microstructure sniper system with controlled risk, testability, and adaptive learning.

**Core Invariant (Non-Negotiable)**

```
Profit = Edge × Probability × Execution × Capital × DataQuality × AdaptationQuality
```

Where:

- **DataQuality** = how well you avoid traps
- **AdaptationQuality** = how fast you correct mistakes

---

## Table of Contents

- [0. System Definition (Control System)](#0-system-definition-control-system)
- [1. Global Control Loop](#1-global-control-loop)
- [2. System Backbone](#2-system-backbone)
- [3. Layer-by-Layer Architecture](#3-layer-by-layer-architecture)
  - [3.0 Detection & Ingestion Engine (Layer 0)](#30-detection--ingestion-engine-layer-0)
  - [3.1 Data Quality Engine (Layer 1)](#31-data-quality-engine-layer-1)
  - [3.2 Feature Extraction (Layer 2)](#32-feature-extraction-layer-2)
  - [3.3 Signal & Edge Discovery (Layer 3)](#33-signal--edge-discovery-layer-3)
  - [3.4 Probability / Slippage / Latency Models (Layer 4)](#34-probability--slippage--latency-models-layer-4)
  - [3.5 Edge Validation (Layer 5)](#35-edge-validation-layer-5)
  - [3.6 Selection Engine (Layer 6)](#36-selection-engine-layer-6)
  - [3.7 Capital Engine (Layer 7)](#37-capital-engine-layer-7)
  - [3.8 Execution Engine (Layer 8)](#38-execution-engine-layer-8)
  - [3.9 Position Engine (Layer 9)](#39-position-engine-layer-9)
  - [3.10 Learning Engine (Layer 10)](#310-learning-engine-layer-10)
  - [3.11 Multi-Market Architecture (EVM + Solana)](#311-multi-market-architecture-evm--solana)
- [4. Meta Systems](#4-meta-systems)
- [5. Control & KPI System](#5-control--kpi-system)
- [6. System Guarantees](#6-system-guarantees)
- [7. Operational Modes](#7-operational-modes)
- [8. Final Characteristics](#8-final-characteristics)

---

## End-to-End Pipeline (Sequence Diagram)

```
Chain Event                                                                              Adjustment
    │                                                                                         ▲
    ▼                                                                                         │
[INGEST]→[DETECT]→[FILTER/DQ]→[FEATURES]→[EDGE]→[P/S/L MODELS]→[VALIDATE]→[SELECT]→[CAPITAL]→[EXECUTE]→[POSITION/EXIT]→[EVALUATE]→[LEARN]
    │                  │           │          │         │             │          │         │          │            │          │
    ▼                  ▼           ▼          ▼         ▼             ▼          ▼         ▼          ▼            ▼          ▼
MarketDataDTO    DataQualityDTO FeatureDTO  EdgeDTO  Prob/Slip/Lat ValidatedEdge Selection Allocation Execution Position  Learning
                                                                    DTO         DTO       DTO        Result     DTO       Record
                                                                                                       ↓                     │
                                                                                           Telegram Event Bus ◄──────────────┤
                                                                                                                             │
                                                              Config/Threshold/Weight Updates (versioned, bounded) ◄─────────┘
```

> **Stage legend:** `INGEST` = Layer 0 (raw blockchain event ingestion → `MarketDataDTO`). `DETECT` = Layer 3 (Signal & Edge Discovery — identifying trading edges from normalized features).

Every stage consumes a DTO, produces a DTO, emits a DecisionLog, and contributes to error signals. The loop runs continuously in micro-batches (2–5s windows).

---

# 0. System Definition (Control System)

**Deterministic, event-driven crypto sniper with data-driven adaptive strictness.**

Extended invariant:

```
Profit = Edge × Probability × Execution × Capital × DataQuality × AdaptationQuality
```

Where:

- **DataQuality** = how well you avoid traps
- **AdaptationQuality** = how fast you correct mistakes

## 0.1 Framing

You are not building a trading bot. You are building a **closed-loop control system under uncertainty**, where:

- inputs are noisy (on-chain chaos)
- environment is adversarial (rug, wash, manipulation)
- decisions are irreversible (real capital)
- feedback is delayed (PnL realized later)

---

## 0.2 Mathematical Interpretation of the Invariant

```
Profit = Edge × Probability × Execution × Capital × DataQuality × AdaptationQuality
```

Each term must be **measurable, observable, and controllable**.

### 0.2.1 Edge (E)

**Definition:** Expected raw opportunity before execution.

```
Edge ≈ (expected price movement within time window)
```

Example:

- token pumps +40% in 10 minutes
- you enter early → edge exists

**Key Property:**

- Edge is **external (market-driven)**
- You don't control it
- You only **detect it**

### 0.2.2 Probability (P)

**Definition:** Likelihood that detected edge actually materializes.

```
P = P(success | features, context)
```

**Important:**

- Edge without probability = noise
- Probability converts opportunity → expectation

### 0.2.3 Execution (X)

**Definition:** How much of the edge you actually capture.

```
Execution = realized_entry / optimal_entry
```

Includes:

- latency
- slippage
- failed tx

**Reality:**

```
perfect edge + bad execution = zero profit
```

### 0.2.4 Capital (C)

**Definition:** How much you allocate to each opportunity.

```
Profit ∝ capital × realized_return
```

**Constraint:**

```
over-allocation  → blow up
under-allocation → wasted edge
```

### 0.2.5 DataQuality (DQ) — CRITICAL MULTIPLIER

**Definition:** Your ability to **avoid fake edges**.

```
DQ = 1 - P(trap)
```

Where trap ∈ {rug, honeypot, wash trading, fake liquidity}.

**Important Insight:**

```
If DQ = 0 → entire system collapses
```

Because `Edge × 0 = 0`.

**Measurable Form:**

```
DQ = 1 - (rug_losses / total_trades)
```

### 0.2.6 AdaptationQuality (AQ) — YOUR TRUE EDGE

**Definition:** How fast and accurately the system improves itself.

```
AQ = f(speed_of_learning, correctness_of_adjustment)
AQ = LearningSpeed × LearningAccuracy
```

Where:

- **LearningSpeed** = `time_to_detect_mistake` (lower is better)
- **LearningAccuracy** = `correct_adjustment / wrong_adjustment`

**Example:**

```
Bad system:  makes mistake → repeats 100 times         → AQ ≈ 0
Good system: makes mistake → adjusts in 3 trades       → AQ high
```

---

## 0.3 Control System View (Engineering Perspective)

You are implementing a **CONTROL SYSTEM WITH FEEDBACK LOOP**.

### 0.3.1 State Variables

At time `t`:

```go
type SystemState struct {
    PassRate          float64
    WinRate           float64
    LossRate          float64
    FalsePositiveRate float64
    FalseNegativeRate float64
    AvgLatency        float64
    SlippageError     float64
}
```

### 0.3.2 Control Inputs (What system can change)

- thresholds (liquidity, tax, score)
- scoring weights
- capital allocation
- execution parameters

### 0.3.3 Control Outputs

- trade decisions
- allocation decisions
- execution actions

---

## 0.4 Feedback Loop (Formalized)

```
Detect → Filter → Score → Select → Execute → Exit → Evaluate → Adjust
```

| Step     | Role                                                     |
| -------- | -------------------------------------------------------- |
| Detect   | Input: raw market events. Output: candidate tokens.      |
| Filter   | Removes `fake_edge → noise` (DataQuality).               |
| Score    | Ranks `good_edge vs mediocre_edge`.                      |
| Select   | Applies constraint: limited capital → best subset.       |
| Execute  | Transforms decision → real position.                     |
| Exit     | Transforms position → realized outcome.                  |
| Evaluate | Computes `expected vs actual` — **most important**.      |
| Adjust   | Updates system parameters — only place behavior changes. |

---

## 0.5 Stability vs Responsiveness Tradeoff

This is where most systems fail.

### 0.5.1 Too Strict (High Stability)

```
low pass rate → low trades → low data
Result: AQ → low (no learning)
```

### 0.5.2 Too Flexible (High Responsiveness)

```
high pass rate → many trades → high noise
Result: DQ ↓ → losses ↑
```

### 0.5.3 Target Operating Zone

```
Pass Rate: 0.5% – 5%
```

---

## 0.6 Adaptive Strictness (Core Mechanism)

```
STRICT → BALANCED → EXPLORATION → VERY_EXPLORATION
```

### 0.6.1 Formal Controller

```
if pass_rate == 0:       relax_thresholds()
if loss_rate ↑:          tighten_thresholds()
if false_negative ↑:     relax_filters()
if false_positive ↑:     tighten_filters()
```

### 0.6.2 Control Variables

- `min_liquidity`
- `max_tax`
- `min_score`
- probability threshold

### 0.6.3 Constraint (IMPORTANT)

Adjustments must be:

```
bounded + gradual + versioned
```

Otherwise:

- system oscillates
- unstable behavior

---

## 0.7 Error Signals (Core Inputs for Adaptation)

The system learns from **error signals**, not raw data.

### 0.7.1 Prediction Error

```
error = predicted_outcome - actual_outcome
```

### 0.7.2 Classification Errors

- **False Positive**: `accepted → loss`
- **False Negative**: `rejected → pump`

### 0.7.3 Execution Error

```
expected_slippage - actual_slippage
expected_latency  - actual_latency
```

---

## 0.8 Control Objectives

**Primary:** maximize expected PnL.

**Secondary:** minimize catastrophic loss (rug).

**Constraints:**

- capital preservation
- bounded risk
- stable operation

---

## 0.9 System Failure Modes (At This Level)

| #   | Mode         | Description                       |
| --- | ------------ | --------------------------------- |
| 1   | DQ collapse  | many rugs pass → system dies      |
| 2   | AQ collapse  | system doesn't learn → stagnation |
| 3   | Overfitting  | too strict → no trades            |
| 4   | Underfitting | too loose → noise trading         |

---

## 0.10 What Makes This System Actually Work

Not speed. Not AI. Not code.

**It works if:**

```
DQ high (avoid traps)  +  AQ high (learn fast)
```

**Final Insight (Most Important).** Most traders optimize:

```
Edge × Execution
```

You are optimizing:

```
(DataQuality × AdaptationQuality)
```

Which means: you don't need to be perfect — you need to **improve faster than you lose**.

---

## 0.11 Implementation Filter

Everything you build later must answer:

1. Does this improve DataQuality?
2. Does this improve AdaptationQuality?

If answer = no → don't build.

---

# 1. Global Control Loop

```
Ingest → Detect → Filter → Score → Select → Execute → Exit → Evaluate → Adjust
```

> **Stage Clarification:**
>
> - `Ingest` = Layer 0 (Detection & Ingestion Engine) — raw blockchain event ingestion, zero business logic, emits `MarketDataDTO`
> - `Detect` = Layer 3 (Signal & Edge Discovery) — identifying trading edges from normalized feature vectors

**Adjustment outputs:**

- thresholds (strict ↔ relaxed)
- scoring weights
- capital allocation
- execution params

This loop is your **only source of edge**. If it's imprecise, everything downstream (models, infra, speed) becomes irrelevant. It is a **deterministic control loop with measurable state transitions and explicit update rules** — no ambiguity.

Each stage:

- consumes a DTO
- produces a DTO
- emits a DecisionLog
- contributes to error signals

The loop runs continuously in **micro-batches (2–5s windows)** to balance latency vs decision quality.

---

## 1.1 Stage Definitions (Precise)

### 1.1.1 DETECT (Signal Ingestion)

**Objective.** Capture candidate opportunities early enough to preserve edge.

**Input.**

- on-chain events (pair creation, liquidity add)
- wallet activity (top traders)
- surge signals

**Output.**

```go
type DetectOutput struct {
    TokenAddress string
    Timestamp    int64
    Source       string // pool | wallet | surge
}
```

**Key Metric.**

```
DetectionLatency = event_time → detect_time
Constraint: DetectionLatency < opportunity_half_life
```

**Failure Mode.** Late detection → no edge left → wasted pipeline.

---

### 1.1.2 FILTER (Data Quality Gate)

**Objective.** Remove invalid opportunities (traps).

**Decision.** `PASS / REJECT / RISKY_PASS`.

**Output.**

```go
type FilterOutput struct {
    TokenAddress string
    RiskScore    float64
    Decision     string
}
```

**Key Metric.**

```
DQ = 1 - (rug_loss_rate)
```

**Failure Modes.**

- false positive → loss
- false negative → missed alpha

---

### 1.1.3 SCORE (Ranking Function)

**Objective.** Estimate relative quality of opportunities.

**Output.**

```go
type ScoreOutput struct {
    TokenAddress string
    Score        float64
    Confidence   float64
}
```

**Key Metric.**

```
RankingCorrelation = corr(score_rank, realized_pnl_rank)
```

**Failure Mode.** High score ≠ high return → useless scoring.

---

### 1.1.4 SELECT (Resource Constraint Solver)

**Objective.** Choose subset of opportunities under constraints.

**Input.** Scored tokens.

**Constraints.**

- max positions (K)
- capital limit
- diversification

**Output.**

```go
type SelectionOutput struct {
    Selected []Token
}
```

**Algorithm (deterministic).**

```
sort by (score × probability × confidence)
take top K
apply constraints
```

**Key Metric.**

```
SelectionEfficiency = PnL(top K) / PnL(all candidates)
```

**Failure Mode.**

- selecting too many → dilution
- selecting too few → missed opportunity

---

### 1.1.5 EXECUTE (Action Realization)

**Objective.** Convert decision → position with minimal degradation.

**Output.**

```go
type ExecutionOutput struct {
    TokenAddress string
    EntryPrice   float64
    LatencyMs    int
    Slippage     float64
    Success      bool
}
```

**Key Metrics.**

```
ExecutionQuality = actual_entry / optimal_entry
Latency
SlippageError
```

**Failure Modes.**

- tx fail
- high slippage
- late inclusion

---

### 1.1.6 EXIT (Outcome Realization)

**Objective.** Convert position → realized PnL.

**Output.**

```go
type ExitOutput struct {
    TokenAddress string
    ExitPrice    float64
    PnL          float64
    DurationSec  int
    ExitReason   string
}
```

**Key Metric.**

```
ExitEfficiency = realized_pnl / max_possible_pnl
```

**Failure Modes.**

- exit too early → lost upside
- exit too late → give back profit

---

### 1.1.7 EVALUATE (Error Computation Engine)

This is where intelligence starts.

**Core Computations.**

**A. Prediction Error**

```
E_pred = expected_return - actual_return
```

**B. Classification Errors**

```
FalsePositive = accepted && loss
FalseNegative = rejected && would_win
```

**C. Execution Error**

```
E_exec    = expected_slippage - actual_slippage
E_latency = expected_latency  - actual_latency
```

**D. Opportunity Loss**

```
MissedPnL = PnL(rejected tokens that pumped)
```

**Output.**

```go
type EvaluationOutput struct {
    FalsePositive   bool
    FalseNegative   bool
    PredictionError float64
    ExecutionError  float64
}
```

---

### 1.1.8 ADJUST (Control Layer)

This is the **only place the system changes behavior**.

---

## 1.2 Adjustment Engine (Core Logic)

### 1.2.1 Inputs

Aggregated over window:

```go
type Metrics struct {
    PassRate          float64
    WinRate           float64
    LossRate          float64
    FalsePositiveRate float64
    FalseNegativeRate float64
    AvgSlippage       float64
    AvgLatency        float64
}
```

### 1.2.2 Adjustment Outputs — Exactly 4 Knobs

1. thresholds
2. scoring weights
3. capital allocation
4. execution params

---

## 1.3 Adjustment Rules (Deterministic)

### 1.3.1 Threshold Controller (STRICT ↔ RELAX)

```
if PassRate == 0:           relax_thresholds(step_small)
if FalseNegativeRate ↑:     relax_filters()
if LossRate ↑:              tighten_filters()
if FalsePositiveRate ↑:     tighten_filters()
```

**Example:**

```
min_liquidity: 10k → 8k
max_tax:      10% → 8%
```

### 1.3.2 Scoring Weight Update

Goal: increase weight of predictive features; decrease noisy features.

**Method (simple + stable):**

```
new_weight = old_weight + α × correlation(feature, pnl)
```

Constraints:

- bounded update
- normalize weights

### 1.3.3 Capital Allocation Update

```
if cohort_win_rate  ↑ : increase allocation
if cohort_loss_rate ↑ : decrease allocation
```

Cohort example: liquidity 10k–20k tokens, tax 5–10%.

### 1.3.4 Execution Parameter Update

```
if latency ↑:         increase gas/priority_fee
if slippage ↑:        reduce position size
if failure_rate ↑:    reduce parallelism
```

---

## 1.4 Time Structure (Important)

**Real-time loop (fast):**

```
Detect → Execute
```

**Batch evaluation loop (slow):**

```
Evaluate → Adjust
```

**Frequency:** every 5–30 minutes OR N trades.

---

## 1.5 Stability Constraints (Non-Negotiable)

- **1.5.1 Bounded Updates:** `Δparameter ≤ small_step`
- **1.5.2 Minimum Sample Size:** if `trades < N` → no adjustment
- **1.5.3 Versioning:** each adjustment → new `version_id`
- **1.5.4 No Multi-parameter Shock:** only adjust few parameters at once

---

## 1.6 Control Objective (Formal)

```
Maximize: Expected PnL

Subject to:
  LossRate  < threshold
  Drawdown  < threshold
  System stable
```

---

## 1.7 System Behavior (What You Should See)

### Healthy System

```
PassRate:     1–3%
WinRate:      improving
LossRate:     controlled
FalseNegative: decreasing
FalsePositive: decreasing
```

### Broken System

| Case | Symptom                                        |
| ---- | ---------------------------------------------- |
| 1    | **Overfitting** — PassRate 0%, no trades       |
| 2    | **Underfitting** — PassRate 20%, LossRate high |
| 3    | **No Learning** — metrics static, PnL stagnant |

---

## 1.8 Final Insight

This loop is essentially:

```
ONLINE LEARNING + CONTROL SYSTEM
```

Where:

- Detect/Filter/Score = **model inference**
- Evaluate = **loss function**
- Adjust = **gradient step (controlled)**
- Execute = **real-world deployment**

**What matters most.** Not more features, not more signals — but **quality of the Evaluate + Adjust loop**.

---

# 2. System Backbone

**Enforced invariants:**

- DTO-only communication
- event bus (append-only)
- worker-based execution (`SELECT … FOR UPDATE SKIP LOCKED`)
- per-market isolation (crypto module only here)
- Telegram via event bus (no direct calls)

This backbone is the **mechanical foundation** that guarantees determinism, scalability, and safe learning. If this is wrong, your control loop becomes non-reproducible and your learning becomes garbage.

---

## 2.1 DTO-Only Communication

### 2.1.1 Definition

All modules communicate via **immutable, versioned data contracts** (DTOs). No shared memory. No direct function calls across modules.

### 2.1.2 Why This Matters

Without DTO isolation:

- hidden coupling → unpredictable behavior
- impossible to replay system
- learning becomes invalid (non-reproducible)

### 2.1.3 DTO Design Rules (STRICT)

**1. Immutable**

```go
type EdgeDTO struct {
    TokenAddress string
    Score        float64
    Timestamp    int64
}
```

- once created → NEVER mutated
- updates = new DTO

**2. Self-contained**

Bad:

```go
type EdgeDTO struct {
    TokenAddress string
}
// requires external lookup → NOT allowed
```

Good:

```go
type EdgeDTO struct {
    TokenAddress string
    Liquidity    float64
    Tax          float64
    Score        float64
}
```

**3. Versioned**

```go
type EdgeDTOv2 struct {
    TokenAddress string
    Score        float64
    Confidence   float64
    Version      int
}
```

**4. Typed per stage**

Each stage has its own DTO:

```
DetectDTO → FilterDTO → ScoreDTO → SelectionDTO → ExecutionDTO
```

### 2.1.4 DTO Flow (Deterministic)

```
Event → DTO → Stored → Consumed → New DTO → Stored
```

### 2.1.5 Key Property

```
Same input DTO → same output DTO
```

This is what enables **replay + learning correctness**.

---

## 2.2 Event Bus (Append-Only)

### 2.2.1 Definition

Central system component:

```
ALL state transitions = events written to storage
```

No overwrites. No updates. Only inserts.

### 2.2.2 Implementation (Postgres)

Single table (simplified):

```sql
events (
  id         BIGSERIAL PRIMARY KEY,
  event_type TEXT,
  payload    JSONB,
  created_at TIMESTAMP,
  processed  BOOLEAN DEFAULT FALSE
)
```

### 2.2.3 Event Types

```
market_data_event
data_quality_event
feature_event
edge_event
selection_event
execution_event
position_event
evaluation_event
adjustment_event
telegram_event
```

### 2.2.4 Flow

```
Producer → INSERT event
Worker   → SELECT unprocessed event
Worker   → process → INSERT next event
```

### 2.2.5 Why Append-Only?

1. **Full audit trail** — reconstruct EVERYTHING (`why did we buy this token?`).
2. **Replay capability** — critical for learning (re-run system with new parameters).
3. **Determinism** — no state mutation = no hidden bugs.

### 2.2.6 Anti-patterns (DO NOT DO)

- updating rows
- deleting events
- mixing state + events

---

## 2.3 Worker-Based Execution (SKIP LOCKED)

### 2.3.1 Problem

Multiple workers processing the same events → race conditions.

### 2.3.2 Solution

```sql
SELECT * FROM events
WHERE processed = FALSE
FOR UPDATE SKIP LOCKED
LIMIT 1;
```

### 2.3.3 How It Works

- worker A locks row
- worker B skips locked row
- no duplicate processing

### 2.3.4 Worker Model

Each stage = independent worker group:

```
DetectWorker
FilterWorker
ScoreWorker
ExecutionWorker
...
```

### 2.3.5 Worker Loop (Go-style)

```go
for {
    event := fetchUnprocessedEvent()
    if event == nil {
        sleep()
        continue
    }
    result := process(event)
    writeNewEvent(result)
    markProcessed(event)
}
```

### 2.3.6 Scaling

You can scale horizontally: `1 → 10 → 100 workers`. No logic changes required.

### 2.3.7 Backpressure Handling

If queue grows → slow downstream → backlog increases.

Solutions:

- increase workers
- drop low-priority events
- throttle upstream

### 2.3.8 Determinism Guarantee

Even with concurrency: **each event processed exactly once**.

---

## 2.4 Per-Market Isolation

### 2.4.1 Definition

Each market = isolated module.

```
modules/
  crypto_dex/
  crypto_cex/
  stocks/
```

### 2.4.2 Why This Matters

Different markets have different data structures, latency, and strategies.

### 2.4.3 Isolation Rules

**1. No cross-module calls**

Bad: `dex module calling cex logic`.

**2. Only shared layer = core**

```
core/
  event_bus
  dto
  learning
  execution_interface
```

**3. Separate configs**

```
crypto_dex:  min_liquidity: 10k
crypto_cex:  min_volume:    1M
```

### 2.4.4 Benefit

- evolve DEX without breaking CEX
- run experiments independently
- isolate failures

### 2.4.5 Important Extension

Also isolate **strategy variants**, e.g. `dex_sniper_v1`, `dex_sniper_v2`.

---

## 2.5 Telegram via Event Bus (No Direct Calls)

### 2.5.1 Definition

Telegram is NOT a controller. It is an **event producer + event consumer**.

### 2.5.2 Flow

**A. System → Telegram**

```
system emits → telegram_event
telegram worker → sends message
```

**B. Telegram → System**

```
user command → telegram_command_event
worker processes → emits system event
```

### 2.5.3 Example

User sends: `/stop`

```
Telegram API
→ create telegram_command_event
→ event bus
→ control worker
→ emits system_control_event
→ execution engine stops
```

### 2.5.4 Why This Matters

1. **No tight coupling** — Telegram failure ≠ system failure
2. **Full audit trail** — who stopped system?
3. **Replayable control** — simulate "what if /stop wasn't sent?"

### 2.5.5 Command DTO

```go
type TelegramCommandDTO struct {
    Command string
    Params  map[string]string
    UserID  string
}
```

### 2.5.6 Alert DTO

```go
type TelegramAlertDTO struct {
    Type     string
    Message  string
    Severity string
}
```

### 2.5.7 Guardrails

- rate limit messages
- queue messages (don't block system)
- retry on failure

---

## 2.6 How All Parts Connect (Full Flow)

> **Ingestion reliability guarantee:** Layer 0 (Ingestion Worker) is the sole entry point for raw blockchain events. It maintains `last_processed_block` per chain, performs gap recovery on reconnect (§ 3.0.6), and guarantees that every confirmed on-chain event matching a configured topic reaches the event bus exactly once before any downstream worker sees it.

```
Ingestion Worker (Layer 0)         ← WebSocket eth_subscribe / HTTP eth_getLogs fallback
   ↓                                  gap recovery on reconnect, heartbeat monitoring,
   ↓                                  exponential backoff RPC failover
INSERT market_data_event (MarketDataDTO)
   ↓
Filter Worker
   ↓
INSERT data_quality_event
   ↓
Score Worker
   ↓
INSERT edge_event
   ↓
Selection Worker
   ↓
INSERT selection_event
   ↓
Execution Worker
   ↓
INSERT execution_event
   ↓
Position Worker
   ↓
INSERT position_event
   ↓
Evaluation Worker
   ↓
INSERT evaluation_event
   ↓
Learning Worker
   ↓
INSERT adjustment_event
```

---

## 2.7 Non-Negotiable Properties

| #   | Property        | Guarantee                                   |
| --- | --------------- | ------------------------------------------- |
| 1   | Reproducibility | same event stream → same result             |
| 2   | Observability   | everything logged + queryable               |
| 3   | Scalability     | workers scale independently                 |
| 4   | Fault Tolerance | worker crash ≠ system crash; events persist |

---

## 2.8 What Will Break If You Violate This

| Violation                | Consequence                         |
| ------------------------ | ----------------------------------- |
| No DTO isolation         | hidden coupling → invalid learning  |
| No append-only           | no replay → no debugging            |
| No SKIP LOCKED           | duplicate trades → capital loss     |
| Telegram bypasses system | inconsistent state → unreproducible |

**Final Insight.** This backbone turns the system into an **event-sourced, deterministic trading machine** — not a script, bot, or random async system.

---

# 3. Layer-by-Layer Architecture

The system is composed of 11 layers (Layer 0 through Layer 10) executing in strict sequence. Each layer:

- consumes typed DTOs from the previous stage
- produces typed DTOs for the next stage
- contributes error signals to Layer 10 (Learning)
- adapts its parameters via bounded, versioned updates

Layer 0 is the ingestion boundary — it produces `MarketDataDTO` from raw blockchain events and has zero business logic. Layers 1–10 execute all business logic on the normalized DTO stream.

---

## 3.0 Detection & Ingestion Engine (Layer 0)

### 3.0.1 Purpose

Layer 0 is the **raw event ingestion boundary** between the blockchain and the system. It has exactly one responsibility:

> Convert on-chain events into normalized `MarketDataDTO` records and write them to the event bus.

This layer:

- performs **zero filtering** — all matching events pass through
- performs **zero scoring** — no ranking or evaluation
- performs **zero mutation** — event data is normalized to schema, not interpreted
- is the **sole source of raw market data** for all downstream layers (1–10)

If this layer fails, the entire pipeline starves. No downstream layer may bypass Layer 0 or read raw blockchain events directly.

---

### 3.0.2 Event Sources (Explicit)

Per-chain configuration defines factory contracts and supported event types. All addresses and endpoints **MUST** be loaded from `config/` YAML — never hardcoded.

**Example configuration (`config/chains.yaml`):**

```yaml
chains:
  ethereum:
    factory_v2: "0x5C69bEe701ef814a2B6a3EDD4B1652CB9cc5aA6f" # Uniswap V2 Factory
    factory_v3: "0x1F98431c8aD98523631AE4a59f267346ea31F984" # Uniswap V3 Factory
    rpc_ws: "wss://mainnet.infura.io/ws/v3/<key>"
    rpc_http: "https://mainnet.infura.io/v3/<key>"
    block_time_ms: 12000
    confirmation_depth: 3
  bsc:
    factory_v2: "0xcA143Ce32Fe78f1f7019d7d551a6402fC5350c73" # PancakeSwap V2 Factory
    rpc_ws: "wss://bsc-ws-node.nariox.org"
    rpc_http: "https://bsc-dataseed1.binance.org"
    block_time_ms: 3000
    confirmation_depth: 15
```

**Supported event types per factory:**

| Event       | Trigger                   | Source Filter            |
| ----------- | ------------------------- | ------------------------ |
| PairCreated | New trading pair listed   | Factory contract address |
| Mint        | Liquidity added to a pool | Pair contract address    |
| Swap        | Trade executed in a pool  | Pair contract address    |

Chain-specific differences are handled at normalization (§ 3.0.8) — downstream layers see a unified `MarketDataDTO` regardless of chain.

---

### 3.0.3 Log Topics (Deterministic)

All event subscriptions use the **keccak256 hash of the full event signature** as the primary topic filter. This is deterministic — the same function signature always produces the same topic hash.

```
PairCreated(address,address,address,uint256)
  → topic0: 0x0d3648bd0f6ba80134a33ba9275ac585d9d315f0ad8355cddefde31afa28d0e9

Mint(address,uint256,uint256)
  → topic0: 0x4c209b5fc8ad50758f13e2e1088ba56a560dff690a1c6fef26394f4c03821c4f

Swap(address,uint256,uint256,uint256,uint256,address)
  → topic0: 0xd78ad95fa46c994b6551d0da85fc275fe613ce37657fb8d5e3d130840159d822
```

**Topic filter rules:**

1. `topic0` MUST match the event signature hash exactly — no wildcards on `topic0`
2. Contract address filtering is applied per-chain at the subscription level
3. Topic hashes MUST be stored in `config/` — never recomputed at runtime
4. Any log not matching a configured topic is silently dropped at the RPC layer

---

### 3.0.4 Subscription Model

**Primary: WebSocket (`eth_subscribe logs`)**

```json
{
  "jsonrpc": "2.0",
  "method": "eth_subscribe",
  "params": [
    "logs",
    {
      "address": ["<factory_v2>", "<factory_v3>"],
      "topics": [["<topic_PairCreated>", "<topic_Mint>", "<topic_Swap>"]]
    }
  ]
}
```

WebSocket is preferred: push-based delivery with sub-100ms latency when the RPC node is co-located.

**Fallback: HTTP Polling (`eth_getLogs`)**

```json
{
  "jsonrpc": "2.0",
  "method": "eth_getLogs",
  "params": [
    {
      "fromBlock": "0x<last_processed_block + 1>",
      "toBlock": "latest",
      "address": ["<factory_v2>"],
      "topics": [["<topic_PairCreated>", "<topic_Mint>", "<topic_Swap>"]]
    }
  ]
}
```

**Fallback trigger conditions:**

- WebSocket connection lost and not recovered within `ws_reconnect_timeout` (config, default: 5s)
- WebSocket receives no heartbeat response within `ws_heartbeat_timeout` (config, default: 30s)
- Three consecutive WebSocket errors on subscribe or send

**Reconciliation rule:** When WebSocket is restored after a polling period, the gap is filled via `eth_getLogs` (§ 3.0.6). Polling halts only after gap recovery is confirmed complete. No events are processed twice (§ 3.0.7 deduplication).

---

### 3.0.5 Reconnect & Resilience

**Heartbeat monitoring:**

```go
// Sent every heartbeat_interval (config, default: 15s)
ws.WriteMessage(websocket.PingMessage, nil)
// Expect Pong within heartbeat_timeout (config, default: 5s)
// If no Pong → trigger reconnect
```

**Reconnect policy (exponential backoff):**

```
attempt 1:  wait 1s
attempt 2:  wait 2s
attempt 3:  wait 4s
attempt N:  wait min(2^(N-1), max_backoff) seconds
max_backoff: 60s   (config)
max_attempts: unlimited (system must reconnect eventually)
```

Exponential backoff prevents thundering herd on RPC node restarts.

**RPC failover:**

```yaml
rpc_endpoints:
  ethereum:
    - "wss://mainnet.infura.io/ws/v3/<key1>" # primary
    - "wss://mainnet.infura.io/ws/v3/<key2>" # fallback
    - "wss://eth-mainnet.alchemyapi.io/v2/<key>" # fallback
```

Failover rules:

1. On reconnect failure after `failover_threshold` consecutive attempts (config, default: 3), switch to next endpoint
2. Cycle through all endpoints before increasing backoff interval
3. On successful reconnect: log endpoint used, reset backoff counter
4. All endpoint URLs stored in config — never hardcoded

---

### 3.0.6 Event Gap Recovery

**Problem:** Any disconnect creates a window of missed blocks. These events are permanently absent from the WebSocket stream and MUST be recovered deterministically.

**Mechanism — `last_processed_block` tracking:**

```sql
-- Dedicated tracking record per chain, updated atomically
-- after each block's events are fully written to the event bus
UPDATE ingestion_state
SET last_processed_block = $1
WHERE chain = $2;
```

**On reconnect (WebSocket or fallback transition):**

```
gap_start = last_processed_block + 1
gap_end   = current_block_number

if gap_end >= gap_start:
    logs = eth_getLogs(fromBlock=gap_start, toBlock=gap_end, ...)
    for each log in logs:
        emit MarketDataDTO  // deduplication via ON CONFLICT (§ 3.0.7)
    update last_processed_block = gap_end
```

**Guarantee:** No event loss — any block range gap is recovered exactly once before the live stream resumes.

**Chain reorg handling:** Gap recovery applies a `confirmation_depth` (per-chain config) before treating events as confirmed. On reorg detection:

1. Mark events from reorganized blocks as `reorged = TRUE` in the event bus
2. Do NOT delete — append-only log preserved
3. Downstream layers MUST check `reorged` flag before processing
4. Emit `reorg_event` to event bus for monitoring

---

### 3.0.7 Ordering & Idempotency

**Event ordering rule:**

All `MarketDataDTO` records written to the event bus are ordered by:

```
(BlockNumber ASC, LogIndex ASC)
```

No event may be written with a lower `(BlockNumber, LogIndex)` than the previously written event for the same chain. This enforces a total order on the event stream.

**Deduplication rule:**

Each on-chain log has a globally unique, content-addressable identifier:

```
event_id = SHA256(chain + tx_hash + log_index)[:16]
```

Before inserting:

```sql
INSERT INTO events (event_id, event_type, payload, created_at)
VALUES ($1, 'market_data_event', $2, CURRENT_TIMESTAMP)
ON CONFLICT (event_id) DO NOTHING;
```

**Idempotency guarantee:** The same on-chain log delivered twice (e.g., from both WebSocket and gap recovery) produces exactly one `MarketDataDTO` in the event bus.

---

### 3.0.8 Multi-Chain Normalization

All chains emit structurally different log formats. Layer 0 normalizes them into a single unified DTO before writing to the event bus. This is the canonical output type of Layer 0:

```go
// MarketDataDTO — canonical output of Layer 0
// All fields required. No optional fields.
type MarketDataDTO struct {
    EventID      string // SHA256(chain + tx_hash + log_index)[:16]
    Chain        string // "ethereum" | "bsc" | per config — lowercase
    TokenAddress string // new token address (non-WETH/WBNB side from PairCreated)
    PairAddress  string // pair/pool contract address
    EventType    string // "PairCreated" | "Mint" | "Swap"
    BlockNumber  int64  // block in which the log appeared
    LogIndex     int64  // position within the block (for total ordering)
    Timestamp    int64  // Unix milliseconds, from block header
    Version      int    // DTO schema version constant
}
```

**Normalization rules:**

- `Chain`: lowercase identifier, defined in config — never inferred from RPC response
- `TokenAddress`: for `PairCreated` = non-WETH/WBNB side; for Mint/Swap = token0 or token1 per config mapping
- `PairAddress`: always the pair contract address
- `EventType`: exact string from config topic map — never derived from raw log data at runtime
- `BlockNumber`: raw from log, never estimated
- `Timestamp`: from block header (one `eth_getBlockByNumber` call per unseen block, cached)
- `Version`: set at build time from DTO schema version constant

**Chain-specific differences handled here:**

| Chain    | Difference                       | Handling                           |
| -------- | -------------------------------- | ---------------------------------- |
| Ethereum | 12s blocks, 3-block confirmation | `confirmation_depth: 3` in config  |
| BSC      | 3s blocks, 15-block confirmation | `confirmation_depth: 15` in config |
| All      | Different factory addresses      | Per-chain factory list in config   |

---

### 3.0.9 Latency Constraints

```
DetectionLatency = block_timestamp → MarketDataDTO.written_to_event_bus

Target:
  Ethereum: ≤ 300 ms  (12s block time, generous budget)
  BSC:      ≤ 100 ms  (3s block time, tighter budget)
```

**Measurement method:**

```
latency_ms = wall_clock_at_write_ms - MarketDataDTO.Timestamp
```

Tracked per-chain as `ingestion_latency_p50`, `ingestion_latency_p95`, `ingestion_latency_p99`.

**Impact on edge decay:**

The edge window for new pair events is 5–15 minutes (§ 3.3.8). A 300ms detection latency consumes less than 0.1% of the edge window — acceptable. Latency > 2s begins meaningfully degrading edge quality. Latency > 30s likely produces zero edge for new pool events.

Sustained `p99 > 500ms` triggers a `[WARN]` alert (§ 4.4 Observability).

---

### 3.0.10 Throughput Model

**Expected events per second (steady state):**

| Chain    | Event Type  | Estimated Rate |
| -------- | ----------- | -------------- |
| Ethereum | Swap        | 10–100 / s     |
| Ethereum | PairCreated | < 1 / s        |
| BSC      | Swap        | 50–500 / s     |
| BSC      | PairCreated | 2–10 / s       |

**Backpressure handling:**

Layer 0 writes to the Postgres event bus at full speed. If event bus write throughput falls below ingestion rate:

1. Layer 0 accumulates an in-process buffer (bounded: `ingest_buffer_max`, config, default: 10,000 events)
2. If buffer reaches `ingest_buffer_warn` (config, default: 8,000), emit `[WARN] ingestion backpressure` alert
3. If buffer reaches `ingest_buffer_max`, Layer 0 applies selective dropping:
   - Drop `Swap` events first (lowest per-event value for new pair detection)
   - Never drop `PairCreated` events (highest value — new opportunities)
   - Never drop `Mint` events on pairs aged < 60s (critical for rug detection)

All limits are config-driven. Hardcoded queue limits are forbidden.

---

### 3.0.11 Failure Modes & Mitigations

| Failure Mode           | Description                                             | Mitigation                                                            |
| ---------------------- | ------------------------------------------------------- | --------------------------------------------------------------------- |
| RPC lag                | Node is behind by > `rpc_lag_threshold` blocks (config) | Switch to next RPC endpoint; emit `[WARN]` alert                      |
| Dropped subscription   | WebSocket silently stops delivering events              | Heartbeat detection (§ 3.0.5); auto-reconnect; gap recovery (§ 3.0.6) |
| Event flood            | Sudden burst (e.g., bot attack, mempool explosion)      | In-process buffer with selective dropping (§ 3.0.10)                  |
| Chain reorg            | Block reorganization invalidates recently seen events   | Confirmation depth per chain; reorg scan on reconnect (§ 3.0.6)       |
| Block timestamp drift  | Node returns incorrect block timestamps                 | Validate within ± `timestamp_drift_max` (config) of wall clock        |
| Duplicate delivery     | Same log delivered by both WebSocket and gap recovery   | `event_id` deduplication via `ON CONFLICT DO NOTHING` (§ 3.0.7)       |
| Database write failure | Postgres unavailable → events would be lost             | In-process buffer absorbs short outages; circuit breaker on DB writes |
| All endpoints down     | Every configured RPC endpoint fails simultaneously      | Alert immediately; halt ingestion cleanly; resume on recovery         |

---

### 3.0.12 Output Contract

Layer 0 has exactly one output: `MarketDataDTO`.

**Rules (non-negotiable):**

1. **No filtering** — every event matching a configured topic is emitted as `MarketDataDTO`
2. **No scoring** — no fields representing quality, rank, or probability
3. **No mutation** — event data is normalized to schema, not interpreted
4. **No business logic** — Layer 0 does not know what tokens are "good" or "bad"
5. **Immutable** — once written to the event bus, a `MarketDataDTO` is never modified
6. **Complete** — all fields populated; no optional or nil fields

**Output contract (formal):**

```sql
-- Layer 0 → Event Bus
INSERT INTO events (event_id, event_type, payload, created_at)
VALUES (
    '<MarketDataDTO.EventID>',
    'market_data_event',
    '<MarketDataDTO as JSONB>',
    CURRENT_TIMESTAMP
)
ON CONFLICT (event_id) DO NOTHING;
```

**What Layer 0 does NOT do:**

- Does not call Layer 1 directly
- Does not perform data quality checks
- Does not filter by liquidity, volume, or any threshold
- Does not compute any features
- Does not make any trading decisions

Layer 1 (Data Quality Engine) is the first layer that applies business logic to `MarketDataDTO` events from the bus.

---

## 3.0.5 Rescan Worker (Layer 0.5)

Layer 0.5 sits between the raw ingestion layer (Layer 0) and the Data Quality Engine (Layer 1). It is a **pure DB reader plus event emitter** — no RPC calls, no on-chain access, no private keys, no new event types. It is intentionally the cheapest possible second-pass mechanism.

### 3.0.5.1 Purpose

Re-emit `market_data_event` for tokens at fixed age bands after first detection, enabling the existing `MOMENTUM_EDGE` path to capture alpha windows that `NEW_LAUNCH_EDGE` missed at t=0:

- **Goal A** — Organic momentum buildup (0–8h): early community growth, volume accumulation on pump.fun and Raydium tokens that were illiquid at first detection.
- **Goal B** — Stalled position reversal (8–24h): second evaluation point for tokens already held or previously filtered.
- **Goal C** — Post-dip recovery and CEX catalyst window (24–48h): macro reversal, exchange listing rumor detection, second-wave narrative momentum.

### 3.0.5.2 14-Band Design

Band density is calibrated to historical memecoin alpha windows. Phase 1 provides dense coverage of the critical early momentum window; Phase 2 sparse coverage of the recovery and catalyst windows.

```
Phase 1 — Early dense (Goal A, 0–8h):
  15m → 30m → 45m → 1h → 1.5h → 2h → 3h → 4h → 6h → 8h

Phase 2 — Recovery checkpoints (Goals B+C, 12–48h):
  12h → 24h → 36h → 48h
```

| Band | Age Window (s)  | Priority | Goal |
| ---- | --------------- | -------- | ---- |
| 15m  | 900 – 1800      | 80       | A    |
| 30m  | 1800 – 2700     | 60       | A    |
| 45m  | 2700 – 3600     | 40       | A    |
| 1h   | 3600 – 5400     | 30       | A    |
| 1.5h | 5400 – 7200     | 28       | A    |
| 2h   | 7200 – 10800    | 26       | A    |
| 3h   | 10800 – 14400   | 24       | A    |
| 4h   | 14400 – 21600   | 22       | A    |
| 6h   | 21600 – 28800   | 20       | A    |
| 8h   | 28800 – 43200   | 18       | A+B  |
| 12h  | 43200 – 86400   | 16       | B    |
| 24h  | 86400 – 129600  | 14       | B+C  |
| 36h  | 129600 – 172800 | 12       | C    |
| 48h  | 172800 – 201600 | 10       | C    |

### 3.0.5.3 EventID and Idempotency

```
EventID = SHA256(chain ‖ token_address ‖ band_name ‖ bucket_ts)[:16]
bucket_ts = floor(unix_now / interval_seconds) * interval_seconds
```

The content-addressable ID guarantees idempotency: `INSERT INTO events ... ON CONFLICT (event_id) DO NOTHING`. Running the worker twice in the same interval window produces zero duplicate events.

### 3.0.5.4 Eligibility (SQL-side)

Eligibility is evaluated at query time in the database engine — never in worker code — to ensure determinism and prevent stale in-memory thresholds.

Filters applied (non-negotiable):

- `honeypot_score ≤ max_honeypot_score` (mode-adaptive)
- `rug_score ≤ max_rug_score` (mode-adaptive)
- `buy_tax_bps ≤ max_buy_tax_bps` (mode-adaptive)
- Skip tokens where `skip_open_positions = true` AND token has an open position
- Structural rejects (honeypot confirmed = true) are always excluded regardless of mode

Mode thresholds adapt automatically:

| Mode             | MaxHoneypot | MaxRug | MaxBuyTaxBps |
| ---------------- | ----------- | ------ | ------------ |
| STRICT           | 0.30        | 0.50   | 1500         |
| BALANCED         | 0.50        | 0.65   | 3000         |
| EXPLORATION      | 0.60        | 0.75   | 4500         |
| VERY_EXPLORATION | 0.75        | 0.85   | 6000         |

### 3.0.5.5 Transport Tag

```
MarketDataDTO.Transport = "rescan_<band_name>"
```

Examples: `"rescan_15m"`, `"rescan_8h"`, `"rescan_48h"`. Used by log-reviewer analytics and the Learning Engine to attribute edges to the rescan track.

### 3.0.5.6 Key Invariants

- **No new event types** — re-emits only `market_data_event` (existing type)
- **No new DTOs** — uses `contracts.MarketDataDTO` exactly as Layer 0 produces it
- **No RPC calls** — pure SQL read from existing `market_data` table
- **No module coupling** — worker in `internal/workers/run_rescan.go`; no imports from `internal/modules/`
- **Fully generic** — iterates `cfg.Rescan.Bands` at runtime; add/remove bands via `config/pipeline.yaml` only, no code changes required
- **Configured in:** `config/pipeline.yaml` → `rescan:` block; defaults in `internal/app/config/rescan_config.go`
- **Full design:** `docs/RESCAN_PLAN.md`

---

## 3.1 Data Quality Engine (Layer 1)

### 3.1.1 Responsibilities

- block scams/manipulation
- **learn false positives/negatives**
- **adapt thresholds**

### 3.1.2 Detectors (Overview)

- **Wash trading** — low unique wallets / high tx count
- **Rug pull risk** — LP unlockable, owner privileges
- **Honeypot** — buy/sell simulation
- **Fake liquidity** — LP add/remove pattern
- **Tax manipulation** — dynamic or high tax

### 3.1.3 Output DTO

```go
type DataQualityDTO struct {
    RiskScore float64
    Flags     []string
    Decision  string // pass | reject | risky-pass
}
```

### 3.1.4 Adaptive Strictness

Maintain **threshold profiles**:

```yaml
profiles:
  strict:
    max_tax: 8
    min_liquidity: 20k
  balanced:
    max_tax: 12
    min_liquidity: 10k
  exploration:
    max_tax: 15
    min_liquidity: 5k
```

**Controller logic:**

```
if false_negative_rate ↑ → relax
if rug_loss_rate       ↑ → tighten
```

### 3.1.5 Learning Signals

- rejected → later pump → **false negative**
- accepted → rug → **false positive**

---

This layer is your **hard gate against adversarial data**. If it's weak, nothing else matters. It is a **deterministic risk engine + adaptive controller** with measurable error signals.

---

### 3.1.6 Responsibilities (Operationalized)

**A. Block scams/manipulation → binary gate + risk score**

- Produce **Decision ∈ {pass, risky-pass, reject}**
- Must be **fast (<200–500ms)** and **idempotent**

**B. Learn errors → label outcomes**

- Tag each decision with later outcomes:
  - `false_positive` (accepted → loss/rug)
  - `false_negative` (rejected → pump)

**C. Adapt thresholds → controlled updates**

- Adjust **threshold profiles** (strict/balanced/exploration/very_exploration)
- Updates are **bounded, versioned, sample-gated**

---

### 3.1.7 Detectors (Concrete Algorithms)

All detectors output **[0,1] risk contributions** and **flags**. Final `RiskScore` is a weighted aggregate.

#### 3.1.7.1 Wash Trading Detector

**Signals**

- `tx_count_1m`
- `unique_wallets_1m`
- `wallet_entropy` (Shannon entropy over traders)
- `repeat_ratio` (same wallets looping)

**Heuristics**

```
if tx_count_1m high AND unique_wallets_1m low → high risk
if wallet_entropy < H_min                     → high risk
if repeat_ratio > R_max                       → high risk
```

**Score**

```
wash_risk =
    w1 * (tx_count_1m / max(unique_wallets_1m,1))_norm
  + w2 * (1 - entropy_norm)
  + w3 * repeat_ratio
```

**Flags**

- `WASH_LOW_UNIQUENESS`
- `WASH_LOOP_TRADES`

---

#### 3.1.7.2 Rug Pull Risk Detector

**On-chain checks**

- LP lock status (locker contract, lock duration)
- owner privileges: `mint()`, `setTax()`, `blacklist()`
- proxy/upgradeable patterns
- top holder concentration

**Heuristics**

```
if LP not locked OR lock_duration < T_min → risk↑
if owner can mint/blacklist               → risk↑
if top_5_holders > P_max                  → risk↑
```

**Score**

```
rug_risk =
    a1 * (1 - lp_lock_strength_norm)
  + a2 * owner_privilege_score
  + a3 * holder_concentration_norm
```

**Flags**

- `LP_UNLOCKED`
- `OWNER_PRIVILEGED`
- `HOLDER_CONCENTRATED`

---

#### 3.1.7.3 Honeypot Detector

**Method.** Simulate `buy` then `sell` via router (dry-run / callStatic).

**Checks**

- sell revert / cannot estimate gas
- effective tax on sell

**Heuristics**

```
if sell_reverts                 → reject
if effective_sell_tax > max_tax → risk↑
```

**Score**

- binary spike if revert
- continuous for tax

**Flags**

- `HONEYPOT_SELL_FAIL`
- `SELL_TAX_HIGH`

---

#### 3.1.7.4 Fake Liquidity Detector

**Signals**

- LP add/remove events in short window
- LP token distribution (burn vs wallet)
- liquidity volatility

**Heuristics**

```
if liquidity_added then removed within Δt_small → risk↑
if LP tokens not burned/locked                  → risk↑
if liquidity volatility high early              → risk↑
```

**Score**

```
liq_risk =
    b1 * short_term_liq_volatility
  + b2 * (1 - lp_lock_strength_norm)
  + b3 * rapid_add_remove_indicator
```

**Flags**

- `LP_VOLATILE`
- `LP_NOT_LOCKED`
- `LP_FLASH_ADDED_REMOVED`

---

#### 3.1.7.5 Tax Manipulation Detector

**Signals**

- `buy_tax`, `sell_tax` (from simulation)
- dynamic tax change functions
- mismatch between quoted vs realized

**Heuristics**

```
if tax > max_tax(profile)        → risk↑
if dynamic_tax_enabled           → risk↑
if buy_tax << sell_tax           → asymmetry risk↑
```

**Score**

```
tax_risk =
    c1 * tax_norm
  + c2 * dynamic_tax_flag
  + c3 * asymmetry_norm
```

**Flags**

- `TAX_HIGH`
- `TAX_DYNAMIC`
- `TAX_ASYMMETRIC`

---

#### 3.1.7.6 Aggregation → RiskScore

Normalize each sub-score to [0,1], then:

```
RiskScore =
    W_wash  * wash_risk
  + W_rug   * rug_risk
  + W_honey * honeypot_risk
  + W_liq   * liq_risk
  + W_tax   * tax_risk
```

Weights are **versioned** and updated by learning.

---

### 3.1.8 Decision Logic (Deterministic)

Given `RiskScore` and profile thresholds:

```go
type Thresholds struct {
    MaxTax         float64
    MinLiquidity   float64
    RiskReject     float64 // e.g. 0.7
    RiskRiskyPass  float64 // e.g. 0.5
}
```

**Decision**

```
if honeypot_sell_fail                           → REJECT (hard)
else if liquidity < min_liquidity(profile)      → REJECT
else if sell_tax > max_tax(profile)             → REJECT
else if RiskScore ≥ RiskReject                  → REJECT
else if RiskScore ≥ RiskRiskyPass               → RISKY_PASS
else                                            → PASS
```

### 3.1.9 Output DTO (final)

```go
type DataQualityDTO struct {
    TokenAddress string
    RiskScore    float64
    Flags        []string
    Decision     string // pass | risky-pass | reject
    Profile      string // strict | balanced | exploration | very_exploration
    Version      int
    Timestamp    int64
}
```

---

### 3.1.10 Adaptive Strictness (Controller)

**Profiles (baseline):**

```yaml
strict:
  max_tax: 8
  min_liquidity: 20000
  risk_reject: 0.65
  risk_risky_pass: 0.45

balanced:
  max_tax: 12
  min_liquidity: 10000
  risk_reject: 0.70
  risk_risky_pass: 0.50

exploration:
  max_tax: 15
  min_liquidity: 5000
  risk_reject: 0.75
  risk_risky_pass: 0.55
```

**Controller Inputs (rolling window)**

```go
type DQMetrics struct {
    PassRate          float64
    FalsePositiveRate float64 // accepted → loss/rug
    FalseNegativeRate float64 // rejected → later pump
    RugLossRate       float64
}
```

**Mode Switching**

```
if PassRate == 0 for T                       → downgrade profile (strict→balanced→exploration→very_exploration)
if RugLossRate ↑ or FalsePositiveRate ↑      → upgrade profile (very_exploration→exploration→balanced→strict)
```

**Threshold Tuning (within profile)**

```
if FalseNegativeRate ↑:
    decrease min_liquidity (−Δ)
    increase max_tax (+Δ)
    increase risk_reject (+ε)   // allow more through

if FalsePositiveRate ↑ or RugLossRate ↑:
    increase min_liquidity (+Δ)
    decrease max_tax (−Δ)
    decrease risk_reject (−ε)   // block more
```

**Constraints**

- `Δ` small (e.g., 5–10% step)
- require `N ≥ N_min` samples before update
- one parameter family per cycle (avoid oscillation)
- version every change

---

### 3.1.11 Learning Signals (Precise Labeling)

**A. False Positive (FP)**

```
Decision ∈ {pass, risky-pass}
AND Outcome ∈ {rug OR PnL < -SL within short horizon}
→ FP = 1
```

**B. False Negative (FN)** — requires **shadow tracking**

```
Decision = reject
AND observed peak_return within T_window ≥ threshold (e.g., +30%)
→ FN = 1
```

**C. Attribution (which detector failed)**

Store per-detector contributions:

```go
type DQAttribution struct {
    WashRisk  float64
    RugRisk   float64
    HoneyRisk float64
    LiqRisk   float64
    TaxRisk   float64
}
```

For each FP/FN, compute:

```
blame = highest_contributing_component OR threshold trigger
```

Used to adjust detector weights `W_*` and specific thresholds.

---

### 3.1.12 Metrics (must be tracked)

```
pass_rate
fp_rate (accepted→loss)
fn_rate (rejected→pump)
rug_loss_rate
avg_risk_score_passed
```

**Healthy targets:**

```
pass_rate:     0.5%–5%
fp_rate:       ↓ over time
fn_rate:       ↓ over time (but not zero)
rug_loss_rate: as close to 0 as possible
```

### 3.1.13 Performance Constraints

- **Latency**: ≤ 200–500 ms per token (parallelizable)
- **RPC calls**: bounded (simulate buy/sell once)
- **Idempotency**: same token → same decision

### 3.1.14 Failure Modes & Guards

- **Over-blocking** (pass_rate → 0) → auto relax profile
- **Under-blocking** (rug spikes) → auto tighten profile
- **Detector drift** (features lose signal) → weight decay + rebalancing via attribution
- **RPC noise (honeypot false fail)** → retry once, else mark `UNKNOWN` and treat as high risk

### 3.1.15 Summary

- Converts **noisy, adversarial tokens → vetted candidates**
- Maintains **controlled pass rate**
- Learns from **FP/FN with attribution**
- Adapts **thresholds and weights** without destabilizing the system

If this layer is correct, downstream layers operate on **clean, high-signal input**. If not, everything else is wasted.

---

## 3.2 Feature Extraction (Layer 2)

### 3.2.1 Features (Overview)

- liquidity_size, growth_rate
- tx_velocity
- holder_distribution
- wallet entropy (anti-wash)
- contract flags

### 3.2.2 Output

`FeatureDTO + FeatureConfidence`

### 3.2.3 Feedback

- feature importance vs PnL
- drift detection (features losing predictive power)

---

This layer turns raw on-chain events into a **compact, predictive state vector**. It must be **fast, consistent, and auditable**, because every downstream decision depends on it.

---

### 3.2.4 Objectives (Operational)

- **Encode early market state** into features usable by scoring/probability
- **Standardize inputs** (scale, normalize, timestamped)
- **Attach confidence** to each feature (data completeness + stability)
- **Emit attribution-ready data** for later learning

---

### 3.2.5 Feature Set (Exact Definitions)

All features are computed over fixed windows (e.g., **t₀ = pool creation**, windows: **[0–30s], [30–120s], [2–5m]**). Keep windows consistent.

#### 3.2.5.1 Liquidity Size & Growth

**Inputs**

- `liquidity_usd(t)`
- LP events

**Features**

```
liquidity_size_0      = liquidity_usd(t_now)
liquidity_growth_rate = (liquidity_usd(t_now) - liquidity_usd(t_Δ)) / Δt
liquidity_volatility  = stddev(liquidity_usd over window)
lp_lock_strength      = normalized(lock_duration, locker_trust)
```

**Normalization**

- log-scale for size: `log1p(liquidity_size_0)`
- clamp growth to percentile bounds

#### 3.2.5.2 Transaction Velocity

**Inputs**

- swaps, transfers

**Features**

```
tx_count_1m
tx_rate               = tx_count_Δ / Δt
buy_sell_ratio        = buys / max(sells,1)
unique_traders_1m
avg_trade_size
trade_size_dispersion (std/mean)
```

**Derived**

```
momentum_proxy = tx_rate * buy_sell_ratio
burstiness     = max(tx_rate_short) / max(tx_rate_long, ε)
```

#### 3.2.5.3 Holder Distribution

**Inputs**

- balances by address

**Features**

```
top1_pct, top5_pct, top10_pct
gini_coefficient
new_holder_rate = Δ(unique_holders)/Δt
holder_churn    = (new + exited) / current_holders
```

**Interpretation**

- high concentration → rug risk
- rising new_holder_rate → adoption/momentum

#### 3.2.5.4 Wallet Entropy (Anti-wash)

**Inputs**

- trader addresses per window

**Feature**

```
p_i            = trades_by_wallet_i / total_trades
wallet_entropy = - Σ p_i log(p_i)
entropy_norm   = wallet_entropy / log(N)
```

**Aux**

```
repeat_ratio = repeated_trades_by_same_wallet / total_trades
```

Low entropy + high repeat_ratio ⇒ wash risk.

#### 3.2.5.5 Contract Flags (Binary/Discrete)

From static + simulated analysis:

```
has_mint
has_blacklist
is_proxy
sell_tax, buy_tax
tax_dynamic
lp_locked
router_compatible
```

Encode as:

- one-hot / binary
- numeric for taxes

---

### 3.2.6 Feature Vector (Unified)

```go
type FeatureDTO struct {
    TokenAddress string

    // Liquidity
    LiquiditySize       float64
    LiquidityGrowth     float64
    LiquidityVolatility float64
    LPLockStrength      float64

    // Activity
    TxRate        float64
    BuySellRatio  float64
    UniqueTraders float64
    AvgTradeSize  float64
    Burstiness    float64

    // Holders
    Top1Pct       float64
    Top5Pct       float64
    Gini          float64
    NewHolderRate float64
    HolderChurn   float64

    // Anti-wash
    WalletEntropy float64
    RepeatRatio   float64

    // Contract
    SellTax      float64
    BuyTax       float64
    HasMint      bool
    HasBlacklist bool
    IsProxy      bool
    LPLocked     bool

    Window    string // e.g. "0-30s"
    Timestamp int64
    Version   int
}
```

### 3.2.7 Feature Confidence (Per-Vector + Per-Field)

You must quantify **data reliability**.

```go
type FeatureConfidence struct {
    Overall float64            // [0,1]
    ByField map[string]float64 // per-feature confidence
    Reasons []string           // e.g. "low_sample", "rpc_partial"
}
```

**Confidence Rules**

```
if unique_traders < N_min       → confidence↓
if window too short             → confidence↓
if RPC partial/missing          → confidence↓
if feature stable across ticks  → confidence↑
```

**Example**

```
TxRate confidence  = min(1, unique_traders / 20)
Entropy confidence = min(1, total_trades / 30)
```

---

### 3.2.8 Normalization & Stability

All numeric features must be:

- **bounded**: map to [0,1] or z-score capped
- **monotonic where possible**
- **robust to outliers**

Examples:

```
LiquiditySize  = log1p(liq) / log1p(L_max)
BuySellRatio   = tanh(buy/sell - 1)
TxRate         = clip(tx_rate / rate_p95, 0, 1)
```

Maintain **normalization stats** (rolling p50/p95) per chain.

---

### 3.2.9 Computation Model

- Compute in **incremental windows** (don't recompute full history)
- Cache per token: last window aggregates, rolling stats
- Single pass per event batch

Pseudo:

```
agg        := updateAggregates(prevAgg, newEvents)
features   := computeFeatures(agg)
confidence := computeConfidence(agg, features)
emit(FeatureDTO, FeatureConfidence)
```

**Latency target:** < 100–200 ms per token.

---

### 3.2.10 Feedback: Feature Importance vs PnL

You must measure **predictive power per feature**.

**3.2.10.1 Cohort Binning.** For each feature `f`, bin values:

```
f ∈ [0-0.2), [0.2-0.4), ... [0.8-1.0]
```

Compute per bin:

```
win_rate
avg_pnl
expectancy = mean(pnl)
```

**3.2.10.2 Correlation (rank-based).**

```
ρ_f = Spearman(feature_value, realized_pnl)
```

Store per version/window.

**3.2.10.3 Lift.**

```
lift_top = avg_pnl(top_20% f) / avg_pnl(all)
```

**3.2.10.4 Output (for learning).**

```go
type FeaturePerformance struct {
    Feature    string
    Spearman   float64
    LiftTop    float64
    Bins       []BinStat
    SampleSize int
    Version    int
}
```

Used to adjust:

- scoring weights
- thresholds (e.g., entropy floor)

---

### 3.2.11 Drift Detection (Feature Decay)

Detect when a feature **loses predictive power**.

**3.2.11.1 Distribution Drift.** Compare current vs baseline:

```
PSI (Population Stability Index)
PSI > 0.2 → moderate drift
PSI > 0.3 → significant drift
```

**3.2.11.2 Predictive Drift.**

```
Δρ_f = ρ_f(current) - ρ_f(baseline)
if Δρ_f < -δ → feature degraded
```

**3.2.11.3 Decision Impact Drift.**

```
if top-bin expectancy ↓ significantly → degrade weight
```

**3.2.11.4 Actions (bounded).**

```
if drift_detected:
    reduce weight(f) by ε
    increase confidence threshold for f
    flag for review
```

Never drop a feature to zero in one step.

---

### 3.2.12 Quality Gates (before emit)

- required fields present
- confidence ≥ `C_min` for critical features (entropy, tax, liquidity)
- values within bounds

If not:

```
mark FeatureConfidence.Overall low
emit anyway (do NOT block) — downstream can down-weight
```

### 3.2.13 Failure Modes & Guards

- **Sparse data early** → low confidence → downstream down-weights
- **RPC inconsistency** → retry once; else mark partial
- **Outlier spikes** → clipped by normalization
- **Feature leakage (using future data)** → strict windowing by timestamp

### 3.2.14 What This Layer Guarantees

- A **stable, normalized feature vector** per token per window
- **Explicit confidence** to avoid over-trusting noisy signals
- **Attribution-ready metrics** for learning (correlation, lift)
- **Drift awareness** to prevent stale signals from dominating

If this layer is clean, scoring becomes a **weighting problem**. If it's noisy, everything downstream is guesswork.

---

## 3.3 Signal & Edge Discovery (Layer 3)

### 3.3.1 Edge Definition

`NEW_LAUNCH_EDGE`

### 3.3.2 Logic

```
if new_pool && quality_pass && early_momentum:
    emit EdgeDTO
```

### 3.3.3 Adaptive Gate

- min momentum threshold adjusts with market conditions

### 3.3.4 Feedback

- which edges convert to profitable exits within T minutes

---

This layer decides **whether a token is even worth competing for**. It converts features into a **time-sensitive trading hypothesis**.

---

### 3.3.5 Edge Definition (Formal)

```
NEW_LAUNCH_EDGE =
    early-stage token
    + sufficient data quality
    + measurable early momentum
    + within exploitable time window
```

**Mathematical Form**

```
EdgeExists =
    I(new_pool)
  × I(DataQuality ∈ {pass, risky-pass})
  × I(MomentumScore ≥ θ_momentum(t))
```

Where:

- `θ_momentum(t)` is adaptive threshold
- `t` = time since launch

### 3.3.6 Inputs (Strict)

From previous layers:

- `DataQualityDTO`
- `FeatureDTO`
- `FeatureConfidence`

NO external calls here.

### 3.3.7 Core Logic (Deterministic)

```go
func detectEdge(f FeatureDTO, dq DataQualityDTO, conf FeatureConfidence, now int64) (EdgeDTO, bool) {
    if dq.Decision == "reject" {
        return nil, false
    }
    if !isNewPool(f.Timestamp, now) {
        return nil, false
    }
    momentum := computeMomentum(f)
    threshold := adaptiveMomentumThreshold(now - f.Timestamp)
    if momentum < threshold {
        return nil, false
    }
    return EdgeDTO{
        TokenAddress: f.TokenAddress,
        Momentum:     momentum,
        Confidence:   conf.Overall,
        AgeSec:       now - f.Timestamp,
    }, true
}
```

### 3.3.8 "New Pool" Constraint (Hard Gate)

**Definition**

```
AgeSec ≤ T_max_edge_window
```

**Typical Values**

```
T_max_edge_window = 5–15 minutes
```

**Why:** after this → edge mostly gone.

---

### 3.3.9 Momentum Model (Core Signal)

Momentum must capture:

- speed of participation
- direction (buy pressure)
- diversity (not fake)

**3.3.9.1 Momentum Score (Composite)**

```
Momentum =
    w1 * TxRate_norm
  + w2 * BuySellRatio_norm
  + w3 * NewHolderRate_norm
  + w4 * Entropy_norm
  + w5 * LiquidityGrowth_norm
```

**Normalized Inputs (from Layer 2):** `TxRate_norm`, `BuySellRatio_norm`, `NewHolderRate_norm`, `Entropy_norm`, `LiquidityGrowth_norm`.

**Weight Constraints**

```
Σ w_i = 1
w_i ≥ 0
```

**3.3.9.2 Anti-Fake Momentum Correction**

Momentum must be penalized if: low entropy, high repeat_ratio, high concentration.

**Penalty**

```
AdjustedMomentum = Momentum × (1 - wash_penalty)

wash_penalty =
    α * (1 - entropy_norm)
  + β * repeat_ratio
```

---

### 3.3.10 Adaptive Momentum Threshold

**3.3.10.1 Problem.** Fixed threshold fails because: market changes (slow vs hype), chain-specific behavior.

**3.3.10.2 Target Behavior.** Maintain edge discovery rate in stable band.

```
EdgePassRate = 0.5% – 5%
```

**3.3.10.3 Controller**

```
if edge_pass_rate == 0:        decrease θ_momentum
if false_positive_rate ↑:      increase θ_momentum
if false_negative_rate ↑:      decrease θ_momentum
if too many edges:             increase θ_momentum
```

**3.3.10.4 Time-Decay Adjustment.** Momentum requirement should decrease with time:

```
θ_momentum(t) = base_threshold × exp(-λ * t)
```

**Intuition:** early → need strong signal; later → allow weaker signal.

---

### 3.3.11 Edge Confidence

Derived from:

```
EdgeConfidence = FeatureConfidence.Overall × stability(momentum over Δt)
```

**Stability Check**

```
if momentum fluctuates wildly → confidence↓
```

### 3.3.12 Output DTO

```go
type EdgeDTO struct {
    TokenAddress     string
    EdgeType         string // NEW_LAUNCH_EDGE
    Momentum         float64
    AdjustedMomentum float64
    Threshold        float64
    Confidence       float64
    AgeSec           int
    Source           string // pool | wallet | surge
    Version          int
    Timestamp        int64
}
```

### 3.3.13 Adaptive Gate Behavior

**3.3.13.1 Two-Level Gate**

```
Level 1: DataQuality → pass/risky-pass
Level 2: Momentum ≥ θ
```

**3.3.13.2 Exploration Path.** If system in exploration mode:

```
allow slightly below threshold
if momentum ≥ (θ - ε):
    emit edge (flagged)
```

---

### 3.3.14 Feedback Loop (Critical)

**3.3.14.1 Labeling Edges.** For each `EdgeDTO`:

```
Outcome:
  success → PnL ≥ target within T
  fail    → otherwise
```

**3.3.14.2 Time Window:** `T = 5–15 minutes`.

**3.3.14.3 Metrics**

```
EdgeHitRate   = success_edges / total_edges
EdgePrecision = profitable_entries / total_entries
```

**3.3.14.4 Attribution.** For each edge, store: momentum value, features, outcome.

**3.3.14.5 Learning Outputs**

- **A. Threshold tuning**
  ```
  if many failures at low momentum:   increase θ
  if many missed winners:             decrease θ
  ```
- **B. Weight tuning** — increase weight of features correlated with success
- **C. Feature pruning** — if feature contributes noise: `reduce weight → 0 gradually`

---

### 3.3.15 Failure Modes & Guards

| Failure            | Symptom                          | Guard                                       |
| ------------------ | -------------------------------- | ------------------------------------------- |
| Fake Momentum Trap | high tx_rate but low entropy     | entropy penalty (mandatory)                 |
| Late Momentum      | strong signal but already peaked | `if AgeSec > T_cutoff: reject regardless`   |
| Over-triggering    | too many edges                   | raise `θ_momentum`                          |
| Under-triggering   | no edges                         | lower `θ_momentum`; enable exploration band |

### 3.3.16 Performance Constraints

- compute time: < 50–100 ms per token
- no RPC calls
- pure function of inputs

### 3.3.17 What This Layer Guarantees

- converts **features → actionable opportunity**
- filters out: weak momentum, fake activity, late entries
- produces **time-sensitive edge candidates**

**Final Insight.** This layer answers one question: _"Is there a tradable opportunity RIGHT NOW?"_ — not "Is this token good?" or "Will it 100x?".

If correct: downstream selection becomes easy; execution captures real edge. If wrong: you chase noise, or miss everything.

---

## 3.4 Probability / Slippage / Latency Models (Layer 4)

### 3.4.1 Overview

- **A. Probability** — P(pump within 5–10 min)
- **B. Slippage** — expected price impact at your size
- **C. Latency** — detection → submit → inclusion delay

**DTOs:** `ProbabilityEstimateDTO`, `SlippageEstimateDTO`, `LatencyProfileDTO`.

**Adaptive Calibration**

```
error = actual - predicted
update coefficients (bounded, versioned)
```

---

This layer answers: _"Even if an edge exists → is it worth trading after real-world friction?"_ You are converting **signal → expected value (EV)**.

### 3.4.2 Overall Formulation

```
ExpectedValue = P(success) × Gain
              - (1 - P(success)) × Loss
              - SlippageCost
              - LatencyCost
```

Where:

- Probability → will it pump?
- Slippage → how much you lose entering
- Latency → how much edge decays before execution

---

### 3.4.3 A. Probability Model

**3.4.3.1 Definition**

```
P = P(price increases ≥ target_return within T window)
```

**3.4.3.2 Target**

```
T = 5–10 minutes
target_return = +20% (example baseline)
```

**3.4.3.3 Model Input (from Layer 2 + 3)**

```
- Momentum (adjusted)
- TxRate
- BuySellRatio
- NewHolderRate
- WalletEntropy
- LiquiditySize
- LiquidityGrowth
- HolderConcentration
```

**3.4.3.4 Model Form (Practical).** Start simple:

```
P = sigmoid(
      w1 * momentum
    + w2 * tx_rate
    + w3 * buy_sell_ratio
    + w4 * entropy
    + w5 * liquidity_growth
    - w6 * concentration
)
```

**Properties**

- bounded [0,1]
- monotonic
- interpretable

**3.4.3.5 Calibration Target**

```
Predicted P ≈ Actual success frequency
```

Example: tokens with `P=0.7` → ~70% should succeed.

**3.4.3.6 Output DTO**

```go
type ProbabilityEstimateDTO struct {
    TokenAddress string
    Probability  float64
    TargetReturn float64
    HorizonSec   int
    Confidence   float64
    Version      int
    Timestamp    int64
}
```

---

### 3.4.4 B. Slippage Model

**3.4.4.1 Definition**

```
Slippage = price_impact + execution_cost
```

**3.4.4.2 Key Factors**

```
- pool liquidity
- trade size
- pool depth curve
- volatility
```

**3.4.4.3 Model (AMM Approximation).** For constant product AMM:

```
price_impact ≈ trade_size / liquidity
```

More precisely:

```
ΔP ≈ (Δx / (x + Δx))
```

**3.4.4.4 Practical Model**

```
Slippage = k1 * (position_size / liquidity_size) + k2 * volatility
```

**3.4.4.5 Adjustments.** Increase slippage if:

```
- high tx_rate (competition)
- low liquidity
- high momentum (crowded entry)
```

**3.4.4.6 Output DTO**

```go
type SlippageEstimateDTO struct {
    TokenAddress     string
    ExpectedSlippage float64
    PositionSize     float64
    Liquidity        float64
    Confidence       float64
    Version          int
    Timestamp        int64
}
```

---

### 3.4.5 C. Latency Model

**3.4.5.1 Definition**

```
Latency = time from detection → transaction inclusion
```

**3.4.5.2 Components**

```
1. detection_delay
2. processing_delay
3. submission_delay
4. network_delay
5. block_inclusion_delay
```

**3.4.5.3 Model**

```
TotalLatency = t_detect + t_compute + t_submit + t_network + t_block
```

**3.4.5.4 Critical Metric**

```
EdgeDecay = f(latency)
```

Example: if pump happens in 30s and latency = 20s → most edge gone.

**3.4.5.5 Latency Penalty**

```
EffectiveEdge = Edge × exp(-λ × latency)
```

**3.4.5.6 Output DTO**

```go
type LatencyProfileDTO struct {
    TokenAddress           string
    EstimatedLatencyMs     int
    ExpectedInclusionBlock int
    Confidence             float64
    Version                int
    Timestamp              int64
}
```

---

### 3.4.6 Combined Decision Input

This layer produces:

```
- ProbabilityEstimateDTO
- SlippageEstimateDTO
- LatencyProfileDTO
```

These feed into Layer 5 (validation).

---

### 3.4.7 Adaptive Calibration (Critical)

This is where models improve.

**3.4.7.1 Error Definition**

- **A. Probability Error:** `E_prob = actual_outcome - predicted_probability`
- **B. Slippage Error:** `E_slip = actual_slippage - expected_slippage`
- **C. Latency Error:** `E_lat = actual_latency - predicted_latency`

**3.4.7.2 Update Rule (Bounded)**

- **Probability Weights**

  ```
  w_new = w_old + α × error × feature_value
  ```

  Constraints: small α (e.g. 0.01), normalized weights, clipped updates.

- **Slippage Coefficients**

  ```
  k_new = k_old + β × E_slip
  ```

- **Latency Model**

  ```
  latency_estimate = moving_average(previous_latency)
  ```

**3.4.7.3 Versioning.** Every update:

```
model_version++
store parameters snapshot
```

**3.4.7.4 Safety Constraints**

```
- require N samples before update
- cap max parameter change per cycle
- rollback if performance worsens
```

---

### 3.4.8 Confidence Propagation

Each model outputs confidence:

```
low data → low confidence → downstream discount
```

### 3.4.9 Failure Modes & Guards

| Failure                   | Symptom                 | Fix                                        |
| ------------------------- | ----------------------- | ------------------------------------------ |
| Overestimated Probability | high P but losses       | recalibrate weights; increase penalty      |
| Underestimated Slippage   | actual entry much worse | increase slippage coefficient; reduce size |
| Latency Blindness         | entering too late       | increase latency penalty; reject high-L_t  |

### 3.4.10 Performance Requirements

- compute time: < 50–100 ms
- no heavy ML (keep deterministic)
- incremental updates only

### 3.4.11 What This Layer Guarantees

- converts **edge → expected value**
- prevents: chasing low-probability trades, entering illiquid traps, being too late

**Final Insight.** This layer is where most bots fail. They assume `edge exists → profit`. You enforce `edge × probability × execution reality → profit`. If correct: you trade less, but with positive expectancy. If wrong: you trade often, but lose slowly.

---

## 3.5 Edge Validation (Layer 5)

### 3.5.1 Overview

Reject if:

- probability < dynamic threshold
- slippage > dynamic cap
- latency too high vs opportunity decay
- bad micro-regime

### 3.5.2 Adaptive Thresholds

```
target_pass_rate = 0.5%–5%

if pass_rate == 0    → relax
if pass_rate > 10%   → tighten
```

### 3.5.3 Feedback

- false rejects vs false accepts tracked

---

This layer is the **last hard gate before capital is committed**. It must convert model outputs into a **binary decision with controlled risk and stable throughput**.

---

### 3.5.4 Objective (Formal)

Accept only if the trade has **positive, risk-adjusted expectancy under real constraints**.

```
ACCEPT ⇔ EV_adj > 0 AND constraints satisfied

Where:
EV_adj =
    P * G
  - (1 - P) * L
  - SlippageCost
  - LatencyCost
  - RiskPenalty
```

### 3.5.5 Inputs (Strict)

From Layer 3–4:

- `EdgeDTO` (momentum, age, confidence)
- `ProbabilityEstimateDTO` (P)
- `SlippageEstimateDTO` (S)
- `LatencyProfileDTO` (L_t)
- (optional) micro-regime features (recent market state)

### 3.5.6 Decision Pipeline (Deterministic)

Order matters — cheap checks first.

```
1) Hard rejects (invalid)
2) Constraint checks (caps)
3) EV check
4) Confidence gate
→ ACCEPT / REJECT / EXPLORE
```

### 3.5.7 Hard Rejects

Immediate drop regardless of EV.

```
if P is NaN or confidence low                              → REJECT
if AgeSec > T_cutoff                                       → REJECT
if honeypot flag present (should already be blocked)       → REJECT
```

### 3.5.8 Constraint Checks (Caps)

**3.5.8.1 Probability Threshold**

```
P ≥ θ_p
Dynamic: θ_p ∈ [0.45, 0.75] (profile-dependent)
```

**3.5.8.2 Slippage Cap**

```
S ≤ θ_s
Dynamic: θ_s ∈ [2%, 10%] (depends on liquidity regime)
```

**3.5.8.3 Latency vs Decay.** Define opportunity half-life `τ` (from historical cohort):

```
L_t ≤ κ * τ
Typical: κ ∈ [0.2, 0.5]
```

If too slow → edge mostly gone.

**3.5.8.4 Micro-Regime Filter.** Compute a lightweight regime score `R ∈ [0,1]`:

```
- recent hit-rate (last N trades)
- avg tx_rate across tokens
- avg entropy (market cleanliness)

Reject if: R < θ_r
(avoid trading in "toxic" periods)
```

### 3.5.9 EV Check (Core)

Compute:

```
EV = P * G - (1 - P) * L - S - LatencyPenalty
```

Where:

- `G` = expected gain (baseline from strategy, e.g., +20–50%)
- `L` = expected loss (e.g., stop-loss magnitude)
- `LatencyPenalty = G * (1 - e^{-λ * L_t})`

Accept if:

```
EV ≥ θ_ev   (θ_ev ≥ 0 with safety margin)
```

### 3.5.10 Confidence Gate

Down-weight or reject low-confidence cases:

```
if overall_confidence < C_min:
    REJECT or EXPLORE

Typical: C_min = 0.5–0.7
```

### 3.5.11 Final Decision

```
if hardReject                  → REJECT
else if any cap violated       → REJECT
else if EV < θ_ev              → REJECT
else if confidence low         → EXPLORE
else                           → ACCEPT
```

### 3.5.12 Output DTO

```go
type ValidatedEdgeDTO struct {
    TokenAddress   string
    Decision       string // accept | reject | explore
    Probability    float64
    ExpectedValue  float64
    Slippage       float64
    LatencyMs      int
    Thresholds     struct {
        Prob         float64
        Slippage     float64
        LatencyRatio float64
        EV           float64
    }
    RegimeScore float64
    Confidence  float64
    Version     int
    Timestamp   int64
}
```

---

### 3.5.13 Adaptive Threshold Controller

**3.5.13.1 Targets**

```
pass_rate_target = 0.5% – 5%
fp_rate ↓
fn_rate ↓ (but not zero)
```

**3.5.13.2 Metrics (rolling window)**

```go
type GateMetrics struct {
    PassRate          float64
    FalsePositiveRate float64 // accepted → loss
    FalseNegativeRate float64 // rejected → would-win
    AvgEV             float64
}
```

**3.5.13.3 Control Rules**

**Throughput Control**

```
if PassRate == 0:
    relax θ_p (−Δ), relax θ_ev (−Δ), increase θ_s (+Δ)

if PassRate > 10%:
    tighten θ_p (+Δ), increase θ_ev (+Δ), decrease θ_s (−Δ)
```

**Risk Control**

```
if FalsePositiveRate ↑:
    increase θ_p
    increase θ_ev
    decrease θ_s
    tighten latency ratio (κ ↓)

if FalseNegativeRate ↑:
    decrease θ_p
    decrease θ_ev
    increase θ_s
```

**Regime Sensitivity**

```
if RegimeScore low:   globally tighten (θ_p↑, θ_ev↑)
if RegimeScore high:  allow mild relaxation
```

**3.5.13.4 Step Size & Safety**

```
Δ small (e.g., 2–5% relative)
one parameter group per cycle
require N_min samples (e.g., ≥ 30–50 trades)
```

**3.5.13.5 Versioning.** Each adjustment:

```
version++
store thresholds snapshot
```

Rollback if: `AvgEV ↓` or `FP spikes`.

---

### 3.5.14 False Rejects vs False Accepts (Labeling)

**3.5.14.1 False Accept (FP)**

```
Decision = ACCEPT
AND realized_pnl < 0 (or hit SL quickly)
```

**3.5.14.2 False Reject (FN)** — requires shadow tracking:

```
Decision = REJECT
AND max_return_in_T ≥ target_return
```

**3.5.14.3 Attribution.** Store which constraint blocked/allowed:

```go
type GateAttribution struct {
    ProbBlocked     bool
    SlippageBlocked bool
    LatencyBlocked  bool
    EVBlocked       bool
}
```

Use to tune **specific thresholds**, not all at once.

---

### 3.5.15 Exploration Path (Controlled)

To avoid starvation:

```
if Decision = REJECT but close to thresholds:
    mark as EXPLORE
    allow small-cap trade (1–5% budget)

Criteria: |P - θ_p| ≤ ε  OR  |EV - θ_ev| ≤ ε
```

### 3.5.16 Failure Modes & Guards

| Failure             | Symptom                        | Fix                                         |
| ------------------- | ------------------------------ | ------------------------------------------- |
| Over-tight Gate     | `pass_rate → 0`                | relax thresholds, enable exploration        |
| Over-loose Gate     | `pass_rate ↑`, FP ↑            | tighten θ_p, θ_ev; lower θ_s                |
| Miscalibrated EV    | EV positive but losses occur   | recalibrate P, S, latency penalty (Layer 4) |
| Latency blind spots | accepting trades with high L_t | lower κ, increase penalty                   |

### 3.5.17 Performance Constraints

- compute time: **< 20–50 ms per token**
- pure function (no RPC)
- deterministic given inputs + thresholds

### 3.5.18 What This Layer Guarantees

- Trades only when **EV is positive under real friction**
- Maintains **controlled pass rate**
- Balances: **opportunity capture** (FN ↓), **risk containment** (FP ↓)

**Final Insight.** This gate enforces: _"Good signal is not enough — only tradeable signal survives."_ If correct: you trade less, but with positive expectancy. If wrong: you either do nothing or bleed slowly.

---

## 3.6 Selection Engine (Layer 6)

### 3.6.1 Input

- validated edges

### 3.6.2 Output

- Top K (5–10)

### 3.6.3 Health Constraints

- **portfolio-level cap**
- **diversity (avoid same deployer/cluster)**

### 3.6.4 Adaptive Logic

- if inactivity window exceeded → allow lower-ranked entries (exploration band)

---

This layer is a **constrained optimizer**: from all validated edges, pick a **small, high-quality, diversified set** that fits capital and risk limits. It must be deterministic, fast, and stable.

---

### 3.6.5 Objective (Formal)

Select a subset S that maximizes portfolio utility under constraints:

```
maximize   Σ_{i ∈ S}  U_i
subject to |S| ≤ K
           capital(S) ≤ C_total
           exposure constraints
           diversity constraints
```

Where utility per edge:

```
U_i = EV_i × Conf_i × AgeDecay_i × ExecFeasibility_i
```

- `EV_i` from Layer 5
- `Conf_i` = combined confidence
- `AgeDecay_i = exp(-λ_age * AgeSec_i)`
- `ExecFeasibility_i = 1 - f(slippage, latency)`

### 3.6.6 Inputs

- `ValidatedEdgeDTO[]` (only `accept` or `explore`)
- Portfolio state: current positions, available capital
- Risk/cluster metadata: deployer, LP address, token graph cluster

### 3.6.7 Output

```go
type SelectionOutput struct {
    Selected  []SelectedEdge // size ≤ K (typically 5–10)
    Rejected  []RejectionReason
    Version   int
    Timestamp int64
}

type SelectedEdge struct {
    TokenAddress string
    Score        float64 // final utility
    Rank         int
    Bucket       string  // e.g., "primary" | "explore"
}
```

### 3.6.8 Ranking (Deterministic)

Compute a **selection score**:

```
Score_i = EV_i × Conf_i × AgeDecay_i × (1 - SlippagePenalty_i) × (1 - LatencyPenalty_i)
```

- Normalize components to [0,1]
- Break ties deterministically (e.g., by token address hash)

Sort descending → initial list `L`.

### 3.6.9 Core Algorithm (Greedy with Constraints)

```
S = []
for i in L:
    if |S| == K: break
    if violates_portfolio_caps(i): continue
    if violates_diversity(i, S): continue
    if not executable(i): continue
    S.append(i)
return S
```

Rationale: fast, predictable, and sufficient given small K.

### 3.6.10 Health Constraints (Hard)

**3.6.10.1 Portfolio-Level Caps**

```
max_positions:                   K (5–10)
max_capital_per_trade:           c_max
max_total_capital:               C_total
max_simultaneous_new_positions:  M (e.g., 3–6)
```

**3.6.10.2 Diversity (Anti-Cluster).** Avoid correlated failures.

**Cluster keys:** `deployer_address`, `lp_address`, `bytecode_hash`, graph cluster id.

**Rules**

```
max_per_deployer ≤ 1
max_per_lp       ≤ 1
max_per_cluster  ≤ 2
```

### 3.6.11 Exploration Band (Controlled Relaxation)

**Inactivity definition**

```
no selections for T_inactive (e.g., 5–10 min) OR pass_rate ≈ 0
```

**Exploration logic.** Fill remaining slots from `L_explore` (edges with `Decision = explore` or just below thresholds), with stricter caps. **Budget:** `explore_capital ≤ 1–5% of C_total`. Mark with `Bucket = "explore"`.

### 3.6.12 Execution Feasibility Filter

Reject if: `expected_slippage > θ_s_eff`, `estimated_latency_ratio > κ_eff`, recent execution failure rate high for similar cohort.

### 3.6.13 Stability & Determinism

Fixed ordering, fixed tie-breakers, no randomness, same inputs → same `Selected`.

### 3.6.14 Metrics

```
selection_count
selection_rate = |S| / candidates
portfolio_diversity_index
capital_utilization
topK_vs_all_pnl (efficiency)
explore_win_rate vs primary_win_rate
```

### 3.6.15 Failure Modes & Guards

- **Over-concentration** → enforce cluster caps
- **Capital fragmentation** → enforce min size / cap positions
- **Starvation** → trigger exploration band
- **Chasing stale edges** → age decay + feasibility filter
- **Over-selection** → K cap + tighten upstream thresholds

### 3.6.16 Complexity & Performance

- Sort: O(N log N); greedy: O(N). Latency: **< 10–20 ms**.

### 3.6.17 What This Layer Guarantees

- Small, high-utility, diversified set
- Respects capital and risk constraints
- Maintains throughput via exploration when needed

**Key takeaway.** You don't win by finding many edges. You win by selecting a few that survive constraints and diversify risk.

---

## 3.7 Capital Engine (Layer 7)

### 3.7.1 Allocation

```
size ∝ score × probability × confidence
```

### 3.7.2 Constraints

- max per position
- max concurrent positions
- exploration budget (1–5%)

### 3.7.3 Adaptive

- increase size for cohorts with positive expectancy
- shrink for underperforming cohorts

---

This layer turns **selected edges → position sizes**. It is a **risk allocator** under uncertainty. If sizing is wrong, a good system still loses.

### 3.7.4 Objective (Formal)

```
maximize   Σ (size_i × EV_i)
subject to Σ size_i ≤ C_total
           size_i ≤ cap_per_position
           active_positions ≤ K
           risk constraints satisfied
```

### 3.7.5 Inputs

- `SelectedEdge[]` with `EV_i`, `P_i`, `Score_i`, `Confidence_i`, `Slippage_i`, `Latency_i`, cohort tags
- Portfolio state: `C_total`, `active_positions`, recent cohort performance stats

### 3.7.6 Base Allocation Rule

```
w_i   = Score_i_norm × P_i × Confidence_i
ŵ_i   = w_i / Σ w_j
size_i* = ŵ_i × C_alloc
```

### 3.7.7 Risk-Adjusted Sizing (Core)

```
size_i = size_i*
       × (1 - SlippagePenalty_i)
       × (1 - LatencyPenalty_i)
       × CohortMultiplier_i
```

**Slippage Penalty**

```
SlippagePenalty_i = clip(S_i / θ_s_eff, 0, 1)
```

**Latency Penalty**

```
LatencyPenalty_i = 1 - exp(-λ * L_t_i)
```

**Cohort Multiplier (Adaptive)**

```
if cohort_EV > 0  → 1.0 – 1.5
if cohort_EV ≈ 0  → 0.5 – 1.0
if cohort_EV < 0  → 0.1 – 0.5
```

### 3.7.8 Hard Constraints

**Max Per Position:** `size_i ≤ c_max` (typically 5–20% of `C_total`).

**Max Concurrent Positions:** `active_positions + new ≤ K` (5–10).

**Minimum Viable Size:** `size_i ≥ c_min` else drop.

**Exploration Budget**

```
C_explore = 1% – 5% of C_total
size_i_explore ≤ small_cap (0.2–1% each)
```

**Capital Conservation:** `Σ size_i ≤ C_total`; scale down proportionally if overflow.

### 3.7.9 Final Allocation DTO

```go
type AllocationDTO struct {
    TokenAddress string
    FinalSize    float64
    Components   struct {
        RawWeight        float64
        NormalizedWeight float64
        SlippagePenalty  float64
        LatencyPenalty   float64
        CohortMultiplier float64
    }
    Bucket    string // primary | explore
    Version   int
    Timestamp int64
}
```

### 3.7.10 Adaptive Mechanism (Cohort-Based Learning)

**Cohort Definition.** Group trades by liquidity/tax/entropy/latency/momentum bands.

**Metrics per Cohort:** `win_rate`, `avg_pnl`, `expectancy`, `drawdown`.

**Multiplier Update (Bounded)**

```
mult_new = mult_old + α × (expectancy - baseline)
```

Constraints: small α (≈0.05), clamp to [0.1, 1.5], require `N ≥ N_min`.

### 3.7.11 Portfolio-Level Risk Controls

**Exposure Limits:** `max_exposure_per_cluster ≤ X%`, `max_exposure_per_time_window ≤ Y%`.

**Drawdown Guard**

```
If recent drawdown exceeds threshold:
    reduce all sizes by factor γ (e.g., 0.5)
    or pause new allocations
```

**Volatility Scaling:** If volatility ↑ → scale sizes down.

### 3.7.12 Rebalancing

If some edges infeasible post-check, redistribute freed capital to next best edges respecting caps.

### 3.7.13 Failure Modes & Guards

| Failure                           | Guard                                                   |
| --------------------------------- | ------------------------------------------------------- |
| Over-allocation to noisy signals  | confidence + penalties + cohort multiplier              |
| Under-allocation (missing upside) | normalization + minimum size + exploration              |
| Concentration risk                | per-position cap + diversity (upstream) + exposure caps |
| Slippage blowups                  | penalty + feasibility filter + size reduction           |

### 3.7.14 Performance Constraints

- compute time: **< 5–10 ms**; no external calls; deterministic math only.

### 3.7.15 What This Layer Guarantees

- Capital proportional to quality and confidence
- Losses bounded by caps and penalties
- System learns where to size up/down over time
- Exploration is funded but contained

**Final Insight.** Selection finds opportunities. Capital decides whether you survive. A small edge with correct sizing compounds. A strong edge with bad sizing blows up.

---

## 3.8 Execution Engine (Layer 8)

### 3.8.1 Design

- wallet sharding (avoid nonce contention)
- prebuilt calldata
- bounded parallelism (5–20)

### 3.8.2 Failure Handling

- retry with fee bump
- fallback RPC

### 3.8.3 Adaptive

```
if inclusion_delay ↑  → increase priority fee
if failure_rate ↑     → reduce concurrency
```

---

This layer converts **allocated intents → confirmed on-chain positions**. It must be **fast, deterministic, and failure-tolerant**, while avoiding nonce collisions.

### 3.8.4 Objectives

- Submit transactions with **predictable ordering and minimal latency**
- **Eliminate nonce contention** via wallet sharding
- **Bound concurrency**
- **Guarantee idempotency** (no duplicate fills)
- Adapt fees/concurrency based on observed inclusion & failures

### 3.8.5 Inputs → Outputs

**Input:** `AllocationDTO[]`, chain config (router address, gas params), latest pool state (cached).

**Output**

```go
type ExecutionResultDTO struct {
    TokenAddress    string
    Wallet          string
    TxHash          string
    Nonce           uint64
    EntryPrice      float64
    AmountIn        float64
    AmountOutMin    float64
    GasUsed         uint64
    PriorityFeeGwei float64
    LatencyMs       int
    Status          string // submitted | included | failed
    ErrorCode       string
    Version         int
    Timestamp       int64
}
```

### 3.8.6 Wallet Sharding (Nonce Isolation)

```
wallet_pool = [W1, W2, W3, ... Wn]
```

- Each wallet has independent nonce stream
- **One in-flight tx per wallet** (or small queue)

**Assignment (deterministic)**

```
hash(TokenAddress) % n → wallet index
or round-robin over available wallets
```

**Invariants:** per-wallet strictly increasing nonce; no concurrent sends with same nonce.

### 3.8.7 Prebuilt Calldata (Zero-Compute on Hot Path)

Build ahead (when edge validated): router method, path (WETH → token), `amountIn`, `amountOutMin`, deadline.

```go
type CallSpec struct {
    To           string
    Data         []byte
    Value        uint256
    AmountOutMin uint256
    Deadline     uint64
}
```

**AmountOutMin**

```
AmountOutMin = quote_out × (1 - slippage_tolerance)
```

No recomputation during submission.

### 3.8.8 Bounded Parallelism

```
concurrency_limit = 5–20 (configurable)
```

Global semaphore caps in-flight submissions. Per-wallet queue length capped.

**Scheduler**

```
acquire(global_sema)
wallet := pickAvailableWallet()
nonce  := nextNonce(wallet)
sendTx(wallet, nonce, CallSpec, feeParams)
release(global_sema when submitted)
```

### 3.8.9 Fee Strategy (EIP-1559 / chain equivalent)

```
maxFee      = baseFee * m + priorityFee
priorityFee = p0 (baseline)

Priority tiers:
p0: normal
p1: fast
p2: urgent
```

Choose tier based on `LatencyProfileDTO` and congestion estimate.

### 3.8.10 Submission & Tracking

**States:** `created → signed → submitted → pending → included | failed`.

Store `txHash`, `wallet`, `nonce`. Poll/subscribe for inclusion. Measure `LatencyMs = submit_ts → included_ts`.

### 3.8.11 Failure Handling

**Retry with Fee Bump (Same Nonce)**

```
if pending_time > T_retry:
    resend same nonce with higher maxFee/priorityFee

priorityFee_new = priorityFee_old × (1 + δ)
maxFee_new      = maxFee_old × (1 + δ)
(δ ≈ 10–20%)
```

Max retries bounded (2–3).

**Revert / Execution Failure.** Mark failed, do NOT retry same calldata. Optionally recompute tighter size next cycle.

**RPC Failure.** Multiple RPC endpoints (A/B/C). On send error, try next RPC with same signed tx. Circuit-break unhealthy RPCs.

**Nonce Desync.** Resync from chain, requeue tx with new nonce.

### 3.8.12 Idempotency & Dedup

- Each `AllocationDTO` has unique `execution_id`
- If already submitted → skip
- Prevent duplicate buys on retries

### 3.8.13 Adaptive Controls

**Inclusion Delay → Fee Adjustment**

```
if avg_inclusion_delay ↑:
    increase priorityFee tier (p0→p1→p2)
```

Bounded: cap max priority fee, decay back when normal.

**Failure Rate → Concurrency Adjustment**

```
if failure_rate ↑:
    concurrency_limit -= step
if stable and low latency:
    concurrency_limit += step
```

Bounds: min 3–5, max 20.

**Slippage Misses → Size Adjustment (signal back).** Emit signal to reduce `amountIn` (feeds Layer 7 next cycle).

### 3.8.14 Determinism Guarantees

- Wallet assignment deterministic
- Nonce progression linear per wallet
- Retry policy fixed and bounded
- Same inputs + same network conditions → same submission sequence

### 3.8.15 Observability

**Per tx:** submit_ts, included_ts, latency_ms, priority_fee, max_fee, slippage_est vs realized, status, error_code, wallet, nonce.

**Aggregates:** inclusion delay p50/p95, failure rate by reason, RPC health.

### 3.8.16 Performance Targets

- submit latency (local): **< 20–50 ms**
- inclusion delay: market-dependent, monitored
- throughput: up to `concurrency_limit` txs in parallel

### 3.8.17 Failure Modes & Guards

- **Nonce contention** → strict per-wallet queue
- **Mempool thrash** → bounded concurrency
- **Fee underbidding** → adaptive bumps
- **Overpaying fees** → capped tiers + decay
- **Duplicate execution** → idempotency key
- **RPC flakiness** → multi-endpoint fallback

### 3.8.18 What This Layer Guarantees

- One clean attempt per opportunity, retried safely if needed
- No nonce collisions, predictable ordering
- Controlled parallelism under load
- Adaptive fees to maintain inclusion without runaway costs

**Key takeaway.** You don't get paid for deciding fast. You get paid for getting filled correctly.

---

### 3.8.19 Nonce Manager

**Purpose.** Guarantee strictly increasing, gap-free nonces per wallet with zero collisions under concurrent submission.

**State (per wallet):**

```sql
CREATE TABLE wallet_nonce_state (
    wallet_address      TEXT      PRIMARY KEY,
    chain               TEXT      NOT NULL,
    next_nonce          BIGINT    NOT NULL,          -- next value to use
    pending_nonces      BIGINT[]  NOT NULL DEFAULT '{}',  -- nonces awaiting confirmation
    last_confirmed_nonce BIGINT   NOT NULL,
    last_sync_at        TIMESTAMP NOT NULL
);
```

**Allocation rule (atomic):**

```sql
UPDATE wallet_nonce_state
SET next_nonce     = next_nonce + 1,
    pending_nonces = array_append(pending_nonces, next_nonce)
WHERE wallet_address = $addr
RETURNING next_nonce - 1 AS allocated_nonce;
```

**Invariants:**

- At most one in-flight tx per nonce per wallet (§ 3.8.6 Wallet Sharding remains authoritative)
- Nonces monotonically increase — no gaps, no reuse
- On confirmation: remove nonce from `pending_nonces`, update `last_confirmed_nonce`
- **Periodic reconciliation** against on-chain state (`eth_getTransactionCount`) at interval `nonce_sync_interval` (config, default: 10s) — drift detection → alert + force-resync

**Collision prevention:**

- All nonce allocations go through the database CAS path above — never via in-memory counters
- On wallet restart or process crash: reload `next_nonce` from DB, then reconcile with `eth_getTransactionCount(pending)` before submitting any tx

---

### 3.8.20 Gas Strategy

**Dynamic gas estimation (per tx, pre-submit):**

```
base_fee       = eth_feeHistory(latest).base_fee_per_gas[-1]
priority_fee   = max(min_priority_tip, percentile(recent_tips, 50))
gas_limit      = eth_estimateGas(calldata) × gas_limit_safety_margin   // e.g. 1.2
max_fee        = base_fee × max_fee_multiplier + priority_fee          // e.g. base×2 + tip
```

All multipliers, percentiles, and safety margins MUST live in `config/gas.yaml` — never hardcoded.

**Priority fee adjustment (adaptive):**

Tracked per chain as a rolling window of the last `N` successful inclusion tips:

```
desired_tip_percentile = config.gas.tip_percentile            // default: 60
priority_fee = percentile(last_N_confirmed_tips, desired_tip_percentile)
```

If `confirmation_latency_p95` exceeds target, controller increases `desired_tip_percentile` within bounds `[tip_min_pct, tip_max_pct]`.

**Retry with higher gas (see § 3.8.22 Replacement):**

```
attempt N:  max_fee_N = max_fee_{N-1} × fee_bump_multiplier    // default: 1.15
            priority_fee_N = priority_fee_{N-1} × fee_bump_multiplier
```

Gas bumps are bounded by `max_fee_cap` and `max_priority_cap` (config) — never unbounded.

**Daily gas spend cap per wallet:**

If a wallet's daily gas spend exceeds `wallet_daily_gas_cap` (config), the wallet is temporarily removed from the active shard pool. See § 4.9.2 Gas Budget.

---

### 3.8.21 Transaction Replacement

**When to replace:**

A pending tx is considered stuck if:

```
now - tx.submitted_at > replacement_threshold_ms   (config, default: 8000ms)
AND tx.status = pending
```

**How to replace (same nonce, higher gas):**

1. Construct a new tx with identical `(to, data, value, nonce)` but bumped `(max_fee, priority_fee)` per § 3.8.20
2. Submit via RPC — node accepts because new fee ≥ `replacement_fee_floor` (typically old + 10%)
3. Track both tx hashes under the same nonce until one confirms

**Cancel tx (abort opportunity):**

If the opportunity is no longer valid (e.g., price moved past edge expiry):

1. Construct a self-transfer tx: `(to=wallet, data=0x, value=0, nonce=stuck_nonce)` with bumped fee
2. This "cancels" the stuck tx by consuming its nonce with a no-op
3. Emit `execution_cancelled_event` with reason

**Bounds:**

- Max replacements per tx: `max_replacements` (config, default: 3)
- After max replacements, tx is considered `FAILED` (§ 4.7.3 transitions to `FAILED` state)

---

### 3.8.22 Mempool Strategy

**Default: public mempool.** Standard RPC submission via `eth_sendRawTransaction`.

**Optional: private RPC routing (anti-sandwich protection).**

For tokens flagged as high-sandwich-risk (e.g., large expected price impact), tx may be routed through a private relay:

```yaml
execution:
  private_rpc:
    enabled: true
    endpoints:
      - "https://relay.flashbots.net"
      - "https://rpc.beaverbuild.org"
    route_threshold_usd: 500 # route if expected position size > $500
```

**Selection logic (deterministic):**

```
if EstimatedPositionUSD >= config.execution.private_rpc.route_threshold_usd
   AND config.execution.private_rpc.enabled:
    submit_via_private_relay()
else:
    submit_via_public_mempool()
```

**Anti-sandwich guard (public mempool):**

- Set `max_slippage` in tx calldata to tight bound (e.g., 2%) — sandwich that exceeds bound reverts tx safely
- Slippage bound comes from `SlippageEstimateDTO.p95_slippage × safety_multiplier` (config)

**Private relay MUST NOT be the only submission path** — all configured endpoints must have a public fallback per § 3.8.11 RPC fallback rules.

---

### 3.8.23 Retry Logic (Formalized)

```
attempt = 0
while attempt < config.execution.max_attempts:
    allocate_nonce()
    submit(tx)
    wait(inclusion_timeout_ms)

    if confirmed:
        return SUCCESS

    if reverted:
        // reversion indicates semantic failure (slippage exceeded, pool state changed)
        if reason_is_retriable(reason):
            attempt += 1
            continue
        return FAILED

    if stuck:
        replace(tx, bumped_fee)    // § 3.8.21
        attempt += 1
        continue

return FAILED_MAX_ATTEMPTS
```

**Fallback strategy on terminal failure:**

1. Mark `TokenState` as `FAILED` per § 4.7.3
2. Emit `execution_failed_event` with full attempt history
3. Release wallet shard reservation (§ 3.8.6)
4. Feed outcome to Layer 10 Learning Engine as a negative sample
5. Do NOT auto-retry the opportunity — the edge window has already decayed

---

### 3.8.24 Extended Output DTO

`ExecutionResultDTO` is extended (additive, backward-compatible) with realism fields:

```go
type ExecutionResultDTO struct {
    // ... existing fields preserved unchanged ...

    // Extended realism fields (additive):
    TxHash           string  // final confirmed tx hash (or last attempted if failed)
    Attempts         int     // total submission attempts (including replacements)
    FinalGasUsed     uint64  // gas units consumed by confirmed tx
    FinalMaxFeeWei   uint64  // max_fee used on final attempt
    FinalPriorityWei uint64  // priority_fee used on final attempt
    Replaced         bool    // TRUE if tx was replaced at least once
    ReplacementCount int     // number of replacements performed
    MempoolRoute     string  // "public" | "private:<endpoint>"
    NonceUsed        uint64  // nonce consumed (for audit)
    WalletShard      string  // wallet address that submitted the tx
}
```

**Additive-only rule:** existing fields remain unchanged — consumers of the prior DTO shape continue to function. Version bump: `ExecutionResultDTO.Version` increments per § 4.5 Data Contracts.

---

### 3.8.25 Execution Failure Modes (Expanded)

| Failure Mode      | Detection                                             | Mitigation                                                             |
| ----------------- | ----------------------------------------------------- | ---------------------------------------------------------------------- |
| Stuck tx          | No inclusion within `replacement_threshold_ms`        | Replace with bumped fee (§ 3.8.21); max 3 replacements                 |
| Dropped tx        | Tx missing from mempool after submission              | Re-submit with same nonce; if persistent, switch RPC endpoint          |
| Nonce gap         | `next_nonce` > `last_confirmed_nonce + pending_count` | Force-resync via `eth_getTransactionCount`; halt wallet until resolved |
| Nonce reuse       | Two pending tx with same nonce                        | IMPOSSIBLE per § 3.8.19 atomic allocation — would be a bug             |
| Reverted tx       | On-chain tx execution reverted                        | Parse revert reason; retry only if retriable                           |
| Underpriced tx    | Node rejects `replacement_fee_floor`                  | Bump by full `fee_bump_multiplier`; retry                              |
| Sandwich attack   | Large unexpected slippage                             | Slippage cap in calldata reverts safely; consider private RPC          |
| RPC endpoint down | All endpoints return error                            | Circuit breaker halts submissions; emit `[ALERT]`                      |

All thresholds and caps MUST be loaded from `config/execution.yaml`.

---

## 3.9 Position Engine (Layer 9)

### 3.9.1 Rules (baseline)

- **TP1:** +20% (partial)
- **TP2:** +50% (rest)
- **SL:** -10%
- **TIME:** 10–30 min

### 3.9.2 Adaptive

- tune TP/SL/time per cohort (liquidity band, tax band)

### 3.9.3 Feedback

- optimal exit vs realized exit

---

This layer is where **profit is realized or destroyed**. Entry gives potential; exit determines actual PnL.

```
maximize realized PnL under uncertainty + time decay
```

### 3.9.4 Objective (Formal)

```
ExitDecision = argmax(realized_value - risk_of_reversal - time_decay)
```

### 3.9.5 State Model

```go
type PositionState struct {
    TokenAddress  string
    EntryPrice    float64
    CurrentPrice  float64
    PeakPrice     float64
    PnL           float64
    AgeSec        int
    Size          float64
    RemainingSize float64
    ExitStage     string // none | TP1_hit | TP2_hit | closed
    CohortID      string
}
```

### 3.9.6 Baseline Exit Rules (Deterministic)

**TP1:** `+20%` → sell 50% position

**TP2:** `+50%` → sell remaining 50%

**SL:** `-10%` → sell 100%

**TIME (critical for sniper):** `10–30 minutes`

```
if AgeSec ≥ T_max → force exit
```

**Trailing Protection (after TP1)**

```
lock profit:
if price drops X% from peak → exit
Example: trail = 10–15% from peak
```

### 3.9.7 Exit Decision Logic (Priority Order)

```
if SL hit:
    exit_all
else if TP2 hit:
    exit_all
else if TP1 hit and not yet executed:
    exit_partial
else if trailing_stop_triggered:
    exit_all
else if time_expired:
    exit_all
```

### 3.9.8 Price Tracking (Real-time Loop)

Must track current price, peak price since entry, time since entry.

```
on price update:
    update PeakPrice
    recompute PnL
    evaluate exit rules
```

**Latency requirement:** < 200–500 ms reaction.

### 3.9.9 Adaptive Exit (Core Intelligence)

**Cohort Definition:** liquidity/tax/entropy/latency/momentum bands.

**Cohort Metrics:** `avg_peak_return`, `avg_time_to_peak`, `avg_drawdown_after_peak`, `win_rate`.

**Adaptive TP/SL — Example Adjustments**

```
if cohort peaks early (≤ 3 min):
    TP1 = 15%, TP2 = 30%, TIME = 10 min

if cohort trends longer:
    TP1 = 25%, TP2 = 60%, TIME = 20–30 min

if volatile:
    SL tighter (e.g., -7%)
```

**Parameter Update (Bounded)**

```
param_new = param_old + α × (observed - expected)
```

Constraints: small α (0.05), bounded range, require N samples.

### 3.9.10 Exit Efficiency

```
ExitEfficiency = realized_pnl / max_possible_pnl
```

Goal: maximize efficiency without increasing risk.

### 3.9.11 Feedback Loop

**Track per trade:** `entry_price`, `exit_price`, `peak_price`, `time_to_peak`, `time_to_exit`.

**Compute**

- **Missed Profit:** `missed = peak_price - exit_price`
- **Overstay Loss:** `overstay = exit_price - peak_after_exit`

**Learning Signals**

- **Case 1 — Exit too early** (peak >> exit) → increase TP thresholds
- **Case 2 — Exit too late** (exit << peak) → tighten TP / add trailing
- **Case 3 — Time decay** (profit early, then flat/down) → shorten TIME window

### 3.9.12 Time Decay Model

Most sniper trades follow: `pump → peak → dump`.

```
ExpectedValue(t) = peak × exp(-λ × t)
```

**Implication:** holding too long reduces EV.

### 3.9.13 Failure Modes & Guards

| Failure               | Symptom               | Fix                              |
| --------------------- | --------------------- | -------------------------------- |
| Greed (hold too long) | give back profit      | enforce time exit; trailing stop |
| Fear (exit too early) | low efficiency        | adjust TP upward per cohort      |
| No exit discipline    | inconsistent outcomes | deterministic rule enforcement   |
| Ignoring volatility   | SL too wide/tight     | volatility-aware SL              |

### 3.9.14 Execution of Exit

Exit uses same execution engine (Layer 8) with prebuilt sell calldata and priority fee logic. Partial exits handled as separate tx.

### 3.9.15 Performance Constraints

- decision latency: < 50 ms
- price update loop: < 200 ms
- no heavy computation

### 3.9.16 What This Layer Guarantees

- Profits captured, not theoretical
- Losses bounded
- System adapts exit timing to reality
- Avoids holding too long / exiting randomly

**Final Insight.** Entry gives opportunity. Exit defines outcome. Most systems focus on entry. Your edge compounds here: _better exits × thousands of trades = massive difference_.

---

## 3.10 Learning Engine (Layer 10)

### 3.10.1 Inputs

From **all layers**: decisions, features, execution metrics, outcomes (PnL, duration), failure types.

### 3.10.2 Core Outputs

- **threshold adjustments**
- **feature weights**
- **model calibration**
- **execution params**
- **capital sizing rules**

### 3.10.3 Key Analyses

- **A. False Negatives** — rejected → later pump
- **B. False Positives** — accepted → loss/rug
- **C. Cohort Analysis** — group by liquidity/tax/concentration/latency bands; compute win rate, expectancy

### 3.10.4 Safety

- updates are **bounded**
- applied via **versioned config**
- require **minimum sample size**

---

This is the **only component that turns your system from static → compounding**. Everything upstream generates data; this layer converts it into **controlled parameter updates**.

### 3.10.5 Inputs (Canonical Dataset)

All inputs must be **joined into a single training row per trade (and per rejected candidate)**.

```go
type LearningRecord struct {
    TokenAddress string
    Version      int

    // Decisions across layers
    DataQualityDecision string
    Score               float64
    Selected            bool
    AllocatedSize       float64

    // Features (snapshot at decision time)
    FeatureVector map[string]float64

    // Execution metrics
    SlippageExpected float64
    SlippageActual   float64
    LatencyExpected  float64
    LatencyActual    float64
    TxSuccess        bool

    // Outcome
    EntryPrice  float64
    ExitPrice   float64
    PeakPrice   float64
    PnL         float64
    DurationSec int

    // Labels
    OutcomeLabel string // success | fail | rug | missed

    // Meta
    CohortID  string
    Timestamp int64
}
```

**Inclusion Rules.** You MUST store:

1. accepted trades (executed)
2. rejected trades (shadow tracked)

Without rejected samples → you cannot compute **false negatives**.

### 3.10.6 Core Outputs (Control Knobs)

This engine only updates **5 things**:

**Threshold Adjustments:** `θ_p`, `θ_s`, `θ_latency`, `θ_ev`, data quality thresholds.

**Feature Weights:** used in scoring (Layer 3), probability model (Layer 4).

**Model Calibration:** probability calibration, slippage coefficients, latency estimates.

**Execution Params:** priority fee baseline, retry policy, concurrency limit.

**Capital Sizing Rules:** cohort multipliers, max position size adjustments.

### 3.10.7 Key Analyses

#### 3.10.7.A False Negatives (Missed Opportunities)

**Definition**

```
Decision = reject
AND peak_return ≥ threshold (e.g., +30% within T)
```

**Metrics**

```
FN_rate   = false_negatives / total_rejects
MissedPnL = Σ (peak_return of FN)
```

**Attribution:** determine **why rejected** (probability / slippage / latency / data quality).

**Action**

```
if FN_rate ↑:
    ↓ probability threshold
    ↑ slippage tolerance (slightly)
    relax data quality filters (carefully)
```

#### 3.10.7.B False Positives (Bad Trades)

**Definition**

```
Decision = accept
AND PnL < 0 OR rug
```

**Metrics**

```
FP_rate  = losing_trades / total_trades
Rug_rate = rugs / total_trades
```

**Attribution:** low entropy? high tax? bad liquidity? overestimated probability? slippage underestimated?

**Action**

```
if FP_rate ↑:
    ↑ probability threshold
    ↓ slippage cap
    tighten data quality filters
```

#### 3.10.7.C Cohort Analysis (Where Edge Exists)

**Cohort Definition:** liquidity/tax/concentration/latency/momentum bands.

**Aggregation**

```sql
SELECT cohort_id,
       COUNT(*) as trades,
       AVG(pnl) as avg_pnl,
       SUM(CASE WHEN pnl > 0 THEN 1 ELSE 0 END)/COUNT(*) as win_rate
FROM learning_records
GROUP BY cohort_id
```

**Action**

```
if cohort_expectancy > 0: increase capital multiplier
if cohort_expectancy < 0: decrease multiplier; possibly block cohort
```

### 3.10.8 Model Calibration

**Probability Calibration.** Goal: `Predicted P ≈ Actual success rate`.

```
E     = actual_outcome - predicted_P
w_new = w_old + α × E × feature
```

Constraints: small α (0.01–0.05), normalized weights.

**Slippage Calibration**

```
E_slip = actual - expected
k_new  = k_old + β × E_slip
```

**Latency Calibration**

```
latency_estimate = EMA(actual_latency)
```

### 3.10.9 Execution Learning

**Metrics:** `failure_rate`, `inclusion_delay`, `slippage_error`.

**Actions**

```
if inclusion_delay ↑: increase priority fee baseline
if failure_rate ↑:    reduce concurrency
```

### 3.10.10 Capital Learning

```
multiplier_new = multiplier_old + α × expectancy
```

Bounded: `[0.1, 1.5]`.

### 3.10.11 Update Pipeline

**Frequency:** every N trades OR every T minutes (e.g., 50 trades / 15 min).

**Steps**

```
1. Aggregate metrics
2. Compute errors (FP, FN, calibration)
3. Propose updates
4. Validate constraints
5. Apply new version
```

### 3.10.12 Safety (Non-Negotiable)

- **Bounded Updates:** `Δparameter ≤ 5–10% per cycle`
- **Minimum Sample Size:** `N ≥ 30–50 before update`
- **Versioning:** `config_version++` with snapshot
- **Rollback:** if performance ↓, revert to previous version
- **Isolation:** updates per cohort / per feature; avoid global changes unless strong signal

### 3.10.13 Output (Config Update)

```go
type StrategyConfig struct {
    Version            int
    Thresholds         map[string]float64
    FeatureWeights     map[string]float64
    SlippageParams     map[string]float64
    LatencyParams      map[string]float64
    CapitalMultipliers map[string]float64
}
```

### 3.10.14 Failure Modes

- **No learning (AQ = 0)** — system repeats mistakes
- **Overfitting** — reacts too fast → unstable
- **Underfitting** — reacts too slow → stagnant
- **Wrong attribution** — fixes wrong parameter → degrades system

### 3.10.15 What This Layer Guarantees

- System improves over time
- Mistakes are not repeated
- Capital flows to working patterns
- Bad patterns are suppressed

**Final Insight.** Your edge is not your strategy. Your edge is **how fast you correct your strategy**. If correct: early performance may be average; long-term performance compounds exponentially.

---

## 3.11 Multi-Market Architecture (EVM + Solana)

> **Status:** Cross-cutting addendum to Layers 0–10. Introduced by Phase 7 (Solana). Defines how additional execution+ingestion domains plug into the existing pipeline **without modifying** Layers 1–7, 9, 10 or any DTO schema.

### 3.11.1 Definition of "Market"

A **market** is an isolated `(ingestion + execution)` domain identified by a stable label. The pipeline runs one logical instance per market; cross-market state sharing is forbidden.

| Market label | Family | Examples                                        |
| ------------ | ------ | ----------------------------------------------- |
| `evm`        | EVM    | `eth-uniswap-v2`, `bsc-pancake-v2` (Phases 1–6) |
| `solana`     | SVM    | `solana-raydium`, `solana-pumpfun` (Phase 7)    |

`MarketDataDTO.Chain` is the canonical discriminator: `eth | bsc | solana`. Future markets extend this enumeration additively — no DTO field is renamed or removed.

### 3.11.2 Core Invariant (Multi-Market)

```
∀ market M ∈ {evm, solana}:
  ingestion(M)  → MarketDataDTO            (chain = M.chain)
  execution(M)  → ExecutionResultDTO       (chain = M.chain)
  Layers 1..7, 9, 10  ARE INVARIANT under M  (chain-agnostic)
```

**Reading rule:** Layers 1–7, 9, 10 MUST never branch on `chain`. They consume normalized DTOs and produce normalized DTOs. If a layer needs chain-specific behaviour, that behaviour belongs in Layer 0 (normalize away) or Layer 8 (route by chain).

### 3.11.3 Per-Market Isolation (STRICT)

Each market owns:

| Responsibility             | Scope                                                                            |
| -------------------------- | -------------------------------------------------------------------------------- |
| Ingestion engine (Layer 0) | One module per market family — `ingestion/` (EVM), `ingestion_solana/` (SVM)     |
| Execution engine (Layer 8) | One module per market family — `execution/` (EVM), `execution_solana/` (SVM)     |
| RPC client pool            | Independent per market — separate endpoints, circuit breakers, budgets           |
| Configuration              | Independent YAML keys — `config/chains.yaml::ethereum`, `…::solana`              |
| Wallet pool                | Independent — EVM uses secp256k1 + nonce; Solana uses ed25519 + recent blockhash |
| Worker groups              | Optional `chain` filter on `ClaimNextEvent`; partitioning is additive            |

**Forbidden cross-market couplings:**

- ❌ Solana logic importing from `internal/modules/ingestion/` (EVM)
- ❌ EVM logic importing from `internal/modules/ingestion_solana/`
- ❌ Shared mutable state between markets (e.g. global nonce table touched by SVM)
- ❌ Cross-market position aggregation outside the chain-agnostic Capital Engine (Layer 7) which already operates on normalized exposures

### 3.11.4 Shared Pipeline (Chain-Agnostic Layers)

Shared layers operate solely on normalized DTOs and remain **untouched** by Phase 7:

```
Layer 0  (Ingestion)         MARKET-SPECIFIC  →  emits MarketDataDTO
Layer 1  (Data Quality)      SHARED
Layer 2  (Feature Extraction)SHARED
Layer 3  (Edge Discovery)    SHARED
Layer 4  (P/S/L Models)      SHARED
Layer 5  (Edge Validation)   SHARED
Layer 6  (Selection)         SHARED
Layer 7  (Capital)           SHARED
Layer 8  (Execution)         MARKET-SPECIFIC  →  routed by allocation.Chain
Layer 9  (Position)          SHARED  (with chain-aware price-fetch adapter)
Layer 10 (Learning)          SHARED
```

The chain discriminator on every DTO already exists (`MarketDataDTO.Chain`, `EdgeDTO.Chain`, `AllocationDTO.Chain`). No new DTO fields are introduced.

### 3.11.5 Execution Routing (Layer 8)

The Execution Engine becomes a thin router:

```go
// Pseudocode — internal/modules/execution/router.go (Phase 7 addition)
func Execute(ctx context.Context, alloc contracts.AllocationDTO) (contracts.ExecutionResultDTO, error) {
    switch alloc.Chain {
    case "eth", "bsc":
        return evm.Execute(ctx, alloc)        // existing Phase 2/6 path
    case "solana":
        return solana.Execute(ctx, alloc)     // Phase 7 addition
    default:
        return contracts.ExecutionResultDTO{}, ErrUnsupportedChain
    }
}
```

**Routing invariants:**

- The router is the **only** chain-aware component in Layer 8.
- Both branches consume the same `AllocationDTO` and emit the same `ExecutionResultDTO` shape.
- Determinism is preserved: same allocation → same routing decision → same on-chain instructions for that family.

### 3.11.6 Solana-Specific Differences (Explicit)

These differences are **contained inside the Solana ingestion + execution modules**. They never leak into Layers 1–7, 9, or 10.

| Concern                  | EVM (Phases 1–6)                            | Solana (Phase 7)                                                         |
| ------------------------ | ------------------------------------------- | ------------------------------------------------------------------------ |
| Account/key model        | secp256k1 ECDSA, 20-byte address            | ed25519, 32-byte pubkey, base58                                          |
| Transaction ordering     | Strictly increasing **nonce** per wallet    | **No nonce** — ordering by `recent_blockhash` + leader slot              |
| Fee market               | EIP-1559 (`max_fee`, `max_priority_fee`)    | Static base fee + optional **priority fee (compute units)**              |
| Tx submission            | `eth_sendRawTransaction` → mempool          | `sendTransaction` → leader RPC → confirmation by signature               |
| Confirmation model       | Block depth (`confirmations >= N`)          | Commitment levels (`processed → confirmed → finalized`)                  |
| Slippage mechanics       | Uniswap V2/V3 `amountOutMin` in calldata    | Raydium/Orca/Jupiter route in instruction data; CLMM-aware               |
| Subscription             | `eth_subscribe(logs)`                       | `logsSubscribe` / `programSubscribe` (Pump.fun, Raydium)                 |
| Decoding                 | ABI-driven topic+data                       | **Borsh** binary deserialization of program instruction data             |
| Failure modes            | Reverted, dropped, replaced, low-gas        | Blockhash expired, dropped before slot, compute-unit OOM, leader skipped |
| Latency target (Layer 0) | p95 ingestion < 500 ms (ETH) / 200 ms (BSC) | p95 ingestion < 800 ms (Solana RPC variance)                             |

**Engineering implications:**

- Phase 7 introduces a separate Solana `LatencyProfileDTO` distribution per chain — but the DTO struct is unchanged; only the calibration data differs.
- The EVM nonce manager (`AllocateNonce`, `ReconcileNonce`) is **not extended** for Solana. Solana ordering uses an independent recent-blockhash adapter (Phase 7 addition).
- Solana confirmation tracking emits the same `ExecutionResultDTO.Status` values (`confirmed | reverted | dropped | failed`); Solana-specific reasons are stored in `ExecutionResultDTO.RejectReason` / `FailureCategory`.

### 3.11.7 Determinism Guarantee (Multi-Market)

For each market M, replay determinism is preserved per the Phase 0 contract:

```
∀ MarketDataDTO d emitted by ingestion(M):
  EventID(d) = SHA256(d.chain || d.tx_hash || d.log_index)[:16]   (EVM)
             = SHA256(d.chain || d.signature || d.instruction_index)[:16]  (Solana)
```

Both forms collapse into the same canonical `EventID` rule: a content hash over the chain-natural ordering keys. Cross-market `EventID` collisions are statistically negligible (`chain` is part of the hash).

### 3.11.8 Failure Modes (Multi-Market)

| Failure                                         | Containment Rule                                                                         |
| ----------------------------------------------- | ---------------------------------------------------------------------------------------- |
| Solana RPC outage                               | EVM ingestion + execution continue unaffected                                            |
| EVM gas spike halts execution                   | Solana execution continues unaffected                                                    |
| Solana logic accidentally reads EVM nonce table | Detected by import-graph validation in `dependency-analysis` skill — phase rollback      |
| Cross-market state contamination                | Adapter rejects writes that mismatch the `chain` field on DTO                            |
| Capital Engine over-allocation across markets   | Layer 7 envelope is chain-agnostic and **already** caps total exposure across all chains |

### 3.11.9 What This Section Does NOT Change

Explicit non-goals — preserved invariants:

- Layers 1–7, 9, 10 unchanged (zero diff to Phase 2–5 module code).
- `contracts/*.go` schemas unchanged (only `Chain` enumeration is widened — additive value).
- `database.Adapter` interface unchanged. `AllocateNonce` / `ReconcileNonce` remain EVM-only by design (callers gate on `chain ∈ {eth, bsc}`).
- Event bus schema unchanged. No new event types are required for Phase 7 — Solana ingestion emits `market_data_event` and Solana execution emits `execution_event`, identical to EVM.
- Operational modes (STRICT / BALANCED / EXPLORATION / VERY_EXPLORATION) apply uniformly across markets.

### 3.11.10 Solana Ingestion & Execution Guarantees (Production-Grade)

This subsection elevates Phase 7 from "multi-market capable" to **multi-market production-grade**. The guarantees below are normative for the Solana ingestion + execution modules and are enforced by Phase 7 exit criteria.

#### 3.11.10.1 Exactly-Once Ingestion (Idempotent)

```
∀ on-chain event e (signature s, instruction index i):
  EventID(e) = SHA256("solana" || s || i)[:16]
  events table: PRIMARY KEY (event_id) — duplicates collapse on INSERT ... ON CONFLICT DO NOTHING
```

**Properties:**

- A single on-chain event observed by N concurrent ingestion workers (or N times by the same worker after reconnect) produces **exactly one** row in `events`.
- Cross-RPC duplicate observation (primary + fallback both emit the same log) is collapsed by `EventID`.
- Replay of fixture logs after a crash produces zero new rows when input is unchanged.

#### 3.11.10.2 Ordering Guarantee

Solana has no global block-level total order, but per-account/per-program ordering is well-defined:

```
Order key (Solana) = (slot ASC, signature ASC, instruction_index ASC)
```

The Solana ingestion worker MUST emit `MarketDataDTO` in monotonically non-decreasing `(slot, signature, instruction_index)` order **per (program_id, account)**. Cross-program ordering is not guaranteed and is not required (downstream layers consume DTOs by `EventID`, not arrival order).

#### 3.11.10.3 Connection Resilience

- Persistent WebSocket subscription with **exponential backoff reconnect** (initial 200 ms, max 30 s, multiplier 2.0, full jitter).
- On reconnect, the worker resumes from the last persisted **watermark** (`solana_ingestion_watermark.slot`).
- Gap recovery via HTTP RPC (`getSignaturesForAddress`) replays missed signatures between `watermark.slot` and `currentSlot`. Replay events flow through the same `EventID` deduplication path — no special-case logic.
- Health is tracked per endpoint (latency p95, error rate, consecutive failures) — the worker fails over when the active endpoint exceeds configured thresholds.

#### 3.11.10.4 Watermark Consistency

```
Watermark write rule:
  After successful INSERT of a batch of MarketDataDTO with max_slot = M:
    UpsertIngestionWatermark(chain="solana", slot=M)  -- monotonic; never decreases
```

The watermark write happens in the **same transaction** as the event INSERTs, or strictly after a successful event INSERT batch. Watermark MUST never advance past an event that failed to persist.

#### 3.11.10.5 Deterministic Replay

- Replay isolation uses the `replay:` event-type prefix per `replay-engine-pattern` skill.
- Given a fixed input log set + fixed `StrategyVersion`, replay produces **bit-for-bit identical** `events` rows (same `EventID`, same DTO field values, same trace chain).
- Wall-clock `IngestedAt` is sourced from event timestamps during replay, not `time.Now()`.

#### 3.11.10.6 Execution Bounded Retries

```
Solana send loop:
  for attempt in 1..max_attempts (default 3, hard cap 5):
    blockhash = recent_blockhash_cache.Get()
    sig = sign(tx, blockhash, keypair)
    err = rpc.SendTransaction(sig)
    if err is RetriableExpired:    refresh blockhash; continue
    if err is RpcTimeout:          rotate endpoint; continue
    if err is SimulationFailure:   classify and STOP   (no retry)
    if err is ProgramError:        classify and STOP   (no retry)
    break
```

**Hard invariants:**

- Maximum **5** total send attempts per `AllocationDTO.ExecutionID`.
- Each retry uses a **fresh recent blockhash** (never replays a stale one).
- Retries are idempotent at the bus level: `(ExecutionID, signature)` is recorded in `solana_signatures` with `INSERT ON CONFLICT DO NOTHING`.
- After exhaustion, an `ExecutionResultDTO` is emitted with `Status="dropped"` and `FailureCategory` set per §3.11.10.8.

#### 3.11.10.7 Confirmation Strategy

| Commitment level | When emitted as final               |
| ---------------- | ----------------------------------- |
| `processed`      | NEVER — too weak; not authoritative |
| `confirmed`      | **DEFAULT** baseline                |
| `finalized`      | Optional, configurable per-strategy |

The execution worker polls `getSignatureStatuses` until commitment ≥ `confirm_commitment` (default `"confirmed"`) or `confirm_timeout_ms` elapses (default 15 s). On timeout, the signature is recorded with `status="dropped"` and `FailureCategory="confirmation_timeout"`.

#### 3.11.10.8 Failure Classification (Authoritative Enum)

`ExecutionResultDTO.FailureCategory` for `chain="solana"` MUST be one of:

| FailureCategory        | Trigger                                                         | Retriable?            |
| ---------------------- | --------------------------------------------------------------- | --------------------- |
| `blockhash_expired`    | RPC returns "Blockhash not found" or expiry detected            | YES                   |
| `simulation_failure`   | `simulateTransaction` returns error (slippage, account state)   | NO                    |
| `rpc_timeout`          | HTTP/WS call exceeds `rpc_timeout_ms`                           | YES (rotate endpoint) |
| `rpc_circuit_open`     | All endpoints in OPEN state                                     | NO (drop)             |
| `program_error`        | Tx confirmed but tx meta reports program error                  | NO                    |
| `compute_oom`          | Compute-unit limit exhausted                                    | NO                    |
| `leader_skip`          | Tx never landed within target slot window                       | YES (one bump)        |
| `confirmation_timeout` | Signature never reached `confirmed` within `confirm_timeout_ms` | NO                    |

Any other category is a bug. The `learning-engine` (Layer 10) consumes `FailureCategory` for cohort attribution.

#### 3.11.10.9 Latency-Aware Execution Gate

Before submission, the execution router consults the per-endpoint `LatencyProfileDTO`:

```
if profile.p95_latency_ms > config.solana.latency_skip_threshold_ms:
    if alloc.size_usd > config.solana.degraded_size_cap_usd:
        REJECT with reason="latency_degraded_size_cap"
    else:
        DOWNSIZE to degraded_size_cap_usd  (Capital Engine envelope re-checked)
```

This gate is the **only** chain-aware logic in Layer 8 besides the router itself. It is contained in the Solana execution module, not in Layer 7 Capital.

#### 3.11.10.10 Execution Isolation

| Resource                   | Isolation Rule                                                           |
| -------------------------- | ------------------------------------------------------------------------ | --- | ------- |
| Wallet pool                | Solana ed25519 keypairs are SEPARATE from EVM secp256k1 wallets          |
| RPC client pool            | Solana endpoints have OWN circuit breakers, OWN budgets, OWN health      |
| Worker goroutines          | Solana ingestion + execution run in dedicated worker groups              |
| Concurrency semaphore      | Solana has its own `concurrency_limit` (default 10) — independent of EVM |
| Failure containment        | Solana RPC outage → Solana halted; EVM unaffected (and vice versa)       |
| Kill switch (Phase 6)      | SHARED — halts both markets simultaneously                               |
| Capital envelope (Phase 6) | SHARED — caps total exposure across `eth                                 | bsc | solana` |

#### 3.11.10.11 Backpressure

Solana ingestion uses a **bounded channel** between the WS subscriber and the event publisher. On overflow:

```
Priority rule (Solana ingestion):
  1. Pool init / new launch events    → NEVER drop
  2. Swap events on TRACKED tokens    → NEVER drop (downstream may have open positions)
  3. Swap events on UNTRACKED tokens  → DROP-OLDEST when channel >= 80% full
```

Drops are counted in `solana_ingestion_drops_total` and emitted as `system_event` for observability. The drop policy NEVER causes loss of an event that affects an open position.

---

# 4. Meta Systems

These components are not part of the per-trade hot path but are essential for the system to be **safe, auditable, debuggable, and continuously improving**. They wrap the 10 execution layers with: version control, replay, monitoring, operator interface, typed contracts, and failure intelligence.

---

## 4.1 Strategy Versioning & A/B Framework

### 4.1.1 Why This Exists

Without versioning you cannot know what changed, when, or whether it improved the system.

### 4.1.2 Concept

Every configuration update creates a **new strategy version**. Versions are **immutable**. You can **A/B** them in parallel. Winner gets **promoted** (bounded, reversible).

### 4.1.3 StrategyVersion Structure

```go
type StrategyVersion struct {
    ID                string
    ParentID          string
    CreatedAt         int64
    Thresholds        map[string]float64
    FeatureWeights    map[string]float64
    ModelParams       map[string]float64
    CohortMultipliers map[string]float64
    ExecutionParams   map[string]float64
    Metadata          map[string]string
}
```

### 4.1.4 A/B Testing Process

Split traffic:

```
Version V1: 80%
Version V2: 20%
```

**Key condition:** both versions face **same opportunity stream** (same DETECT output) — fairness required. Each version produces its own decisions + trades.

### 4.1.5 Evaluation Metrics (per version)

```
expectancy
win_rate
drawdown
sharpe / sortino (rolling)
rug_loss_rate
fn_rate, fp_rate
```

### 4.1.6 Promotion Rules (bounded)

**Promote V2 if:**

```
expectancy(V2) > expectancy(V1) × (1 + δ)   (e.g., δ=5%)
AND drawdown(V2) ≤ drawdown(V1)
AND N_samples(V2) ≥ N_min (30–100)
```

Otherwise: rollback.

### 4.1.7 Safety

- only ONE new version at a time
- new versions start small (exploration budget)
- all changes are reversible

### 4.1.8 Logging

Every trade logs `strategy_version_id`. Makes PnL attribution per version possible.

---

## 4.2 Replay Engine (Determinism Verification)

### 4.2.1 Purpose

Reproduce exact decisions for debugging and research.

### 4.2.2 Principle

```
same inputs + same config version + same seed → same output
```

System is **deterministic** — a non-negotiable property.

### 4.2.3 Inputs to Replay

- raw blockchain events (logs) — sourced from Layer 0 ingestion records (`market_data_event` entries in the event bus)
- pool states
- historical RPC snapshots
- tx mempool data (if stored)

> **Ingestion determinism requirement:** Replay correctness depends on Layer 0 determinism. The `MarketDataDTO` stream in the event bus is the canonical, ordered, deduplicated representation of on-chain events. Replay MUST feed `MarketDataDTO` records from the event bus directly — never re-subscribe to an RPC node, never re-derive from raw logs at replay time. The ingestion step produces bit-for-bit identical `MarketDataDTO` records for the same on-chain input because: (a) `event_id` is content-addressable, (b) `Timestamp` comes from block headers (not wall clock), (c) normalization is deterministic per config version.

### 4.2.4 What Replay Must Verify

- Layer 0 produces identical `MarketDataDTO` records for the same event bus input
- Layer 1–4 produce identical outputs
- Layer 5 makes identical decisions
- Layer 7 computes identical sizes
- Layer 9 produces identical exit decisions

### 4.2.5 Use Cases

- regression testing
- debugging live anomalies
- strategy tuning
- adversarial analysis

### 4.2.6 Requirements for Replay

- no randomness
- no wall-clock time dependencies (use event timestamps)
- no external nondeterministic calls

---

## 4.3 Opportunity Monitor

### 4.3.1 Why It Exists

In a sniper market, **not trading is itself a risk**. Must detect **starvation** (no trades for too long) and **overtrading** (too many → noise).

### 4.3.2 Metrics Tracked

```
trades_per_hour
accept_rate
reject_rate
selection_rate
capital_utilization
```

### 4.3.3 Rules

**Starvation detection**

```
if accept_rate == 0 for T_min (e.g., 30 min):
    → enter EXPLORATION mode
```

**Overtrading detection**

```
if accept_rate > X%  (e.g., 10%):
    → tighten gates
```

### 4.3.4 Impact

- Controls **system's pulse**
- Prevents silent death (system alive but idle)
- Prevents unhealthy signal flood

---

## 4.4 Observability & Alerting (Telegram + Internal Events)

The Telegram layer is the **only human interface to the system**. Everything goes through the event bus first (see § 2.4).

### 4.4.1 Alert Streams

**A. Ingestion Health (Layer 0)**

```
[INFO]  ingestion started: ethereum (WebSocket) — events/s: 42
[WARN]  ingestion backpressure: buffer at 8200/10000 — dropping Swap events
[WARN]  ingestion latency p99 = 620ms (threshold: 500ms) — ethereum
[WARN]  RPC failures ↑ → switched to fallback endpoint (attempt 2/3)
[ALERT] all RPC endpoints exhausted — ingestion halted: ethereum
[INFO]  WebSocket reconnected — gap recovery: blocks 19802100–19802147 (47 blocks)
[INFO]  chain reorg detected: 2 blocks reorganized — ethereum
```

**Ingestion metrics tracked (per chain):**

```
ingestion_events_per_sec        (rate)
ingestion_latency_p50_ms        (latency)
ingestion_latency_p95_ms
ingestion_latency_p99_ms
ingestion_reconnect_count       (counter, reset daily)
ingestion_gap_recovery_blocks   (total blocks recovered via eth_getLogs)
ingestion_buffer_depth          (current in-process buffer size)
ingestion_dropped_events_total  (swap drops due to backpressure)
```

**B. Trade Signals**

```
[BUY]  TokenX 0.5 ETH @ price=X slippage=2% P=0.72
[SELL] TokenX +28% in 4:32
```

**C. System Events**

```
[ALERT] pass_rate = 0 for 35min → switched to EXPLORATION
[WARN]  RPC failures ↑ → switched fallback endpoint
```

**D. PnL Reports**

```
1h:  +4.2%
4h:  +7.8%
24h: +18.4% (34 trades, WR 52%)
```

### 4.4.2 Command Interface

```
/status
/mode STRICT|BALANCED|EXPLORATION|VERY_EXPLORATION
/pnl
/positions
/executions
/kill
/resume
/version
```

### 4.4.3 Safety

- **all commands require confirmation** for critical actions (e.g., `/kill`)
- commands are logged
- no remote code execution (ever)

### 4.4.4 Why Telegram (vs dashboard)

- real-time, accessible anywhere, low friction, ideal for alerts + control

### 4.4.5 Anti-Pattern (avoid)

- pushing state directly from modules (bypasses event bus) → breaks auditability
- only a **Telegram dispatcher service** reads from event bus → sends messages

---

## 4.5 Data Contracts (Hardened DTO Layer)

### 4.5.1 DTOs (canonical list)

```
DataQualityDTO
FeatureDTO
FeatureConfidence
EdgeDTO
ProbabilityEstimateDTO
SlippageEstimateDTO
LatencyProfileDTO
ValidatedEdgeDTO
SelectionOutput
AllocationDTO
ExecutionResultDTO
PositionState
LearningRecord
StrategyConfig
StrategyVersion
```

### 4.5.2 Rules

See § 2.2. All DTOs: immutable, versioned, schema-validated, no ad-hoc fields.

### 4.5.3 Benefits

- No hidden coupling
- Easy to test each layer in isolation
- Easy to replay
- Easy to evolve

---

## 4.6 Failure Intelligence Layer

### 4.6.1 Idea

Most systems log failures. This system **classifies and learns from failures**.

### 4.6.2 Failure Classes

```
A. Data failures      (RPC, missing logs)
B. Detection failures (wash passed, rug not detected)
C. Execution failures (reverts, stale nonce, gas underbid)
D. Market failures    (peak missed, drawdown excess)
E. System failures    (worker crash, config mismatch)
```

### 4.6.3 Outputs

```go
type FailureEvent struct {
    Class       string
    Layer       string
    Cause       string
    DTOContext  map[string]interface{}
    Severity    string
    ActionTaken string
    Timestamp   int64
}
```

### 4.6.4 Automatic Responses

- **RPC fails** → switch provider
- **Honeypot passed** → tighten filters (increase weight of sell-revert detector)
- **Execution revert spikes** → reduce concurrency, bump fee floor
- **Peak missed** → adjust exit rules per cohort

### 4.6.5 Operator Visibility

- All failure events → Telegram via event bus
- Daily summary report
- Trend analysis (failure rates over time)

---

## 4.7 Token Lifecycle State Machine

### 4.7.1 Purpose

Every token observed by the system MUST traverse a strict, forward-only state machine. This guarantees:

- replay correctness — state transitions are deterministic
- auditability — every token's full history is reconstructible from the event bus
- failure isolation — tokens stuck in invalid states are quarantined, not silently processed
- no double-processing — a token cannot re-enter a state it has already passed

This state machine is the **temporal spine** of the per-token pipeline. The Global Control Loop (§ 1) describes the stage sequence for the system; this state machine describes the stage sequence for each individual token.

---

### 4.7.2 TokenState Enum

```go
type TokenState string

const (
    DETECTED       TokenState = "DETECTED"       // Layer 0 emitted MarketDataDTO
    FILTERED       TokenState = "FILTERED"       // Layer 1 passed Data Quality
    FEATURED       TokenState = "FEATURED"       // Layer 2 emitted FeatureDTO
    EDGE_DETECTED  TokenState = "EDGE_DETECTED"  // Layer 3 emitted EdgeDTO
    VALIDATED      TokenState = "VALIDATED"      // Layer 5 emitted ValidatedEdgeDTO (EV-positive)
    SELECTED       TokenState = "SELECTED"       // Layer 6 included token in SelectionOutput
    EXECUTED       TokenState = "EXECUTED"       // Layer 8 confirmed entry tx on-chain
    POSITION_OPEN  TokenState = "POSITION_OPEN"  // Layer 9 tracking live position
    EXITED         TokenState = "EXITED"         // Layer 9 closed position (TP/SL/TIME)
    EVALUATED      TokenState = "EVALUATED"      // Layer 10 generated LearningRecord
)
```

**Terminal states:** `EVALUATED` is the only terminal state. Tokens rejected at any layer transition to a **terminal rejection state** rather than staying mid-stream — see § 4.7.6.

---

### 4.7.3 Allowed Transitions

```
DETECTED ──▶ FILTERED ──▶ FEATURED ──▶ EDGE_DETECTED ──▶ VALIDATED ──▶ SELECTED ──▶ EXECUTED ──▶ POSITION_OPEN ──▶ EXITED ──▶ EVALUATED
    │            │            │              │               │            │            │              │             │
    ▼            ▼            ▼              ▼               ▼            ▼            ▼              ▼             ▼
 REJECTED    REJECTED    REJECTED       REJECTED       REJECTED      REJECTED     FAILED       FAILED         FAILED
```

**Transition rules (strict):**

1. Only forward transitions along the main path are allowed
2. No state may be skipped — e.g., `FEATURED → VALIDATED` is **invalid** (must pass through `EDGE_DETECTED`)
3. From any non-terminal state, a token may transition to a terminal rejection state (`REJECTED` or `FAILED`)
4. From any terminal state, no further transitions are allowed — invalid input is logged and dropped
5. A token may NOT return to a prior state under any condition — re-entry into the pipeline creates a new `TokenLifecycleID` with fresh history

**Valid transition table:**

| From            | Allowed To                  | Rejected From This State |
| --------------- | --------------------------- | ------------------------ |
| `DETECTED`      | `FILTERED`, `REJECTED`      | Layer 1 rejection        |
| `FILTERED`      | `FEATURED`, `REJECTED`      | Layer 2 quality gate     |
| `FEATURED`      | `EDGE_DETECTED`, `REJECTED` | Layer 3 no-edge          |
| `EDGE_DETECTED` | `VALIDATED`, `REJECTED`     | Layer 5 EV-negative      |
| `VALIDATED`     | `SELECTED`, `REJECTED`      | Layer 6 not in top-K     |
| `SELECTED`      | `EXECUTED`, `FAILED`        | Layer 8 tx failure       |
| `EXECUTED`      | `POSITION_OPEN`             | (always forward)         |
| `POSITION_OPEN` | `EXITED`                    | (always forward)         |
| `EXITED`        | `EVALUATED`                 | (always forward)         |
| `EVALUATED`     | (terminal)                  | —                        |
| `REJECTED`      | (terminal)                  | —                        |
| `FAILED`        | (terminal)                  | —                        |

---

### 4.7.4 State Store (Persisted)

The state machine is persisted in Postgres, keyed by `(Chain, TokenAddress, TokenLifecycleID)`:

```sql
CREATE TABLE token_lifecycle (
    token_lifecycle_id   TEXT        PRIMARY KEY,      -- SHA256(chain+token_address+first_detected_block)[:16]
    chain                TEXT        NOT NULL,
    token_address        TEXT        NOT NULL,
    current_state        TEXT        NOT NULL,          -- TokenState value
    state_version        BIGINT      NOT NULL DEFAULT 1, -- monotonic, increments on each transition
    first_detected_at    TIMESTAMP   NOT NULL,
    last_transition_at   TIMESTAMP   NOT NULL,
    terminal             BOOLEAN     NOT NULL DEFAULT FALSE,
    terminal_reason      TEXT,                          -- non-null only when terminal=TRUE
    strategy_version_id  TEXT        NOT NULL           -- version snapshot at detection
);

CREATE INDEX idx_token_lifecycle_state ON token_lifecycle (current_state, terminal);
CREATE UNIQUE INDEX idx_token_lifecycle_active ON token_lifecycle (chain, token_address)
    WHERE terminal = FALSE;  -- at most one active lifecycle per token
```

**Transition records (append-only audit log):**

```sql
CREATE TABLE token_state_transition (
    transition_id        BIGSERIAL PRIMARY KEY,
    token_lifecycle_id   TEXT      NOT NULL REFERENCES token_lifecycle(token_lifecycle_id),
    from_state           TEXT      NOT NULL,
    to_state             TEXT      NOT NULL,
    state_version        BIGINT    NOT NULL,
    transitioned_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    caused_by_event_id   TEXT      NOT NULL,            -- event bus event that triggered transition
    layer                TEXT      NOT NULL             -- which layer performed the transition
);
```

**Idempotent transition SQL (applied in a single transaction):**

```sql
UPDATE token_lifecycle
SET current_state      = $new_state,
    state_version      = state_version + 1,
    last_transition_at = CURRENT_TIMESTAMP,
    terminal           = $is_terminal,
    terminal_reason    = $terminal_reason
WHERE token_lifecycle_id = $id
  AND current_state      = $expected_from_state    -- CAS guard
  AND state_version      = $expected_version;      -- optimistic lock
-- If row count = 0: transition rejected (either already applied or invalid)
```

---

### 4.7.5 DTO Integration

Every DTO produced by layers 0–10 MUST include the following three fields:

```go
TokenAddress        string      // canonical token identifier (lowercase hex)
State               TokenState  // state the token WILL be in after this DTO is processed
TokenLifecycleID    string      // links this DTO to the lifecycle record
```

**Rules:**

1. A DTO whose `State` does not match the expected target state for its producing layer is **malformed** and MUST be rejected at the event bus write
2. The orchestrator validates the transition `(current_state → DTO.State)` against § 4.7.3 before applying
3. `TokenLifecycleID` is set once at Layer 0 (`SHA256(chain+token_address+first_detected_block)[:16]`) and propagates unchanged through all subsequent DTOs
4. DTOs without these three fields are rejected at the `contracts/` validation boundary

**Layer → State mapping (producer contract):**

| Layer | DTO produced         | DTO.State for successful transition |
| ----- | -------------------- | ----------------------------------- |
| 0     | `MarketDataDTO`      | `DETECTED`                          |
| 1     | `DataQualityDTO`     | `FILTERED` (or `REJECTED`)          |
| 2     | `FeatureDTO`         | `FEATURED`                          |
| 3     | `EdgeDTO`            | `EDGE_DETECTED` (or `REJECTED`)     |
| 5     | `ValidatedEdgeDTO`   | `VALIDATED` (or `REJECTED`)         |
| 6     | `SelectionOutput`    | `SELECTED` (or `REJECTED`)          |
| 8     | `ExecutionResultDTO` | `EXECUTED` (or `FAILED`)            |
| 9     | `PositionState`      | `POSITION_OPEN` → `EXITED`          |
| 10    | `LearningRecord`     | `EVALUATED`                         |

Layer 4 (Probability/Slippage/Latency) does not transition state — it enriches the `EdgeDTO` and contributes to Layer 5's validation decision.

---

### 4.7.6 Replay & Idempotency Guarantees

**Determinism:**

- Given the same ordered `MarketDataDTO` input stream and the same `StrategyVersion`, the state machine produces bit-for-bit identical transition sequences
- No transition depends on wall-clock time — all timestamps come from event data
- Transition order is deterministic: events are consumed in `(BlockNumber, LogIndex)` order per token

**Idempotency:**

- Duplicate input events (identical `event_id`) are dropped at the event bus layer (§ 3.0.7) before they reach the state machine
- Duplicate transition attempts (same `(token_lifecycle_id, from_state, state_version)`) are rejected by the CAS guard in § 4.7.4
- Applying the same transition twice is a no-op — the second attempt finds `current_state ≠ expected_from_state` and returns 0 rows updated

---

### 4.7.7 Failure Handling

**Duplicate events:** dropped silently at the event bus via `ON CONFLICT DO NOTHING` on `event_id`. No state transition occurs.

**Invalid transitions:** logged and quarantined.

```
Trigger condition:
    - DTO arrives with DTO.State = X
    - Current lifecycle state = Y
    - Transition (Y → X) is not in § 4.7.3 table

Action:
    1. INSERT into token_state_violation (token_lifecycle_id, attempted_from, attempted_to, event_id, observed_at)
    2. Do NOT apply the transition
    3. Do NOT mark the event as processed (allows investigation)
    4. Emit telegram_event with severity=WARN
    5. If the same token accumulates ≥ quarantine_threshold violations (config, default: 3),
       mark token_lifecycle.terminal = TRUE, terminal_reason = "quarantined:state_violation"
```

**Stuck tokens:** tokens that remain in a non-terminal state past `state_timeout_seconds` (config, per-state) are transitioned to `FAILED` with `terminal_reason = "timeout:" + state_name`. Timeouts per state MUST live in `config/` — never hardcoded.

**Quarantine policy:** Quarantined tokens are never retried by the live system. They remain in the database for replay analysis and debugging.

---

### 4.7.8 Metrics Tracked

```
tokens_by_state                 (gauge, per state)
state_transitions_per_sec       (rate, per transition)
state_transition_latency_p95    (per layer: event received → transition committed)
invalid_transition_count        (counter, per layer)
quarantined_tokens_total        (counter)
stuck_tokens_count              (gauge, per state beyond timeout)
terminal_reason_distribution    (counter, per terminal_reason)
```

All metrics surface via § 4.4 Observability. Sustained `invalid_transition_count > 0` triggers an `[ALERT]` — it indicates a producer/consumer contract violation.

---

## 4.8 Traceability & Correlation System

### 4.8.1 Purpose

Enable **full causal tracing** across every event, decision, and transition in the system. Every trade must be answerable:

- **Why** was this trade executed? (decision chain)
- **Which signals** triggered it? (input provenance)
- **Which config version** was in effect? (strategy snapshot)
- **What caused** each event? (parent-child relationships)

This is distinct from the Token Lifecycle State Machine (§ 4.7): the state machine tracks **what state** a token is in; traceability tracks **why** each transition happened and **what inputs** produced it.

---

### 4.8.2 Required Fields on All DTOs

Every DTO produced by any layer MUST include the following four correlation fields in addition to § 4.7.5 requirements:

```go
TraceID        string  // full lifecycle trace for one token — equals TokenLifecycleID
CorrelationID  string  // one decision chain (one attempt to trade this token)
CausationID    string  // event_id of the parent event that directly caused this DTO
VersionID      string  // strategy_version_id at the moment of DTO production
```

**Field semantics (non-negotiable):**

| Field           | Scope                              | Lifetime                            | Uniqueness                         |
| --------------- | ---------------------------------- | ----------------------------------- | ---------------------------------- |
| `TraceID`       | One token's entire lifecycle       | From Layer 0 `DETECTED` to terminal | Unique per `TokenLifecycleID`      |
| `CorrelationID` | One decision chain / trade attempt | From `FILTERED` through `EVALUATED` | Unique per decision attempt        |
| `CausationID`   | One parent event                   | Single-event scope                  | Equals upstream event's `event_id` |
| `VersionID`     | Strategy configuration snapshot    | Pinned at DTO production            | Equals `StrategyVersion.id`        |

**Relationships:**

- One `TraceID` may contain multiple `CorrelationID` values if a token re-enters detection (e.g., after being rejected)
- Each `CorrelationID` forms a connected DAG of events linked by `CausationID` chains
- `VersionID` MAY change across events within one `CorrelationID` only if `StrategyVersion` is promoted mid-trade — such transitions are logged as `version_drift_event`

---

### 4.8.3 Event Bus Integration

The `events` table (§ 2.2) is extended with trace metadata columns. All new event bus inserts MUST populate them:

```sql
ALTER TABLE events
    ADD COLUMN trace_id       TEXT NOT NULL,
    ADD COLUMN correlation_id TEXT NOT NULL,
    ADD COLUMN causation_id   TEXT,                      -- NULL only for root events (Layer 0)
    ADD COLUMN version_id     TEXT NOT NULL;

CREATE INDEX idx_events_trace       ON events (trace_id);
CREATE INDEX idx_events_correlation ON events (correlation_id);
CREATE INDEX idx_events_causation   ON events (causation_id);
```

**Root event rule:** Only Layer 0 (`market_data_event`) events have `causation_id = NULL` — they are the roots of all causal chains. Every other event MUST have a non-null `causation_id` pointing to the `event_id` of its direct cause. An event bus write with `causation_id = NULL` from any non-Layer-0 producer is rejected.

**Parent-child guarantee:** For every non-root event `E`, there exists exactly one event `P` such that `E.causation_id = P.event_id`, and `P.trace_id = E.trace_id`.

---

### 4.8.4 Trace Guarantees

The trace system MUST be able to answer the following queries deterministically:

**Q1. Why was this trade executed?**

```sql
-- Given an execution_event, reconstruct the full decision chain
WITH RECURSIVE causal_chain AS (
    SELECT * FROM events WHERE event_id = $execution_event_id
    UNION ALL
    SELECT e.* FROM events e
    JOIN causal_chain c ON e.event_id = c.causation_id
)
SELECT event_type, payload, created_at FROM causal_chain
ORDER BY created_at ASC;
```

**Q2. Which signals triggered this decision?**

```sql
-- Get all Layer 0–3 events contributing to a CorrelationID
SELECT event_type, payload FROM events
WHERE correlation_id = $correlation_id
  AND event_type IN ('market_data_event', 'data_quality_event', 'feature_event', 'edge_event')
ORDER BY created_at ASC;
```

**Q3. Which config version produced this outcome?**

```sql
-- Pin the exact StrategyVersion that was active
SELECT DISTINCT version_id FROM events WHERE correlation_id = $correlation_id;
-- Result must be a single version_id; if multiple → version_drift occurred (investigate)
```

---

### 4.8.5 Observability Query Patterns

**Pattern A — Trace by token:** full history of a token from first sight to learning outcome.

```sql
SELECT event_type, created_at, payload
FROM events
WHERE trace_id = $token_lifecycle_id
ORDER BY created_at ASC;
```

**Pattern B — Trace by decision:** one trade attempt's full causal DAG.

```sql
SELECT event_id, causation_id, event_type, payload
FROM events
WHERE correlation_id = $correlation_id
ORDER BY created_at ASC;
```

**Pattern C — Trace by failure:** what happened leading to a specific failure.

```sql
-- Starting from a failure/quarantine event, walk backwards through causation_id chain
WITH RECURSIVE failure_chain AS (
    SELECT * FROM events WHERE event_id = $failure_event_id
    UNION ALL
    SELECT e.* FROM events e
    JOIN failure_chain f ON e.event_id = f.causation_id
)
SELECT event_type, payload, created_at FROM failure_chain
ORDER BY created_at ASC;
```

**Pattern D — Version impact:** compare behavior across strategy versions.

```sql
-- All trades executed under each version, with outcomes
SELECT version_id, COUNT(*) AS trades, SUM((payload->>'pnl_bps')::int) AS total_pnl
FROM events
WHERE event_type = 'evaluation_event'
GROUP BY version_id;
```

---

### 4.8.6 Trace Metrics

```
trace_completeness_ratio        (% of correlation_ids with a terminal event)
orphan_event_count              (events with invalid causation_id — MUST be 0)
trace_depth_p95                 (causal chain length distribution)
version_drift_incidents         (correlation_ids spanning multiple version_ids)
query_latency_trace_by_token_p95
```

Sustained `orphan_event_count > 0` triggers an `[ALERT]` — it indicates a broken causal chain, which breaks replay and auditability.

---

## 4.9 Runtime Resource Control System

### 4.9.1 Purpose

Prevent the system from exploding cost, overloading RPC endpoints, or saturating compute resources. Every external call and internal queue operates under explicit, config-driven budgets. When budgets are exhausted, controlled degradation prioritizes high-value work.

**Core principle:** The system MUST fail gracefully under overload, never uncontrollably.

---

### 4.9.2 Resource Budgets

**A. RPC Budget (per chain, per endpoint)**

```yaml
resource_control:
  rpc:
    ethereum:
      global_max_rps: 500 # hard ceiling across all endpoints
      per_endpoint_max_rps: 100 # per-endpoint ceiling
      daily_request_cap: 10_000_000
      burst_window_ms: 1000
      burst_allowance: 1.5 # 150% of max_rps for up to burst_window_ms
    bsc:
      global_max_rps: 300
      per_endpoint_max_rps: 60
      daily_request_cap: 5_000_000
```

Enforcement: token bucket per endpoint, refilled at `per_endpoint_max_rps`. On bucket empty → request queued up to `rpc_queue_max`, else dropped.

**B. Gas Budget**

```yaml
resource_control:
  gas:
    max_gas_per_trade: 1_500_000 # gas units
    wallet_daily_gas_cap: 50_000_000 # per wallet
    system_daily_gas_cap: 500_000_000 # all wallets combined
    abort_threshold_pct: 90 # warn at 90% of any cap
    halt_threshold_pct: 100 # halt new submissions at 100%
```

When `system_daily_gas_cap` reaches `halt_threshold_pct`: all new executions are blocked (`SELECTED → FAILED` with `terminal_reason = "gas_budget_exhausted"`). Positions remain manageable (exits still allowed).

**C. Compute Budget**

```yaml
resource_control:
  compute:
    max_workers_per_layer:
      data_quality: 20
      feature_extraction: 10
      edge_discovery: 10
      validation: 5
      selection: 2
      execution: 10
      position: 5
      evaluation: 5
      learning: 2
    queue_max_depth:
      data_quality_event: 5000
      feature_event: 2000
      edge_event: 1000
      execution_event: 500
```

Worker counts and queue limits are tunable at runtime via config reload — never hardcoded.

---

### 4.9.3 Priority System

When resources are scarce, work is prioritized deterministically:

**Priority score (per event, computed at enqueue):**

```
priority = PRIORITY_BASE[event_type]
         + 0.4 × TokenAge_score      // newer pools rank higher (decays over 15min)
         + 0.3 × EstimatedEdge_score // higher edge ranks higher
         + 0.3 × Liquidity_score     // larger pools rank higher
```

Constants and weights live in `config/priority.yaml`.

**PRIORITY_BASE (event type base priorities):**

| Event Type              | Base Priority  |
| ----------------------- | -------------- |
| `position_event` (exit) | 1000 (highest) |
| `execution_event`       | 900            |
| `selection_event`       | 800            |
| `validation_event`      | 700            |
| `edge_event`            | 600            |
| `feature_event`         | 500            |
| `data_quality_event`    | 400            |
| `market_data_event`     | 300            |
| `evaluation_event`      | 100            |
| `adjustment_event`      | 50 (lowest)    |

**Rule:** Exit-path events (positions, executions) always outrank entry-path events. Under pressure, the system prefers closing existing positions over opening new ones.

---

### 4.9.4 Backpressure Strategy

When any queue depth exceeds `queue_max_depth × warn_ratio` (config, default: 0.8):

1. Emit `[WARN] queue backpressure: <queue_name> at <depth>/<max>`
2. Shed low-priority work deterministically:
   - **Drop** low-value `market_data_event` records whose `priority < drop_threshold` (Layer 0 Swap events on pairs aged > 1h are the first to drop)
   - **Reduce exploration**: Layer 5 `exploration_band` is temporarily lowered to `exploration_band_min` (config, default: 1%)
   - **Tighten selection**: Layer 6 reduces `top_K` by `top_k_shed_ratio` (config, default: 0.5)
3. When queue drops below `queue_max_depth × recover_ratio` (config, default: 0.5), restore normal operation

**Rule:** Dropped events MUST be logged (`resource_drop_event`) — silent dropping is forbidden. Dropped events are still recorded in the event bus as null-outcome `LearningRecord` entries so Layer 10 can track the cost of backpressure.

**Forbidden backpressure actions:**

- Dropping position exit events under any condition
- Dropping execution confirmation events
- Dropping quarantine/failure events
- Skipping state machine transitions (§ 4.7)

---

### 4.9.5 Metrics Tracked

```
# Cost
cost_per_trade_usd               (rolling 1h mean)
gas_spend_daily_usd              (per wallet, per chain, system-wide)
rpc_request_cost_daily_usd       (per endpoint)

# Utilization
rpc_usage_rps                    (per chain, per endpoint)
rpc_budget_remaining_pct         (per chain, per endpoint)
gas_budget_remaining_pct         (per wallet, system-wide)

# Queues
queue_depth                      (per queue name)
queue_max_latency_ms             (per queue, p95)
queue_drop_rate                  (events dropped per second)

# Worker
worker_active_count              (per layer)
worker_utilization_pct           (per layer)

# Backpressure state
backpressure_active              (boolean, per queue)
backpressure_events_shed_total   (counter)
exploration_band_current         (gauge)
```

---

### 4.9.6 Halt Conditions

The system halts **new position entry** (but not exits) when ANY of the following triggers:

| Condition                                           | Halted Path                      | Auto-Resume?             |
| --------------------------------------------------- | -------------------------------- | ------------------------ |
| `system_daily_gas_cap` reached                      | All new entries                  | At UTC midnight rollover |
| All RPC endpoints unreachable for `rpc_dead_window` | All entries for that chain       | On endpoint recovery     |
| `queue_depth > queue_max_depth × halt_ratio` (0.95) | Entry events for saturated queue | When depth < warn_ratio  |
| Operator `/kill` command (§ 4.4)                    | All new entries                  | Only via `/resume`       |
| `StrategyVersion` rollback in progress              | All new entries                  | When rollback completes  |

**Halt propagation:** A halt condition is published as `system_halt_event` on the event bus with `trace_id=system`. All downstream orchestrator workers observe the halt and gate new entries.

**Exit-path guarantee:** No halt condition blocks exits. Open positions can always be closed even when the system is halted for new entries.

---

### 4.9.7 Failure Modes

| Mode                      | Cause                         | Mitigation                                                 |
| ------------------------- | ----------------------------- | ---------------------------------------------------------- |
| RPC quota exceeded        | Daily cap hit mid-day         | Failover to secondary endpoint; alert operator             |
| Gas price spike           | Network congestion            | Wider `max_fee` bounds within config cap; alert at 90%     |
| Queue overflow            | Downstream worker starved     | Shed low-priority per § 4.9.4; scale workers if available  |
| Worker crash loop         | Repeated worker failures      | Circuit breaker halts worker class; alert operator         |
| Priority computation skew | Bug in priority formula       | Fallback to PRIORITY_BASE only; alert for investigation    |
| Config reload race        | Budget changed mid-evaluation | Atomic config version pin per decision; no mid-trade drift |

All halt and backpressure events are subject to the same trace requirements as § 4.8 — `system_halt_event` and `resource_drop_event` must have valid `trace_id`, `correlation_id`, `causation_id`, and `version_id`.

---

## 4.10 Production Hardening Invariants

This section is **NORMATIVE**. Every invariant below is required at production deployment. Violations are exit-criteria failures and MUST block release. The contract: **same input → same output, even under concurrency, retries, and failure**.

### 4.10.A Event Ordering (Critical)

#### 4.10.A.1 Ordering Keys

Every `MarketDataDTO` carries a deterministic, totally-ordered `OrderingKey` derived from on-chain primitives. The exact composition is chain-specific:

| Chain              | Composition                            | Lexicographic comparison rule                    |
| ------------------ | -------------------------------------- | ------------------------------------------------ |
| EVM (`eth`, `bsc`) | `(block_number, tx_hash, log_index)`   | `block_number ASC, tx_hash ASC, log_index ASC`   |
| Solana             | `(slot, signature, instruction_index)` | `slot ASC, signature ASC, instruction_index ASC` |

Encoding for the SQL column is a single `BYTEA` (or fixed-width `TEXT`) such that byte-wise sort equals semantic sort:

```
EVM:    8-byte BE block_number || 32-byte tx_hash bytes || 4-byte BE log_index
Solana: 8-byte BE slot          || 64-byte signature   || 4-byte BE instruction_index
```

#### 4.10.A.2 Event Bus Ordering Contract

The `events` table read query MUST order by `logical_order_key`, NOT by `created_at`:

```sql
-- FORBIDDEN
SELECT ... FROM events WHERE chain = $1 ORDER BY created_at ASC;

-- REQUIRED
SELECT ... FROM events
 WHERE chain = $1 AND consumer = $2 AND processed = false
 ORDER BY logical_order_key ASC
 FOR UPDATE SKIP LOCKED
 LIMIT $3;
```

`created_at` is wall-clock and non-deterministic under retry; `logical_order_key` is content-addressed.

#### 4.10.A.3 DTO Requirement (Additive)

`MarketDataDTO` MUST include:

```go
type MarketDataDTO struct {
    // ... existing fields ...
    OrderingKey []byte // canonical encoding per § 4.10.A.1; immutable; required
}
```

This is an **additive** change: `OrderingKey` is appended to the existing DTO; existing producers MUST populate it; existing consumers ignore it until they migrate. See `docs/dto_contracts.md` for the formal field entry.

The `events` table gains a column `logical_order_key BYTEA NOT NULL` indexed `(chain, consumer, logical_order_key)`.

### 4.10.B Worker Determinism

#### 4.10.B.1 Strict Ascending Processing

Every worker MUST process events from a partition in **strict ascending `OrderingKey` order**. A worker MUST NOT process event K+1 until event K is committed (success, DLQ, or skipped per the version-mismatch rule § 4.10.G.1).

#### 4.10.B.2 Concurrency Constraint — One Worker per Token Lifecycle

A given token's lifecycle (its full sequence of events: pool init → swaps → execution → position → exit) MUST be processed by **exactly one worker at a time**. Two workers MUST NOT concurrently advance the same token's state.

Enforcement:

- Lifecycle table CAS transitions (per § 4.7) prevent state corruption even if accidentally violated.
- Partitioning (§ 4.10.B.3) makes the violation impossible by construction.

#### 4.10.B.3 Partitioning Strategy

```
partition_key = uint32(SHA256(token_address || chain)[:4]) mod num_workers
```

- Worker `w` processes only events with `partition_key % num_workers == w`.
- `num_workers` is a config-time constant; runtime change requires a coordinated drain + restart (§ 4.10.G.2).
- The `events` table SELECT query adds `AND (HASHTEXT(token_address) % $num_workers) = $worker_id` (or pre-computed `partition_key` column for portability).

This guarantees that the same token's events always land on the same worker, satisfying § 4.10.B.2 by construction.

### 4.10.C Dead Letter Queue (DLQ)

#### 4.10.C.1 Retry Policy

| Failure Class                                          | Max Retries | Backoff                                         |
| ------------------------------------------------------ | ----------- | ----------------------------------------------- |
| Transient (RPC timeout, connection drop, 5xx)          | **5**       | exponential, full jitter, 200 ms base, 30 s cap |
| Application (handler exception, parse error)           | **3**       | exponential, full jitter, 500 ms base, 10 s cap |
| Determinism violation (version mismatch, ordering gap) | **0**       | NEVER retried — moved to DLQ immediately        |

Retry counters are persisted on the event row, NOT in worker memory.

#### 4.10.C.2 DLQ Table

```sql
CREATE TABLE dead_letter_events (
    event_id          TEXT PRIMARY KEY,                  -- references events.event_id
    chain             TEXT NOT NULL,
    consumer          TEXT NOT NULL,                     -- which worker class failed
    reason            TEXT NOT NULL,                     -- error class / category
    error_message     TEXT,                              -- last observed error (truncated)
    retry_count       INTEGER NOT NULL,
    first_failed_at   TIMESTAMPTZ NOT NULL,
    last_failed_at    TIMESTAMPTZ NOT NULL,
    moved_to_dlq_at   TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    payload_snapshot  JSONB,                             -- full DTO at time of failure (for replay)
    trace_id          TEXT NOT NULL,
    correlation_id    TEXT NOT NULL,
    causation_id      TEXT,
    version_id        TEXT NOT NULL
);
CREATE INDEX idx_dlq_consumer_reason ON dead_letter_events (consumer, reason, moved_to_dlq_at);
```

#### 4.10.C.3 Worker Rule

```
on event_failed(event, err):
    retry_count = adapter.IncrementRetry(event.event_id, consumer)
    if retry_count > max_retries(classify(err)):
        adapter.MoveToDLQ(event, consumer, classify(err), err.Error())
        emit dlq_event{event_id, consumer, reason}
        advance_consumer_offset(event.logical_order_key)   // unblock the partition
    else:
        sleep(backoff(retry_count, classify(err)))
        // event remains in events table; SKIP LOCKED returns it on next poll
```

DLQ entries are operator-actionable: a Telegram alert fires on every DLQ insertion (§ 4.4). Manual reprocessing is via a `requeue_dlq` admin command which copies the row back into `events` with `retry_count = 0`.

### 4.10.D Exactly-Once Execution

#### 4.10.D.1 Execution Lock

Before any wallet/RPC submission, the executor MUST verify that no row exists in the `executions` table for the candidate `execution_id`:

```sql
INSERT INTO executions (execution_id, ...)
VALUES ($1, ...)
ON CONFLICT (execution_id) DO NOTHING
RETURNING execution_id;
```

If `RETURNING` is empty, another worker already claimed this execution; the current worker MUST NOT submit. This is the **single** authoritative dedup boundary — in-memory locks are advisory only.

#### 4.10.D.2 Idempotency Key

```
execution_id = SHA256(trace_id || version_id || token_address || chain)[:16]
```

Properties:

- **Deterministic** — same inputs always produce the same key.
- **Version-scoped** — a strategy version bump produces a new key, allowing the same token to be re-evaluated under the new version (intentional).
- **Chain-scoped** — same token symbol on different chains never collides.
- Computed by the Selection or Capital layer at decision time; carried on `AllocationDTO`.

#### 4.10.D.3 Adapter Enforcement

All inserts into `executions`, `solana_signatures`, `positions`, `dead_letter_events`, and `events` use `INSERT ... ON CONFLICT DO NOTHING`. The application MUST treat "0 rows inserted" as the **success** path for idempotent retries — never as an error.

### 4.10.E Position Consistency

#### 4.10.E.1 Single-Position Invariant

```
Confirmed execution_event(execution_id) ⇒ exactly one row in positions where source_execution_id = execution_id
```

The `positions` table has a `UNIQUE` constraint on `source_execution_id`. Position entry is via `INSERT ... ON CONFLICT DO NOTHING` keyed on this column.

#### 4.10.E.2 Reconciliation Worker

A periodic reconciliation worker (default cadence: 30 s, configurable via `cfg.reconciliation.interval_ms`) compares on-chain wallet state against the `positions` projection:

```
for each open position p:
    onchain_balance = rpc.GetTokenBalance(p.wallet, p.token)
    if abs(onchain_balance - p.amount) > tolerance:
        emit reconciliation_event{position_id, db_amount, onchain_amount, action}
        if onchain_balance == 0 and p.status == "open":
            // position was exited externally (manual sell, rug, transfer)
            adapter.ClosePositionForced(p, reason="onchain_zero")
        else:
            adapter.AdjustPositionAmount(p, onchain_balance, reason="reconciliation")
```

Reconciliation is **non-destructive**: it never deletes positions; it only adjusts amounts and emits events. Every adjustment carries full traceability fields and increments a `reconciliation_adjustments_total` metric.

### 4.10.F Latency Closed Loop

#### 4.10.F.1 Latency Event Emission (Mandatory)

Every `ExecutionResultDTO` emit MUST be accompanied by a `latency_event` capturing:

```
latency_event {
    execution_id, chain, endpoint, version_id,
    decision_to_send_ms,         // Selection emit → RPC sendTransaction
    send_to_first_observe_ms,    // sendTransaction → first signature/receipt observation
    first_observe_to_confirm_ms, // first observation → terminal commitment
    total_ms,
    outcome,                     // confirmed | reverted | dropped | timeout
    timestamp
}
```

#### 4.10.F.2 Feedback Loop

```
execution → latency_event → LatencyProfileDTO update → Probability/Slippage models → next execution
```

The Probability and Slippage models (Layer 4) consume `LatencyProfileDTO` (rolling p50/p95/p99 per endpoint per chain, last 5 min window). Layer 8 (Execution) consults the model output via `LatencyProfileDTO` before each submission (the latency-aware gate per § 3.11.10.9 and § 7.3.3 of the roadmap).

A latency event MUST be emitted even on failure paths (RPC timeout, circuit-open, dropped tx) — without negative-outcome data, the model overestimates endpoint health.

### 4.10.G Config Consistency

#### 4.10.G.1 Version-Mismatch Rejection

Every event carries `version_id` (immutable). Workers process only events whose `version_id == active_strategy_version_id` at the time of dequeue:

```
on event_dequeue(event):
    active = adapter.GetActiveStrategyVersion()
    if event.version_id != active.version_id:
        adapter.MoveToDLQ(event, consumer, reason="version_mismatch", err="...")
        return  // do NOT advance offset until DLQ commit succeeds
```

Rationale: a config change creates a strict before/after split. Events emitted under the old version MUST NOT be processed by handlers running the new version (and vice versa). Mixed-version processing produces non-replayable behavior.

#### 4.10.G.2 Mid-Run Protection

A new `StrategyVersion` MUST NOT activate while any in-flight pipeline exists. Activation procedure:

```
1. operator or learning engine: CreateStrategyVersion(v_new, status="pending")
2. orchestrator: drain — stop accepting new market_data_event into ingestion
3. orchestrator: wait for all consumer_offsets to reach the head OR for
   max_drain_seconds (default 60); residual events remain queued
4. atomic UPDATE: strategy_versions SET status="active" WHERE id=v_new
   AND old "active" → status="superseded" in same transaction
5. orchestrator: resume ingestion
6. residual events with old version_id are processed by the version-mismatch rule
   above (DLQ with reason="version_mismatch_drain_residual")
```

No config file edit is honored without going through this state machine.

### 4.10.H Circuit Breaker (Critical)

#### 4.10.H.1 Global Kill Switch

A global `system_halt` flag is checked by every execution call site **before** any RPC submission. Triggers (any one):

| Trigger                           | Threshold (default; config-driven)                    | Action                                       |
| --------------------------------- | ----------------------------------------------------- | -------------------------------------------- |
| Loss-rate spike                   | rolling 10-min loss_rate > 60% with N ≥ 20 trades     | HALT + alert                                 |
| Tx-failure spike                  | rolling 5-min tx_failure_rate > 30% with N ≥ 30 sends | HALT + alert                                 |
| Latency spike                     | rolling 5-min p95 send→confirm > 5000 ms              | HALT + alert                                 |
| Drawdown breach                   | session loss ≥ `cfg.risk.session_loss_floor_usd`      | HALT + alert (per drawdown-protection skill) |
| Manual operator command (`/kill`) | —                                                     | HALT + alert                                 |

#### 4.10.H.2 Execution Refusal

```
func (e *Executor) Execute(ctx, alloc) (ExecutionResultDTO, error) {
    halted, reason := adapter.IsSystemHalted(ctx)
    if halted {
        return ExecutionResultDTO{
            Status:          "rejected",
            FailureCategory: "system_halted",
            RejectReason:    reason,
        }, nil
    }
    // ... proceed
}
```

Halt is **persistent**: it survives process restart (stored in `system_state.halt_status`). Resume requires explicit `/resume` operator command, which is logged with operator identity, time, and reason.

### 4.10.I Replay Validation

#### 4.10.I.1 State Snapshot

```
state_hash = SHA256(
    canonicalize(positions, exclude=[updated_at]) ||
    canonicalize(executions, exclude=[]) ||
    canonicalize(strategy_versions, exclude=[]) ||
    canonicalize(consumer_offsets) ||
    canonicalize(token_lifecycle, exclude=[updated_at]) ||
    canonicalize(learning_records)
)
```

Where `canonicalize(table)` = sort all rows by primary key, serialize each row as deterministic JSON (sorted keys, no whitespace, no wall-clock columns).

The snapshot CLI: `sniper snapshot --output=state_hash.txt` produces one hex digest.

#### 4.10.I.2 Replay Validation

```
1. Capture pre-replay snapshot:  H_prod = state_hash(prod DB)
2. Spin up replay DB; load events with replay: prefix
3. Run the full pipeline against the replay DB to completion
4. Capture post-replay snapshot: H_replay = state_hash(replay DB)
5. ASSERT H_replay == H_prod
```

Failure of step 5 is a **release-blocking** determinism violation. Diagnosis follows the pattern in § 4.2 (Replay Engine).

The CI pipeline runs replay validation on every merge to `main` against a fixed fixture set. A green replay is a hard gate for production deployment.

---

## § 4.11 — Execution Model Unification, Atomicity, Reorg, Evaluation, Backpressure

This section closes the remaining production risks. It is normative and additive.

### 4.11.A — Execution Model: Pure Event-Driven (Option A)

The system is **pure event-driven**. The orchestrator is a **supervisor**, not a sequencer.

| Concern                                     | Owner              |
| ------------------------------------------- | ------------------ |
| Lifecycle (boot, shutdown, signal handling) | Orchestrator       |
| Worker pool sizing & partition assignment   | Orchestrator       |
| Health checks & circuit breakers            | Orchestrator       |
| Strategy version drain & promotion          | Orchestrator       |
| **Stage-to-stage DTO routing**              | **Event bus only** |
| **Module invocation**                       | **Workers only**   |

The orchestrator MUST NOT call module functions mid-pipeline. Stage transitions happen exclusively via `INSERT INTO events` → `ClaimNextEvents` by the next consumer. Sequential execution at runtime is FORBIDDEN.

This resolves the latent conflict between `orchestrator_spec.md` § 2 (sequential stages) and § 2.3 (event workers): § 2 describes _logical_ dependency order; § 2.3 is the _runtime_ execution model. Logical order is encoded in `consumer` names; runtime ordering inside a consumer is `logical_order_key` ASC (§ 4.10.A).

### 4.11.B — Partition Ownership in the Adapter

Partition assignment is adapter-authoritative, not worker-authoritative.

```sql
CREATE TABLE partition_leases (
    chain         TEXT NOT NULL,
    consumer      TEXT NOT NULL,
    partition_key INTEGER NOT NULL,
    worker_id     TEXT NOT NULL,
    leased_at     TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    expires_at    TIMESTAMPTZ NOT NULL,
    PRIMARY KEY (chain, consumer, partition_key)
);
```

Adapter methods:

```go
ClaimPartitions(ctx, chain, consumer, workerID string, n int, ttlSec int) ([]int, error)
RenewPartitions(ctx, chain, consumer, workerID string) error
ReleasePartitions(ctx, chain, consumer, workerID string) error
```

A worker that has not successfully called `ClaimPartitions` MUST NOT call `ClaimNextEvents` for that partition. `ClaimNextEvents` filters by `partition_key IN (worker's leased set)`. Lease TTL default 30 s; renewal cadence ≤ TTL/3.

### 4.11.C — Execution Atomicity & Crash-Safe Recovery

```
Phase 1 (RESERVE):    BEGIN TX
                      ClaimExecution(allocDTO)        -- INSERT execution row, status='in_flight'
                      INSERT execution_attempt row, status='reserved'
                      COMMIT
Phase 2 (SEND):       tx_hash := rpc.SendTransaction(prebuilt_calldata)
                      UPDATE execution_attempt SET tx_hash=?, status='sent'
Phase 3 (CONFIRM):    receipt := rpc.WaitForReceipt(tx_hash)
                      BEGIN TX
                      UPDATE execution row: status, gas_used, block_number, ...
                      INSERT execution_event into events
                      INSERT latency_event
                      COMMIT
```

**Crash-safe recovery** runs at orchestrator boot before worker pools start:

```
1. SELECT * FROM executions WHERE status='in_flight'
2. For each: read execution_attempt rows
3. If any attempt has tx_hash:
     poll RPC for receipt via rpc.GetTransactionByHash(tx_hash)
     if receipt found  → finalize Phase 3
     if not found AND  age > recovery_grace_sec → mark 'lost', emit ExecutionResultDTO{status: failed, FailureCategory: 'crash_unknown_tx'}
   Else (reserved but never sent):
     mark execution row status='aborted', release reservation
4. Recovery is logged and emits a system_event with reason='crash_recovery'
```

`recovery_grace_sec` default = `2 * receipt_timeout_sec` from `cfg.execution`. Recovery MUST complete before any worker accepts new events; orchestrator gates worker startup on `recovery_complete=true`.

### 4.11.D — Reorg Handling

EVM and Solana both support shallow reorgs. The system handles them via **invalidation**, not rollback.

```sql
CREATE TABLE reorg_events (
    chain          TEXT NOT NULL,
    old_block      BIGINT NOT NULL,
    new_block      BIGINT NOT NULL,
    detected_at    TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    depth          INTEGER NOT NULL,
    affected_count INTEGER NOT NULL,
    PRIMARY KEY (chain, old_block, detected_at)
);
```

Adapter methods:

```go
RecordReorg(ctx, chain string, oldBlock, newBlock int64, depth int) error
InvalidateBlockRange(ctx, chain string, fromBlock, toBlock int64) (affected int, err error)
MarkPositionsUncertain(ctx, chain string, fromBlock int64, reason string) error
```

**Detection:** ingestion compares parent hash of new block with stored hash of `block_number-1`. Mismatch → `RecordReorg` + `InvalidateBlockRange(chain, new_block_minus_depth, latest)`.

**Invalidation cascade:**

1. Mark all `events` rows in `[old_block, latest]` with `invalidated_at=NOW()`. Workers MUST skip events with non-null `invalidated_at`.
2. Mark all `executions` whose `block_number ∈ [old_block, latest]` with `confirmation_status='reorg_pending'`.
3. Mark all `positions` whose `entry_execution_id` is in step 2 set as `status='uncertain'`. Position monitoring loop pauses exits for uncertain positions.
4. After `reorg_settle_blocks` confirmations on the new chain head:
   - Re-resolve each execution via `rpc.GetTransactionByHash(tx_hash)`. Three outcomes:
     - **Still confirmed** (re-included): clear `reorg_pending`, restore `position.status='open'`.
     - **Dropped** (not in new chain): mark execution `status='reorged_out'`, position `status='void'` (capital returned, not a loss).
     - **Different result** (e.g., partial fill): emit corrective `execution_event` with `FailureCategory='reorg_mutation'`.

Reorg depth cap: `max_reorg_depth` default 12 (EVM) / 32 (Solana). Beyond cap → `SetSystemHalt(reason='reorg_exceeds_max_depth')`.

### 4.11.E — Evaluation Coverage Invariant

Every `execution_event` MUST produce exactly one downstream `evaluation_event`.

Adapter enforces via materialized invariant:

```sql
CREATE TABLE evaluation_invariant (
    execution_id     TEXT PRIMARY KEY,
    has_evaluation   BOOLEAN NOT NULL DEFAULT false,
    deadline_at      TIMESTAMPTZ NOT NULL,
    -- evaluation_deadline_sec from cfg.evaluation, default 300
    FOREIGN KEY (execution_id) REFERENCES executions(execution_id)
);
```

Adapter methods:

```go
RecordExecutionForEvaluation(ctx, executionID string, deadlineSec int) error  // called atomically with execution_event INSERT
MarkEvaluationDone(ctx, executionID string) error                               // called by evaluation worker
ListMissingEvaluations(ctx) ([]MissingEvaluation, error)                        // deadline_at < NOW() AND NOT has_evaluation
```

A janitor worker runs every `cfg.evaluation.janitor_interval_sec` (default 60), calls `ListMissingEvaluations`, and emits a `system_event{level=warn, reason='evaluation_missing', execution_id=X}` for each. Three consecutive janitor cycles with the same missing record → `system_event{level=critical}` (potential evaluation worker failure). Coverage ratio is a Phase 8 KPI: `1 - (count(missing) / count(executions))` MUST be ≥ 0.999 over any 1h window.

### 4.11.F — Backpressure & Drop Policy

```yaml
# cfg.backpressure
events:
  max_unprocessed_per_consumer: 50000 # hard cap per consumer name
  warn_threshold: 20000
  ingest_pause_threshold: 40000 # ingestion pauses when crossed
  ingest_resume_threshold: 20000 # ingestion resumes only after dropping back

ingestion:
  publish_buffer_size: 5000 # in-process buffer per chain
  drop_policy: score_based # score_based | tail | head_low_priority
  drop_priority_features: [liquidity_usd, momentum_z, holder_count]
  drop_min_score: 0.20 # tokens with composite_score < 0.20 are dropped first under pressure
```

Adapter methods:

```go
GetUnprocessedCount(ctx, chain, consumer string) (int64, error)
RecordDrop(ctx, chain, reason, tokenAddress, score string) error  // INSERT into ingestion_drops table
```

Ingestion worker loop:

```
if GetUnprocessedCount(chain, "data_quality") > cfg.events.ingest_pause_threshold:
    pause_ingestion(chain)
    emit system_event{level=warn, reason='backpressure_pause'}
elif paused AND GetUnprocessedCount(...) < cfg.events.ingest_resume_threshold:
    resume_ingestion(chain)

# When publish_buffer is full:
if buffer.full() AND policy == 'score_based':
    drop the lowest-score candidate not yet published
    RecordDrop(...)
```

Pool-init events (new pair creation) are **never** dropped — they bypass the score filter. Only momentum/swap events are subject to the drop policy.

Backpressure observability: `unprocessed_events_total{consumer}`, `ingestion_drops_total{chain, reason}`, `ingest_paused_seconds_total{chain}` are mandatory metrics.

### 4.11.G — Final Guarantee

Combined with § 4.10, the system satisfies:

> **Same input → identical output, under failure, retries, and concurrency.**

Where:

- **Same input** = identical event stream (replay determinism, § 4.10.I)
- **Identical output** = identical `state_hash` (§ 4.10.I.1)
- **Under failure** = DLQ + crash-safe recovery + reorg invalidation (§ 4.10.C, § 4.11.C, § 4.11.D)
- **Under retries** = exactly-once `ClaimExecution` + position UNIQUE (§ 4.10.D, § 4.10.E)
- **Under concurrency** = adapter-authoritative partition leases (§ 4.11.B) + `SELECT FOR UPDATE SKIP LOCKED` (§ 4.10.A)

---

# 5. Control & KPI System

KPIs make the system **measurable, controllable, and testable**. They are split per layer, with explicit thresholds, trigger rules, and automatic actions.

## 5.1 Per-Layer KPIs

### 5.1.1 Data Quality (Layer 1)

| KPI                   | Target      | Action if Breached             |
| --------------------- | ----------- | ------------------------------ |
| pass_rate             | 0.5–5%      | auto profile up/down           |
| fp_rate (rug-related) | ↓           | tighten rug/honeypot detectors |
| fn_rate               | ↓ (bounded) | relax filters carefully        |
| rug_loss_rate         | ≈ 0         | increase rug detector weight   |

### 5.1.2 Feature Extraction (Layer 2)

| KPI                   | Target | Action                          |
| --------------------- | ------ | ------------------------------- |
| confidence_avg        | ≥ 0.7  | improve data sources if low     |
| PSI drift per feature | ≤ 0.2  | reduce weight, flag for review  |
| spearman vs PnL       | stable | prune features that decorrelate |

### 5.1.3 Signal/Edge (Layer 3)

| KPI            | Target | Action                        |
| -------------- | ------ | ----------------------------- |
| edge_pass_rate | 0.5–5% | adjust θ_momentum             |
| edge_hit_rate  | ↑      | retune weights / add features |

### 5.1.4 P/S/L Models (Layer 4)

| KPI               | Target | Action                    |
| ----------------- | ------ | ------------------------- |
| probability error | ↓      | recalibrate weights       |
| slippage error    | ↓      | bump slippage coefficient |
| latency error     | ↓      | update EMA / fee tier     |

### 5.1.5 Validation (Layer 5)

| KPI       | Target    | Action                             |
| --------- | --------- | ---------------------------------- |
| pass_rate | 0.5–5%    | relax/tighten per controller       |
| fp_rate   | ↓         | raise θ_p, θ_ev                    |
| fn_rate   | ↓ bounded | lower θ_p, enable exploration band |

### 5.1.6 Selection (Layer 6)

| KPI             | Target           | Action                        |
| --------------- | ---------------- | ----------------------------- |
| selection_count | ≈ K when healthy | starvation → exploration      |
| diversity_index | above threshold  | enforce stricter cluster caps |
| topK_vs_all_pnl | ↑                | tune scoring components       |

### 5.1.7 Capital (Layer 7)

| KPI                 | Target   | Action                               |
| ------------------- | -------- | ------------------------------------ |
| drawdown            | bounded  | γ-scale all sizes; pause allocations |
| cohort expectancy   | ↑        | increase cohort multiplier           |
| capital_utilization | balanced | rebalance distributions              |

### 5.1.8 Execution (Layer 8)

| KPI                | Target | Action                           |
| ------------------ | ------ | -------------------------------- |
| inclusion_delay    | low    | bump priority fee tier           |
| failure_rate       | ↓      | reduce concurrency               |
| slippage miss rate | ↓      | tighten AmountOutMin / size down |

### 5.1.9 Position (Layer 9)

| KPI               | Target | Action                          |
| ----------------- | ------ | ------------------------------- |
| ExitEfficiency    | ↑      | tune TP/SL/trailing per cohort  |
| avg overstay_loss | ↓      | shorten TIME / tighten trailing |
| avg missed_profit | ↓      | raise TP thresholds             |

### 5.1.10 Learning (Layer 10)

| KPI              | Target | Action                  |
| ---------------- | ------ | ----------------------- |
| updates_applied  | steady | verify sample sizes     |
| rollbacks        | rare   | investigate instability |
| FN rate, FP rate | ↓      | re-run attribution      |

## 5.2 Global KPIs (Portfolio-Level)

```
hourly_pnl
24h_pnl
sharpe (rolling)
max_drawdown
trade_throughput
rug_loss_ratio
exit_efficiency_global
```

## 5.3 Control Rules (Master)

- All controllers use **bounded step sizes**
- Require **minimum sample size** before update
- Only **one parameter family per cycle** (avoid oscillation)
- Every change is **versioned and rollback-able**

---

# 6. System Guarantees

## 6.1 Determinism

- Same input + same config → identical output
- No random seeds, no wall-clock dependencies (event timestamps only)
- Enforced via replay (§ 4.2)

## 6.2 Reproducibility

- All decisions stamped with `strategy_version_id`
- All DTOs logged to append-only event bus
- Full state reconstructible from event log

## 6.3 Safety

- Hard caps at every layer (RiskScore, θ_p, size, concurrency)
- Drawdown guard globally
- Telegram `/kill` command
- Circuit-breakers on RPC / execution failures
- All learning updates bounded and versioned

## 6.4 Scalability

- Per-market isolation (horizontal parallelism)
- Stateless modules (pure functions on DTOs)
- Append-only event bus with `SELECT ... FOR UPDATE SKIP LOCKED` workers
- Bounded concurrency in execution (5–20)

---

# 7. Operational Modes

The system runs in one of **four** profiles arranged in an adaptive mode wheel. Profiles are
switchable via Telegram `/mode` or automatically by the risk-appetite controller.

```
STRICT ←→ BALANCED ←→ EXPLORATION ←→ VERY_EXPLORATION
          (adaptive mode wheel — bidirectional)
```

## 7.1 STRICT

```yaml
mode: STRICT
data_quality:
  max_tax: 8
  min_liquidity: 20000
  risk_reject: 0.30 # tightest reject band
edge:
  theta_momentum: high
  edge_strength_min: 0.75
validation:
  theta_p: 0.70
  theta_s: 0.03
  ev_threshold_bps: 150
capital:
  explore_budget: 1%
  cohort_multiplier_max: 1.2
  mode_multiplier: 0.5x
```

Use when: rug rate rising, toxic regime, drawdown spike. Safety floor.

## 7.2 BALANCED (default)

```yaml
mode: BALANCED
data_quality:
  max_tax: 12
  min_liquidity: 10000
  risk_reject: 0.50
edge:
  theta_momentum: medium
  edge_strength_min: 0.60
validation:
  theta_p: 0.60
  theta_s: 0.05
  ev_threshold_bps: 100
capital:
  explore_budget: 2–3%
  cohort_multiplier_max: 1.3
  mode_multiplier: 1.0x
```

Default cold-start mode. Operator can return here from any mode with `/mode balanced`.

## 7.3 EXPLORATION

```yaml
mode: EXPLORATION
data_quality:
  max_tax: 15
  min_liquidity: 5000
  risk_reject: 0.65
  min_token_age: disabled # new-launch sniping targets creation-time tokens
edge:
  theta_momentum: low
  edge_strength_min: 0.45
validation:
  theta_p: 0.50
  theta_s: 0.08
  ev_threshold_bps: 60
capital:
  explore_budget: 3–5%
  cohort_multiplier_max: 1.5
  mode_multiplier: 1.3x
```

Auto-entered when starvation is detected (no validated edge for ≥30 min). Use when the
market is underserved and the strategy needs to discover new cohorts.

## 7.4 VERY_EXPLORATION

```yaml
mode: VERY_EXPLORATION
data_quality:
  max_tax: 20
  min_liquidity: 1000
  risk_reject: 0.75 # maximally permissive — accept borderline tokens
  min_token_age: disabled # catch tokens at the moment of creation
edge:
  theta_momentum: minimal
  edge_strength_min: 0.30 # very wide net — recall over precision
validation:
  theta_p: 0.40
  theta_s: 0.12
  ev_threshold_bps: 30 # accept thin edges
capital:
  explore_budget: 8%
  cohort_multiplier_max: 1.8
  mode_multiplier: 1.5x
```

Auto-entered when starvation **persists** in EXPLORATION mode for a full adaptive window.
This is maximum aggression for new-launch token sniping — targets tokens at creation.
**Auto-exits** (downgrade to EXPLORATION) on rug-rate spike or manual `/mode` override.
Telecommand: `/mode very_explore` or `/mode very_exploration`.

> **Risk note:** VERY_EXPLORATION maximises recall at the cost of precision. Every
> loss-pattern classification still runs; Learning Engine feedback applies. Never remove
> the drawdown kill switch — it is the safety floor regardless of mode.

## 7.5 Mode Transition Rules

**Adaptive mode wheel** (bidirectional, one transition per `transition_window_sec`):

```
STRICT ←→ BALANCED ←→ EXPLORATION ←→ VERY_EXPLORATION
```

- **Auto-upgrade** (starvation): no validated edge for `starvation_trigger_sec` → step up one mode
  - STRICT → BALANCED → EXPLORATION → VERY_EXPLORATION
- **Auto-downgrade** (safety): rug rate > `rug_rate_auto_downgrade` OR FP rate > `fp_rate_auto_downgrade` → step down one mode
  - VERY_EXPLORATION → EXPLORATION → BALANCED → STRICT
- **Manual override** via Telegram `/mode <strict|balanced|explore|very_explore>` (logged, reversible, survives one adaptive window)
- **One transition per window** — bounded by `transition_window_sec` (default 1h); prevents oscillation
- **Drawdown safety mode** is orthogonal: DEGRADED and HALTED are owned by the drawdown controller and override all adaptive modes
- Already at VERY_EXPLORATION with no edges → emit `starvation_critical` alert (market conditions suspect, operator review required)
- Already at STRICT with rug spike → emit `high_rug_rate_in_strict` alert (strategy review required)

---

# 8. Final Characteristics

## 8.1 Definitive Properties

The combined architecture yields a system that is:

1. **Anti-manipulation** — layered defense against wash, rug, honeypot, fake liquidity, tax manipulation (Layer 1).
2. **Fast execution** — wallet sharding, prebuilt calldata, bounded parallelism, adaptive fees (Layer 8).
3. **Adaptive learning** — FP/FN analysis, cohort expectancy, bounded updates, versioned rollback (Layer 10).
4. **Capital safety** — hard caps, drawdown guard, cohort multipliers, exploration budget (Layer 7).
5. **Deterministic & auditable** — typed DTOs, append-only event bus, replayable decisions (§ 2, § 4.2).
6. **Event-sourced** — all state reconstructible from event log (§ 2.4).
7. **Scalable** — per-market isolation, SKIP LOCKED workers (§ 2.3, § 2.5).
8. **Operator-controlled** — Telegram commands, mode switching, kill switch (§ 4.4).

## 8.2 Break It Down — The Core Invariant

```
Profit = Edge × Probability × Execution × Capital × DataQuality × AdaptationQuality
```

- **Edge** — Is there a real opportunity? (Layer 3)
- **Probability** — Will it succeed given the data? (Layer 4)
- **Execution** — Can we actually get filled? (Layer 8)
- **Capital** — How much to risk? (Layer 7)
- **DataQuality** — Is the input trustworthy? (Layer 1)
- **AdaptationQuality** — Are we correcting mistakes fast enough? (Layer 10)

If any factor → 0, profit → 0. If all factors > 0 and compound, expectancy > 0 long-term.

## 8.3 Mental Model

- Not a bot. Not a script.
- **A deterministic, event-sourced, self-calibrating trading machine.**
- Every trade is a data point. Every failure is a correction. Every version is an experiment.

## 8.4 Sanity Test

Given the same raw blockchain event stream + same `strategy_version_id`:

- The system must produce **identical decisions**, **identical position sizes**, and **identical exit actions**.
- Replay must match live execution bit-for-bit (minus network-side latency).

If this test fails → determinism is broken → stop trading, fix, and version.

## 8.5 Closing Statement

This architecture is not maximizing speed. It is maximizing **controlled expectancy under adversarial data + hostile execution**.

You win by:

1. Filtering aggressively (Layer 1–5).
2. Executing cleanly (Layer 8).
3. Exiting with discipline (Layer 9).
4. Correcting faster than the market changes (Layer 10).

Everything else is noise.

---

_End of Document — Unified Architecture v1.0_
