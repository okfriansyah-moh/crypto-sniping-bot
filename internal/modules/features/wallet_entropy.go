package features

import "math"

// WalletEntropyScore returns a [0,1] Shannon-entropy-based score for the
// wallet share distribution.  Inputs `shares` are non-negative weights that
// will be L1-normalised internally.  Higher entropy = more uniform =
// healthier distribution.
//
// Returns 0 when fewer than 2 wallets are provided, or when all weight is
// concentrated in a single wallet.  Returns 1 for a perfectly uniform
// distribution.
func WalletEntropyScore(shares []float64) float64 {
	if len(shares) < 2 {
		return 0
	}
	var total float64
	for _, s := range shares {
		if s > 0 {
			total += s
		}
	}
	if total <= 0 {
		return 0
	}
	var h float64
	for _, s := range shares {
		if s <= 0 {
			continue
		}
		p := s / total
		h -= p * math.Log2(p)
	}
	hMax := math.Log2(float64(len(shares)))
	if hMax <= 0 {
		return 0
	}
	score := h / hMax
	if score < 0 {
		return 0
	}
	if score > 1 {
		return 1
	}
	return score
}
