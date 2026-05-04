package data_quality

// DetectWashTrading returns true when the (volume, holder, age) signature
// looks consistent with circular-volume self-trading.
//
// Heuristic (Phase 4):
//   - Volume24h ÷ HolderCount > $5,000 per holder, AND
//   - HolderCount in (0, 50), AND
//   - PoolAge ≤ 1 hour
//
// This is intentionally conservative: real wash detection requires per-trader
// graph analysis (Phase 5+).  Inputs are pre-enriched primitives — this file
// is dependency-free so it can be unit-tested without a contracts.MarketDataDTO.
func DetectWashTrading(volume24hUsd float64, holderCount int32, poolAgeSeconds int32) bool {
	if holderCount <= 0 || holderCount >= 50 {
		return false
	}
	if volume24hUsd <= 0 {
		return false
	}
	perHolder := volume24hUsd / float64(holderCount)
	if perHolder < 5000.0 {
		return false
	}
	if poolAgeSeconds <= 0 || poolAgeSeconds > 3600 {
		return false
	}
	return true
}
