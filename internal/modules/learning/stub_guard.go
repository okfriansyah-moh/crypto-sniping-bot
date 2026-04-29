// Phase 9 (Profitability Restoration § 9.7) — stub-feature guard.
// Detects FeatureDTO instances where every Layer 2 signal is the legacy
// hardcoded `0.5` placeholder. Recording these as ground truth poisons
// the learning loop with noise, so callers must drop them before they
// reach LearningRecord aggregation.
package learning

import (
	"math"

	"crypto-sniping-bot/contracts"
)

// stubValue is the historical placeholder used by Layer 2 prior to Phase 9
// real-signal wiring. Treated as a sentinel here.
const stubValue = 0.5

// stubEpsilon bounds float-equality comparison to 0.5 (allows for tiny
// drift in serialization round-trips).
const stubEpsilon = 1e-9

// AllStubs reports whether every numeric feature in `f` still equals the
// legacy 0.5 placeholder. Cold-start, partial-feature, or low-confidence
// records are NOT stubs — they have varying values. AllStubs only returns
// true when the *entire* feature vector is the original placeholder.
//
// Used by the learning recorder to skip records that would otherwise
// contaminate cohort statistics and threshold updates.
func AllStubs(f contracts.FeatureDTO) bool {
	checks := []float64{
		f.TxVelocityScore,
		f.WalletEntropy,
		f.VolumeMomentum,
		f.PriceMomentum,
	}
	for _, v := range checks {
		if !isStub(v) {
			return false
		}
	}
	return true
}

func isStub(v float64) bool {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return false
	}
	return math.Abs(v-stubValue) < stubEpsilon
}
