// Phase 9 (Profitability Restoration § 9.4) — mode multiplier lookup.
package capital

import "crypto-sniping-bot/internal/app/config"

// ModeMultiplier returns the size multiplier for the active operational
// mode. Falls back to 1.0 (BALANCED) when:
//   - mode is empty / unrecognized
//   - configured map is empty
//   - cfg.FailurePolicy.OnModeLookupStale != "reject"
//
// Caller is responsible for honoring "reject" failure policy.
func ModeMultiplier(mode string, cfg *config.CapitalConfig) (mult float64, fallbackUsed bool) {
	if cfg == nil || len(cfg.ModeMultipliers) == 0 {
		return 1.0, true
	}
	if v, ok := cfg.ModeMultipliers[mode]; ok && v > 0 {
		return v, false
	}
	if v, ok := cfg.ModeMultipliers["BALANCED"]; ok && v > 0 {
		return v, true
	}
	return 1.0, true
}
