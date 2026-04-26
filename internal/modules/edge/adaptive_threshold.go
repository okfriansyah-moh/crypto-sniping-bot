package edge

// AdaptiveThreshold scales the base liquidity threshold up when network
// congestion is high (less reliable signals) and down when low.
//
//	congestion ∈ [0,1]   (0 = idle, 1 = max)
//	scaleFactor = 1 + adjustFactor * (congestion - 0.5) * 2
//
// adjustFactor = 0.2 by default produces a threshold range of ±20% around base.
func AdaptiveThreshold(baseThreshold, congestion, adjustFactor float64) float64 {
	if congestion < 0 {
		congestion = 0
	}
	if congestion > 1 {
		congestion = 1
	}
	scale := 1 + adjustFactor*(congestion-0.5)*2
	out := baseThreshold * scale
	if out < 0 {
		return 0
	}
	if out > 1 {
		return 1
	}
	return out
}
