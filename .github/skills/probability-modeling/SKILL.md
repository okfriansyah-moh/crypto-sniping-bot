---
name: probability-modeling
type: skill
description: >
  Probability/Slippage/Latency model patterns (Layer 4). Use when implementing or
  reviewing P(success) estimation, slippage impact modeling, latency decay modeling,
  Expected Value computation, and adaptive model calibration. Converts signal → EV.
---

# Probability Modeling Skill

## Purpose

Enforce correct implementation of the three models that convert a detected edge into a
quantified expected value. Without these models, capital allocation is guesswork.

**Core equation:**

```
EV = P(success) × Gain - (1 - P(success)) × Loss - SlippageCost - LatencyCost
```

Only trade when `EV > 0` after all friction costs. This is the math of profit-first.

---

## Rules

### Three Model DTOs

These three DTOs are produced independently and consumed together by the validation layer:

| DTO                      | Answers                               | File                       |
| ------------------------ | ------------------------------------- | -------------------------- |
| `ProbabilityEstimateDTO` | Will it pump? P ∈ [0,1]               | `contracts/probability.go` |
| `SlippageEstimateDTO`    | How much do we lose on entry? %       | `contracts/slippage.go`    |
| `LatencyProfileDTO`      | How much edge decays from latency? ms | `contracts/latency.go`     |

Each model is a pure function — no DB, no RPC, no external calls.

### Probability Model

**Definition:** `P = P(price increases ≥ target_return within T window)`

```
target_return = config.target_return_pct   # e.g., 20%
T = config.prediction_window_sec           # e.g., 300s (5 min)
```

**Model inputs (from FeatureDTO):**

```
Momentum       → primary signal
TxRate         → market activity
BuySellRatio   → directional pressure
NewHolderRate  → fresh demand
WalletEntropy  → anti-wash signal
LiquiditySize  → friction proxy
LiquidityGrowth → sustained interest
HolderConcentration → concentration risk
```

**Model form (logistic — start simple, calibrate over time):**

```go
func estimateProbability(f contracts.FeatureDTO, cfg ProbabilityConfig) float64 {
    // Linear combination + sigmoid
    logit := cfg.Intercept +
        cfg.W_momentum * f.MomentumScore +
        cfg.W_txrate * f.TxRate1m +
        cfg.W_buysell * f.BuySellRatio +
        cfg.W_entropy * f.WalletEntropy +
        cfg.W_liquidity * math.Log1p(f.LiquidityUSD)
    return sigmoid(logit)
}

func sigmoid(x float64) float64 {
    return 1.0 / (1.0 + math.Exp(-x))
}
```

**Bootstrap coefficients (from config — adjust via learning):**

```yaml
# config/models.yaml
probability:
  intercept: -1.5
  w_momentum: 2.0
  w_txrate: 0.8
  w_buysell: 1.2
  w_entropy: 0.5
  w_liquidity: 0.3
  target_return_pct: 20.0
  prediction_window_sec: 300
```

### Slippage Model

**Definition:** Expected price impact when entering at your allocation size.

```
AMM price impact (Uniswap V2 constant product):
  price_impact = trade_size / (liquidity + trade_size)

Adjusted slippage estimate:
  slippage = price_impact × (1 + tax_pct/100) × cfg.SlippageMultiplier
```

```go
func estimateSlippage(allocationUSD float64, f contracts.FeatureDTO, cfg SlippageConfig) float64 {
    // AMM formula: x*y=k → price impact
    impact := allocationUSD / (f.LiquidityUSD + allocationUSD)
    // Add tax cost
    taxCost := f.TotalTax / 100.0
    // Apply historical calibration multiplier
    estimated := (impact + taxCost) * cfg.Multiplier
    return clamp(estimated, 0.0, 1.0)  // as fraction, not percentage
}
```

**Calibration:** Track `actual_slippage` vs `estimated_slippage` per trade.
Update `cfg.Multiplier` via learning engine (bounded ±10% per cycle).

### Latency Model

**Definition:** Expected latency from detection → submission → inclusion on-chain.

```
total_latency = detection_latency + submission_latency + inclusion_latency

Edge decay: the longer it takes to get included, the less edge remains.
decay_factor = exp(-decay_rate × latency_ms)
```

```go
func estimateLatency(market string, cfg LatencyConfig) contracts.LatencyProfileDTO {
    // Use exponential moving average of historical latencies
    detectionP50  := cfg.DetectionLatencyP50[market]
    submissionP50 := cfg.SubmissionLatencyP50[market]
    inclusionP50  := cfg.InclusionLatencyP50[market]

    totalEstimated := detectionP50 + submissionP50 + inclusionP50
    edgeDecay := math.Exp(-cfg.EdgeDecayRate * float64(totalEstimated))

    return contracts.LatencyProfileDTO{
        DetectionMs:      detectionP50,
        SubmissionMs:     submissionP50,
        InclusionMs:      inclusionP50,
        TotalEstimatedMs: totalEstimated,
        EdgeDecayFactor:  edgeDecay,       // [0, 1] — multiply by edge score
        // ...
    }
}
```

### Adaptive Calibration (Learning Engine feeds this)

```
After each trade:
  probability_error = actual_outcome - predicted_probability
  slippage_error    = actual_slippage - estimated_slippage
  latency_error     = actual_latency - estimated_latency

Update rules (bounded):
  Δcoefficient ≤ 5% per learning cycle
  Require N ≥ 30 samples per cohort before update
  Every update creates new StrategyVersion
```

### ProbabilityEstimateDTO Output

```go
ProbabilityEstimateDTO{
    EventID:      SHA256(canonical_json(dto))[:16],
    TokenAddress: f.TokenAddress,
    P:            float64,  // [0.0, 1.0] probability of success
    PWindow:      int64,    // prediction window in seconds
    Confidence:   float64,  // model confidence [0.0, 1.0]
    ModelVersion: cfg.ModelVersion,
    EstimatedAt:  ISO8601UTC,
    TraceID:      edge.TraceID,
    CorrelationID: edge.CorrelationID,
    CausationID:  edge.EventID,
    VersionID:    activeStrategyVersion.VersionID,
}
```

### Anti-Patterns

```go
// ❌ Magic number coefficients
logit := -1.5 + 2.0*momentum  // Wrong — use config-driven weights

// ❌ Random initial coefficients
cfg.Intercept = rand.Float64()  // Wrong — must be deterministic from config

// ❌ Slippage ignoring tax
impact = tradeSize / liquidity  // Wrong — must add tax cost

// ❌ Latency model ignoring edge decay
return LatencyProfileDTO{TotalMs: 200}  // Wrong — must include EdgeDecayFactor

// ❌ P outside [0, 1]
return P  // Wrong — always clamp(sigmoid(logit), 0.0, 1.0)

// ✅ Correct
p := clamp(sigmoid(logit), 0.0, 1.0)
return ProbabilityEstimateDTO{P: p, Confidence: computeConfidence(f)}
```

---

## Expected Value Gate (Used by Validation Layer)

```go
// Validation layer uses these three DTOs to compute EV
EV = prob.P * cfg.TargetReturn
   - (1 - prob.P) * cfg.ExpectedLoss
   - slip.EstimatedSlippage * allocationUSD
   - latency.EdgeDecayFactor  // subtract as decay penalty

// Only pass to selection if EV > cfg.MinEVThreshold
```

---

## Checklist

```
[ ] Probability model is a pure function — no DB, no RPC
[ ] Coefficients loaded from config — not hardcoded
[ ] P is always clamped to [0.0, 1.0]
[ ] Slippage model uses AMM formula + tax cost
[ ] LatencyProfileDTO includes EdgeDecayFactor
[ ] All three DTOs carry TraceID/CorrelationID/CausationID
[ ] CausationID = EdgeDTO.EventID (not empty)
[ ] Adaptive calibration errors tracked (actual vs estimated)
[ ] Model updates bounded: ≤5% per cycle, N ≥ 30 samples
[ ] Each model update creates new StrategyVersion
[ ] Module has zero DB imports — pure functions only
```

---

## References

- Architecture context: `docs/archive/architecture-context/6_slippage_models.md`
- DTO spec: `docs/reference/dto_contracts.md` § 3.5 (ProbabilityEstimateDTO, SlippageEstimateDTO, LatencyProfileDTO)
- Roadmap: `docs/reference/implementation_roadmap.md` Phase 4
- Config: `config/models.yaml`
