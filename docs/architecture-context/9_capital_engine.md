# **9\. Layer 7 — Capital Engine (Risk-Aware, Adaptive)**

## **Allocation**

size ∝ score × probability × confidence

## **Constraints**

* max per position  
* max concurrent positions  
* exploration budget (1–5%)

## **Adaptive**

* increase size for cohorts with positive expectancy  
* shrink for underperforming cohorts

---

This layer turns **selected edges → position sizes**. It is a **risk allocator** under uncertainty, not a profit maximizer. If sizing is wrong, a good system still loses.

---

# **9\. LAYER 7 — CAPITAL ENGINE (RISK-AWARE, ADAPTIVE)**

---

## **9.1 Objective (Formal)**

Allocate capital to maximize **portfolio expectancy** under strict risk constraints:

maximize   Σ (size\_i × EV\_i)

subject to Σ size\_i ≤ C\_total

          size\_i ≤ cap\_per\_position

          active\_positions ≤ K

          risk constraints satisfied

---

## **9.2 Inputs**

* `SelectedEdge[]` (from Layer 6\) with:  
  * `EV_i`, `P_i`, `Score_i`, `Confidence_i`  
  * `Slippage_i`, `Latency_i`  
  * cohort tags (liquidity band, tax band, entropy band, etc.)  
* Portfolio state:  
  * `C_total` (available capital)  
  * `active_positions`  
  * recent cohort performance stats

---

## **9.3 Base Allocation Rule (Deterministic)**

Start with a **raw weight** per edge:

w\_i \=

 Score\_i\_norm

× P\_i

× Confidence\_i

Normalize:

ŵ\_i \= w\_i / Σ w\_j

Propose size:

size\_i\* \= ŷ\_i × C\_alloc

Where:

* `C_alloc ≤ C_total` (may reserve buffer)  
* `Score_i_norm ∈ [0,1]`

---

## **9.4 Risk-Adjusted Sizing (Core)**

Convert proposed size to **final size** using penalties:

size\_i \=

 size\_i\*

× (1 \- SlippagePenalty\_i)

× (1 \- LatencyPenalty\_i)

× CohortMultiplier\_i

---

### **9.4.1 Slippage Penalty**

SlippagePenalty\_i \= clip(S\_i / θ\_s\_eff, 0, 1\)

---

### **9.4.2 Latency Penalty**

LatencyPenalty\_i \= 1 \- exp(-λ \* L\_t\_i)

---

### **9.4.3 Cohort Multiplier (Adaptive)**

CohortMultiplier\_i \= f(expectancy\_cohort)

Example:

if cohort\_EV \> 0 → 1.0 – 1.5

if cohort\_EV ≈ 0 → 0.5 – 1.0

if cohort\_EV \< 0 → 0.1 – 0.5

---

## **9.5 Hard Constraints (Enforced After Sizing)**

---

### **9.5.1 Max Per Position**

size\_i ≤ c\_max

Typical:

c\_max \= 5% – 20% of C\_total (strategy dependent)

---

### **9.5.2 Max Concurrent Positions**

active\_positions \+ new\_positions ≤ K

(usually 5–10)

---

### **9.5.3 Minimum Viable Size**

Avoid dust trades:

size\_i ≥ c\_min

Else:

* drop or merge into reserve

---

### **9.5.4 Exploration Budget**

Reserve:

C\_explore \= 1% – 5% of C\_total

Rules:

only used for Bucket \= "explore"

size\_i\_explore ≤ small\_cap (e.g., 0.2–1% each)

---

### **9.5.5 Capital Conservation**

Σ size\_i ≤ C\_total

If overflow:

* scale down proportionally

---

## **9.6 Final Allocation DTO**

type AllocationDTO struct {

   TokenAddress string

   FinalSize float64

   Components struct {

       RawWeight float64

       NormalizedWeight float64

       SlippagePenalty float64

       LatencyPenalty float64

       CohortMultiplier float64

   }

   Bucket string // primary | explore

   Version int

   Timestamp int64

}

---

## **9.7 Adaptive Mechanism (Cohort-Based Learning)**

---

### **9.7.1 Cohort Definition**

Group trades by:

\- liquidity band (5–10k, 10–20k, …)

\- tax band

\- entropy band

\- entry latency band

\- momentum band

---

### **9.7.2 Metrics per Cohort**

win\_rate

avg\_pnl

expectancy \= mean(pnl)

drawdown

---

### **9.7.3 Multiplier Update (Bounded)**

mult\_new \= mult\_old \+ α × (expectancy \- baseline)

Constraints:

* `α` small (e.g., 0.05)  
* clamp to \[0.1, 1.5\]  
* require `N ≥ N_min` samples

---

### **9.7.4 Capital Shift**

if cohort performs well:

   increase its allocation share (via multiplier)

if cohort underperforms:

   shrink allocation

---

## **9.8 Portfolio-Level Risk Controls**

---

### **9.8.1 Exposure Limits**

max\_exposure\_per\_cluster ≤ X%

max\_exposure\_per\_time\_window ≤ Y%

(align with Selection diversity)

---

### **9.8.2 Drawdown Guard**

If recent drawdown exceeds threshold:

reduce all sizes by factor γ (e.g., 0.5)

or pause new allocations

---

### **9.8.3 Volatility Scaling**

If market volatility ↑:

scale sizes down (risk parity behavior)

---

## **9.9 Rebalancing (Intra-Batch)**

If some edges become infeasible post-check:

* redistribute freed capital to next best edges (respecting caps)  
* never exceed K or diversity limits

---

## **9.10 Failure Modes & Guards**

---

### **Over-allocation to noisy signals**

* Guard: confidence \+ penalties \+ cohort multiplier

---

### **Under-allocation (missing upside)**

* Guard: normalization \+ minimum size \+ exploration

---

### **Concentration risk**

* Guard: per-position cap \+ diversity (upstream) \+ exposure caps

---

### **Slippage blowups**

* Guard: penalty \+ feasibility filter \+ size reduction

---

## **9.11 Performance Constraints**

* compute time: **\< 5–10 ms**  
* no external calls  
* deterministic math only

---

## **9.12 What This Layer Guarantees**

* Capital is **proportional to quality and confidence**  
* Losses are **bounded by caps and penalties**  
* System **learns where to size up/down over time**  
* Exploration is **funded but contained**

---

## **Final Insight**

Selection finds opportunities.

Capital decides whether you survive.

A small edge with correct sizing compounds.  
 A strong edge with bad sizing blows up.

