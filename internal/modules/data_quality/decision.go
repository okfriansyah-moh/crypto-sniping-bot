package data_quality

import (
	"strings"

	"crypto-sniping-bot/internal/app/config"
)

// canonicalProfile is the fallback decision band used when YAML omits a
// `mode_profiles.<mode>` entry. Values come from the data-quality-engine
// skill — STRICT / BALANCED / EXPLORATION / VERY_EXPLORATION canonical thresholds.
var canonicalProfile = map[string]config.DataQualityModeProfile{
	"STRICT":           {RejectAbove: 0.30, RiskyPassAbove: 0.15, UnknownFactor: 0.5},
	"BALANCED":         {RejectAbove: 0.50, RiskyPassAbove: 0.25, UnknownFactor: 0.0},
	"EXPLORATION":      {RejectAbove: 0.65, RiskyPassAbove: 0.35, UnknownFactor: 0.0, MinTokenAgeSeconds: -1},
	"VERY_EXPLORATION": {RejectAbove: 0.75, RiskyPassAbove: 0.45, UnknownFactor: 0.0, MinTokenAgeSeconds: -1},
}

// resolveProfile returns the operational-mode profile used to gate the
// final RiskScore. Unknown / empty / DEGRADED / HALTED modes collapse to
// STRICT — the conservative default — never to a more permissive profile.
func resolveProfile(mode string, rt *config.DataQualityRuntimeConfig) (string, config.DataQualityModeProfile) {
	canonical := strings.ToUpper(strings.TrimSpace(mode))
	switch canonical {
	case "STRICT", "BALANCED", "EXPLORATION", "VERY_EXPLORATION":
		// keep as-is
	default:
		// DEGRADED / HALTED / "" → conservative default.
		canonical = "STRICT"
	}

	// YAML override (lower-case keys).
	if rt != nil {
		if p, ok := rt.ModeProfiles[strings.ToLower(canonical)]; ok && (p.RejectAbove > 0 || p.RiskyPassAbove > 0) {
			return canonical, p
		}
	}
	return canonical, canonicalProfile[canonical]
}

// applyDetectorWeight adds a known-detector contribution to the running score.
func applyDetectorWeight(score, weight float64) float64 {
	if weight <= 0 {
		return 0
	}
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score * weight
}

// applyUnknownContribution computes the per-profile risk contribution of a
// `dq_unknown_*` detector. UnknownFactor=0 means "ignore"; >0 means
// "treat unknown as a fraction of full risk for this detector".
func applyUnknownContribution(weight, unknownFactor float64) float64 {
	if weight <= 0 || unknownFactor <= 0 {
		return 0
	}
	return weight * unknownFactor
}

// makeDecision turns an aggregated [0,1] risk score plus hard-reject flags
// into the final Decision label. Hard rejects always win.
func makeDecision(riskScore float64, hardReject bool, prof config.DataQualityModeProfile) string {
	if hardReject {
		return "REJECT"
	}
	if riskScore >= prof.RejectAbove {
		return "REJECT"
	}
	if riskScore >= prof.RiskyPassAbove {
		return "RISKY_PASS"
	}
	return "PASS"
}
