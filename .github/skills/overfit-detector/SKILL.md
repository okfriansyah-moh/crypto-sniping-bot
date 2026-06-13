---
name: overfit-detector
type: skill
description: >
  Anti-overfitting gate for the probability model and feature configuration.
  Use when adding indicators, tuning model parameters, or reviewing feature sets.
  Enforces: max 5 indicators per composite, max 3 tunable parameters per indicator,
  and minimum 100 samples before full scoring. Blocks scoring until audit passes.
---

# Overfit Detector Skill

## Purpose

Prevent the probability model and edge scoring from being over-engineered to
historical data at the expense of forward performance. Overfitted models degrade
AdaptationQuality: they appear to work on backtests but collapse in production.

**Hard limits (no exceptions in production):**

```
Max indicators per composite:     5
Max tunable parameters per indicator: 3
Min samples for full scoring:     100
Min samples for cold start:       30
Min samples to block scoring:     < 30 → always reject
```

---

## Rules

### Indicator Count Gate

```go
// An "indicator" is any function that consumes FeatureDTO fields
// and returns a numeric signal score.
// Composites are EdgeDTO.Score or ProbabilityEstimateDTO.Score.

type IndicatorSpec struct {
    Name       string
    Parameters map[string]float64  // tunable parameters only
    Enabled    bool
}

type IndicatorAuditResult struct {
    Valid           bool
    Count           int
    MaxAllowed      int
    Reason          string
}

func ValidateIndicatorCount(indicators []IndicatorSpec) IndicatorAuditResult {
    const maxIndicators = 5  // read from config in production
    active := 0
    for _, ind := range indicators {
        if ind.Enabled { active++ }
    }
    if active > maxIndicators {
        return IndicatorAuditResult{
            Valid:      false,
            Count:      active,
            MaxAllowed: maxIndicators,
            Reason:     fmt.Sprintf("too_many_indicators: %d > %d", active, maxIndicators),
        }
    }
    return IndicatorAuditResult{Valid: true, Count: active, MaxAllowed: maxIndicators}
}
```

### Parameter Count Gate

```go
type ParamAuditResult struct {
    Valid       bool
    Name        string
    ParamCount  int
    MaxAllowed  int
    Reason      string
}

func ValidateParameterCount(ind IndicatorSpec) ParamAuditResult {
    const maxParams = 3  // read from config in production
    count := 0
    for _, v := range ind.Parameters {
        _ = v
        count++
    }
    if count > maxParams {
        return ParamAuditResult{
            Valid:      false,
            Name:       ind.Name,
            ParamCount: count,
            MaxAllowed: maxParams,
            Reason:     fmt.Sprintf("%s has %d params > max %d", ind.Name, count, maxParams),
        }
    }
    return ParamAuditResult{Valid: true, Name: ind.Name, ParamCount: count, MaxAllowed: maxParams}
}
```

### Sample Size Gate

```go
// Three states depending on available sample count:
//   sufficient:    N >= 100 → full confidence scoring
//   cold_start:    30 <= N < 100 → scoring allowed, but confidence decayed
//   insufficient:  N < 30 → scoring BLOCKED, return zero-confidence

type SampleSizeStatus string

const (
    SampleSufficient   SampleSizeStatus = "sufficient"
    SampleColdStart    SampleSizeStatus = "cold_start"
    SampleInsufficient SampleSizeStatus = "insufficient"
)

type SampleSizeResult struct {
    Status           SampleSizeStatus
    N                int
    ConfidenceDecay  float64  // [0,1]: 1.0 = no decay (sufficient), <1 = apply as multiplier
}

func ValidateSampleSize(n int, featureName string) SampleSizeResult {
    // Thresholds from config/pipeline.yaml
    const fullScoreMin = 100
    const coldStartMin = 30

    switch {
    case n >= fullScoreMin:
        return SampleSizeResult{
            Status:          SampleSufficient,
            N:               n,
            ConfidenceDecay: 1.0,
        }
    case n >= coldStartMin:
        // Linear decay from 0.3 (at 30 samples) to 1.0 (at 100 samples)
        decay := 0.3 + 0.7*float64(n-coldStartMin)/float64(fullScoreMin-coldStartMin)
        return SampleSizeResult{
            Status:          SampleColdStart,
            N:               n,
            ConfidenceDecay: decay,
        }
    default:
        return SampleSizeResult{
            Status:          SampleInsufficient,
            N:               n,
            ConfidenceDecay: 0.0,
        }
    }
}
```

### Full Audit

```go
type OverfitAuditResult struct {
    Valid           bool
    Issues          []string
    TotalIndicators int
    TotalParameters int
    RiskLevel       string  // "low" | "medium" | "high"
    SampleStatus    SampleSizeStatus
    ConfidenceDecay float64
}

func RunOverfitAudit(
    indicators []IndicatorSpec,
    sampleCount int,
) OverfitAuditResult {
    result := OverfitAuditResult{Valid: true}
    var issues []string

    // Indicator count
    indAudit := ValidateIndicatorCount(indicators)
    result.TotalIndicators = indAudit.Count
    if !indAudit.Valid {
        issues = append(issues, indAudit.Reason)
    }

    // Parameter count per indicator
    totalParams := 0
    for _, ind := range indicators {
        if !ind.Enabled { continue }
        paramAudit := ValidateParameterCount(ind)
        totalParams += paramAudit.ParamCount
        if !paramAudit.Valid {
            issues = append(issues, paramAudit.Reason)
        }
    }
    result.TotalParameters = totalParams

    // Sample size
    sampleResult := ValidateSampleSize(sampleCount, "composite")
    result.SampleStatus = sampleResult.Status
    result.ConfidenceDecay = sampleResult.ConfidenceDecay
    if sampleResult.Status == SampleInsufficient {
        issues = append(issues, fmt.Sprintf("insufficient_samples: %d < 30", sampleCount))
    }

    // Risk classification
    result.RiskLevel = classifyOverfitRisk(indAudit.Count, totalParams)
    result.Issues = issues
    result.Valid = len(issues) == 0

    return result
}

func classifyOverfitRisk(indicatorCount, totalParams int) string {
    if indicatorCount <= 3 && totalParams <= 6 {
        return "low"
    }
    if indicatorCount <= 5 && totalParams <= 12 {
        return "medium"
    }
    return "high"
}
```

### Integration: Block Scoring on Audit Failure

```go
// The edge or probability module MUST run this audit at startup
// and on every config change. Failures BLOCK the module from scoring.

func (m *ProbabilityModule) OnConfigChange(
    ctx     context.Context,
    adapter database.Adapter,
    cfg     ProbabilityConfig,
) error {
    audit := RunOverfitAudit(cfg.Indicators, m.sampleCount)
    if !audit.Valid {
        return fmt.Errorf("overfit_audit_failed: %v", audit.Issues)
    }
    // Log risk level
    if audit.RiskLevel != "low" {
        adapter.EmitSystemEvent(ctx, "overfit_risk_elevated", map[string]any{
            "risk_level":        audit.RiskLevel,
            "indicator_count":   audit.TotalIndicators,
            "total_parameters":  audit.TotalParameters,
            "sample_count":      m.sampleCount,
        })
    }
    return nil
}
```

---

## Anti-Patterns

```
❌ Adding a 6th indicator "just for this market" — the cap is hard
❌ Combining multiple signals into one "super-indicator" to bypass the count limit
❌ Proceeding with scoring when SampleInsufficient — produces random-quality output
❌ Running the audit only once at startup (must re-run on every config change)
❌ Using ML-based indicators without 1000+ samples (rule-based first)
❌ Setting ConfidenceDecay = 1.0 during cold start (defeats the purpose)
```

---

## Config Reference (`config/pipeline.yaml`)

```yaml
overfit_detection:
  max_indicators: 5
  max_params_per_indicator: 3
  full_score_min_samples: 100
  cold_start_min_samples: 30
  block_below_samples: 30
```

---

## Checklist

- [ ] Audit runs at module startup and on every config change
- [ ] Scoring is BLOCKED when `SampleInsufficient` (N < 30)
- [ ] Cold start applies confidence decay multiplier to score
- [ ] Indicator count includes only enabled indicators
- [ ] Parameter count counts tunable params only (not fixed constants)
- [ ] `RiskLevel` emitted as `system_event` when medium or high
- [ ] All limits read from `config/pipeline.yaml`, none hardcoded

---

## References

- `docs/reference/architecture.md` § 3.5 — Edge Validation (Layer 5)
- `docs/archive/architecture-context/6_slippage_models.md` — Model parameter guidelines
- `.github/skills/probability-modeling/SKILL.md` — P(success) model patterns
- `.github/skills/learning-engine/SKILL.md` — Bounded updates and sample requirements
- `contracts/probability.go` — `ProbabilityEstimateDTO`
