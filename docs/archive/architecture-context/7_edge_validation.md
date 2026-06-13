# **7\. Layer 5 — Edge Validation (Adaptive Gate)**

Reject if:

* probability \< dynamic threshold  
* slippage \> dynamic cap  
* latency too high vs opportunity decay  
* bad micro-regime

## **Adaptive Thresholds**

target\_pass\_rate \= 0.5%–5%

if pass\_rate \== 0 → relax  
if pass\_rate \> 10% → tighten

## **Feedback**

* false rejects vs false accepts tracked

---

This layer is the **last hard gate before capital is committed**. It must convert model outputs into a **binary decision with controlled risk and stable throughput**.

---

# **7\. LAYER 5 — EDGE VALIDATION (ADAPTIVE GATE)**

---

## **7.1 Objective (Formal)**

Accept only if the trade has **positive, risk-adjusted expectancy under real constraints**.

ACCEPT ⇔ EV\_adj \> 0 AND constraints satisfied

Where:

EV\_adj \=

 P \* G

\- (1 \- P) \* L

\- SlippageCost

\- LatencyCost

\- RiskPenalty

---

## **7.2 Inputs (Strict)**

From Layer 3–4:

* `EdgeDTO` (momentum, age, confidence)  
* `ProbabilityEstimateDTO` (P)  
* `SlippageEstimateDTO` (S)  
* `LatencyProfileDTO` (L\_t)  
* (optional) micro-regime features (recent market state)

---

## **7.3 Decision Pipeline (Deterministic)**

Order matters—cheap checks first.

1\) Hard rejects (invalid)

2\) Constraint checks (caps)

3\) EV check

4\) Confidence gate

→ ACCEPT / REJECT / EXPLORE

---

## **7.4 Hard Rejects**

Immediate drop regardless of EV.

if P is NaN or confidence low → REJECT

if AgeSec \> T\_cutoff → REJECT

if honeypot flag present (should already be blocked) → REJECT

---

## **7.5 Constraint Checks (Caps)**

### **7.5.1 Probability Threshold**

P ≥ θ\_p

Dynamic:

θ\_p ∈ \[0.45, 0.75\] (profile-dependent)

---

### **7.5.2 Slippage Cap**

S ≤ θ\_s

Dynamic:

θ\_s ∈ \[2%, 10%\] (depends on liquidity regime)

---

### **7.5.3 Latency vs Decay**

Define opportunity half-life `τ` (from historical cohort):

L\_t ≤ κ \* τ

Typical:

κ ∈ \[0.2, 0.5\]

If too slow → edge mostly gone.

---

### **7.5.4 Micro-Regime Filter**

Compute a lightweight regime score `R ∈ [0,1]`:

\- recent hit-rate (last N trades)

\- avg tx\_rate across tokens

\- avg entropy (market cleanliness)

Reject if:

R \< θ\_r

(avoid trading in “toxic” periods)

---

## **7.6 EV Check (Core)**

Compute:

EV \= P \* G \- (1 \- P) \* L \- S \- LatencyPenalty

Where:

* `G` \= expected gain (baseline from strategy, e.g., \+20–50%)  
* `L` \= expected loss (e.g., stop-loss magnitude)  
* `LatencyPenalty = G * (1 - e^{-λ * L_t})`

Accept if:

EV ≥ θ\_ev   (θ\_ev ≥ 0 with safety margin)

---

## **7.7 Confidence Gate**

Down-weight or reject low-confidence cases:

if overall\_confidence \< C\_min:

   REJECT or EXPLORE

Typical:

C\_min \= 0.5–0.7

---

## **7.8 Final Decision**

if hardReject → REJECT

else if any cap violated → REJECT

else if EV \< θ\_ev → REJECT

else if confidence low → EXPLORE

else → ACCEPT

---

## **7.9 Output DTO**

type ValidatedEdgeDTO struct {

   TokenAddress string

   Decision string // accept | reject | explore

   Probability float64

   ExpectedValue float64

   Slippage float64

   LatencyMs int

   Thresholds struct {

       Prob float64

       Slippage float64

       LatencyRatio float64

       EV float64

   }

   RegimeScore float64

   Confidence float64

   Version int

   Timestamp int64

}

---

# **7.10 Adaptive Threshold Controller**

---

## **7.10.1 Targets**

pass\_rate\_target \= 0.5% – 5%

fp\_rate ↓

fn\_rate ↓ (but not zero)

---

## **7.10.2 Metrics (rolling window)**

type GateMetrics struct {

   PassRate float64

   FalsePositiveRate float64 // accepted → loss

   FalseNegativeRate float64 // rejected → would-win

   AvgEV float64

}

---

## **7.10.3 Control Rules**

### **Throughput Control**

if PassRate \== 0:

   relax θ\_p (−Δ), relax θ\_ev (−Δ), increase θ\_s (+Δ)

if PassRate \> 10%:

   tighten θ\_p (+Δ), increase θ\_ev (+Δ), decrease θ\_s (−Δ)

---

### **Risk Control**

if FalsePositiveRate ↑:

   increase θ\_p

   increase θ\_ev

   decrease θ\_s

   tighten latency ratio (κ ↓)

if FalseNegativeRate ↑:

   decrease θ\_p

   decrease θ\_ev

   increase θ\_s

---

### **Regime Sensitivity**

if RegimeScore low:

   globally tighten (θ\_p↑, θ\_ev↑)

if RegimeScore high:

   allow mild relaxation

---

## **7.10.4 Step Size & Safety**

Δ small (e.g., 2–5% relative)

one parameter group per cycle

require N\_min samples (e.g., ≥ 30–50 trades)

---

## **7.10.5 Versioning**

Each adjustment:

version++

store thresholds snapshot

Rollback if:

AvgEV ↓ or FP spikes

---

# **7.11 False Rejects vs False Accepts (Labeling)**

---

## **7.11.1 False Accept (FP)**

Decision \= ACCEPT

AND realized\_pnl \< 0 (or hit SL quickly)

---

## **7.11.2 False Reject (FN)**

Requires shadow tracking:

Decision \= REJECT

AND max\_return\_in\_T ≥ target\_return

---

## **7.11.3 Attribution**

Store which constraint blocked/allowed:

type GateAttribution struct {

   ProbBlocked bool

   SlippageBlocked bool

   LatencyBlocked bool

   EVBlocked bool

}

Use to tune **specific thresholds**, not all at once.

---

# **7.12 Exploration Path (Controlled)**

To avoid starvation:

if Decision \= REJECT but close to thresholds:

   mark as EXPLORE

   allow small-cap trade (1–5% budget)

Criteria:

|P \- θ\_p| ≤ ε  OR  |EV \- θ\_ev| ≤ ε

---

# **7.13 Failure Modes & Guards**

---

### **Over-tight Gate (starvation)**

* `pass_rate → 0`  
* Fix: relax thresholds, enable exploration

---

### **Over-loose Gate (overtrading)**

* `pass_rate ↑`, FP ↑  
* Fix: tighten θ\_p, θ\_ev; lower θ\_s

---

### **Miscalibrated EV**

* EV positive but losses occur  
* Fix: recalibrate P, S, latency penalty (Layer 4\)

---

### **Latency blind spots**

* accepting trades with high L\_t  
* Fix: lower κ, increase penalty

---

# **7.14 Performance Constraints**

* compute time: **\< 20–50 ms per token**  
* pure function (no RPC)  
* deterministic given inputs \+ thresholds

---

# **7.15 What This Layer Guarantees**

* Trades only when **EV is positive under real friction**  
* Maintains **controlled pass rate**  
* Balances:  
  * **opportunity capture** (FN ↓)  
  * **risk containment** (FP ↓)

---

## **Final Insight**

This gate enforces:

“Good signal is not enough — only tradeable signal survives.”

If this layer is correct:

* you trade **less**, but with **positive expectancy**

If wrong:

* you either **do nothing** or **bleed slowly**

