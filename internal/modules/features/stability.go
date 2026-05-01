package features

// Feature stability gating per the feature-stability-checker skill.
//
// A feature whose direction flips chaotically over the rolling window is
// noise, not signal. The gate sets its weight to 0 and redistributes the
// freed weight proportionally across the stable features so the composite
// remains comparable to the un-gated baseline.

// StabilityConfig captures the gate's thresholds.
type StabilityConfig struct {
	MinConsistency float64 // e.g. 0.60
	MinBars        int     // cold-start threshold; below this, treat as stable
	Lookback       int     // rolling window size for the gate
}

// DefaultStabilityConfig mirrors the feature-stability-checker skill defaults.
func DefaultStabilityConfig() StabilityConfig {
	return StabilityConfig{
		MinConsistency: 0.60,
		MinBars:        30,
		Lookback:       50,
	}
}

// ConsistencyResult is the output of the per-feature directional consistency
// computation.
type ConsistencyResult struct {
	Consistency       float64
	DominantDirection string // "up" | "down" | "flat"
	TotalChanges      int
	IsZeroChange      bool
}

const consistencyEpsilon = 1e-10

// ComputeDirectionalConsistency returns the fraction of consecutive deltas
// pointing in the dominant direction. Zero-change series are flagged as
// stale (potential data pipeline issue) and treated as trivially consistent.
func ComputeDirectionalConsistency(values []float64) ConsistencyResult {
	if len(values) < 2 {
		return ConsistencyResult{Consistency: 1.0, DominantDirection: "flat"}
	}
	ups, downs := 0, 0
	for i := 1; i < len(values); i++ {
		d := values[i] - values[i-1]
		switch {
		case d > consistencyEpsilon:
			ups++
		case d < -consistencyEpsilon:
			downs++
		}
	}
	total := ups + downs
	if total == 0 {
		return ConsistencyResult{
			Consistency:       1.0,
			DominantDirection: "flat",
			TotalChanges:      0,
			IsZeroChange:      true,
		}
	}
	dom := ups
	dir := "up"
	if downs > ups {
		dom = downs
		dir = "down"
	}
	return ConsistencyResult{
		Consistency:       float64(dom) / float64(total),
		DominantDirection: dir,
		TotalChanges:      total,
	}
}

// FeatureStabilityResult is the per-feature gate verdict.
type FeatureStabilityResult struct {
	Stable        bool
	FeatureName   string
	Consistency   float64
	BarsAvailable int
	IsStale       bool
	Reason        string
}

// CheckFeatureStability decides whether a feature passes the directional
// consistency gate. Cold start (n < MinBars) returns Stable=true so early
// trades are not blocked.
func CheckFeatureStability(name string, values []float64, cfg StabilityConfig) FeatureStabilityResult {
	n := len(values)
	if n < cfg.MinBars {
		return FeatureStabilityResult{
			Stable:        true,
			FeatureName:   name,
			BarsAvailable: n,
			Reason:        "cold_start_assume_stable",
		}
	}
	window := values
	if cfg.Lookback > 0 && len(values) > cfg.Lookback {
		window = values[len(values)-cfg.Lookback:]
	}
	res := ComputeDirectionalConsistency(window)
	stable := res.Consistency >= cfg.MinConsistency
	reason := ""
	if !stable {
		reason = "consistency_below_threshold"
	}
	return FeatureStabilityResult{
		Stable:        stable,
		FeatureName:   name,
		Consistency:   res.Consistency,
		BarsAvailable: n,
		IsStale:       res.IsZeroChange,
		Reason:        reason,
	}
}

// RedistributeWeights zeroes the weight of unstable features and redistributes
// the freed mass proportionally across the stable features. The total weight
// sum is preserved (within floating-point error). The original map is not
// mutated.
func RedistributeWeights(
	original map[string]float64,
	stable map[string]bool,
) map[string]float64 {
	out := make(map[string]float64, len(original))
	if len(original) == 0 {
		return out
	}
	var freed, stableSum float64
	for name, w := range original {
		if stable[name] {
			stableSum += w
		} else {
			freed += w
		}
	}
	for name, w := range original {
		if !stable[name] {
			out[name] = 0
			continue
		}
		if stableSum <= 0 {
			out[name] = w
			continue
		}
		out[name] = w + freed*(w/stableSum)
	}
	return out
}
