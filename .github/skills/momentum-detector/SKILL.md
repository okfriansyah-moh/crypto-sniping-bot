---
name: momentum-detector
type: skill
description: >
  Momentum signal detection for DEX sniping edge discovery. Use when implementing
  or reviewing early-token momentum confirmation — trend strength scoring, volume
  confirmation, and time-decay gating. Momentum WITHOUT regime and volume confirmation
  is noise. Do not trade momentum in a cascading or post-rug regime.
---

# Momentum Detector Skill

## Purpose

Provide a concrete momentum scoring function that feeds into `EdgeDTO` production
(Layer 3). The momentum detector confirms that an identified edge has measurable
directional strength before committing to probability modeling and capital sizing.

**A momentum signal without these three confirmations is noise:**

```
MomentumValid =
  TrendStrength ≥ θ_trend (adaptive)          [directional signal]
  AND VolumeRatio ≥ 1.5× (per config)        [volume confirmation]
  AND NOT (regime ∈ {cascade, post_rug})     [regime filter]
  AND RSI ∈ (30, 70)                          [overbought/oversold filter]
```

**Relationship to profit invariant:**

```
Profit = Edge × Probability × Execution × Capital × DataQuality × AdaptationQuality
         ↑
  Momentum Detector sharpens Edge detection accuracy
```

---

## Rules

### Thresholds (Config-Driven)

```go
// All in config/pipeline.yaml — never hardcode.
type MomentumConfig struct {
    MinTrendStrength     float64 // default: 0.3
    MinVolumeRatio       float64 // default: 1.5 (crypto), higher for low-liquidity pools
    RSIOverbought        float64 // default: 70.0
    RSIOversold          float64 // default: 30.0
    AdaptiveDecaySec     int64   // seconds since pool launch to apply decay
}
```

### Trend Strength Scoring

```go
// TrendStrength measures directional consistency across recent price bars.
// Returns [0.0, 1.0]. Higher = stronger trend.
// Uses a weighted sum of directional bar count.
func ComputeTrendStrength(prices []float64, weights []float64) float64 {
    if len(prices) < 2 {
        return 0.0
    }
    // Default: weights = [1,2,3,4,5] (recent bars count more)
    // Ensure len(weights) == len(prices)-1
    var positiveWeight, totalWeight float64
    for i := 1; i < len(prices); i++ {
        w := 1.0
        if i-1 < len(weights) {
            w = weights[i-1]
        }
        totalWeight += w
        if prices[i] > prices[i-1] {
            positiveWeight += w
        }
    }
    if totalWeight == 0 {
        return 0.0
    }
    return positiveWeight / totalWeight
}
```

### Volume Confirmation

```go
type VolumeConfirmResult struct {
    Ratio     float64 // current_vol / rolling_avg
    Confirmed bool    // ratio >= MinVolumeRatio
}

func ConfirmVolumeForMomentum(
    recentVolume  float64,
    baselineVols  []float64,
    minRatio      float64,
) VolumeConfirmResult {
    if len(baselineVols) == 0 {
        return VolumeConfirmResult{Confirmed: false}
    }
    var sum float64
    for _, v := range baselineVols { sum += v }
    avg := sum / float64(len(baselineVols))
    if avg == 0 {
        return VolumeConfirmResult{Confirmed: false}
    }
    ratio := recentVolume / avg
    return VolumeConfirmResult{
        Ratio:     ratio,
        Confirmed: ratio >= minRatio,
    }
}
```

### RSI Filter

```go
// RSI filters prevent momentum entries into overbought/oversold conditions.
// Overbought (RSI > 70): late entry, risk of reversal.
// Oversold (RSI < 30): counter-trend momentum would need inverse edge type.
func ComputeRSI(closePrices []float64, period int) float64 {
    if len(closePrices) < period+1 {
        return 50.0  // neutral — insufficient data
    }
    var gains, losses float64
    for i := len(closePrices) - period; i < len(closePrices); i++ {
        delta := closePrices[i] - closePrices[i-1]
        if delta > 0 {
            gains += delta
        } else {
            losses -= delta
        }
    }
    avgGain := gains / float64(period)
    avgLoss := losses / float64(period)
    if avgLoss == 0 {
        return 100.0
    }
    rs := avgGain / avgLoss
    return 100.0 - (100.0 / (1.0 + rs))
}

func RSIInValidRange(rsi, oversold, overbought float64) bool {
    return rsi > oversold && rsi < overbought
}
```

### Regime Gate

```go
// Momentum is invalid in these DataQualityDTO decision contexts:
// - "reject" (already filtered upstream)
// - Any token flagged with FAKE_LIQUIDITY_CASCADE, HONEYPOT_SELL_FAIL
// Additionally, if the system operational mode is STRICT,
// apply a tighter trend strength threshold (from config).

var invalidMomentumFlags = map[string]bool{
    "FAKE_LIQUIDITY_CASCADE": true,
    "HONEYPOT_SELL_FAIL":     true,
    "SELL_BLOCKED":           true,
    "RUG_PULL_HIGH_PROB":     true,
}

func MomentumBlockedByFlags(flags []string) bool {
    for _, f := range flags {
        if invalidMomentumFlags[f] { return true }
    }
    return false
}
```

### Primary Entry Point

```go
type MomentumResult struct {
    Valid          bool
    Score          float64  // [0.0, 1.0] — feeds EdgeDTO.Score directly
    TrendStrength  float64
    VolumeRatio    float64
    RSI            float64
    RejectReason   string   // empty if Valid=true
}

// DetectMomentum is a PURE FUNCTION — no DB calls, no RPC calls.
// Called from within the edge detection module.
func DetectMomentum(
    prices       []float64,
    volumes      []float64,
    dqFlags      []string,
    ageSeconds   int64,
    cfg          MomentumConfig,
) MomentumResult {
    // 1. Regime gate
    if MomentumBlockedByFlags(dqFlags) {
        return MomentumResult{Valid: false, RejectReason: "blocked_by_dq_flag"}
    }

    // 2. Volume confirmation (minimum 10 baseline bars)
    if len(volumes) < 11 {
        return MomentumResult{Valid: false, RejectReason: "insufficient_volume_history"}
    }
    volResult := ConfirmVolumeForMomentum(volumes[len(volumes)-1], volumes[:len(volumes)-1], cfg.MinVolumeRatio)
    if !volResult.Confirmed {
        return MomentumResult{Valid: false, RejectReason: "volume_insufficient",
            VolumeRatio: volResult.Ratio}
    }

    // 3. RSI filter
    rsi := ComputeRSI(prices, 14)
    if !RSIInValidRange(rsi, cfg.RSIOversold, cfg.RSIOverbought) {
        return MomentumResult{Valid: false, RejectReason: "rsi_out_of_range", RSI: rsi}
    }

    // 4. Trend strength (use recent 5 bars with ascending weights)
    n := len(prices)
    if n < 6 {
        return MomentumResult{Valid: false, RejectReason: "insufficient_price_history"}
    }
    weights := []float64{1.0, 2.0, 3.0, 4.0, 5.0}
    trend := ComputeTrendStrength(prices[n-6:], weights)

    // 5. Adaptive threshold: stricter for older pools
    threshold := cfg.MinTrendStrength
    if ageSeconds > int64(cfg.AdaptiveDecaySec) {
        // Older pools need stronger momentum to justify entry
        ageFactor := float64(ageSeconds) / float64(cfg.AdaptiveDecaySec)
        threshold = math.Min(0.85, cfg.MinTrendStrength*math.Sqrt(ageFactor))
    }

    if trend < threshold {
        return MomentumResult{Valid: false, RejectReason: "trend_below_threshold",
            TrendStrength: trend}
    }

    // All gates passed — compute composite score
    score := trend*0.6 + math.Min(volResult.Ratio/5.0, 1.0)*0.4
    score = math.Min(1.0, score)

    return MomentumResult{
        Valid:         true,
        Score:         score,
        TrendStrength: trend,
        VolumeRatio:   volResult.Ratio,
        RSI:           rsi,
    }
}
```

---

## Anti-Patterns

```
❌ Trading momentum with RSI > 70 (overbought — chase behavior)
❌ Using momentum as an independent signal (it must combine with DQ + features)
❌ Ignoring volume confirmation (price moves without volume = manipulation likely)
❌ Applying a fixed trend threshold regardless of pool age
❌ Calling DetectMomentum with <6 price bars (returns invalid, do not treat as valid)
❌ Skipping the DQ flag gate (a cascade-flagged token can still show "momentum")
```

---

## Config Reference (`config/pipeline.yaml`)

```yaml
momentum:
  min_trend_strength: 0.30
  min_volume_ratio: 1.5
  rsi_overbought: 70.0
  rsi_oversold: 30.0
  adaptive_decay_sec: 300 # 5 minutes — after this, threshold rises
  rsi_period: 14
  trend_bars: 5
```

---

## Checklist

- [ ] `DetectMomentum` is a pure function (zero DB/network calls)
- [ ] DQ flag gate is the FIRST check (fast path reject)
- [ ] Volume confirmation uses baseline history, not just current bar
- [ ] RSI computed over 14 periods minimum
- [ ] Adaptive threshold increases for pools older than `adaptive_decay_sec`
- [ ] Composite score combines trend (60%) + volume (40%)
- [ ] MomentumResult.Score feeds into EdgeDTO.Score (not stored separately)

---

## References

- `docs/architecture.md` § 3.3 — Signal & Edge Discovery (Layer 3)
- `docs/architecture-context/5_sniper_mode.md` — NEW_LAUNCH_EDGE definition
- `.github/skills/edge-detection/SKILL.md` — Gate sequence for EdgeDTO production
- `.github/skills/liquidity-event-detector/SKILL.md` — Volume spike and cascade flags
- `contracts/edge.go` — `EdgeDTO` fields
- `config/pipeline.yaml` — All momentum thresholds
