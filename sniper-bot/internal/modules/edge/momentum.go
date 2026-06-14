package edge

// MomentumScore composes price- and volume-momentum into a single [0,1]
// signal.  Both inputs are expected to already be in [0,1].
//
// Weights (Phase 4): volume 0.6, price 0.4 — volume leads price for early
// snipes.
func MomentumScore(priceMomentum, volumeMomentum float64) float64 {
	score := 0.6*volumeMomentum + 0.4*priceMomentum
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}
