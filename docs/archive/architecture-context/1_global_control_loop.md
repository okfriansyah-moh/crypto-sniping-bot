---

# **1\. Global Control Loop (New Core)**

Detect → Filter → Score → Select → Execute → Exit → Evaluate → Adjust

**Adjustment outputs:**

* thresholds (strict ↔ relaxed)  
* scoring weights  
* capital allocation  
* execution params

---

This loop is your **only source of edge**. If it’s imprecise, everything downstream (models, infra, speed) becomes irrelevant.

We’ll define it as a **deterministic control loop with measurable state transitions and explicit update rules**—no ambiguity.

---

Detect → Filter → Score → Select → Execute → Exit → Evaluate → Adjust

Each stage:

* consumes a DTO  
* produces a DTO  
* emits a DecisionLog  
* contributes to error signals

The loop runs continuously in **micro-batches (2–5s windows)** to balance latency vs decision quality.

---

# **2\. STAGE DEFINITIONS (PRECISE)**

---

## **2.1 DETECT (Signal Ingestion)**

### **Objective**

Capture **candidate opportunities early enough** to preserve edge.

### **Input**

* on-chain events (pair creation, liquidity add)  
* wallet activity (top traders)  
* surge signals

  ### **Output**

  type DetectOutput struct {  
     TokenAddress string  
     Timestamp int64  
     Source string // pool | wallet | surge  
  }  
  ---

  ### **Key Metric**

  DetectionLatency \= event\_time → detect\_time

Constraint:

DetectionLatency \< opportunity\_half\_life

---

### **Failure Mode**

* late detection → no edge left → wasted pipeline  
  ---

  ## **2.2 FILTER (Data Quality Gate)**

  ### **Objective**

Remove **invalid opportunities (traps)**

### **Decision**

PASS / REJECT / RISKY\_PASS

---

### **Output**

type FilterOutput struct {

   TokenAddress string

   RiskScore float64

   Decision string

}

---

### **Key Metric**

DQ \= 1 \- (rug\_loss\_rate)

---

### **Failure Modes**

* false positive → loss  
* false negative → missed alpha  
  ---

  ## **2.3 SCORE (Ranking Function)**

  ### **Objective**

Estimate **relative quality of opportunities**

---

### **Output**

type ScoreOutput struct {

   TokenAddress string

   Score float64

   Confidence float64

}

---

### **Key Metric**

RankingCorrelation \= corr(score\_rank, realized\_pnl\_rank)

---

### **Failure Mode**

* high score ≠ high return → useless scoring  
  ---

  ## **2.4 SELECT (Resource Constraint Solver)**

  ### **Objective**

Choose **subset of opportunities under constraints**

---

### **Input**

* scored tokens

  ### **Constraints**

* max positions (K)  
* capital limit  
* diversification  
  ---

  ### **Output**

  type SelectionOutput struct {  
     Selected \[\]Token  
  }  
  ---

  ### **Algorithm (deterministic)**

  sort by (score × probability × confidence)  
  take top K  
  apply constraints  
  ---

  ### **Key Metric**

  SelectionEfficiency \= PnL(top K) / PnL(all candidates)  
  ---

  ### **Failure Mode**

* selecting too many → dilution  
* selecting too few → missed opportunity  
  ---

  ## **2.5 EXECUTE (Action Realization)**

  ### **Objective**

Convert decision → position with minimal degradation

---

### **Output**

type ExecutionOutput struct {

   TokenAddress string

   EntryPrice float64

   LatencyMs int

   Slippage float64

   Success bool

}

---

### **Key Metrics**

ExecutionQuality \= actual\_entry / optimal\_entry

Latency

SlippageError

---

### **Failure Modes**

* tx fail  
* high slippage  
* late inclusion  
  ---

  ## **2.6 EXIT (Outcome Realization)**

  ### **Objective**

Convert position → realized PnL

---

### **Output**

type ExitOutput struct {

   TokenAddress string

   ExitPrice float64

   PnL float64

   DurationSec int

   ExitReason string

}

---

### **Key Metric**

ExitEfficiency \= realized\_pnl / max\_possible\_pnl

---

### **Failure Modes**

* exit too early → lost upside  
* exit too late → give back profit  
  ---

  ## **2.7 EVALUATE (Error Computation Engine)**

This is where intelligence starts.

---

### **Core Computations**

---

### **A. Prediction Error**

E\_pred \= expected\_return \- actual\_return

---

### **B. Classification Errors**

FalsePositive \= accepted && loss

FalseNegative \= rejected && would\_win

---

### **C. Execution Error**

E\_exec \= expected\_slippage \- actual\_slippage

E\_latency \= expected\_latency \- actual\_latency

---

### **D. Opportunity Loss**

MissedPnL \= PnL(rejected tokens that pumped)

---

### **Output**

type EvaluationOutput struct {

   FalsePositive bool

   FalseNegative bool

   PredictionError float64

   ExecutionError float64

}

---

## **2.8 ADJUST (Control Layer)**

This is the **only place system changes behavior**.

---

# **3\. ADJUSTMENT ENGINE (CORE LOGIC)**

---

## **3.1 Inputs**

Aggregated over window:

type Metrics struct {

   PassRate float64

   WinRate float64

   LossRate float64

   FalsePositiveRate float64

   FalseNegativeRate float64

   AvgSlippage float64

   AvgLatency float64

}

---

## **3.2 Adjustment Outputs**

Exactly 4 knobs:

1\. thresholds

2\. scoring weights

3\. capital allocation

4\. execution params

---

# **4\. ADJUSTMENT RULES (DETERMINISTIC)**

---

## **4.1 Threshold Controller (STRICT ↔ RELAX)**

if PassRate \== 0:

   relax\_thresholds(step\_small)

if FalseNegativeRate ↑:

   relax\_filters()

if LossRate ↑:

   tighten\_filters()

if FalsePositiveRate ↑:

   tighten\_filters()

---

### **Example:**

min\_liquidity: 10k → 8k

max\_tax: 10% → 8%

---

## **4.2 Scoring Weight Update**

Goal:

increase weight of predictive features

decrease noisy features

---

### **Method (simple \+ stable)**

new\_weight \= old\_weight \+ α × correlation(feature, pnl)

Constraints:

* bounded update  
* normalize weights  
  ---

  ## **4.3 Capital Allocation Update**

  if cohort\_win\_rate ↑:  
     increase allocation  
    
  if cohort\_loss\_rate ↑:  
     decrease allocation  
  ---

  ### **Cohort example:**

  liquidity 10k–20k tokens  
  tax 5–10%  
  ---

  ## **4.4 Execution Parameter Update**

  if latency ↑:  
     increase gas/priority\_fee  
    
  if slippage ↑:  
     reduce position size  
    
  if failure\_rate ↑:  
     reduce parallelism  
  ---

  # **5\. TIME STRUCTURE (IMPORTANT)**

  ---

  ## **Real-time loop (fast)**

  Detect → Execute  
  ---

  ## **Batch evaluation loop (slow)**

  Evaluate → Adjust

Frequency:

every 5–30 minutes OR N trades

---

# **6\. STABILITY CONSTRAINTS (NON-NEGOTIABLE)**

---

## **6.1 Bounded Updates**

Δparameter ≤ small\_step

---

## **6.2 Minimum Sample Size**

if trades \< N → no adjustment

---

## **6.3 Versioning**

each adjustment → new version\_id

---

## **6.4 No Multi-parameter Shock**

only adjust few parameters at once

---

# **7\. CONTROL OBJECTIVE (FORMAL)**

Maximize:

Expected PnL

Subject to:

LossRate \< threshold

Drawdown \< threshold

System stable

---

# **8\. SYSTEM BEHAVIOR (WHAT YOU SHOULD SEE)**

---

## **Healthy System**

PassRate: 1–3%

WinRate: improving

LossRate: controlled

FalseNegative: decreasing

FalsePositive: decreasing

---

## **Broken System**

### **Case 1 — Overfitting**

PassRate: 0%

No trades

---

### **Case 2 — Underfitting**

PassRate: 20%

LossRate high

---

### **Case 3 — No Learning**

metrics static over time

PnL stagnant

---

# **9\. FINAL INSIGHT**

This loop is essentially:

ONLINE LEARNING \+ CONTROL SYSTEM

Where:

* Detect/Filter/Score \= **model inference**  
* Evaluate \= **loss function**  
* Adjust \= **gradient step (controlled)**  
* Execute \= **real-world deployment**  
  ---

  # **10\. What Matters Most**

Not:

* more features  
* more signals

But:

quality of Evaluate \+ Adjust loop

