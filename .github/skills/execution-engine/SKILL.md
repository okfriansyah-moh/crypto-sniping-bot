---
name: execution-engine
type: skill
description: >
  Execution engine patterns: wallet sharding, nonce management, prebuilt calldata,
  bounded parallelism, RPC fallback, and fee bumping (Layer 8). Use when implementing
  or reviewing transaction submission, idempotency, and on-chain execution flow.
  Bad execution converts a perfect edge to zero profit.
---

# Execution Engine Skill

## Purpose

Enforce correct implementation of the execution layer that converts `AllocationDTO`
decisions into confirmed on-chain positions. This layer must be fast, deterministic,
and failure-tolerant without nonce collisions.

**Core reality:** `perfect edge + bad execution = zero profit`

---

## Rules

### Wallet Sharding (Mandatory)

Each wallet has an independent nonce stream. Never share a wallet across concurrent
transactions — nonce contention causes failures and lost fees.

```go
// Deterministic wallet assignment
func assignWallet(tokenAddress string, walletPool []Wallet) Wallet {
    // Hash-based deterministic assignment
    h := sha256.Sum256([]byte(tokenAddress))
    idx := binary.BigEndian.Uint64(h[:8]) % uint64(len(walletPool))
    return walletPool[idx]
}
```

**Invariants (non-negotiable):**

- Strictly increasing nonce per wallet — never reuse, never skip
- One in-flight tx per wallet at any time (or small bounded queue: 1–2)
- No concurrent sends with the same nonce

```yaml
# config/execution.yaml
execution:
  wallet_pool_size: 5 # number of sharding wallets
  max_in_flight_per_wallet: 1 # never more than this
  concurrency_limit: 10 # global semaphore cap
```

### Prebuilt Calldata (Hot Path Zero-Compute)

Build transaction calldata when the edge is validated — NOT during submission.
Submission must be sub-millisecond.

```go
type CallSpec struct {
    To           string   // router address
    Data         []byte   // encoded function call
    Value        string   // ETH value (decimal string — not big.Int)
    AmountOutMin string   // slippage-protected minimum output
    Deadline     uint64   // block timestamp + buffer
    ExecutionID  string   // idempotency key from AllocationDTO
}

// Build when allocation is confirmed (pre-submission)
func buildCallSpec(alloc contracts.AllocationDTO, pool PoolState, cfg ExecConfig) CallSpec {
    amountOutMin := quoteOut * (1 - cfg.SlippageTolerance)
    return CallSpec{
        To:           cfg.RouterAddress,   // from config
        Data:         encodeSwap(alloc, pool, amountOutMin),
        AmountOutMin: formatDecimal(amountOutMin),
        Deadline:     currentBlockTimestamp + cfg.TxDeadlineBuffer,
        ExecutionID:  alloc.ExecutionID,   // content-addressable ID
    }
}
```

**No recomputation during submission.** If pool state changes, rebuild before resubmit.

### Bounded Parallelism

```go
// Global semaphore — never exceed concurrency_limit
sema := make(chan struct{}, cfg.ConcurrencyLimit)

func submitWithSema(ctx context.Context, spec CallSpec, wallet Wallet) error {
    sema <- struct{}{}           // acquire
    defer func() { <-sema }()   // release

    return submitTransaction(ctx, spec, wallet)
}
```

**Adaptive concurrency:**

```
if failure_rate > config.failure_rate_threshold → reduce concurrency_limit by 1
if inclusion_delay > config.max_inclusion_ms    → increase priority_fee
if failure_rate returns to normal               → restore concurrency_limit
```

### Idempotency (Duplicate Prevention)

```go
// Every AllocationDTO has a unique ExecutionID
// Before submitting: check if already submitted
func submitIdempotent(ctx context.Context, alloc contracts.AllocationDTO) error {
    // ExecutionID is content-addressable — same allocation = same ID
    if adapter.ExecutionExists(ctx, alloc.ExecutionID) {
        logger.Info("duplicate submission prevented", "execution_id", alloc.ExecutionID)
        return nil
    }
    return submitNew(ctx, alloc)
}
```

**ExecutionID rule:** `SHA256(token_address || wallet || amount || block_number)[:16]`

### Fee Bumping (Stuck Transaction Recovery)

```go
// Stuck tx: not included after config.stuck_tx_timeout_ms
func bumpFee(original TxParams, attempt int) TxParams {
    if attempt > cfg.MaxFeeRetries {  // default: 2-3
        return TxParams{Abort: true}
    }
    return TxParams{
        Nonce:            original.Nonce,             // SAME nonce — replace tx
        PriorityFeeGwei:  original.PriorityFeeGwei * (1 + cfg.FeeBumpPct/100),  // +10-20%
        MaxFeeGwei:       original.MaxFeeGwei * (1 + cfg.FeeBumpPct/100),
        AmountOutMin:     original.AmountOutMin,      // unchanged
    }
}
```

**Rule:** Fee bump always uses the SAME nonce. Changing nonce creates a new tx, not a bump.

### Multi-Endpoint RPC Fallback

```go
// Ordered endpoints — try primary first, failover on error
type RPCPool struct {
    endpoints []string     // from config.rpc.endpoints
    current   int
    mu        sync.Mutex
}

func (p *RPCPool) Submit(ctx context.Context, tx *types.Transaction) (string, error) {
    for i := 0; i < len(p.endpoints); i++ {
        client := p.getClient((p.current + i) % len(p.endpoints))
        hash, err := client.SendTransaction(ctx, tx)
        if err == nil {
            return hash, nil
        }
        logger.Warn("rpc endpoint failed", "endpoint", p.endpoints[i], "err", err)
    }
    return "", ErrAllEndpointsFailed
}
```

**Circuit breaker:** If an endpoint fails N consecutive times, mark as degraded and skip
for `circuit_breaker_window_sec`. See `rpc-management` skill.

### ExecutionResultDTO Output

```go
ExecutionResultDTO{
    EventID:          SHA256(canonical_json(dto))[:16],
    ExecutionID:      alloc.ExecutionID,         // idempotency key
    TokenAddress:     alloc.TokenAddress,
    Wallet:           wallet.Address,
    TxHash:           "0x...",
    Nonce:            uint64,
    EntryPrice:       float64,
    AmountIn:         float64,
    AmountOutMin:     float64,
    AmountOutActual:  float64,
    GasUsed:          uint64,
    PriorityFeeGwei:  float64,
    LatencyMs:        int64,
    Status:           "submitted" | "included" | "failed",
    ErrorCode:        "",  // non-empty only on failure
    FeeAttempts:      int,
    CompletedAt:      ISO8601UTC,
    // Traceability
    TraceID:          alloc.TraceID,
    CorrelationID:    alloc.CorrelationID,
    CausationID:      alloc.EventID,
    VersionID:        activeStrategyVersion.VersionID,
}
```

### Execution Quality Integration

After `N ≥ 30` executions (config: `execution.quality_audit_min_samples`), the
orchestrator calls `EmitExecutionQualityReport()` from the execution-quality-analyzer
skill. This is not called inside the execution module — only via the orchestrator
after a batch of `ExecutionResultDTO`s is accumulated.

```go
// Orchestrator calls this after accumulating N results:
results := adapter.QueryRecentExecutionResults(ctx, n)
report  := exec_quality.EmitExecutionQualityReport(ctx, adapter, results)

// Respond to critical signals:
if report.CostAnalysis.CostAsEdgePct > cfg.CostEdgeAlertThreshold { // 0.50
    adapter.EmitSystemEvent(ctx, "execution_strategy_review", map[string]any{
        "reason":          "cost_dominates_edge",
        "cost_as_edge_pct": report.CostAnalysis.CostAsEdgePct,
        "version_id":      report.VersionID,
    })
}
if report.SlippageAnalysis.AvgBPS > cfg.SlippagePoorThresholdBPS { // 8 bps
    adapter.EmitSystemEvent(ctx, "rpc_health_check_requested", map[string]any{
        "reason":           "high_slippage",
        "slippage_avg_bps": report.SlippageAnalysis.AvgBPS,
        "version_id":       report.VersionID,
    })
}
```

**Quality thresholds (from `config/execution.yaml`):**

| Metric             | Good    | Review Trigger |
| ------------------ | ------- | -------------- |
| `slippage_avg_bps` | < 3 bps | > 8 bps        |
| `fill_rate`        | > 95%   | < 85%          |
| `p90_latency_ms`   | < 300ms | > 500ms        |
| `cost_as_edge_pct` | < 25%   | > 50%          |

> See `.github/skills/execution-quality-analyzer/SKILL.md` for
> `EmitExecutionQualityReport()`, `AnalyzeSlippage()`, `AnalyzeFillQuality()`,
> and `ComputeExecutionCost()` implementations.

### Anti-Patterns

```go
// ❌ Same wallet for all transactions
wallet := wallets[0]  // Wrong — deterministic sharding by token address

// ❌ Compute calldata during submission (hot path)
func submit(alloc AllocationDTO) {
    data := encodeSwap(alloc, fetchCurrentPool())  // Wrong — network call on hot path
    ...
}

// ❌ No concurrency limit
for _, alloc := range allocs {
    go submit(alloc)  // Wrong — unbounded goroutines, mempool thrashing
}

// ❌ Fee bump with new nonce
newNonce := wallet.NextNonce()  // Wrong — stuck tx must use SAME nonce

// ❌ No idempotency check
submitTransaction(ctx, alloc)  // Wrong — must check ExecutionID first

// ✅ Correct
wallet := assignWallet(alloc.TokenAddress, walletPool)
spec   := buildCallSpec(alloc, cachedPool, cfg)       // pre-built, not computed now
sema <- struct{}{}
defer func() { <-sema }()
return submitIdempotent(ctx, spec, wallet)
```

---

## Config Reference

```yaml
# config/execution.yaml
execution:
  wallet_pool_size: 5
  max_in_flight_per_wallet: 1
  concurrency_limit: 10 # [5, 20] range
  slippage_tolerance: 0.02 # 2%
  tx_deadline_buffer_sec: 30
  stuck_tx_timeout_ms: 15000
  fee_bump_pct: 15 # 15% fee increase per retry
  max_fee_retries: 2
  failure_rate_threshold: 0.15 # 15% failures → reduce concurrency
  max_inclusion_ms: 12000 # 12s → increase priority fee
```

---

## Checklist

```
[ ] Execution quality audit runs after every N≥30 executions (config-driven)
[ ] CostAsEdgePct > 0.50 triggers execution strategy review event
[ ] SlippageAvgBPS > 8 triggers RPC endpoint health check
[ ] Wallet assignment is deterministic: hash(token) % n
[ ] Nonce is strictly increasing per wallet
[ ] Max in-flight per wallet is config-driven (≤ 2)
[ ] Calldata is prebuilt before submission (no hot-path computation)
[ ] Global semaphore caps concurrency at config.concurrency_limit
[ ] ExecutionID is content-addressable (SHA256-based)
[ ] Idempotency check before every submission
[ ] Fee bump uses same nonce — never new nonce
[ ] Max fee retries is config-driven (2-3)
[ ] Multi-endpoint RPC pool with fallback
[ ] ExecutionResultDTO carries all fields including LatencyMs
[ ] CausationID = AllocationDTO.EventID
[ ] Adaptive: reduce concurrency on high failure rate
[ ] Adaptive: increase priority fee on inclusion delay
```

---

## References

- Architecture context: `docs/architecture-context/10_execution_engine.md`
- DTO spec: `docs/dto_contracts.md` § 3.9 (ExecutionResultDTO)
- Roadmap: `docs/implementation_roadmap.md` Phase 2.8
- Config: `config/execution.yaml`
- Related skill: `rpc-management` (circuit breaker, endpoint health)
- `.github/skills/execution-quality-analyzer/SKILL.md` — Post-execution quality audit (slippage/fill/latency/cost-as-edge)
