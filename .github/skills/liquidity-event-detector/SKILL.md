---
name: liquidity-event-detector
type: skill
description: >
  DEX liquidity event detection — volume spikes, order imbalance, and liquidation
  cascades on-chain. Use when implementing or reviewing liquidity regime filters in
  the Data Quality Engine (Layer 1) or Edge Validation (Layer 5). Liquidity events
  are filters and safety gates, NEVER standalone alpha sources.
---

# Liquidity Event Detector Skill

## Purpose

Detect abnormal liquidity conditions on-chain that invalidate an edge or increase
execution risk. This layer sits between raw data ingestion (Layer 0) and edge
scoring (Layer 3). When liquidity is abnormal, the DataQualityDTO risk score rises
and the pipeline filters the token.

**Core principle:** Abnormal liquidity destroys Execution and DataQuality factors
simultaneously — high slippage + potential rug/exit-scam are correlated.

```
LiquidityScore = VolumeSpike_component - ImbalancePenalty + CascadeRisk
Range: [-1.0, +1.0]
Negative = unhealthy. Positive = healthy.
```

---

## Rules

### Volume Spike Detection (DEX-specific)

```go
// Crypto DEX thresholds are more aggressive than traditional markets.
// All thresholds live in shared/config/pipeline.yaml.
type VolumeSpikeConfig struct {
    SpikeMultiplierCrypto float64 // default: 3.0×
    BaselineWindowBars    int     // default: 20 bars
}

type VolumeSpikeResult struct {
    Ratio   float64 // current_vol / rolling_avg_vol
    IsSpike bool
    Valid   bool    // false if insufficient baseline data
}

func ComputeVolumeSpike(volumes []float64, cfg VolumeSpikeConfig) VolumeSpikeResult {
    if len(volumes) < cfg.BaselineWindowBars+1 {
        return VolumeSpikeResult{Valid: false}
    }
    baseline := volumes[:cfg.BaselineWindowBars]
    current := volumes[len(volumes)-1]

    var sum float64
    for _, v := range baseline { sum += v }
    avg := sum / float64(len(baseline))

    if avg == 0 {
        return VolumeSpikeResult{Valid: false}
    }

    ratio := current / avg
    return VolumeSpikeResult{
        Ratio:   ratio,
        IsSpike: ratio >= cfg.SpikeMultiplierCrypto,
        Valid:   true,
    }
}
```

### Order Imbalance (Buy/Sell Pressure)

```go
// On-chain DEX imbalance: aggressive buys vs sells in the current window.
// Range: [-1, +1]. Positive = buy pressure. Negative = sell pressure.
// |imbalance| >= 0.7 = imbalanced (potential manipulation or dump)

type ImbalanceResult struct {
    Imbalance    float64  // (buy_vol - sell_vol) / total_vol
    IsImbalanced bool     // abs(imbalance) >= threshold (config-driven)
    Direction    string   // "buy_heavy" | "sell_heavy" | "balanced"
}

func ComputeOrderImbalance(
    buyVolume float64,
    sellVolume float64,
    imbalanceThreshold float64, // from config, e.g., 0.7
) ImbalanceResult {
    total := buyVolume + sellVolume
    if total == 0 {
        return ImbalanceResult{Direction: "balanced"}
    }

    imbalance := (buyVolume - sellVolume) / total
    abs := math.Abs(imbalance)
    dir := "balanced"
    if imbalance > imbalanceThreshold {
        dir = "buy_heavy"
    } else if imbalance < -imbalanceThreshold {
        dir = "sell_heavy"
    }

    return ImbalanceResult{
        Imbalance:    imbalance,
        IsImbalanced: abs >= imbalanceThreshold,
        Direction:    dir,
    }
}
```

### Liquidation Cascade Detection

```go
// Cascade = rapid price drop >3% + extreme volume spike (≥5×) in a 5-bar window.
// On DEX: manifests as LP removal + large sells in a short burst.

type CascadeResult struct {
    IsCascade bool
    Severity  string  // "low" | "medium" | "high"
    PriceDrop float64 // e.g., 0.045 = 4.5% drop
    VolRatio  float64 // spike ratio during cascade window
}

func DetectLiquidationCascade(
    prices []float64,
    volumes []float64,
    priceDropThreshold float64,  // config: 0.03
    volSpikeThreshold  float64,  // config: 5.0
    windowBars         int,      // config: 5
) CascadeResult {
    n := len(prices)
    if n < windowBars+1 {
        return CascadeResult{}
    }

    window := prices[n-windowBars:]
    peakPrice := window[0]
    troughPrice := window[0]
    for _, p := range window {
        if p > peakPrice  { peakPrice = p }
        if p < troughPrice { troughPrice = p }
    }

    if peakPrice == 0 {
        return CascadeResult{}
    }
    drop := (peakPrice - troughPrice) / peakPrice

    // Volume in cascade window vs prior baseline
    priorAvgVol := meanFloat64(volumes[:n-windowBars])
    cascadeAvgVol := meanFloat64(volumes[n-windowBars:])
    ratio := 0.0
    if priorAvgVol > 0 {
        ratio = cascadeAvgVol / priorAvgVol
    }

    isCascade := drop >= priceDropThreshold && ratio >= volSpikeThreshold
    severity := "low"
    if isCascade {
        switch {
        case drop >= 0.15 || ratio >= 10.0:
            severity = "high"
        case drop >= 0.07 || ratio >= 7.0:
            severity = "medium"
        }
    }

    return CascadeResult{
        IsCascade: isCascade,
        Severity:  severity,
        PriceDrop: drop,
        VolRatio:  ratio,
    }
}
```

### Composite Liquidity Score

```go
// Aggregate all three components into a single health score.
// Score ∈ [-1.0, +1.0]. Higher = healthier.
// This score feeds into DataQualityDTO.LiquidityRiskScore.

func ComputeLiquidityScore(
    spikeResult    VolumeSpikeResult,
    imbalResult    ImbalanceResult,
    cascadeResult  CascadeResult,
) float64 {
    score := 0.0

    // Volume spike component: spike hurts score
    if spikeResult.Valid {
        if spikeResult.IsSpike {
            // Penalize proportional to spike severity
            score -= math.Min(spikeResult.Ratio/10.0, 0.5)
        } else {
            score += 0.2  // healthy volume
        }
    }

    // Imbalance penalty: extreme imbalance is a red flag
    score -= math.Abs(imbalResult.Imbalance) * 0.3

    // Cascade: severe hit
    if cascadeResult.IsCascade {
        switch cascadeResult.Severity {
        case "high":   score -= 0.6
        case "medium": score -= 0.4
        case "low":    score -= 0.2
        }
    }

    // Clamp to [-1.0, +1.0]
    return math.Max(-1.0, math.Min(1.0, score))
}
```

### Integration with DataQualityDTO

```go
// Liquidity event detection is a sub-component of the DataQuality module.
// It contributes a LiquidityRiskScore to the risk aggregation in anti-manipulation.
// A cascade always adds the FAKE_LIQUIDITY_CASCADE flag.

func EnrichDQWithLiquidity(
    dq contracts.DataQualityDTO,
    volumes, prices []float64,
    buyVol, sellVol float64,
    cfg LiquidityConfig,
) contracts.DataQualityDTO {
    spike   := ComputeVolumeSpike(volumes, cfg.SpikeConfig)
    imbal   := ComputeOrderImbalance(buyVol, sellVol, cfg.ImbalanceThreshold)
    cascade := DetectLiquidationCascade(prices, volumes,
        cfg.CascadeDropThreshold, cfg.CascadeVolThreshold, cfg.CascadeWindowBars)

    flags := dq.Flags
    if cascade.IsCascade {
        flags = append(flags, "FAKE_LIQUIDITY_CASCADE")
    }
    if imbal.Direction == "sell_heavy" && imbal.IsImbalanced {
        flags = append(flags, "SELL_PRESSURE_EXTREME")
    }

    liquidityScore := ComputeLiquidityScore(spike, imbal, cascade)
    // NOTE: LiquidityRiskScore maps to [-1,+1]; 0 = neutral risk.
    // Negative = higher risk contribution to DataQualityDTO.RiskScore.
    // The DQ module negates and clamps: risk += max(0, -liquidityScore)

    return contracts.DataQualityDTO{
        EventID:       dq.EventID,
        TraceID:       dq.TraceID,
        CorrelationID: dq.CorrelationID,
        CausationID:   dq.CausationID,
        VersionID:     dq.VersionID,
        // ... copy all existing fields ...
        Flags: flags,
        // LiquidityRiskScore added as a field: see contracts/data_quality.go
    }
}
```

---

## Anti-Patterns

```
❌ Using liquidity signals as alpha source (they are filters only)
❌ Logging raw orderbook data (privacy + volume risk — aggregate only)
❌ Sharing a single volume-spike baseline across markets (must be per-pool)
❌ Triggering cascade detection with <5 bars of data
❌ Setting imbalance threshold above 0.9 (allows obviously manipulated pools)
❌ Not flagging FAKE_LIQUIDITY_CASCADE — downstream learning can't classify loss cause
```

---

## Config Reference (`shared/config/pipeline.yaml`)

```yaml
liquidity:
  spike_multiplier_crypto: 3.0
  baseline_window_bars: 20
  imbalance_threshold: 0.7
  cascade:
    price_drop_threshold: 0.03 # 3% drop in window
    vol_spike_threshold: 5.0 # 5× baseline volume
    window_bars: 5
```

---

## Checklist

- [ ] Volume spike baseline uses at least 20 bars (not current candle)
- [ ] Order imbalance denominator guards against zero total volume
- [ ] Cascade detection requires both price drop AND volume spike (AND, not OR)
- [ ] `FAKE_LIQUIDITY_CASCADE` flag always set on cascade detection
- [ ] Liquidity score feeds into DataQualityDTO risk aggregation (not a standalone gate)
- [ ] All thresholds in `shared/config/pipeline.yaml`, none hardcoded

---

## References

- `docs/reference/architecture.md` § 3.1 — Data Quality Engine (Layer 1)
- `docs/archive/architecture-context/3_data_quality_engine.md` — Detector algorithms
- `.github/skills/anti-manipulation/SKILL.md` — Risk score aggregation (this feeds into it)
- `.github/skills/data-quality-engine/SKILL.md` — Full DQ module architecture
- `shared/contracts/data_quality.go` — `DataQualityDTO` field definitions
