# **4\. Layer 2 — Feature Extraction (Instrumented)**

## **Features**

* liquidity\_size, growth\_rate  
* tx\_velocity  
* holder\_distribution  
* wallet entropy (anti-wash)  
* contract flags

## **Output**

FeatureDTO \+ FeatureConfidence

## **Feedback**

* feature importance vs PnL  
* drift detection (features losing predictive power)

---

This layer turns raw on-chain events into a **compact, predictive state vector**. It must be **fast, consistent, and auditable**, because every downstream decision depends on it.

---

---

## **4.1 Objectives (Operational)**

* **Encode early market state** into features usable by scoring/probability  
* **Standardize inputs** (scale, normalize, timestamped)  
* **Attach confidence** to each feature (data completeness \+ stability)  
* **Emit attribution-ready data** for later learning

---

## **4.2 Feature Set (Exact Definitions)**

All features are computed over fixed windows (e.g., **t₀ \= pool creation**, windows: **\[0–30s\], \[30–120s\], \[2–5m\]**). Keep windows consistent.

---

### **4.2.1 Liquidity Size & Growth**

**Inputs**

* `liquidity_usd(t)`  
* LP events

**Features**

liquidity\_size\_0 \= liquidity\_usd(t\_now)

liquidity\_growth\_rate \= (liquidity\_usd(t\_now) \- liquidity\_usd(t\_Δ)) / Δt

liquidity\_volatility \= stddev(liquidity\_usd over window)

lp\_lock\_strength \= normalized(lock\_duration, locker\_trust)

**Normalization**

* log-scale for size: `log1p(liquidity_size_0)`  
* clamp growth to percentile bounds

---

### **4.2.2 Transaction Velocity**

**Inputs**

* swaps, transfers

**Features**

tx\_count\_1m

tx\_rate \= tx\_count\_Δ / Δt

buy\_sell\_ratio \= buys / max(sells,1)

unique\_traders\_1m

avg\_trade\_size

trade\_size\_dispersion (std/mean)

**Derived**

momentum\_proxy \= tx\_rate \* buy\_sell\_ratio

burstiness \= max(tx\_rate\_short) / max(tx\_rate\_long, ε)

---

### **4.2.3 Holder Distribution**

**Inputs**

* balances by address

**Features**

top1\_pct, top5\_pct, top10\_pct

gini\_coefficient

new\_holder\_rate \= Δ(unique\_holders)/Δt

holder\_churn \= (new \+ exited) / current\_holders

**Interpretation**

* high concentration → rug risk  
* rising new\_holder\_rate → adoption/momentum

---

### **4.2.4 Wallet Entropy (Anti-wash)**

**Inputs**

* trader addresses per window

**Feature**

p\_i \= trades\_by\_wallet\_i / total\_trades

wallet\_entropy \= \- Σ p\_i log(p\_i)

entropy\_norm \= wallet\_entropy / log(N)

**Aux**

repeat\_ratio \= repeated\_trades\_by\_same\_wallet / total\_trades

Low entropy \+ high repeat\_ratio ⇒ wash risk.

---

### **4.2.5 Contract Flags (Binary/Discrete)**

From static \+ simulated analysis:

has\_mint

has\_blacklist

is\_proxy

sell\_tax, buy\_tax

tax\_dynamic

lp\_locked

router\_compatible

Encode as:

* one-hot / binary  
* numeric for taxes

---

## **4.3 Feature Vector (Unified)**

type FeatureDTO struct {

   TokenAddress string

   // Liquidity

   LiquiditySize float64

   LiquidityGrowth float64

   LiquidityVolatility float64

   LPLockStrength float64

   // Activity

   TxRate float64

   BuySellRatio float64

   UniqueTraders float64

   AvgTradeSize float64

   Burstiness float64

   // Holders

   Top1Pct float64

   Top5Pct float64

   Gini float64

   NewHolderRate float64

   HolderChurn float64

   // Anti-wash

   WalletEntropy float64

   RepeatRatio float64

   // Contract

   SellTax float64

   BuyTax float64

   HasMint bool

   HasBlacklist bool

   IsProxy bool

   LPLocked bool

   Window string // e.g. "0-30s"

   Timestamp int64

   Version int

}

---

## **4.4 Feature Confidence (Per-Vector \+ Per-Field)**

You must quantify **data reliability**.

type FeatureConfidence struct {

   Overall float64            // \[0,1\]

   ByField map\[string\]float64 // per-feature confidence

   Reasons \[\]string           // e.g. "low\_sample", "rpc\_partial"

}

---

### **Confidence Rules**

if unique\_traders \< N\_min → confidence↓

if window too short → confidence↓

if RPC partial/missing → confidence↓

if feature stable across ticks → confidence↑

**Example**

TxRate confidence \= min(1, unique\_traders / 20\)

Entropy confidence \= min(1, total\_trades / 30\)

---

## **4.5 Normalization & Stability**

All numeric features must be:

* **bounded**: map to \[0,1\] or z-score capped  
* **monotonic where possible**  
* **robust to outliers**

Examples:

LiquiditySize \= log1p(liq) / log1p(L\_max)

BuySellRatio \= tanh(buy/sell \- 1\)

TxRate \= clip(tx\_rate / rate\_p95, 0, 1\)

Maintain **normalization stats** (rolling p50/p95) per chain.

---

## **4.6 Computation Model**

* Compute in **incremental windows** (don’t recompute full history)  
* Cache per token:  
  * last window aggregates  
  * rolling stats  
* Single pass per event batch

Pseudo:

agg := updateAggregates(prevAgg, newEvents)

features := computeFeatures(agg)

confidence := computeConfidence(agg, features)

emit(FeatureDTO, FeatureConfidence)

Latency target: **\< 100–200 ms** per token.

---

## **4.7 Feedback: Feature Importance vs PnL**

You must measure **predictive power per feature**.

---

### **4.7.1 Cohort Binning**

For each feature `f`, bin values:

f ∈ \[0-0.2), \[0.2-0.4), ... \[0.8-1.0\]

Compute per bin:

win\_rate

avg\_pnl

expectancy \= mean(pnl)

---

### **4.7.2 Correlation (rank-based)**

ρ\_f \= Spearman(feature\_value, realized\_pnl)

Store per version/window.

---

### **4.7.3 Lift**

lift\_top \= avg\_pnl(top\_20% f) / avg\_pnl(all)

---

### **4.7.4 Output (for learning)**

type FeaturePerformance struct {

   Feature string

   Spearman float64

   LiftTop float64

   Bins \[\]BinStat

   SampleSize int

   Version int

}

Used to adjust:

* scoring weights  
* thresholds (e.g., entropy floor)

---

## **4.8 Drift Detection (Feature Decay)**

Detect when a feature **loses predictive power**.

---

### **4.8.1 Distribution Drift**

Compare current vs baseline:

PSI (Population Stability Index)

PSI \> 0.2 → moderate drift

PSI \> 0.3 → significant drift

---

### **4.8.2 Predictive Drift**

Δρ\_f \= ρ\_f(current) \- ρ\_f(baseline)

if Δρ\_f \< \-δ → feature degraded

---

### **4.8.3 Decision Impact Drift**

if top-bin expectancy ↓ significantly → degrade weight

---

### **4.8.4 Actions (bounded)**

if drift\_detected:

 reduce weight(f) by ε

 increase confidence threshold for f

 flag for review

Never drop a feature to zero in one step.

---

## **4.9 Quality Gates (before emit)**

* required fields present  
* confidence ≥ `C_min` for critical features (entropy, tax, liquidity)  
* values within bounds

If not:

mark FeatureConfidence.Overall low

emit anyway (do NOT block), downstream can down-weight

---

## **4.10 Failure Modes & Guards**

* **Sparse data early** → low confidence → downstream down-weights  
* **RPC inconsistency** → retry once; else mark partial  
* **Outlier spikes** → clipped by normalization  
* **Feature leakage (using future data)** → strict windowing by timestamp

---

## **4.11 What This Layer Guarantees**

* A **stable, normalized feature vector** per token per window  
* **Explicit confidence** to avoid over-trusting noisy signals  
* **Attribution-ready metrics** for learning (correlation, lift)  
* **Drift awareness** to prevent stale signals from dominating

If this layer is clean, scoring becomes a **weighting problem**. If it’s noisy, everything downstream is guesswork.

