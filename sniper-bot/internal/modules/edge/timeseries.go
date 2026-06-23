// Package edge — 20-slot time-series bottom detection (P7).
//
// AnalyzeBottom scores the likelihood that the current price is at or
// recovering from a local bottom in the observed slot window.  A high
// score (≥0.7) supports entry because the price appears to have found
// a floor; a low score warns against catching a falling knife.
//
// Algorithm:
//  1. Trim the slot window to at most maxSlots observations.
//  2. Locate the price trough (global minimum within the window).
//  3. Require the trough to be at a slot BEFORE the last one (recovery
//     must have started — not still declining).
//  4. Compute three sub-scores:
//     depth_score     — how deep the descent was (deeper = stronger signal)
//     recovery_score  — what fraction of the trough drop was recovered
//     position_score  — how early the trough occurred (earlier = better)
//  5. Weighted-average the three sub-scores → BottomDetectionScore [0,1].
//
// Design invariants:
//   - Deterministic: same slots → same output.
//   - No external I/O or clocks.
//   - Returns score=0 (no signal) when data is insufficient (< MinSlots).
package edge

const (
	// bottomMinSlots is the minimum number of price observations required
	// to compute a meaningful bottom signal.
	bottomMinSlots = 3

	// bottomDefaultMaxSlots is the default window size when the caller
	// passes maxSlots ≤ 0.
	bottomDefaultMaxSlots = 20
)

// PriceSlot is a single price observation in the time-series window.
// Slots must be ordered oldest-first (ascending timestamp).
type PriceSlot struct {
	// PriceUsd is the token price in USD at this observation.
	// Must be > 0 to be considered valid.
	PriceUsd float64

	// SlotIndex is an optional monotonic index (0-based, oldest=0).
	// Used for deterministic ordering when price data arrives out-of-order.
	// Zero value is acceptable when slots are already sorted.
	SlotIndex int
}

// BottomSignal is the output of AnalyzeBottom.
type BottomSignal struct {
	// BottomDetectionScore is the composite score in [0, 1].
	// 0   = no bottom pattern found (insufficient data or still descending).
	// 1   = strong V-shape recovery detected.
	BottomDetectionScore float64

	// TroughDepthBps is how far the price fell from the window open to the
	// trough, expressed in basis points. e.g. 500 = fell 5%.
	TroughDepthBps int32

	// RecoveryBps is how much of the trough drop was recovered by the last
	// slot, expressed in basis points relative to the trough price.
	// e.g. 200 = recovered 2% from trough.
	RecoveryBps int32

	// SlotsAnalyzed is the number of slots actually used (≤ maxSlots).
	SlotsAnalyzed int
}

// AnalyzeBottom computes the bottom detection signal for the supplied
// price slots.  maxSlots constrains the window; pass ≤ 0 to use the
// default (20).
//
// Returns a BottomSignal with BottomDetectionScore=0 when the data is
// insufficient or no bottom pattern is found.  Never returns an error —
// callers may safely ignore the score when it is 0.
func AnalyzeBottom(slots []PriceSlot, maxSlots int) BottomSignal {
	if maxSlots <= 0 {
		maxSlots = bottomDefaultMaxSlots
	}

	// Trim to the most recent maxSlots observations (keep the tail).
	if len(slots) > maxSlots {
		slots = slots[len(slots)-maxSlots:]
	}

	n := len(slots)
	if n < bottomMinSlots {
		return BottomSignal{SlotsAnalyzed: n}
	}

	// Find the first valid slot (>0) to seed open/trough deterministically.
	openIdx := -1
	for i := 0; i < n; i++ {
		if slots[i].PriceUsd > 0 {
			openIdx = i
			break
		}
	}
	if openIdx == -1 {
		return BottomSignal{SlotsAnalyzed: n}
	}

	// Locate the trough (global minimum valid price) from the first valid open.
	troughIdx := openIdx
	troughPrice := slots[openIdx].PriceUsd
	for i := openIdx + 1; i < n; i++ {
		if slots[i].PriceUsd > 0 && slots[i].PriceUsd < troughPrice {
			troughPrice = slots[i].PriceUsd
			troughIdx = i
		}
	}

	// Use the latest valid slot as terminal point; trailing invalid values
	// should not suppress a real recovery signal.
	lastValidIdx := -1
	for i := n - 1; i >= openIdx; i-- {
		if slots[i].PriceUsd > 0 {
			lastValidIdx = i
			break
		}
	}
	if lastValidIdx == -1 {
		return BottomSignal{SlotsAnalyzed: n}
	}

	// The trough must not be the last slot — recovery must have started.
	if troughIdx == lastValidIdx {
		return BottomSignal{SlotsAnalyzed: n} // still descending
	}

	openPrice := slots[openIdx].PriceUsd
	lastPrice := slots[lastValidIdx].PriceUsd

	if openPrice <= 0 || troughPrice <= 0 {
		return BottomSignal{SlotsAnalyzed: n}
	}

	// Descent depth: (open - trough) / open, clamped to [0, 1].
	descentPct := (openPrice - troughPrice) / openPrice
	if descentPct < 0 {
		descentPct = 0
	}
	if descentPct > 1 {
		descentPct = 1
	}

	// Recovery: (last - trough) / trough, clamped to [0, 1].
	recoveryPct := 0.0
	if lastPrice > troughPrice {
		recoveryPct = (lastPrice - troughPrice) / troughPrice
		if recoveryPct > 1 {
			recoveryPct = 1
		}
	}

	// Sub-scores in [0, 1].
	// depth_score: a 10% descent achieves full depth score.
	const fullDepthPct = 0.10
	depthScore := descentPct / fullDepthPct
	if depthScore > 1 {
		depthScore = 1
	}

	// recovery_score: a 5% recovery off the trough achieves full score.
	const fullRecoveryPct = 0.05
	recoveryScore := recoveryPct / fullRecoveryPct
	if recoveryScore > 1 {
		recoveryScore = 1
	}

	// position_score: trough occurring earlier in the window is better
	// because it allows more slots for recovery confirmation.
	// troughIdx=0 (trough at start) → 1.0; troughIdx=n-2 (penultimate) → ~0.
	positionScore := 0.0
	if n > 2 {
		positionScore = float64(n-1-troughIdx) / float64(n-1)
	}

	// Weighted average: recovery is the most important signal,
	// depth provides confirmation, position is a mild bonus.
	const (
		wRecovery = 0.50
		wDepth    = 0.30
		wPosition = 0.20
	)
	composite := wRecovery*recoveryScore + wDepth*depthScore + wPosition*positionScore

	// Minimum meaningful recovery required: score stays 0 until at least
	// a small price move off the trough is observed.
	if recoveryPct == 0 {
		composite = 0
	}

	troughDepthBps := int32(descentPct * 10000)
	recoveryBps := int32(recoveryPct * 10000)

	return BottomSignal{
		BottomDetectionScore: composite,
		TroughDepthBps:       troughDepthBps,
		RecoveryBps:          recoveryBps,
		SlotsAnalyzed:        n,
	}
}
