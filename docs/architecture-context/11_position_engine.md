# **11\. Layer 9 — Position Engine (Fast Exit, Adaptive)**

## **Rules (baseline)**

* TP1: \+20% (partial)  
* TP2: \+50% (rest)  
* SL: \-10%  
* TIME: 10–30 min

## **Adaptive**

* tune TP/SL/time per cohort (liquidity band, tax band)

## **Feedback**

* optimal exit vs realized exit

---

This layer is where **profit is realized or destroyed**.  
 Entry gives you *potential*, but exit determines *actual PnL*.

You are not optimizing “max gain” — you are optimizing:

maximize realized PnL under uncertainty \+ time decay

---

# **11\. LAYER 9 — POSITION ENGINE (FAST EXIT, ADAPTIVE)**

---

# **11.1 Objective (Formal)**

Convert open position → **optimal exit decision under time \+ risk constraints**

ExitDecision \= argmax(realized\_value \- risk\_of\_reversal \- time\_decay)

---

# **11.2 State Model (Per Position)**

Each position maintains:

type PositionState struct {

   TokenAddress string

   EntryPrice float64

   CurrentPrice float64

   PeakPrice float64

   PnL float64

   AgeSec int

   Size float64

   RemainingSize float64

   ExitStage string // none | TP1\_hit | TP2\_hit | closed

   CohortID string

}

---

# **11.3 Baseline Exit Rules (Deterministic)**

---

## **11.3.1 Take Profit 1 (Partial Exit)**

TP1: \+20%

sell: 50% position

---

## **11.3.2 Take Profit 2 (Full Exit)**

TP2: \+50%

sell: remaining 50%

---

## **11.3.3 Stop Loss**

SL: \-10%

sell: 100%

---

## **11.3.4 Time Exit (Critical for sniper)**

TIME: 10–30 minutes

---

### **Rule**

if AgeSec ≥ T\_max → force exit

---

## **11.3.5 Trailing Protection (Optional but recommended)**

After TP1:

lock profit:

if price drops X% from peak → exit

Example:

trail \= 10–15% from peak

---

# **11.4 Exit Decision Logic (Priority Order)**

if SL hit:

   exit\_all

else if TP2 hit:

   exit\_all

else if TP1 hit and not yet executed:

   exit\_partial

else if trailing\_stop\_triggered:

   exit\_all

else if time\_expired:

   exit\_all

---

# **11.5 Price Tracking (Real-time Loop)**

Must track:

\- current price

\- peak price since entry

\- time since entry

Update loop:

on price update:

   update PeakPrice

   recompute PnL

   evaluate exit rules

Latency requirement:

* \< 200–500 ms reaction

---

# **11.6 Adaptive Exit (Core Intelligence)**

Static TP/SL is not enough.

You adapt per **cohort behavior**.

---

## **11.6.1 Cohort Definition**

Group positions by:

\- liquidity band

\- tax band

\- entropy band

\- entry latency band

\- momentum band

---

## **11.6.2 Cohort Metrics**

For each cohort:

avg\_peak\_return

avg\_time\_to\_peak

avg\_drawdown\_after\_peak

win\_rate

---

## **11.6.3 Adaptive TP/SL**

---

### **Example Adjustments**

if cohort peaks early (≤ 3 min):

   TP1 \= 15%

   TP2 \= 30%

   TIME \= 10 min

if cohort trends longer:

   TP1 \= 25%

   TP2 \= 60%

   TIME \= 20–30 min

if volatile:

   SL tighter (e.g., \-7%)

---

## **11.6.4 Parameter Update (Bounded)**

param\_new \= param\_old \+ α × (observed \- expected)

Constraints:

* small α (e.g., 0.05)  
* bounded range  
* require N samples

---

# **11.7 Exit Efficiency (Key Metric)**

---

## **11.7.1 Definition**

ExitEfficiency \=

 realized\_pnl / max\_possible\_pnl

---

### **Example**

max \= \+100%

you exit at \+30%

efficiency \= 0.3

---

## **11.7.2 Goal**

maximize efficiency without increasing risk

---

# **11.8 Feedback Loop (Critical)**

---

## **11.8.1 Track for Each Trade**

entry\_price

exit\_price

peak\_price

time\_to\_peak

time\_to\_exit

---

## **11.8.2 Compute**

---

### **A. Missed Profit**

missed \= peak\_price \- exit\_price

---

### **B. Overstay Loss**

overstay \= exit\_price \- peak\_after\_exit

---

## **11.8.3 Learning Signals**

---

### **Case 1 — Exit too early**

peak \>\> exit

→ increase TP thresholds

---

### **Case 2 — Exit too late**

exit \<\< peak

→ tighten TP / add trailing

---

### **Case 3 — Time decay**

profit early, then flat/down

→ shorten TIME window

---

# **11.9 Time Decay Model (IMPORTANT)**

Most sniper trades follow:

pump → peak → dump

---

## **Model**

ExpectedValue(t) \= peak × exp(-λ × t)

---

### **Implication**

holding too long reduces EV

---

# **11.10 Failure Modes & Guards**

---

## **Failure 1 — Greed (hold too long)**

Symptom:

* give back profit

Fix:

* enforce time exit  
* trailing stop

---

## **Failure 2 — Fear (exit too early)**

Symptom:

* low efficiency

Fix:

* adjust TP upward per cohort

---

## **Failure 3 — No exit discipline**

Symptom:

* inconsistent outcomes

Fix:

* deterministic rule enforcement

---

## **Failure 4 — Ignoring volatility**

Symptom:

* SL too wide or too tight

Fix:

* volatility-aware SL

---

# **11.11 Execution of Exit (Integration with Layer 8\)**

* exit \= same execution engine  
* uses:  
  * prebuilt sell calldata  
  * priority fee logic  
* partial exits handled as separate tx

---

# **11.12 Performance Constraints**

* decision latency: \< 50 ms  
* price update loop: \< 200 ms  
* no heavy computation

---

# **11.13 What This Layer Guarantees**

* profits are **captured, not theoretical**  
* losses are **bounded**  
* system **adapts exit timing to reality**  
* avoids:  
  * holding too long  
  * exiting randomly

---

# **FINAL INSIGHT**

Entry gives you opportunity

Exit defines your outcome

Most systems focus on entry.

Your edge compounds here:

better exits × thousands of trades \= massive difference

