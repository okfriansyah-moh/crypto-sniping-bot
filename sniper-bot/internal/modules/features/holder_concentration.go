package features

// Phase 11 (Reference-Repo Improvements R2 — FEATURES) — pure
// holder-concentration feature extractor. Adapted from hexnome's holder
// distribution sampler.
//
// The extractor accepts a sorted list of holder balances (descending)
// and the total supply, computes top-N concentration in basis points,
// and produces a normalized score where higher concentration = LOWER
// score (concentration is risk).
//
// Pure function. No RPC, no database, no module imports.

import "math"

// ComputeTopNConcentrationBps returns combined top-N supply pct in bps
// (0..10000). Returns 0 when supply <= 0 or holders is empty
// (treated as "unknown"). The caller decides whether to populate the
// FeatureDTO field or leave it zero.
func ComputeTopNConcentrationBps(sortedDescBalances []float64, totalSupply float64, topN int) int32 {
	if totalSupply <= 0 || topN <= 0 || len(sortedDescBalances) == 0 {
		return 0
	}
	n := topN
	if n > len(sortedDescBalances) {
		n = len(sortedDescBalances)
	}
	var sum float64
	for i := 0; i < n; i++ {
		sum += sortedDescBalances[i]
	}
	if sum < 0 {
		sum = 0
	}
	frac := sum / totalSupply
	if frac > 1.0 {
		frac = 1.0
	}
	return int32(math.Round(frac * 10000))
}

// HolderConcentrationScore maps a top-N concentration (in bps) to a
// normalized [0,1] score where 1.0 = perfectly distributed and
// 0.0 = single holder owns everything. Linear in bps.
//
// score = 1 - clamp(top_n_bps / 10000, 0, 1)
//
// Used by the Edge module's feature aggregator.
func HolderConcentrationScore(topNBps int32) float64 {
	if topNBps <= 0 {
		return 0 // unknown
	}
	if topNBps >= 10000 {
		return 0
	}
	return 1.0 - float64(topNBps)/10000.0
}
