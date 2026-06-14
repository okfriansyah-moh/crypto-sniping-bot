package features

import "math"

// DriftZScore returns the z-score of `current` relative to a rolling baseline
// (mean, stdDev).  When stdDev is 0 the function returns 0 (no drift signal).
//
// Used as a Phase 4 helper for detecting feature-level distribution drift.
// Higher absolute value = more drift.
func DriftZScore(current, mean, stdDev float64) float64 {
	if stdDev <= 0 || math.IsNaN(stdDev) {
		return 0
	}
	z := (current - mean) / stdDev
	if math.IsNaN(z) || math.IsInf(z, 0) {
		return 0
	}
	return z
}
