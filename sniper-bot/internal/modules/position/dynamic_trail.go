// Package position — dynamic trailing stop tier calculator (P6).
//
// A DynamicTrailTier maps a profit threshold to a trailing-stop width:
//
//	"once unrealized gain ≥ TriggerBps, trail with a TrailBps-wide floor"
//
// Tiers are evaluated from most-profitable to least-profitable; the
// first matching tier wins.  When no tier matches the current gain the
// caller falls back to the flat TrailingStopBps configured on the position.
//
// Design invariants:
//   - Deterministic: identical gain → identical TrailBps.
//   - No external I/O.
//   - Tier list is sorted descending by TriggerBps on construction.
package position

import (
	"sort"
)

// DynamicTrailTier defines one tier in the dynamic trailing-stop schedule.
type DynamicTrailTier struct {
	// TriggerBps is the minimum unrealized gain (in bps) required to
	// activate this tier.  e.g. 10000 = +100% (2x), 20000 = +200% (3x).
	TriggerBps int32

	// TrailBps is the trailing-stop width (in bps) when this tier is
	// active.  e.g. 2000 = trail 20% below peak.
	TrailBps int32
}

// DynamicTrailCalculator holds a sorted tier list and computes the
// effective TrailBps for a given unrealized gain.
type DynamicTrailCalculator struct {
	// tiers is sorted descending by TriggerBps (highest threshold first).
	tiers []DynamicTrailTier
}

// NewDynamicTrailCalculator creates a DynamicTrailCalculator from a set of
// tiers.  Tiers are sorted internally — callers do not need to pre-sort.
// Tiers with TrailBps ≤ 0 or TriggerBps < 0 are silently dropped.
func NewDynamicTrailCalculator(tiers []DynamicTrailTier) *DynamicTrailCalculator {
	valid := make([]DynamicTrailTier, 0, len(tiers))
	for _, t := range tiers {
		if t.TrailBps > 0 && t.TriggerBps >= 0 {
			valid = append(valid, t)
		}
	}
	// Sort descending: highest profit tier evaluated first.
	sort.Slice(valid, func(i, j int) bool {
		return valid[i].TriggerBps > valid[j].TriggerBps
	})
	return &DynamicTrailCalculator{tiers: valid}
}

// TrailBpsForGain returns the effective trailing-stop width (in bps) for the
// given unrealized gain (in bps).  gainBps may be negative (loss scenario) or
// zero (at entry).
//
// Returns 0 when no tier matches (meaning the caller should fall back to the
// flat TrailingStopBps on the PositionStateDTO or keep the trailing inactive).
func (c *DynamicTrailCalculator) TrailBpsForGain(gainBps int32) int32 {
	for _, t := range c.tiers {
		if gainBps >= t.TriggerBps {
			return t.TrailBps
		}
	}
	return 0
}

// Len returns the number of active tiers.
func (c *DynamicTrailCalculator) Len() int { return len(c.tiers) }
