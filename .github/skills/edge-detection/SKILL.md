---
name: edge-detection
type: skill
description: >
  Signal & Edge Discovery patterns for the sniper pipeline (Layer 3). Use when
  implementing or reviewing NEW_LAUNCH_EDGE detection, adaptive momentum thresholds,
  EdgeDTO production, and time-window gating. The edge defines whether a trade is
  worth attempting at all.
---

# Edge Detection Skill

## Purpose

Enforce correct implementation of the edge detection layer that converts normalized
features into a time-sensitive trading hypothesis. An edge that does not exist should
never become a trade. A real edge that is missed is a false negative that costs profit.

**Core definition:**

```
NEW_LAUNCH_EDGE =
  early-stage token
  + sufficient data quality (pass or risky-pass)
  + measurable early momentum
  + within exploitable time window
```

**Mathematical form:**

```
EdgeExists =
  I(new_pool)
  × I(DataQuality ∈ {pass, risky-pass})
  × I(MomentumScore ≥ θ_momentum(t))

Where θ_momentum(t) is an adaptive threshold that depends on time since launch.
```

---

## Rules

### Inputs (Strict)

Edge detection receives exactly two DTOs — no external calls allowed:

```go
func DetectEdge(
    dq contracts.DataQualityDTO,
    f  contracts.FeatureDTO,
    cfg EdgeConfig,
    nowUnixSec int64,
) (contracts.EdgeDTO, bool)
```

**No DB calls. No RPC calls. No network calls.** Pure function.

### Gate Sequence (Ordered — Do Not Reorder)

```go
// 1. Hard reject gate (fast path)
if dq.Decision == "reject" {
    return EdgeDTO{}, false
}

// 2. Time window gate
ageSeconds := nowUnixSec - f.TimestampUnix
if ageSeconds > cfg.MaxPoolAgeSec {  // config-driven, e.g., 600s
    return EdgeDTO{}, false
}

// 3. Momentum gate (adaptive threshold)
momentum := computeMomentum(f)
threshold := adaptiveMomentumThreshold(ageSeconds, cfg)
if momentum < threshold {
    return EdgeDTO{}, false
}

// 4. Minimum liquidity gate
if f.LiquidityUSD < cfg.MinLiquidityUSD {
    return EdgeDTO{}, false
}

// 5. Edge confirmed
return buildEdgeDTO(dq, f, momentum, threshold, nowUnixSec), true
```

### Adaptive Momentum Threshold

The momentum threshold DECREASES as time since launch increases — early opportunities
have higher bars; older pools get relaxed thresholds.

```go
// Adaptive: threshold decays with pool age
// θ(t) = base_threshold × exp(-decay_rate × age_seconds)
func adaptiveMomentumThreshold(ageSeconds int64, cfg EdgeConfig) float64 {
    decay := math.Exp(-cfg.MomentumDecayRate * float64(ageSeconds))
    threshold := cfg.BaseMomentumThreshold * decay
    return math.Max(threshold, cfg.MinMomentumThreshold)  // floor — never go to 0
}
```

Config:

```yaml
# config/edge.yaml
edge:
  max_pool_age_sec: 600 # 10 minutes — beyond this, edge is stale
  base_momentum_threshold: 0.6 # starting threshold for brand-new pools
  min_momentum_threshold: 0.25 # floor — never below this
  momentum_decay_rate: 0.002 # per-second decay
  min_liquidity_usd: 10000.0
```

### Momentum Computation

```go
// Deterministic — same FeatureDTO → same momentum
func computeMomentum(f contracts.FeatureDTO) float64 {
    // Weighted combination of price and volume momentum signals
    priceMomentum := f.PriceChange5m      // % change in 5 min
    volumeMomentum := f.VolumeChange1m    // % volume change
    txMomentum := f.TxCountChange1m       // tx count acceleration

    // Weights from config (adaptive over time)
    return clamp(
        cfg.PriceWeight*priceMomentum +
        cfg.VolumeWeight*volumeMomentum +
        cfg.TxWeight*txMomentum,
        0.0, 1.0,
    )
}
```

**Rule:** Weights are config-driven. The learning engine updates them per cohort.

### EdgeDTO Output

```go
EdgeDTO{
    EventID:          SHA256(canonical_json(dto))[:16],
    TokenAddress:     f.TokenAddress,
    EdgeType:         "NEW_LAUNCH_EDGE",           // only valid edge type in Phase 2
    Score:            float64,                     // [0.0, 1.0]
    MomentumScore:    float64,                     // raw momentum value
    MomentumThreshold: float64,                    // threshold at detection time
    AgeSeconds:       int64,                       // pool age at detection
    LiquidityUSD:     float64,
    DataQualityDecision: dq.Decision,              // carry forward
    DetectedAt:       ISO8601UTC,
    // Traceability
    TraceID:          dq.TraceID,                  // copied — never regenerated
    CorrelationID:    dq.CorrelationID,            // copied
    CausationID:      dq.EventID,                  // parent event ID
    VersionID:        activeStrategyVersion.VersionID,
}
```

### Cohort Tagging (Required for Learning)

Every `EdgeDTO` MUST carry cohort tags for the learning engine to perform cohort analysis:

```go
EdgeDTO{
    // ...
    LiquidityBand:  classifyLiquidity(f.LiquidityUSD),  // "micro", "small", "mid"
    TaxBand:        classifyTax(f.TotalTax),             // "zero", "low", "mid", "high"
    EntropyBand:    classifyEntropy(f.WalletEntropy),    // "low", "mid", "high"
}
```

Config-driven band definitions:

```yaml
# config/cohorts.yaml
liquidity_bands:
  micro: [0, 10000]
  small: [10000, 50000]
  mid: [50000, 200000]
  large: [200000, 9999999999]
```

### Feedback Loop Integration

```
After trade completes:
  - was EdgeDTO → profitable exit within T minutes? → positive feedback
  - was EdgeDTO → loss/rug?                         → negative feedback

These outcomes feed back to adaptive threshold updates in the Learning Engine.
Never update thresholds directly in this module — only emit LearningRecord.
```

### Anti-Patterns

```go
// ❌ External calls in edge detection
price := rpcClient.GetCurrentPrice(token)  // Wrong — pure function only

// ❌ Time-based non-determinism
if time.Now().Unix()-poolAge > 600 { ... }  // Wrong — pass nowUnixSec as parameter

// ❌ Hardcoded momentum threshold
if momentum > 0.5 { ... }  // Wrong — use adaptive config-driven threshold

// ❌ Missing cohort tags
return EdgeDTO{Score: 0.8}  // Wrong — must include LiquidityBand, TaxBand, EntropyBand

// ❌ Accepting "reject" DataQuality
if dq.RiskScore < 0.8 { return EdgeDTO{}, true }  // Wrong — check dq.Decision, not score

// ✅ Correct
edge, ok := DetectEdge(dq, feature, cfg, time.Now().Unix())
// DetectEdge is a pure function with explicit time parameter
```

### Anti-Patterns

```go
// ❌ External calls in edge detection
price := rpcClient.GetCurrentPrice(token)  // Wrong — pure function only

// ❌ Time-based non-determinism
if time.Now().Unix()-poolAge > 600 { ... }  // Wrong — pass nowUnixSec as parameter

// ❌ Hardcoded momentum threshold
if momentum > 0.5 { ... }  // Wrong — use adaptive config-driven threshold

// ❌ Missing cohort tags
return EdgeDTO{Score: 0.8}  // Wrong — must include LiquidityBand, TaxBand, EntropyBand

// ❌ Accepting "reject" DataQuality
if dq.RiskScore < 0.8 { return EdgeDTO{}, true }  // Wrong — check dq.Decision, not score

// ✅ Correct
edge, ok := DetectEdge(dq, feature, cfg, time.Now().Unix())
// DetectEdge is a pure function with explicit time parameter
```

---

### Gate 5: Momentum Confirmation

After volume validation (Gate 4), the momentum gate must pass before
`EdgeDTO` is produced. This gate runs `DetectMomentum()` from the
momentum-detector skill.

```go
// Gate 5 is called after Gates 1-4 all pass.
// MomentumResult.Score contributes to EdgeDTO.Score via weighted sum.

func applyMomentumGate(
    feature contracts.FeatureDTO,
    cfg     EdgeConfig,
    poolAgeSeconds int64,
) (MomentumResult, bool) {
    result := DetectMomentum(feature, cfg.MomentumCfg, poolAgeSeconds)
    // Pass if score >= adaptive threshold (stricter for older pools)
    passed := result.Score >= result.AdaptiveThreshold
    return result, passed
}

// Contribution to EdgeDTO.Score (example weighted combination):
//   baseScore := liquidityScore × 0.4 + momentumScore × 0.6
//   EdgeDTO.Score = min(1.0, max(0.0, baseScore))
```

The momentum score (from `DetectMomentum`) uses:

- Trend strength (weighted bar consistency)
- Volume confirmation (≥1.5× baseline)
- RSI confirmation (30–70 range for new launches)
- Adaptive threshold (stricter for pools older than `max_pool_age_sec/2`)

> See `.github/skills/momentum-detector/SKILL.md` for `DetectMomentum()`
> implementation, RSI computation, and adaptive threshold formula.

---

## Checklist

```
[ ] Gate 5 (momentum confirmation) runs after Gate 4 (volume)
[ ] MomentumResult.Score contributes to EdgeDTO.Score via weighted sum
[ ] DetectEdge is a pure function — no DB, no RPC, no network calls
[ ] "reject" DataQualityDTO always returns (EdgeDTO{}, false)
[ ] Pool age check uses config.max_pool_age_sec, not hardcoded
[ ] Momentum threshold is adaptive (config-driven decay function)
[ ] Momentum computation weights are config-driven
[ ] EdgeType is always "NEW_LAUNCH_EDGE" (Phase 2)
[ ] EdgeDTO carries cohort tags: LiquidityBand, TaxBand, EntropyBand
[ ] CausationID = DataQualityDTO.EventID
[ ] TraceID/CorrelationID copied from input — never regenerated mid-pipeline
[ ] Score is always in [0.0, 1.0]
[ ] Module has zero DB imports
```

---

## References

- Architecture context: `docs/archive/architecture-context/5_sniper_mode.md`
- DTO spec: `docs/reference/dto_contracts.md` § 3.4 (EdgeDTO)
- Roadmap: `docs/reference/implementation_roadmap.md` Phase 2.3
- Config: `config/edge.yaml`, `config/cohorts.yaml`
- `.github/skills/momentum-detector/SKILL.md` — Gate 5 implementation (trend, RSI, volume)
