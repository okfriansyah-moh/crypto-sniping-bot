package features

// HolderDistributionScore returns a [0,1] score based on holder count.
// Higher = more decentralised distribution (lower concentration risk).
//
// Phase 4 stub: a saturating curve over the raw holder count, since per-holder
// balance data is not yet available in MarketDataDTO.  Real Gini-coefficient
// computation requires on-chain enrichment (deferred to Phase 5).
//
//	holders <= 1   → 0.0   (single holder = creator, max risk)
//	holders == 50  → ~0.5
//	holders >= 500 → ~1.0  (well-distributed)
func HolderDistributionScore(holderCount int64) float64 {
	if holderCount <= 1 {
		return 0.0
	}
	const k = 100.0
	score := float64(holderCount) / (float64(holderCount) + k)
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}
