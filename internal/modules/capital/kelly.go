// Phase 9 (Profitability Restoration § 9.4) — Kelly fraction sizing.
package capital

import (
	"math"

	"crypto-sniping-bot/internal/app/config"
)

// KellyFraction returns the bounded Kelly fraction for a given probability
// estimate `p` and the configured prior gain/loss bps ratio.
//
// f_kelly = (P × R − (1 − P)) / R     where R = priorGainBps / priorLossBps
//
// When p is invalid (NaN/Inf, ≤0, ≥1) or the ratio R is ≤0, the function
// returns 0 (caller responsible for handling).  Negative output means a
// losing edge; the caller may reject when cfg.Kelly.RejectNegative is true.
//
// The result is clamped to cfg.Kelly.Cap (or the mode-specific cap when the
// caller has already substituted it). Callers select the appropriate cap
// before invoking this helper.
func KellyFraction(p float64, k config.CapitalKellyConfig, cap float64) float64 {
	if math.IsNaN(p) || math.IsInf(p, 0) || p <= 0 || p >= 1 {
		return 0
	}
	if k.PriorLossBps <= 0 {
		return 0
	}
	r := float64(k.PriorGainBps) / float64(k.PriorLossBps)
	if r <= 0 {
		return 0
	}
	f := (p*r - (1 - p)) / r
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0
	}
	if cap > 0 && f > cap {
		f = cap
	}
	return f
}

// KellyCapForMode returns the appropriate Kelly cap for the provided
// operational mode (STRICT, BALANCED, EXPLORATION). Falls back to the
// default Cap on unrecognized modes.
func KellyCapForMode(mode string, k config.CapitalKellyConfig) float64 {
	switch mode {
	case "STRICT":
		if k.CapStrict > 0 {
			return k.CapStrict
		}
	case "EXPLORATION":
		if k.CapExploration > 0 {
			return k.CapExploration
		}
	}
	return k.Cap
}
