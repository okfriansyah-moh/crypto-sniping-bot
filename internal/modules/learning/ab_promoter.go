package learning

import (
	"context"
	"fmt"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// ABPromoter evaluates whether a shadow candidate version should be promoted to active.
// Promotion conditions (all must hold):
//   - candidateEval.SampleSize >= MinSampleSize (30)
//   - candidateEval.Expectancy > baselineEval.Expectancy × 1.05
//   - candidateEval.MaxDrawdownPct <= baselineEval.MaxDrawdownPct
type ABPromoter struct {
	cfg *config.LearningConfig
}

// NewABPromoter returns an ABPromoter.
func NewABPromoter(cfg *config.LearningConfig) *ABPromoter {
	return &ABPromoter{cfg: cfg}
}

// ShouldPromote returns true when all promotion conditions are satisfied.
// candidateEval and baselineEval are EvaluationDTOs for the candidate and
// current active version respectively.
func (p *ABPromoter) ShouldPromote(
	_ context.Context,
	candidateEval contracts.EvaluationDTO,
	baselineEval contracts.EvaluationDTO,
) (bool, string, error) {
	minSamples := p.cfg.MinSampleSize
	if minSamples <= 0 {
		minSamples = 30
	}

	if int(candidateEval.SampleSize) < minSamples {
		return false, fmt.Sprintf("insufficient_samples: have %d need %d",
			candidateEval.SampleSize, minSamples), nil
	}

	if candidateEval.Expectancy <= baselineEval.Expectancy*1.05 {
		return false, fmt.Sprintf("expectancy_not_improved: candidate=%.4f baseline=%.4f",
			candidateEval.Expectancy, baselineEval.Expectancy), nil
	}

	if candidateEval.MaxDrawdownPct > baselineEval.MaxDrawdownPct {
		return false, fmt.Sprintf("drawdown_worse: candidate=%.4f baseline=%.4f",
			candidateEval.MaxDrawdownPct, baselineEval.MaxDrawdownPct), nil
	}

	return true, "promotion_conditions_met", nil
}

// ShouldRollback returns true when promotedEval.Expectancy has degraded by
// more than rollbackThreshold relative to baselineEval.Expectancy.
func ShouldRollback(
promotedEval contracts.EvaluationDTO,
baselineEval contracts.EvaluationDTO,
rollbackThreshold float64,
) bool {
if baselineEval.Expectancy <= 0 {
return false
}
degradation := (baselineEval.Expectancy - promotedEval.Expectancy) / baselineEval.Expectancy
return degradation > rollbackThreshold
}
