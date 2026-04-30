package evaluation

// Phase 11 (Reference-Repo Improvements R2 — EVALUATE) — simulated-vs-
// realized execution variance. Adapted from AxisBotV2's pre-/post-trade
// simulation diff: when the orchestrator captures a pre-trade
// getAmountsOut / quote simulation, the post-trade evaluator can
// compute how much execution actually deviated from the model.
//
// Pure function. Inputs are decimal strings to preserve precision; the
// caller (evaluation module) parses them via math/big or shopspring/decimal
// and passes the parsed float64 amounts here. We accept float64 because
// the ratio is dimensionless and we only need basis-point precision.
//
// Algorithm:
//   variance_bps = round( (realized - simulated) / simulated * 10000 )
//
// Sign convention: NEGATIVE = we got LESS than simulated (slippage,
// MEV, rounding); POSITIVE = better than simulated (rare). 0 = match.
//
// Returns (0, false) when simulated <= 0 (cannot divide).

import "math"

// ComputeExecutionVariance returns variance in bps and a bool indicating
// whether the diff was computable.
func ComputeExecutionVariance(simulatedOut float64, realizedOut float64) (int32, bool) {
	if simulatedOut <= 0 {
		return 0, false
	}
	frac := (realizedOut - simulatedOut) / simulatedOut
	bps := math.Round(frac * 10000)
	// Clamp to int32 range defensively.
	if bps > math.MaxInt32 {
		bps = math.MaxInt32
	}
	if bps < math.MinInt32 {
		bps = math.MinInt32
	}
	return int32(bps), true
}
