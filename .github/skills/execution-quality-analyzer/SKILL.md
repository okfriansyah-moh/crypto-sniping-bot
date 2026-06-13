---
name: execution-quality-analyzer
type: skill
description: >
  Audit on-chain execution quality across slippage, fill rate, latency, and total
  cost as a percentage of edge. Use when implementing or reviewing post-execution
  analysis, execution strategy comparison, and the trigger conditions for RPC
  endpoint rotation or concurrency limit adjustment.
---

# Execution Quality Analyzer Skill

## Purpose

Measure how much of the theoretical edge is actually captured during execution.
An edge that exists in theory but is lost to slippage, failed txs, or latency
is an Execution factor collapse — `Execution → 0 means Profit → 0`.

**5 analysis dimensions:**

```
1. Slippage analysis    — actual vs estimated, percentile distribution
2. Fill quality         — full fills vs partial fills vs rejections
3. Latency analysis     — submission-to-confirmation percentiles
4. Total cost           — (slippage + gas + fees) as % of notional AND % of edge
5. Strategy comparison  — execution quality per wallet/strategy variant
```

**Minimum sample size:** 30 executions before producing statistically meaningful metrics.

---

## Rules

### Slippage Analysis

```go
// Slippage measured in basis points (bps): 100 bps = 1%.
// Prediction bias = mean(actual - estimated). Negative = underestimating slippage.

type SlippageMetrics struct {
    AvgBPS        float64
    MedianBPS     float64
    P90BPS        float64
    P99BPS        float64
    PredictionBias float64  // actual - estimated; negative = underestimate
    SampleCount    int
}

func AnalyzeSlippage(
    actualBPS    []float64,
    estimatedBPS []float64,
) (SlippageMetrics, error) {
    if len(actualBPS) < 30 {
        return SlippageMetrics{}, ErrInsufficientSamples
    }
    sorted := make([]float64, len(actualBPS))
    copy(sorted, actualBPS)
    sort.Float64s(sorted)

    n := len(sorted)
    bias := 0.0
    for i, a := range actualBPS {
        if i < len(estimatedBPS) {
            bias += a - estimatedBPS[i]
        }
    }
    bias /= float64(n)

    return SlippageMetrics{
        AvgBPS:         mean(sorted),
        MedianBPS:      sorted[n/2],
        P90BPS:         sorted[int(float64(n)*0.90)],
        P99BPS:         sorted[int(float64(n)*0.99)],
        PredictionBias: bias,
        SampleCount:    n,
    }, nil
}

// Quality thresholds for crypto DEX execution:
//   AvgBPS < 3  → good
//   AvgBPS 3-8  → acceptable
//   AvgBPS > 8  → poor — trigger strategy review
```

### Fill Quality

```go
type FillQualityMetrics struct {
    FullFillRate     float64  // full fills / total attempts
    PartialFillRate  float64  // partial fills / total
    RejectionRate    float64  // reverts / total
    CancelRate       float64  // cancelled / total
    EffectiveFillPct float64  // avg fill_amount / requested_amount across partials
    SampleCount      int
}

func AnalyzeFillQuality(execResults []contracts.ExecutionResultDTO) FillQualityMetrics {
    n := len(execResults)
    if n == 0 {
        return FillQualityMetrics{}
    }

    var full, partial, reject, cancel int
    var partialFillSum float64
    for _, r := range execResults {
        switch r.Status {
        case "filled":
            full++
        case "partial":
            partial++
            if r.RequestedAmount > 0 {
                partialFillSum += r.FilledAmount / r.RequestedAmount
            }
        case "failed", "reverted":
            reject++
        case "cancelled":
            cancel++
        }
    }

    effectiveFill := 1.0
    if partial > 0 {
        effectiveFill = partialFillSum / float64(partial)
    }

    return FillQualityMetrics{
        FullFillRate:     float64(full) / float64(n),
        PartialFillRate:  float64(partial) / float64(n),
        RejectionRate:    float64(reject) / float64(n),
        CancelRate:       float64(cancel) / float64(n),
        EffectiveFillPct: effectiveFill,
        SampleCount:      n,
    }
}

// Quality thresholds:
//   FullFillRate > 0.95 → good
//   FullFillRate < 0.85 → poor — investigate RPC endpoints and gas settings
```

### Latency Analysis

```go
// Latency = time from tx submission to on-chain confirmation (ms).
// Target for crypto DEX sniping: p90 < 300ms.

type LatencyMetrics struct {
    P50MS     int64
    P90MS     int64
    P95MS     int64
    P99MS     int64
    TargetPct float64  // pct within target threshold
    Target    int64    // from config
    SampleCount int
}

func AnalyzeLatency(latenciesMS []int64, targetMS int64) LatencyMetrics {
    if len(latenciesMS) == 0 {
        return LatencyMetrics{}
    }
    sorted := make([]int64, len(latenciesMS))
    copy(sorted, latenciesMS)
    sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })

    n := len(sorted)
    withinTarget := 0
    for _, l := range sorted {
        if l <= targetMS { withinTarget++ }
    }

    return LatencyMetrics{
        P50MS:       sorted[n/2],
        P90MS:       sorted[int(float64(n)*0.90)],
        P95MS:       sorted[int(float64(n)*0.95)],
        P99MS:       sorted[int(float64(n)*0.99)],
        TargetPct:   float64(withinTarget) / float64(n),
        Target:      targetMS,
        SampleCount: n,
    }
}
```

### Total Cost as % of Edge

```go
// This is the KEY metric: how much of the theoretical edge is consumed by costs?
// If total_cost > 50% of edge → execution strategy must be reviewed immediately.

type ExecutionCostMetrics struct {
    AvgSlippagePct float64  // avg slippage bps / 10000
    AvgGasPct      float64  // avg gas cost / notional
    AvgFeesPct     float64  // avg protocol fees / notional
    TotalCostPct   float64  // sum of above
    CostAsEdgePct  float64  // total_cost / expected_edge_pct
    CostTooHigh    bool     // CostAsEdgePct > 0.50
}

func ComputeExecutionCost(
    slippageBPSSlice []float64,
    gasCostUSD       []float64,
    protocolFeeUSD   []float64,
    notionalUSD      []float64,
    expectedEdgePct  float64,  // from AllocationDTO or EdgeDTO
) ExecutionCostMetrics {
    n := len(slippageBPSSlice)
    if n == 0 { return ExecutionCostMetrics{} }

    var avgSlip, avgGas, avgFee float64
    for i := 0; i < n; i++ {
        notional := 1.0
        if i < len(notionalUSD) && notionalUSD[i] > 0 { notional = notionalUSD[i] }
        avgSlip += slippageBPSSlice[i] / 10000.0
        if i < len(gasCostUSD)     { avgGas += gasCostUSD[i] / notional }
        if i < len(protocolFeeUSD) { avgFee += protocolFeeUSD[i] / notional }
    }
    avgSlip /= float64(n)
    avgGas /= float64(n)
    avgFee /= float64(n)

    total := avgSlip + avgGas + avgFee
    costAsEdge := 0.0
    if expectedEdgePct > 0 {
        costAsEdge = total / expectedEdgePct
    }

    return ExecutionCostMetrics{
        AvgSlippagePct: avgSlip,
        AvgGasPct:      avgGas,
        AvgFeesPct:     avgFee,
        TotalCostPct:   total,
        CostAsEdgePct:  costAsEdge,
        CostTooHigh:    costAsEdge > 0.50,
    }
}
```

### Quality Report + Action Triggers

```go
// Produce a summary report and trigger events for degraded execution quality.
func EmitExecutionQualityReport(
    ctx     context.Context,
    adapter database.Adapter,
    slip    SlippageMetrics,
    fill    FillQualityMetrics,
    lat     LatencyMetrics,
    cost    ExecutionCostMetrics,
    versionID string,
) error {
    // Determine overall quality
    qualityGood := slip.AvgBPS < 3.0 && fill.FullFillRate > 0.95 && lat.TargetPct > 0.90
    qualityPoor := slip.AvgBPS > 8.0 || fill.FullFillRate < 0.85 || cost.CostTooHigh

    status := "acceptable"
    if qualityGood  { status = "good" }
    if qualityPoor  { status = "poor" }

    return adapter.EmitSystemEvent(ctx, "execution_quality_report", map[string]any{
        "status":           status,
        "avg_slippage_bps": slip.AvgBPS,
        "fill_rate":        fill.FullFillRate,
        "p90_latency_ms":   lat.P90MS,
        "cost_as_edge_pct": cost.CostAsEdgePct,
        "sample_count":     slip.SampleCount,
        "version_id":       versionID,
    })
}
```

---

## Anti-Patterns

```
❌ Running analysis on <30 samples (noise masquerades as signal)
❌ Not computing slippage prediction bias (missing it hides systematic underestimation)
❌ Using execution quality metrics to override trading decisions inline
   (they feed learning + alerts, not real-time gates)
❌ Ignoring partial fills — effective fill rate matters for P&L accuracy
❌ Not correlating cost_as_edge_pct with VersionID (can't attribute to strategy)
```

---

## Config Reference (`config/pipeline.yaml`)

```yaml
execution_quality:
  min_samples: 30
  slippage_good_bps: 3.0
  slippage_poor_bps: 8.0
  fill_rate_good: 0.95
  fill_rate_poor: 0.85
  latency_target_ms: 300
  cost_as_edge_review_threshold: 0.50
```

---

## Checklist

- [ ] Requires ≥30 samples before producing metrics
- [ ] Slippage prediction bias computed (not just average)
- [ ] Fill quality tracks partial fills separately
- [ ] Latency p90 compared against config-driven target (not hardcoded 300ms)
- [ ] Total cost computed as % of edge, not just % of notional
- [ ] Quality report emitted as `system_event` with `version_id`
- [ ] `CostTooHigh=true` triggers execution strategy review (not inline trade block)

---

## References

- `docs/reference/architecture.md` § 3.8 — Execution Engine (Layer 8)
- `docs/archive/architecture-context/10_execution_engine.md` — Wallet sharding, prebuilt calldata
- `.github/skills/execution-engine/SKILL.md` — Fee bump, idempotency keys
- `.github/skills/observability/SKILL.md` — system_event emission
- `contracts/execution.go` — `ExecutionResultDTO` fields
