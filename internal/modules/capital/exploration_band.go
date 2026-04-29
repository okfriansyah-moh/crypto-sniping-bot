// Phase 9 (Profitability Restoration § 9.4) — bounded exploration band.
package capital

import "crypto-sniping-bot/internal/app/config"

// ExplorationBand bounds capital allocation between min/max % of the total
// portfolio when EXPLORATION mode is active and exploration is enabled.
// In other modes (or when disabled), returns the input size unchanged.
//
// Inputs are absolute USD values.  Returns size clamped to
// [min_pct_of_total × portfolio, max_pct_of_total × portfolio] when active.
// portfolioUsd ≤ 0 short-circuits to the unchanged size.
func ExplorationBand(sizeUsd float64, mode string, portfolioUsd float64, cfg *config.CapitalConfig) float64 {
	if cfg == nil || !cfg.Exploration.Enabled || mode != "EXPLORATION" || portfolioUsd <= 0 {
		return sizeUsd
	}
	minPct := cfg.Exploration.MinPctOfTotal
	maxPct := cfg.Exploration.MaxPctOfTotal
	if minPct > 0 {
		floor := minPct * portfolioUsd
		if sizeUsd < floor {
			sizeUsd = floor
		}
	}
	if maxPct > 0 {
		ceil := maxPct * portfolioUsd
		if sizeUsd > ceil {
			sizeUsd = ceil
		}
	}
	return sizeUsd
}
