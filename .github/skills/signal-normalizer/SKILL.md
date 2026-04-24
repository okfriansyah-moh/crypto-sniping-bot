---
name: signal-normalizer
type: skill
description: >
  Two-stage signal normalization pipeline: Z-score per signal variance, then
  sigmoid compression to [-1, +1]. Use when implementing or reviewing feature
  score normalization in the Edge module (Layer 3) and Probability module (Layer 4).
  Normalization is deterministic — same raw score always produces identical output.
---

# Signal Normalizer Skill

## Purpose

Raw indicator scores have incompatible scales: RSI lives in [0,100], momentum
score in [-2,+2], liquidity imbalance in [-1,+1]. Mixing raw scores in a composite
produces garbage dominated by the highest-variance indicator. This skill normalizes
everything to a common range before weighting.

**Two-stage pipeline:**

```
Stage 1: Z-score per signal (variance normalization)
           z = (raw - μ) / σ  [σ floored at 1e-8]

Stage 2: Sigmoid compression (bounded output)
           s = (2 / (1 + exp(-k × z))) - 1  → maps R to (-1, +1)
           k is market-specific (crypto k=3.0, from config)
```

**Determinism invariant:** Same input + same config = identical output. No randomness.

---

## Rules

### Stage 1: Z-Score Variance Normalization

```go
// NormalizeSignalVariance computes the Z-score of a single raw signal value
// given a rolling history of that signal.
//
// lookback: number of historical values to use for μ and σ.
//   Recommended: 60 bars (from config).
//   If fewer bars available, use all available history.

type ZScoreResult struct {
    ZScore   float64
    Mean     float64
    Sigma    float64
    N        int
    Floored  bool  // true if sigma was floored (near-zero variance)
}

func NormalizeSignalVariance(
    raw      float64,
    history  []float64,  // recent historical values of this signal
    lookback int,
) ZScoreResult {
    if len(history) == 0 {
        return ZScoreResult{ZScore: 0, N: 0}  // zero = neutral when no history
    }

    // Use last `lookback` values only
    window := history
    if len(history) > lookback {
        window = history[len(history)-lookback:]
    }

    n := len(window)
    var sum float64
    for _, v := range window {
        sum += v
    }
    mu := sum / float64(n)

    var variance float64
    for _, v := range window {
        d := v - mu
        variance += d * d
    }
    sigma := math.Sqrt(variance / float64(n))

    // Floor sigma to prevent division by zero
    floored := false
    const sigmaFloor = 1e-8
    if sigma < sigmaFloor {
        sigma = sigmaFloor
        floored = true
    }

    return ZScoreResult{
        ZScore:  (raw - mu) / sigma,
        Mean:    mu,
        Sigma:   sigma,
        N:       n,
        Floored: floored,
    }
}
```

### Stage 2: Sigmoid Compression

```go
// SigmoidNormalize compresses a Z-score (or weighted composite) into [-1, +1].
// k controls steepness: higher k = sharper transitions around zero.
// Crypto default: k=3.0 (from config).
//
// Raw input is clamped to [-10, +10] BEFORE exp() to prevent float64 overflow.

type SigmoidResult struct {
    Score   float64  // [-1, +1]
    Clamped bool     // true if raw was clamped before exp
    K       float64
}

func SigmoidNormalize(raw float64, k float64) SigmoidResult {
    const clampBound = 10.0
    clamped := false
    if raw > clampBound {
        raw = clampBound
        clamped = true
    } else if raw < -clampBound {
        raw = -clampBound
        clamped = true
    }

    // Sigmoid: maps (-∞, +∞) to (-1, +1)
    score := (2.0 / (1.0 + math.Exp(-k*raw))) - 1.0

    return SigmoidResult{
        Score:   score,
        Clamped: clamped,
        K:       k,
    }
}
```

### Full Two-Stage Pipeline

```go
// NormalizeSignal applies both stages in sequence:
//   raw → Z-score (variance normalize) → sigmoid (bound to [-1,+1])

type NormalizeConfig struct {
    Lookback int     // bars for μ/σ calculation (default: 60)
    K        float64 // sigmoid steepness (crypto: 3.0)
}

type NormalizedSignal struct {
    Raw       float64
    ZScore    float64
    Score     float64  // final bounded score [-1, +1]
    Direction string   // "long" | "short" | "neutral"
    Strength  float64  // abs(Score) → [0, 1]
}

func NormalizeSignal(
    raw     float64,
    history []float64,
    cfg     NormalizeConfig,
) NormalizedSignal {
    zResult := NormalizeSignalVariance(raw, history, cfg.Lookback)
    sResult := SigmoidNormalize(zResult.ZScore, cfg.K)

    direction, threshold := ExtractDirection(sResult.Score, cfg.DirectionThreshold)

    return NormalizedSignal{
        Raw:       raw,
        ZScore:    zResult.ZScore,
        Score:     sResult.Score,
        Direction: direction,
        Strength:  math.Abs(sResult.Score),
    }
    _ = threshold
}
```

### Direction + Strength Extraction

```go
// DirectionThreshold is market-specific (crypto: 0.15, from config).
// Score in (-threshold, +threshold) → neutral.

type DirectionConfig struct {
    Threshold float64  // crypto: 0.15
}

func ExtractDirection(score float64, cfg DirectionConfig) (string, float64) {
    switch {
    case score > cfg.Threshold:
        return "long", cfg.Threshold
    case score < -cfg.Threshold:
        return "short", cfg.Threshold
    default:
        return "neutral", cfg.Threshold
    }
}

func ExtractStrength(score float64) float64 {
    return math.Abs(score)  // [0, 1]
}
```

### Composite Score (Weighted Sum of Normalized Signals)

```go
// Each indicator score is normalized independently (Stage 1),
// then the weighted sum is passed through a single shared sigmoid (Stage 2).
// Do NOT apply sigmoid per-indicator — only at the composite level.

type IndicatorScore struct {
    Name    string
    ZScore  float64
    Weight  float64
}

func ComputeCompositeScore(
    indicators []IndicatorScore,
    cfg        NormalizeConfig,
) NormalizedSignal {
    var weightedSum float64
    var totalWeight float64
    for _, ind := range indicators {
        weightedSum += ind.ZScore * ind.Weight
        totalWeight += ind.Weight
    }
    if totalWeight == 0 {
        return NormalizedSignal{Score: 0, Direction: "neutral"}
    }
    compositeZ := weightedSum / totalWeight

    sResult := SigmoidNormalize(compositeZ, cfg.K)
    direction, _ := ExtractDirection(sResult.Score, cfg.DirectionConfig)
    return NormalizedSignal{
        ZScore:    compositeZ,
        Score:     sResult.Score,
        Direction: direction,
        Strength:  math.Abs(sResult.Score),
    }
}
```

---

## Anti-Patterns

```
❌ Applying sigmoid per-indicator then averaging sigmoids (wrong — one sigmoid at composite)
❌ Not clamping raw input before math.Exp() (causes +Inf overflow for large Z-scores)
❌ Not flooring sigma (division by zero for constant signals)
❌ Using different k values per scoring cycle (k must be config-driven and stable)
❌ Using wall-clock randomness in the lookback window selection
❌ Returning 0 direction when abs(score) > threshold (boundary case bug)
```

---

## Config Reference (`config/pipeline.yaml`)

```yaml
signal_normalizer:
  lookback_bars: 60 # rolling window for Z-score computation
  sigmoid_k_crypto: 3.0 # sigmoid steepness for crypto markets
  direction_threshold: 0.15 # [-threshold, +threshold] = neutral
  sigma_floor: 1.0e-8 # prevents division by zero
  raw_clamp_bound: 10.0 # pre-sigmoid clamp to prevent overflow
```

---

## Checklist

- [ ] Stage 1 (Z-score) applied per-signal before weighting
- [ ] Stage 2 (sigmoid) applied once at composite level, not per-indicator
- [ ] Sigma floored at 1e-8 to prevent division by zero
- [ ] Raw composite Z-score clamped to [-10, +10] before sigmoid
- [ ] Same raw score + same config = identical output (no randomness)
- [ ] Direction threshold from config (not hardcoded 0.15)
- [ ] Sigmoid k from config (not hardcoded 3.0)
- [ ] Strength = abs(score) ∈ [0, 1]

---

## References

- `docs/architecture.md` § 3.3 — Signal & Edge Discovery (Layer 3)
- `docs/architecture-context/5_sniper_mode.md` — Signal gating for sniper
- `.github/skills/edge-detection/SKILL.md` — EdgeDTO score production
- `.github/skills/probability-modeling/SKILL.md` — P(success) model inputs
- `.github/skills/feature-stability-checker/SKILL.md` — Stable features feed into normalizer
- `contracts/edge.go` — `EdgeDTO.Score` field
