# **8\. Layer 6 — Selection Engine (Top-K with Health Targets)**

## **Input**

* validated edges

## **Output**

* Top K (5–10)

## **Health Constraints**

* **portfolio-level cap**  
* **diversity (avoid same deployer/cluster)**

## **Adaptive Logic**

* if inactivity window exceeded → allow lower-ranked entries (exploration band)

---

This layer is a **constrained optimizer**: from all validated edges, pick a **small, high-quality, diversified set** that fits capital and risk limits. It must be deterministic, fast, and stable.

---

# **8\. LAYER 6 — SELECTION ENGINE (Top-K with Health Targets)**

---

## **8.1 Objective (Formal)**

Select a subset SSS that maximizes portfolio utility under constraints:

maximize   Σ\_{i ∈ S}  U\_i

subject to |S| ≤ K

          capital(S) ≤ C\_total

          exposure constraints

          diversity constraints

Where utility per edge:

U\_i \= EV\_i × Conf\_i × AgeDecay\_i × ExecFeasibility\_i

* `EV_i` from Layer 5  
* `Conf_i` \= combined confidence  
* `AgeDecay_i = exp(-λ_age * AgeSec_i)`  
* `ExecFeasibility_i = 1 - f(slippage, latency)`

---

## **8.2 Inputs**

* `ValidatedEdgeDTO[]` (only `accept` or `explore`)  
* Portfolio state:  
  * current positions  
  * available capital  
* Risk/cluster metadata (from prior layers):  
  * deployer, LP address, token graph cluster

---

## **8.3 Output**

type SelectionOutput struct {

   Selected \[\]SelectedEdge // size ≤ K (typically 5–10)

   Rejected \[\]RejectionReason

   Version  int

   Timestamp int64

}

type SelectedEdge struct {

   TokenAddress string

   Score float64       // final utility

   Rank  int

   Bucket string       // e.g., "primary" | "explore"

}

---

## **8.4 Ranking (Deterministic)**

Compute a **selection score**:

Score\_i \=

 EV\_i

× Conf\_i

× AgeDecay\_i

× (1 \- SlippagePenalty\_i)

× (1 \- LatencyPenalty\_i)

* Normalize components to \[0,1\]  
* Break ties deterministically (e.g., by token address hash)

Sort descending → initial list `L`.

---

## **8.5 Core Algorithm (Greedy with Constraints)**

S \= \[\]

for i in L:

 if |S| \== K: break

 if violates\_portfolio\_caps(i): continue

 if violates\_diversity(i, S): continue

 if not executable(i): continue

 S.append(i)

return S

Rationale: fast, predictable, and sufficient given small K.

---

## **8.6 Health Constraints (Hard)**

### **8.6.1 Portfolio-Level Caps**

Define caps:

max\_positions: K (5–10)

max\_capital\_per\_trade: c\_max

max\_total\_capital: C\_total

max\_simultaneous\_new\_positions: M (e.g., 3–6)

Checks:

if capital\_used \+ c\_i \> C\_total → skip

if c\_i \> c\_max → downsize or skip

if active\_new\_positions ≥ M → skip

---

### **8.6.2 Diversity (Anti-Cluster)**

Avoid correlated failures (same deployer/liquidity cluster).

**Cluster keys**

* `deployer_address`  
* `lp_address`  
* `bytecode_hash`  
* graph cluster id (shared holders/routers)

**Rules**

max\_per\_deployer ≤ 1

max\_per\_lp ≤ 1

max\_per\_cluster ≤ 2

Check:

if count(cluster(i) in S) ≥ limit → skip

---

## **8.7 Exploration Band (Controlled Relaxation)**

When the system is starved:

if inactivity\_window\_exceeded:

   enable exploration

**Inactivity definition**

no selections for T\_inactive (e.g., 5–10 min)

OR pass\_rate ≈ 0

**Exploration logic**

* Create secondary list `L_explore`:  
  * edges with `Decision = explore`  
  * or just below thresholds (|EV \- θ| ≤ ε)  
* Fill remaining slots:

S\_primary \= top-ranked accepts

remaining \= K \- |S\_primary|

S\_explore \= take top remaining from L\_explore

          with stricter caps:

            smaller size

            stricter diversity

**Budget**

explore\_capital ≤ 1–5% of C\_total

Mark these with `Bucket = "explore"`.

---

## **8.8 Execution Feasibility Filter**

Before finalizing:

reject if:

 expected\_slippage \> θ\_s\_eff

 estimated\_latency\_ratio \> κ\_eff

 recent execution failure rate high for similar cohort

(uses rolling stats; no RPC here)

---

## **8.9 Stability & Determinism**

* Fixed ordering, fixed tie-breakers  
* No randomness  
* Same inputs → same `Selected`

---

## **8.10 Metrics (for feedback)**

selection\_count

selection\_rate \= |S| / candidates

portfolio\_diversity\_index

capital\_utilization

topK\_vs\_all\_pnl (efficiency)

explore\_win\_rate vs primary\_win\_rate

Targets:

* `|S| ≈ K` when healthy  
* high `topK_vs_all_pnl`  
* diversity index above threshold

---

## **8.11 Failure Modes & Guards**

* **Over-concentration** → enforce cluster caps  
* **Capital fragmentation** → enforce min size / cap positions  
* **Starvation** → trigger exploration band  
* **Chasing stale edges** → age decay \+ feasibility filter  
* **Over-selection** → K cap \+ tighten upstream thresholds

---

## **8.12 Complexity & Performance**

* Sort: O(N log N) for N candidates (small N)  
* Greedy pass: O(N)  
* Latency: **\< 10–20 ms**

---

## **8.13 What This Layer Guarantees**

* Picks a **small, high-utility, diversified** set  
* Respects **capital and risk constraints**  
* Maintains **throughput** via exploration when needed  
* Provides stable, deterministic inputs to execution

---

**Key takeaway**

You don’t win by finding many edges.

You win by selecting a few that survive constraints and diversify risk.

