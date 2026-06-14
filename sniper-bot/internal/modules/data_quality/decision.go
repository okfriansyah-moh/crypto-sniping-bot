package data_quality

import (
	"strings"

	"crypto-sniping-bot/internal/app/config"
)

// canonicalProfile is the fallback decision band used when YAML omits a
// `mode_profiles.<mode>` entry. Values must exactly match config/data_quality.yaml
// defaults (Tasks 12 + 14). Updated to include per-mode serial-launcher override
// fields (Phase 3 — Section 9 Step 4).
//
// STRICT and BALANCED: MaxCreatorPrevTokenCount=0 (sentinel → use global=1);
// serial-launcher hard-reject behaviour is UNCHANGED for those modes.
// EXPLORATION: up to 5 prior launches, quality gates: social=true, max_risk=0.40, min_holders=50.
// VERY_EXPLORATION: up to 10 prior launches, quality gates: social=true, max_risk=0.45, min_holders=25.
var canonicalProfile = map[string]config.DataQualityModeProfile{
	"STRICT": {
		RejectAbove:    0.30,
		RiskyPassAbove: 0.15,
		UnknownFactor:  0.5,
		// Serial-launcher sentinel: 0 = use global threshold (1) → hard REJECT unchanged.
		MaxCreatorPrevTokenCount:          0,
		SerialLauncherRequiresSocialLinks: false,
		SerialLauncherMaxRiskScore:        0.0,
		SerialLauncherMinHolderCount:      0,
	},
	"BALANCED": {
		RejectAbove:    0.50,
		RiskyPassAbove: 0.25,
		UnknownFactor:  0.0,
		// Serial-launcher sentinel: 0 = use global threshold (1) → hard REJECT unchanged.
		MaxCreatorPrevTokenCount:          0,
		SerialLauncherRequiresSocialLinks: false,
		SerialLauncherMaxRiskScore:        0.0,
		SerialLauncherMinHolderCount:      0,
	},
	"EXPLORATION": {
		RejectAbove:        0.65,
		RiskyPassAbove:     0.35,
		UnknownFactor:      0.0,
		MinTokenAgeSeconds: -1,
		// Serial-launcher conditional path: RISKY_PASS when all quality gates pass,
		// SKIP when any gate fails. Up to 5 prior launches allowed.
		MaxCreatorPrevTokenCount:          5,
		SerialLauncherRequiresSocialLinks: true,
		SerialLauncherMaxRiskScore:        0.40,
		SerialLauncherMinHolderCount:      50,
	},
	"VERY_EXPLORATION": {
		RejectAbove:        0.75,
		RiskyPassAbove:     0.45,
		UnknownFactor:      0.0,
		MinTokenAgeSeconds: -1,
		// Serial-launcher conditional path: same as EXPLORATION with looser gates.
		// Up to 10 prior launches allowed.
		MaxCreatorPrevTokenCount:          10,
		SerialLauncherRequiresSocialLinks: true,
		SerialLauncherMaxRiskScore:        0.45,
		SerialLauncherMinHolderCount:      25,
	},
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
