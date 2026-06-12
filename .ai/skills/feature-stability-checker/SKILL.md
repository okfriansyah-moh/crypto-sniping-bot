# Feature Stability Checker Skill

## Purpose

Prevent noisy or oscillating features from polluting composite scoring in the
Edge and Probability layers. A feature that alternates direction unpredictably
adds no signal — it only adds variance. This skill provides the gate and weight
redistribution logic.

**Minimum bars before stability check:** 30 (cold start uses all features)
**Consistency threshold:** 0.60 (60% directional consistency minimum)

```
DirectionalConsistency = dominant_direction_changes / total_changes
```

---

## Rules

### Directional Consistency Computation

```go
// Measures what fraction of consecutive changes point in the dominant direction.
// Returns [0.0, 1.0]. Higher = more consistent (more stable).

type ConsistencyResult struct {
    Consistency       float64  // [0,1]
    DominantDirection string   // "up" | "down" | "flat"
    TotalChanges      int
    IsZeroChange      bool     // true if series never changes — flag as potentially stale
}

func ComputeDirectionalConsistency(values []float64) ConsistencyResult {
    if len(values) < 2 {
        return ConsistencyResult{Consistency: 1.0}  // trivially consistent
    }

    ups, downs := 0, 0
    for i := 1; i < len(values); i++ {
        delta := values[i] - values[i-1]
        if delta > 1e-10      { ups++ }
        else if delta < -1e-10 { downs++ }
    }

    total := ups + downs
    if total == 0 {
        return ConsistencyResult{
            Consistency:  1.0,
            IsZeroChange: true,
            TotalChanges: 0,
        }
    }

    dominant := ups
    dir := "up"
    if downs > ups {
        dominant = downs
        dir = "down"
    }

    return ConsistencyResult{
        Consistency:       float64(dominant) / float64(total),
        DominantDirection: dir,
        TotalChanges:      total,
        IsZeroChange:      false,
    }
}
```

### Per-Feature Stability Check

```go
type FeatureStabilityResult struct {
    Stable        bool
    FeatureName   string
    Consistency   float64
    BarsAvailable int
    Reason        string  // why unstable
    IsStale       bool    // zero-change series flag
}

type StabilityConfig struct {
    MinConsistency float64  // default: 0.60 (config-driven)
    MinBars        int      // default: 30 (cold start threshold)
}

func CheckFeatureStability(
    featureName string,
    values       []float64,
    cfg          StabilityConfig,
) FeatureStabilityResult {
    n := len(values)

    // Cold start: not enough bars → assume stable (don't block cold start)
    if n < cfg.MinBars {
        return FeatureStabilityResult{
            Stable:        true,
            FeatureName:   featureName,
            BarsAvailable: n,
            Reason:        "cold_start_assume_stable",
        }
    }

    // Use rolling window (last MinBars values)
    window := values
    if len(values) > cfg.MinBars {
        window = values[len(values)-cfg.MinBars:]
    }

    result := ComputeDirectionalConsistency(window)

    stable := result.Consistency >= cfg.MinConsistency
    reason := ""
    if !stable {
        reason = fmt.Sprintf("consistency_%.2f_below_threshold_%.2f",
            result.Consistency, cfg.MinConsistency)
    }

    return FeatureStabilityResult{
        Stable:        stable,
        FeatureName:   featureName,
        Consistency:   result.Consistency,
        BarsAvailable: n,
        Reason:        reason,
        IsStale:       result.IsZeroChange,
    }
}
```

### Check All Features + Weight Redistribution

```go
type AllFeaturesStabilityResult struct {
    FeatureResults  map[string]FeatureStabilityResult
    StableFeatures  []string
    UnstableFeatures []string
    StabilityRatio  float64  // stable / total
}

type FeatureWeights map[string]float64

// CheckAllAndRedistribute gates unstable features and redistributes
// their weight proportionally to stable features.
func CheckAllAndRedistribute(
    featureSeries map[string][]float64,
    originalWeights FeatureWeights,
    cfg StabilityConfig,
) (AllFeaturesStabilityResult, FeatureWeights) {
    result := AllFeaturesStabilityResult{
        FeatureResults: make(map[string]FeatureStabilityResult),
    }

    for name, series := range featureSeries {
        sr := CheckFeatureStability(name, series, cfg)
        result.FeatureResults[name] = sr
        if sr.Stable {
            result.StableFeatures = append(result.StableFeatures, name)
        } else {
            result.UnstableFeatures = append(result.UnstableFeatures, name)
        }
    }

    total := len(featureSeries)
    if total > 0 {
        result.StabilityRatio = float64(len(result.StableFeatures)) / float64(total)
    }

    // Weight redistribution
    newWeights := make(FeatureWeights)

    // Sum of weights going to unstable features
    var redistWeight float64
    for _, name := range result.UnstableFeatures {
        if w, ok := originalWeights[name]; ok {
            redistWeight += w
        }
        newWeights[name] = 0.0  // zero out unstable
    }

    // Distribute unstable weight proportionally to stable features
    if len(result.StableFeatures) > 0 && redistWeight > 0 {
        var stableWeightSum float64
        for _, name := range result.StableFeatures {
            if w, ok := originalWeights[name]; ok {
                stableWeightSum += w
            }
        }
        for _, name := range result.StableFeatures {
            baseWeight := originalWeights[name]
            if stableWeightSum > 0 {
                bonus := redistWeight * (baseWeight / stableWeightSum)
                newWeights[name] = baseWeight + bonus
            } else {
                newWeights[name] = baseWeight
            }
        }
    } else {
        for _, name := range result.StableFeatures {
            newWeights[name] = originalWeights[name]
        }
    }

    return result, newWeights
}
```

### Integration Pattern (Edge Module)

```go
// Called at the start of every edge scoring cycle — not cached.
// Stale feature warnings are emitted as system_event but do NOT block scoring.

func (m *EdgeModule) AdjustWeightsForStability(
    ctx    context.Context,
    series map[string][]float64,
) FeatureWeights {
    stabilityResult, adjustedWeights := CheckAllAndRedistribute(
        series, m.cfg.BaseFeatureWeights, m.cfg.StabilityCfg)

    // Warn on stale features
    for name, fr := range stabilityResult.FeatureResults {
        if fr.IsStale {
            m.adapter.EmitSystemEvent(ctx, "feature_stale", map[string]any{
                "feature": name,
                "bars":    fr.BarsAvailable,
            })
        }
    }

    // Log if many features are unstable
    if stabilityResult.StabilityRatio < 0.5 {
        m.adapter.EmitSystemEvent(ctx, "feature_stability_degraded", map[string]any{
            "stability_ratio":  stabilityResult.StabilityRatio,
            "unstable_count":   len(stabilityResult.UnstableFeatures),
            "unstable_features": stabilityResult.UnstableFeatures,
        })
    }

    return adjustedWeights
}
```

---

## Anti-Patterns

```
❌ Caching stability results across scoring cycles (must recheck every cycle)
❌ Setting feature weight to 0 during cold start (cold start = assume stable)
❌ Blocking scoring entirely when many features are unstable (redistribute, don't block)
❌ Using a lookback larger than available history (use min(len, cfg.MinBars))
❌ Ignoring stale features (zero-change features may indicate data pipeline issues)
❌ Applying weight redistribution without bounding (sum of weights must remain 1.0)
```

---

## Config Reference (`config/pipeline.yaml`)

```yaml
feature_stability:
  min_consistency: 0.60 # 60% directional consistency required
  min_bars: 30 # cold start threshold
  lookback_bars: 50 # rolling window for stability check
```

---

## Checklist

- [ ] Stability check runs every scoring cycle (never cached)
- [ ] Cold start (N < 30) returns `Stable=true` to avoid blocking early trades
- [ ] Unstable features get `weight=0`; redistributed weight goes to stable features
- [ ] Redistributed weights sum to same total as original weights
- [ ] Stale features (zero-change) flagged as `system_event` (not blocked)
- [ ] Stability ratio < 0.5 triggers `feature_stability_degraded` system event
- [ ] All thresholds from `config/pipeline.yaml`

---

## References

- `docs/architecture.md` § 3.2 — Feature Extraction (Layer 2)
- `docs/architecture-context/4_feature_extraction.md` — Feature confidence
- `.github/skills/edge-detection/SKILL.md` — EdgeDTO production using features
- `.github/skills/probability-modeling/SKILL.md` — P(success) model inputs
- `contracts/feature.go` — `FeatureDTO`, `FeatureConfidence` fields