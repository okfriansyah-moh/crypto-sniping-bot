---

# **3\. Layer 1 — Data Quality Engine (Adaptive Firewall)**

## **3.1 Responsibilities**

* block scams/manipulation  
* **learn false positives/negatives**  
* **adapt thresholds**

## **3.2 Detectors**

* **Wash trading**  
  * low unique wallets / high tx count  
* **Rug pull risk**  
  * LP unlockable, owner privileges  
* **Honeypot**  
  * buy/sell simulation  
* **Fake liquidity**  
  * LP add/remove pattern  
* **Tax manipulation**  
  * dynamic or high tax

## **3.3 Output (DTO)**

type DataQualityDTO struct {  
   RiskScore float64  
   Flags \[\]string  
   Decision string // pass | reject | risky-pass  
}

## **3.4 Adaptive Strictness**

Maintain **threshold profiles**:

profiles:  
 strict:  
   max\_tax: 8  
   min\_liquidity: 20k  
 balanced:  
   max\_tax: 12  
   min\_liquidity: 10k  
 exploration:  
   max\_tax: 15  
   min\_liquidity: 5k

### **Controller logic**

if false\_negative\_rate ↑ → relax  
if rug\_loss\_rate ↑ → tighten

## **3.5 Learning Signals**

* rejected → later pump → **false negative**  
* accepted → rug → **false positive**

---

This layer is your **hard gate against adversarial data**. If it’s weak, nothing else matters.  
 We’ll define it as a **deterministic risk engine \+ adaptive controller** with measurable error signals.

---

# **3\. LAYER 1 — DATA QUALITY ENGINE (ADAPTIVE FIREWALL)**

---

## **3.1 Responsibilities (Operationalized)**

### **A. Block scams/manipulation → binary gate \+ risk score**

* Produce **Decision ∈ {pass, risky-pass, reject}**  
* Must be **fast (\<200–500ms)** and **idempotent**

### **B. Learn errors → label outcomes**

* Tag each decision with later outcomes:  
  * `false_positive` (accepted → loss/rug)  
  * `false_negative` (rejected → pump)

### **C. Adapt thresholds → controlled updates**

* Adjust **threshold profiles** (strict/balanced/exploration)  
* Updates are **bounded, versioned, sample-gated**

---

## **3.2 Detectors (Concrete Algorithms)**

All detectors output **\[0,1\] risk contributions** and **flags**. Final `RiskScore` is a weighted aggregate.

---

### **3.2.1 Wash Trading Detector**

**Signals**

* `tx_count_1m`  
* `unique_wallets_1m`  
* `wallet_entropy` (Shannon entropy over traders)  
* `repeat_ratio` (same wallets looping)

**Heuristics**

if tx\_count\_1m high AND unique\_wallets\_1m low → high risk

if wallet\_entropy \< H\_min → high risk

if repeat\_ratio \> R\_max → high risk

**Score**

wash\_risk \=

 w1 \* (tx\_count\_1m / max(unique\_wallets\_1m,1))\_norm

\+ w2 \* (1 \- entropy\_norm)

\+ w3 \* repeat\_ratio

**Flags**

* `WASH_LOW_UNIQUENESS`  
* `WASH_LOOP_TRADES`

---

### **3.2.2 Rug Pull Risk Detector**

**On-chain checks**

* LP lock status (locker contract, lock duration)  
* owner privileges:  
  * `mint()`, `setTax()`, `blacklist()`  
* proxy/upgradeable patterns  
* top holder concentration

**Heuristics**

if LP not locked OR lock\_duration \< T\_min → risk↑

if owner can mint/blacklist → risk↑

if top\_5\_holders \> P\_max → risk↑

**Score**

rug\_risk \=

 a1 \* (1 \- lp\_lock\_strength\_norm)

\+ a2 \* owner\_privilege\_score

\+ a3 \* holder\_concentration\_norm

**Flags**

* `LP_UNLOCKED`  
* `OWNER_PRIVILEGED`  
* `HOLDER_CONCENTRATED`

---

### **3.2.3 Honeypot Detector**

**Method**

* Simulate `buy` then `sell` via router (dry-run / callStatic)

**Checks**

* sell revert / cannot estimate gas  
* effective tax on sell

**Heuristics**

if sell\_reverts → reject

if effective\_sell\_tax \> max\_tax → risk↑

**Score**

* binary spike if revert  
* continuous for tax

**Flags**

* `HONEYPOT_SELL_FAIL`  
* `SELL_TAX_HIGH`

---

### **3.2.4 Fake Liquidity Detector**

**Signals**

* LP add/remove events in short window  
* LP token distribution (burn vs wallet)  
* liquidity volatility

**Heuristics**

if liquidity\_added then removed within Δt\_small → risk↑

if LP tokens not burned/locked → risk↑

if liquidity volatility high early → risk↑

**Score**

liq\_risk \=

 b1 \* short\_term\_liq\_volatility

\+ b2 \* (1 \- lp\_lock\_strength\_norm)

\+ b3 \* rapid\_add\_remove\_indicator

**Flags**

* `LP_VOLATILE`  
* `LP_NOT_LOCKED`  
* `LP_FLASH_ADDED_REMOVED`

---

### **3.2.5 Tax Manipulation Detector**

**Signals**

* buy\_tax, sell\_tax (from simulation)  
* dynamic tax change functions  
* mismatch between quoted vs realized

**Heuristics**

if tax \> max\_tax(profile) → risk↑

if dynamic\_tax\_enabled → risk↑

if buy\_tax \<\< sell\_tax → asymmetry risk↑

**Score**

tax\_risk \=

 c1 \* tax\_norm

\+ c2 \* dynamic\_tax\_flag

\+ c3 \* asymmetry\_norm

**Flags**

* `TAX_HIGH`  
* `TAX_DYNAMIC`  
* `TAX_ASYMMETRIC`

---

## **3.2.6 Aggregation → RiskScore**

Normalize each sub-score to \[0,1\], then:

RiskScore \=

 W\_wash \* wash\_risk

\+ W\_rug  \* rug\_risk

\+ W\_honey\* honeypot\_risk

\+ W\_liq  \* liq\_risk

\+ W\_tax  \* tax\_risk

Weights are **versioned** and updated by learning.

---

## **3.3 Decision Logic (Deterministic)**

Given `RiskScore` and profile thresholds:

type Thresholds struct {

   MaxTax float64

   MinLiquidity float64

   RiskReject float64     // e.g. 0.7

   RiskRiskyPass float64  // e.g. 0.5

}

**Decision**

if honeypot\_sell\_fail → REJECT (hard)

else if liquidity \< min\_liquidity(profile) → REJECT

else if sell\_tax \> max\_tax(profile) → REJECT

else if RiskScore ≥ RiskReject → REJECT

else if RiskScore ≥ RiskRiskyPass → RISKY\_PASS

else → PASS

---

## **3.3 Output DTO (final)**

type DataQualityDTO struct {

   TokenAddress string

   RiskScore    float64

   Flags        \[\]string

   Decision     string // pass | risky-pass | reject

   Profile      string // strict | balanced | exploration

   Version      int

   Timestamp    int64

}

---

## **3.4 Adaptive Strictness (Controller)**

Profiles (baseline):

strict:

 max\_tax: 8

 min\_liquidity: 20000

 risk\_reject: 0.65

 risk\_risky\_pass: 0.45

balanced:

 max\_tax: 12

 min\_liquidity: 10000

 risk\_reject: 0.70

 risk\_risky\_pass: 0.50

exploration:

 max\_tax: 15

 min\_liquidity: 5000

 risk\_reject: 0.75

 risk\_risky\_pass: 0.55

### **Controller Inputs (rolling window)**

type DQMetrics struct {

   PassRate            float64

   FalsePositiveRate   float64 // accepted → loss/rug

   FalseNegativeRate   float64 // rejected → later pump

   RugLossRate         float64

}

### **Mode Switching**

if PassRate \== 0 for T → downgrade profile (strict→balanced→exploration)

if RugLossRate ↑ or FalsePositiveRate ↑ → upgrade profile (exploration→balanced→strict)

### **Threshold Tuning (within profile)**

if FalseNegativeRate ↑:

 decrease min\_liquidity (−Δ)

 increase max\_tax (+Δ)

 increase risk\_reject (+ε)  // allow more through

if FalsePositiveRate ↑ or RugLossRate ↑:

 increase min\_liquidity (+Δ)

 decrease max\_tax (−Δ)

 decrease risk\_reject (−ε)  // block more

**Constraints**

* `Δ` small (e.g., 5–10% step)  
* require `N ≥ N_min` samples before update  
* one parameter family per cycle (avoid oscillation)  
* version every change

---

## **3.5 Learning Signals (Precise Labeling)**

### **A. False Positive (FP)**

Decision ∈ {pass, risky-pass} AND

Outcome ∈ {rug OR PnL \< \-SL within short horizon}

Label:

FP \= 1

### **B. False Negative (FN)**

Requires **shadow tracking**:

Decision \= reject AND

Observed peak\_return within T\_window ≥ threshold (e.g., \+30%)

Label:

FN \= 1

### **C. Attribution (which detector failed)**

Store per-detector contributions:

type DQAttribution struct {

   WashRisk float64

   RugRisk  float64

   HoneyRisk float64

   LiqRisk  float64

   TaxRisk  float64

}

For each FP/FN, compute:

blame \= highest\_contributing\_component OR threshold trigger

Used to adjust:

* detector weights `W_*`  
* specific thresholds (e.g., max\_tax too low/high)

---

## **3.6 Metrics (must be tracked)**

pass\_rate

fp\_rate (accepted→loss)

fn\_rate (rejected→pump)

rug\_loss\_rate

avg\_risk\_score\_passed

Healthy targets:

pass\_rate: 0.5%–5%

fp\_rate: ↓ over time

fn\_rate: ↓ over time (but not zero)

rug\_loss\_rate: as close to 0 as possible

---

## **3.7 Performance Constraints**

* **Latency**: ≤ 200–500 ms per token (parallelizable)  
* **RPC calls**: bounded (simulate buy/sell once)  
* **Idempotency**: same token → same decision

---

## **3.8 Failure Modes & Guards**

* **Over-blocking** (pass\_rate → 0\)  
  * auto relax profile  
* **Under-blocking** (rug spikes)  
  * auto tighten profile  
* **Detector drift** (features lose signal)  
  * weight decay \+ rebalancing via attribution  
* **RPC noise (honeypot false fail)**  
  * retry once, else mark `UNKNOWN` and treat as high risk

---

## **3.9 Summary (what this layer guarantees)**

* Converts **noisy, adversarial tokens → vetted candidates**  
* Maintains **controlled pass rate**  
* Learns from **FP/FN with attribution**  
* Adapts **thresholds and weights** without destabilizing the system

If this layer is correct, downstream layers operate on **clean, high-signal input**. If not, everything else is wasted.

