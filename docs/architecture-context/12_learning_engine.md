# **12\. Layer 10 — Learning Engine (Cross-Layer Brain)**

## **12.1 Inputs**

From **all layers**:

* decisions (accept/reject/score)  
* features  
* execution metrics  
* outcomes (PnL, duration)  
* failure types

## **12.2 Core Outputs**

* **threshold adjustments**  
* **feature weights**  
* **model calibration**  
* **execution params**  
* **capital sizing rules**

## **12.3 Key Analyses**

### **A. False Negatives**

* rejected → later pump

### **B. False Positives**

* accepted → loss/rug

### **C. Cohort Analysis**

group by:  
\- liquidity band  
\- tax band  
\- holder concentration  
\- entry latency

Compute:

* win rate  
* expectancy

## **12.4 Safety**

* updates are **bounded**  
* applied via **versioned config**  
* require **minimum sample size**

---

This is the **only component that turns your system from static → compounding**.  
 Everything upstream generates data; this layer converts it into **controlled parameter updates**.

---

# **12\. LAYER 10 — LEARNING ENGINE (CROSS-LAYER BRAIN)**

---

# **12.1 INPUTS (CANONICAL DATASET)**

All inputs must be **joined into a single training row per trade (and per rejected candidate)**.

---

## **12.1.1 Unified Record (per token event)**

type LearningRecord struct {

   TokenAddress string

   Version int

   // Decisions across layers

   DataQualityDecision string

   Score float64

   Selected bool

   AllocatedSize float64

   // Features (snapshot at decision time)

   FeatureVector map\[string\]float64

   // Execution metrics

   SlippageExpected float64

   SlippageActual float64

   LatencyExpected float64

   LatencyActual float64

   TxSuccess bool

   // Outcome

   EntryPrice float64

   ExitPrice float64

   PeakPrice float64

   PnL float64

   DurationSec int

   // Labels

   OutcomeLabel string // success | fail | rug | missed

   // Meta

   CohortID string

   Timestamp int64

}

---

## **12.1.2 Inclusion Rules**

You MUST store:

1\. accepted trades (executed)

2\. rejected trades (shadow tracked)

Without rejected samples → you cannot compute **false negatives**.

---

# **12.2 CORE OUTPUTS (CONTROL KNOBS)**

This engine only updates **5 things**:

---

## **12.2.1 Threshold Adjustments**

* `θ_p` (probability)  
* `θ_s` (slippage cap)  
* `θ_latency`  
* `θ_ev`  
* data quality thresholds (tax, liquidity)

---

## **12.2.2 Feature Weights**

Used in:

* scoring (Layer 3\)  
* probability model (Layer 4\)

---

## **12.2.3 Model Calibration**

* probability calibration  
* slippage coefficients  
* latency estimates

---

## **12.2.4 Execution Params**

* priority fee baseline  
* retry policy  
* concurrency limit

---

## **12.2.5 Capital Sizing Rules**

* cohort multipliers  
* max position size adjustments

---

# **12.3 KEY ANALYSES (ACTUAL LEARNING LOGIC)**

---

# **12.3.A FALSE NEGATIVES (MISSED OPPORTUNITIES)**

---

## **Definition**

Decision \= reject

AND peak\_return ≥ threshold (e.g., \+30% within T)

---

## **Query (conceptual)**

SELECT \*

FROM learning\_records

WHERE selected \= false

AND (peak\_price \- entry\_price)/entry\_price ≥ 0.3

---

## **Metrics**

FN\_rate \= false\_negatives / total\_rejects

MissedPnL \= Σ (peak\_return of FN)

---

## **Attribution**

Determine **why rejected**:

blocked\_by:

\- probability threshold?

\- slippage cap?

\- latency?

\- data quality?

---

## **Action**

if FN\_rate ↑:

 ↓ probability threshold

 ↑ slippage tolerance (slightly)

 relax data quality filters (carefully)

---

# **12.3.B FALSE POSITIVES (BAD TRADES)**

---

## **Definition**

Decision \= accept

AND PnL \< 0 OR rug

---

## **Query**

SELECT \*

FROM learning\_records

WHERE selected \= true

AND pnl \< 0

---

## **Metrics**

FP\_rate \= losing\_trades / total\_trades

Rug\_rate \= rugs / total\_trades

---

## **Attribution**

cause:

\- low entropy?

\- high tax?

\- bad liquidity?

\- overestimated probability?

\- slippage underestimated?

---

## **Action**

if FP\_rate ↑:

 ↑ probability threshold

 ↓ slippage cap

 tighten data quality filters

---

# **12.3.C COHORT ANALYSIS (WHERE EDGE EXISTS)**

---

## **Cohort Definition**

Group by:

\- liquidity band (5–10k, 10–20k, …)

\- tax band

\- holder concentration

\- entry latency

\- momentum band

---

## **Aggregation**

SELECT cohort\_id,

      COUNT(\*) as trades,

      AVG(pnl) as avg\_pnl,

      SUM(CASE WHEN pnl \> 0 THEN 1 ELSE 0 END)/COUNT(\*) as win\_rate

FROM learning\_records

GROUP BY cohort\_id

---

## **Compute**

expectancy \= avg\_pnl

win\_rate

drawdown

---

## **Action**

if cohort\_expectancy \> 0:

 increase capital multiplier

if cohort\_expectancy \< 0:

 decrease multiplier

 possibly block cohort

---

# **12.4 MODEL CALIBRATION**

---

## **12.4.1 Probability Calibration**

Goal:

Predicted P ≈ Actual success rate

---

### **Error**

E \= actual\_outcome \- predicted\_P

---

### **Update**

w\_new \= w\_old \+ α × E × feature

Constraints:

* small α (0.01–0.05)  
* normalized weights

---

## **12.4.2 Slippage Calibration**

E\_slip \= actual \- expected

k\_new \= k\_old \+ β × E\_slip

---

## **12.4.3 Latency Calibration**

latency\_estimate \= EMA(actual\_latency)

---

# **12.5 EXECUTION LEARNING**

---

## **Metrics**

failure\_rate

inclusion\_delay

slippage\_error

---

## **Actions**

if inclusion\_delay ↑:

 increase priority fee baseline

if failure\_rate ↑:

 reduce concurrency

---

# **12.6 CAPITAL LEARNING**

---

## **From cohort results**

multiplier\_new \=

 multiplier\_old \+ α × expectancy

Bounded:

* \[0.1, 1.5\]

---

# **12.7 UPDATE PIPELINE (IMPORTANT)**

---

## **Frequency**

every N trades OR every T minutes (e.g., 50 trades / 15 min)

---

## **Steps**

1\. Aggregate metrics

2\. Compute errors (FP, FN, calibration)

3\. Propose updates

4\. Validate constraints

5\. Apply new version

---

# **12.8 SAFETY (NON-NEGOTIABLE)**

---

## **12.8.1 Bounded Updates**

Δparameter ≤ 5–10% per cycle

---

## **12.8.2 Minimum Sample Size**

N ≥ 30–50 before update

---

## **12.8.3 Versioning**

config\_version++

store snapshot

---

## **12.8.4 Rollback**

if performance ↓:

 revert to previous version

---

## **12.8.5 Isolation**

* updates per cohort / per feature  
* avoid global changes unless strong signal

---

# **12.9 OUTPUT (CONFIG UPDATE)**

type StrategyConfig struct {

   Version int

   Thresholds map\[string\]float64

   FeatureWeights map\[string\]float64

   SlippageParams map\[string\]float64

   LatencyParams map\[string\]float64

   CapitalMultipliers map\[string\]float64

}

---

# **12.10 FAILURE MODES**

---

## **No learning (AQ \= 0\)**

* system repeats mistakes

---

## **Overfitting**

* reacts too fast → unstable

---

## **Underfitting**

* reacts too slow → stagnant

---

## **Wrong attribution**

* fixes wrong parameter → degrades system

---

# **12.11 WHAT THIS LAYER GUARANTEES**

* system **improves over time**  
* mistakes are **not repeated**  
* capital flows to **working patterns**  
* bad patterns are **suppressed**

---

# **FINAL INSIGHT**

Your edge is not your strategy.

Your edge is how fast you correct your strategy.

---

## **If this layer is correct:**

* early performance may be average  
* long-term performance compounds exponentially

## **If wrong:**

* system stays flat or degrades

