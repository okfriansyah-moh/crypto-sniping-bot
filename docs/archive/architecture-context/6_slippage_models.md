# **6\. Layer 4 — Probability / Slippage / Latency Models**

## **A. Probability**

* P(pump within 5–10 min)

## **B. Slippage**

* expected price impact at your size

## **C. Latency**

* detection → submit → inclusion delay

## **DTOs (from contract layer)**

* ProbabilityEstimateDTO  
* SlippageEstimateDTO  
* LatencyProfileDTO

## **Adaptive Calibration**

error \= actual \- predicted  
update coefficients (bounded, versioned)  
---

This layer answers a single hard question:

“Even if an edge exists → is it worth trading after real-world friction?”

You are converting **signal → expected value (EV)**.

---

# **6\. LAYER 4 — PROBABILITY / SLIPPAGE / LATENCY MODELS**

---

# **6.1 OVERALL FORMULATION**

You are estimating:

ExpectedValue \= P(success) × Gain

             \- (1 \- P(success)) × Loss

             \- SlippageCost

             \- LatencyCost

Where:

* Probability → will it pump?  
* Slippage → how much you lose entering  
* Latency → how much edge decays before execution

---

# **6.2 A. PROBABILITY MODEL**

---

## **6.2.1 Definition**

P \= P(price increases ≥ target\_return within T window)

---

## **6.2.2 Target**

T \= 5–10 minutes

target\_return \= \+20% (example baseline)

---

## **6.2.3 Model Input (from Layer 2 \+ 3\)**

\- Momentum (adjusted)

\- TxRate

\- BuySellRatio

\- NewHolderRate

\- WalletEntropy

\- LiquiditySize

\- LiquidityGrowth

\- HolderConcentration

---

## **6.2.4 Model Form (Practical)**

Start simple:

P \= sigmoid(

     w1 \* momentum

   \+ w2 \* tx\_rate

   \+ w3 \* buy\_sell\_ratio

   \+ w4 \* entropy

   \+ w5 \* liquidity\_growth

   \- w6 \* concentration

)

---

### **Properties**

* bounded \[0,1\]  
* monotonic  
* interpretable

---

## **6.2.5 Calibration Target**

Predicted P ≈ Actual success frequency

Example:

tokens with P=0.7 → \~70% should succeed

---

## **6.2.6 Output DTO**

type ProbabilityEstimateDTO struct {

   TokenAddress string

   Probability float64

   TargetReturn float64

   HorizonSec int

   Confidence float64

   Version int

   Timestamp int64

}

---

# **6.3 B. SLIPPAGE MODEL**

---

## **6.3.1 Definition**

Slippage \= price\_impact \+ execution\_cost

---

## **6.3.2 Key Factors**

\- pool liquidity

\- trade size

\- pool depth curve

\- volatility

---

## **6.3.3 Model (AMM Approximation)**

For constant product AMM:

price\_impact ≈ trade\_size / liquidity

More precisely:

ΔP ≈ (Δx / (x \+ Δx))

---

## **6.3.4 Practical Model**

Slippage \=

 k1 \* (position\_size / liquidity\_size)

\+ k2 \* volatility

---

## **6.3.5 Adjustments**

Increase slippage if:

\- high tx\_rate (competition)

\- low liquidity

\- high momentum (crowded entry)

---

## **6.3.6 Output DTO**

type SlippageEstimateDTO struct {

   TokenAddress string

   ExpectedSlippage float64

   PositionSize float64

   Liquidity float64

   Confidence float64

   Version int

   Timestamp int64

}

---

# **6.4 C. LATENCY MODEL**

---

## **6.4.1 Definition**

Latency \= time from detection → transaction inclusion

---

## **6.4.2 Components**

1\. detection\_delay

2\. processing\_delay

3\. submission\_delay

4\. network\_delay

5\. block\_inclusion\_delay

---

## **6.4.3 Model**

TotalLatency \=

 t\_detect

\+ t\_compute

\+ t\_submit

\+ t\_network

\+ t\_block

---

## **6.4.4 Critical Metric**

EdgeDecay \= f(latency)

---

### **Example**

if pump happens in 30s

and latency \= 20s

→ most edge gone

---

## **6.4.5 Latency Penalty**

EffectiveEdge \= Edge × exp(-λ × latency)

---

## **6.4.6 Output DTO**

type LatencyProfileDTO struct {

   TokenAddress string

   EstimatedLatencyMs int

   ExpectedInclusionBlock int

   Confidence float64

   Version int

   Timestamp int64

}

---

# **6.5 COMBINED DECISION INPUT**

This layer produces:

\- ProbabilityEstimateDTO

\- SlippageEstimateDTO

\- LatencyProfileDTO

These feed into Layer 5 (validation).

---

# **6.6 ADAPTIVE CALIBRATION (CRITICAL)**

This is where models improve.

---

## **6.6.1 Error Definition**

---

### **A. Probability Error**

E\_prob \= actual\_outcome \- predicted\_probability

---

### **B. Slippage Error**

E\_slip \= actual\_slippage \- expected\_slippage

---

### **C. Latency Error**

E\_lat \= actual\_latency \- predicted\_latency

---

## **6.6.2 Update Rule (Bounded)**

---

### **Probability Weights**

w\_new \= w\_old \+ α × error × feature\_value

Constraints:

* small α (e.g. 0.01)  
* normalized weights  
* clipped updates

---

### **Slippage Coefficients**

k\_new \= k\_old \+ β × E\_slip

---

### **Latency Model**

latency\_estimate \=

 moving\_average(previous\_latency)

---

## **6.6.3 Versioning**

Every update:

model\_version++

store parameters snapshot

---

## **6.6.4 Safety Constraints**

\- require N samples before update

\- cap max parameter change per cycle

\- rollback if performance worsens

---

# **6.7 CONFIDENCE PROPAGATION**

Each model outputs confidence:

low data → low confidence → downstream discount

---

# **6.8 FAILURE MODES & GUARDS**

---

## **Failure 1 — Overestimated Probability**

Symptom:

* high P but losses

Fix:

* recalibrate weights  
* increase penalty

---

## **Failure 2 — Underestimated Slippage**

Symptom:

* actual entry much worse

Fix:

* increase slippage coefficient  
* reduce position size

---

## **Failure 3 — Latency Blindness**

Symptom:

* entering too late

Fix:

* increase latency penalty  
* reject high-latency trades

---

# **6.9 PERFORMANCE REQUIREMENTS**

* compute time: \< 50–100 ms  
* no heavy ML (keep deterministic)  
* incremental updates only

---

# **6.10 WHAT THIS LAYER GUARANTEES**

* converts **edge → expected value**  
* prevents:  
  * chasing low-probability trades  
  * entering illiquid traps  
  * being too late

---

# **FINAL INSIGHT**

This layer is where most bots fail.

They assume:

edge exists → profit

You enforce:

edge × probability × execution reality → profit

---

# **If this layer is correct:**

* you trade less  
* but with **positive expectancy**

If wrong:

* you trade often  
* but lose slowly

