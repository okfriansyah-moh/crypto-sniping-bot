# **10\. Layer 8 — Execution Engine (Multi-Wallet, Deterministic)**

## **Design**

* wallet sharding (avoid nonce contention)  
* prebuilt calldata  
* bounded parallelism (5–20)

## **Failure Handling**

* retry with fee bump  
* fallback RPC

## **Adaptive**

if inclusion\_delay ↑ → increase priority fee  
if failure\_rate ↑ → reduce concurrency  
---

This layer converts **allocated intents → confirmed on-chain positions**. It must be **fast, deterministic, and failure-tolerant**, while avoiding nonce collisions and unpredictable side effects.

---

# **10\. LAYER 8 — EXECUTION ENGINE (MULTI-WALLET, DETERMINISTIC)**

---

## **10.1 Objectives (Operational)**

* Submit transactions with **predictable ordering and minimal latency**  
* **Eliminate nonce contention** via wallet sharding  
* **Bound concurrency** to avoid mempool thrashing  
* **Guarantee idempotency** (no duplicate fills)  
* Adapt fees/concurrency based on **observed inclusion & failures**

---

## **10.2 Inputs → Outputs**

**Input**

* `AllocationDTO[]` (token, size, bucket)  
* Chain config (router address, gas params)  
* Latest pool state (cached)

**Output**

type ExecutionResultDTO struct {

   TokenAddress string

   Wallet string

   TxHash string

   Nonce uint64

   EntryPrice float64

   AmountIn float64

   AmountOutMin float64

   GasUsed uint64

   PriorityFeeGwei float64

   LatencyMs int

   Status string // submitted | included | failed

   ErrorCode string

   Version int

   Timestamp int64

}

---

## **10.3 Wallet Sharding (Nonce Isolation)**

### **Design**

wallet\_pool \= \[W1, W2, W3, ... Wn\]

* Each wallet has an independent nonce stream  
* **One in-flight tx per wallet** (or a small queue)

### **Assignment (deterministic)**

hash(TokenAddress) % n → wallet index

or

round-robin over available wallets

### **Invariants**

per-wallet: strictly increasing nonce

no concurrent sends with same nonce

---

## **10.4 Prebuilt Calldata (Zero-Compute on Hot Path)**

### **Build ahead (when edge validated)**

* Router method (e.g., `swapExactETHForTokens` / chain-specific)  
* Path (WETH → token)  
* `amountIn` (from Allocation)  
* `amountOutMin` (slippage-protected)  
* deadline

type CallSpec struct {

   To string

   Data \[\]byte

   Value uint256

   AmountOutMin uint256

   Deadline uint64

}

### **AmountOutMin**

AmountOutMin \= quote\_out × (1 \- slippage\_tolerance)

No recomputation during submission.

---

## **10.5 Bounded Parallelism**

### **Worker pool**

concurrency\_limit \= 5–20 (configurable)

* Global semaphore caps in-flight submissions  
* Per-wallet queue length capped (e.g., 1–2)

### **Scheduler**

acquire(global\_sema)

wallet := pickAvailableWallet()

nonce := nextNonce(wallet)

sendTx(wallet, nonce, CallSpec, feeParams)

release(global\_sema when submitted)

---

## **10.6 Fee Strategy (EIP-1559 / chain equivalent)**

### **Initial params**

maxFee \= baseFee \* m \+ priorityFee

priorityFee \= p0 (baseline)

### **Priority tiers**

p0: normal

p1: fast

p2: urgent

Choose tier based on:

* `LatencyProfileDTO`  
* congestion estimate

---

## **10.7 Submission & Tracking**

### **States**

created → signed → submitted → pending → included | failed

### **Tracking**

* store `txHash`, `wallet`, `nonce`  
* poll or subscribe for inclusion  
* measure `LatencyMs = submit_ts → included_ts`

---

## **10.8 Failure Handling**

---

### **10.8.1 Retry with Fee Bump (Same Nonce)**

If pending too long or dropped:

if pending\_time \> T\_retry:

   resend same nonce with higher maxFee/priorityFee

Bump rule:

priorityFee\_new \= priorityFee\_old × (1 \+ δ)

maxFee\_new \= maxFee\_old × (1 \+ δ)

(δ ≈ 10–20%)

Max retries bounded (e.g., 2–3).

---

### **10.8.2 Revert / Execution Failure**

Common causes:

* slippage exceeded (`INSUFFICIENT_OUTPUT_AMOUNT`)  
* pool changed

Action:

mark failed

do NOT retry same calldata

optionally recompute with tighter size (next cycle only)

---

### **10.8.3 RPC Failure**

* multiple RPC endpoints (A/B/C)

on send error:

 try next RPC (same signed tx)

* circuit-break unhealthy RPCs

---

### **10.8.4 Nonce Desync**

If mismatch detected:

resync nonce from chain

requeue tx (new nonce)

---

## **10.9 Idempotency & Dedup**

* Each `AllocationDTO` has a unique `execution_id`  
* Maintain table:

if execution\_id already submitted:

   skip

* Prevent duplicate buys on retries

---

## **10.10 Adaptive Controls**

---

### **10.10.1 Inclusion Delay → Fee Adjustment**

if avg\_inclusion\_delay ↑:

   increase priorityFee tier (p0→p1→p2)

Bounded:

* cap max priority fee  
* decay back when normal

---

### **10.10.2 Failure Rate → Concurrency Adjustment**

if failure\_rate ↑ (reverts / drops):

   concurrency\_limit \-= step

if stable and low latency:

   concurrency\_limit \+= step

Bounds:

* min 3–5  
* max 20

---

### **10.10.3 Slippage Misses → Size Adjustment (signal back)**

If frequent slippage failures:

* emit signal to reduce `amountIn` (feeds Layer 7 next cycle)

---

## **10.11 Determinism Guarantees**

* Wallet assignment is deterministic  
* Nonce progression is linear per wallet  
* Retry policy fixed and bounded  
* Same inputs \+ same network conditions → same submission sequence

---

## **10.12 Observability (must emit)**

Per tx:

submit\_ts, included\_ts, latency\_ms

priority\_fee, max\_fee

slippage\_est vs realized

status, error\_code

wallet, nonce

Aggregates:

* inclusion delay p50/p95  
* failure rate by reason  
* RPC health

---

## **10.13 Performance Targets**

* submit latency (local): **\< 20–50 ms**  
* inclusion delay: market-dependent, monitored  
* throughput: up to `concurrency_limit` txs in parallel

---

## **10.14 Failure Modes & Guards**

* **Nonce contention** → strict per-wallet queue  
* **Mempool thrash** → bounded concurrency  
* **Fee underbidding** → adaptive bumps  
* **Overpaying fees** → capped tiers \+ decay  
* **Duplicate execution** → idempotency key  
* **RPC flakiness** → multi-endpoint fallback

---

## **10.15 What This Layer Guarantees**

* **One clean attempt per opportunity**, retried safely if needed  
* **No nonce collisions**, predictable ordering  
* **Controlled parallelism** under load  
* **Adaptive fees** to maintain inclusion without runaway costs

---

**Key takeaway**

You don’t get paid for deciding fast.

You get paid for getting filled correctly.