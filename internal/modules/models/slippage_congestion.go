package models

// Phase 11 (Reference-Repo Improvements R2 — P/S/L MODELS) — congestion-
// aware slippage uplift. Adapted from mux/solana-trading-bot's pattern
// of scaling expected slippage by an RPC-latency excess factor.
//
// Algorithm (pure, deterministic):
//
//   excess  = max(0, (latencyP95Ms - anchorMs)) / max(anchorMs, 1)
//   factor  = 1 + excess                    // 1.0 = no extra slippage
//   factor  = clamp(factor, 1.0, maxFactor) // hard cap (e.g. 2.0)
//   p95Out  = round(p95In * factor)
//
// The function never DECREASES the input p95: factor is always ≥ 1.
// When the feature is disabled (anchor or maxFactor ≤ 0) we return the
// input unchanged.
//
// Bounded ≤ maxFactor per the Phase 11 learning-safety rule: a single
// adjustment never inflates slippage by more than maxFactor× anchor.

import "math"

// ApplyCongestion scales a base p95 slippage (bps) by a latency-derived
// factor. Returns (newBps, factor). factor is also recorded on
// SlippageEstimateDTO.CongestionMultiplier for replay.
func ApplyCongestion(baseP95Bps int32, latencyP95Ms int32, anchorMs int32, maxFactor float64) (int32, float64) {
	if anchorMs <= 0 || maxFactor <= 1.0 {
		return baseP95Bps, 1.0
	}
	excess := float64(latencyP95Ms-anchorMs) / float64(anchorMs)
	if excess < 0 {
		excess = 0
	}
	factor := 1.0 + excess
	if factor > maxFactor {
		factor = maxFactor
	}
	if factor < 1.0 {
		factor = 1.0
	}
	return int32(math.Round(float64(baseP95Bps) * factor)), factor
}
