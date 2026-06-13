# **5\. Layer 3 — Signal & Edge Discovery (Sniper Mode)**

## **Edge Definition**

NEW\_LAUNCH\_EDGE

## **Logic**

if new\_pool && quality\_pass && early\_momentum:  
 emit EdgeDTO

## **Adaptive Gate**

* min momentum threshold adjusts with market conditions

## **Feedback**

* which edges convert to profitable exits within T minutes

---

This layer decides **whether a token is even worth competing for**. It converts features into a **time-sensitive trading hypothesis**.

---

# **5\. LAYER 3 — SIGNAL & EDGE DISCOVERY (SNIPER MODE)**

---

## **5.1 Edge Definition (Formal)**

NEW\_LAUNCH\_EDGE \=

 early-stage token

 \+ sufficient data quality

 \+ measurable early momentum

 \+ within exploitable time window

---

### **Mathematical Form**

EdgeExists \=

 I(new\_pool)

× I(DataQuality ∈ {pass, risky-pass})

× I(MomentumScore ≥ θ\_momentum(t))

Where:

* `θ_momentum(t)` is adaptive threshold  
* `t` \= time since launch

---

## **5.2 Inputs (Strict)**

From previous layers:

* `DataQualityDTO`  
* `FeatureDTO`  
* `FeatureConfidence`

NO external calls here.

---

## **5.3 Core Logic (Deterministic)**

func detectEdge(f FeatureDTO, dq DataQualityDTO, conf FeatureConfidence, now int64) (EdgeDTO, bool) {

   if dq.Decision \== "reject" {

       return nil, false

   }

   if \!isNewPool(f.Timestamp, now) {

       return nil, false

   }

   momentum := computeMomentum(f)

   threshold := adaptiveMomentumThreshold(now \- f.Timestamp)

   if momentum \< threshold {

       return nil, false

   }

   return EdgeDTO{

       TokenAddress: f.TokenAddress,

       Momentum: momentum,

       Confidence: conf.Overall,

       AgeSec: now \- f.Timestamp,

   }, true

}

---

## **5.4 "New Pool" Constraint (Hard Gate)**

---

### **Definition**

AgeSec ≤ T\_max\_edge\_window

---

### **Typical Values**

T\_max\_edge\_window \= 5–15 minutes

---

### **Why**

after this → edge mostly gone

---

## **5.5 Momentum Model (Core Signal)**

Momentum must capture:

* speed of participation  
* direction (buy pressure)  
* diversity (not fake)

---

## **5.5.1 Momentum Score (Composite)**

Momentum \=

 w1 \* TxRate\_norm

\+ w2 \* BuySellRatio\_norm

\+ w3 \* NewHolderRate\_norm

\+ w4 \* Entropy\_norm

\+ w5 \* LiquidityGrowth\_norm

---

### **Normalized Inputs (from Layer 2\)**

* `TxRate_norm`  
* `BuySellRatio_norm`  
* `NewHolderRate_norm`  
* `Entropy_norm`  
* `LiquidityGrowth_norm`

---

### **Weight Constraints**

Σ w\_i \= 1

w\_i ≥ 0

---

## **5.5.2 Anti-Fake Momentum Correction**

Momentum must be penalized if:

low entropy

high repeat\_ratio

high concentration

---

### **Penalty**

AdjustedMomentum \=

 Momentum × (1 \- wash\_penalty)

Where:

wash\_penalty \=

 α \* (1 \- entropy\_norm)

\+ β \* repeat\_ratio

---

## **5.6 Adaptive Momentum Threshold**

---

## **5.6.1 Problem**

Fixed threshold fails because:

* market changes (slow vs hype)  
* chain-specific behavior

---

## **5.6.2 Target Behavior**

Maintain edge discovery rate in stable band

---

### **Target Range**

EdgePassRate \= 0.5% – 5%

---

## **5.6.3 Controller**

if edge\_pass\_rate \== 0:

   decrease θ\_momentum

if false\_positive\_rate ↑:

   increase θ\_momentum

if false\_negative\_rate ↑:

   decrease θ\_momentum

if too many edges:

   increase θ\_momentum

---

## **5.6.4 Time-Decay Adjustment**

Momentum requirement should decrease with time:

θ\_momentum(t) \= base\_threshold × exp(-λ \* t)

---

### **Intuition**

early → need strong signal

later → allow weaker signal

---

## **5.7 Edge Confidence**

Derived from:

EdgeConfidence \=

 FeatureConfidence.Overall

× stability(momentum over Δt)

---

### **Stability Check**

if momentum fluctuates wildly → confidence↓

---

## **5.8 Output DTO**

type EdgeDTO struct {

   TokenAddress string

   EdgeType string // NEW\_LAUNCH\_EDGE

   Momentum float64

   AdjustedMomentum float64

   Threshold float64

   Confidence float64

   AgeSec int

   Source string // pool | wallet | surge

   Version int

   Timestamp int64

}

---

## **5.9 Adaptive Gate Behavior**

---

## **5.9.1 Two-Level Gate**

Level 1: DataQuality → pass/risky-pass

Level 2: Momentum ≥ θ

---

## **5.9.2 Exploration Path**

If system in exploration mode:

allow slightly below threshold

if momentum ≥ (θ \- ε):

   emit edge (flagged)

---

## **5.10 Feedback Loop (Critical)**

---

## **5.10.1 Labeling Edges**

For each EdgeDTO:

Outcome:

 success → PnL ≥ target within T

 fail → otherwise

---

## **5.10.2 Time Window**

T \= 5–15 minutes

---

## **5.10.3 Metrics**

EdgeHitRate \= success\_edges / total\_edges

EdgePrecision \= profitable\_entries / total\_entries

---

## **5.10.4 Attribution**

For each edge:

store:

\- momentum value

\- features

\- outcome

---

## **5.10.5 Learning Outputs**

### **A. Threshold tuning**

if many failures at low momentum:

   increase θ

if many missed winners:

   decrease θ

---

### **B. Weight tuning**

increase weight of features correlated with success

---

### **C. Feature pruning**

if feature contributes noise:

   reduce weight → 0 gradually

---

## **5.11 Failure Modes & Guards**

---

### **Failure 1 — Fake Momentum Trap**

Symptom:

* high tx\_rate but low entropy

Guard:

* entropy penalty (mandatory)

---

### **Failure 2 — Late Momentum**

Symptom:

* strong signal but already peaked

Guard:

if AgeSec \> T\_cutoff:

   reject regardless of momentum

---

### **Failure 3 — Over-triggering**

Symptom:

* too many edges

Guard:

* raise θ\_momentum

---

### **Failure 4 — Under-triggering**

Symptom:

* no edges

Guard:

* lower θ\_momentum  
* enable exploration band

---

## **5.12 Performance Constraints**

* compute time: \< 50–100 ms per token  
* no RPC calls  
* pure function of inputs

---

## **5.13 What This Layer Guarantees**

* converts **features → actionable opportunity**  
* filters out:  
  * weak momentum  
  * fake activity  
  * late entries  
* produces **time-sensitive edge candidates**

---

# **FINAL INSIGHT**

This layer answers one question:

“Is there a tradable opportunity RIGHT NOW?”

Not:

* “Is this token good?”  
* “Will it 100x?”

---

## **If this layer is correct:**

* downstream selection becomes easy  
* execution captures real edge

## **If wrong:**

* you chase noise  
* or miss everything

