package data_quality

import (
	"crypto-sniping-bot/contracts"
)

// DevReputationResult is the structured output of the dev-reputation detector.
type DevReputationResult struct {
	Score       float64
	Flags       []string
	Unknown     bool
	UnknownFlag string
}

// DetectDevReputation scores risk from two independent sub-signals:
//
//  1. Serial launcher: if CreatorPrevTokenCountKnown is true and
//     CreatorPrevTokenCount >= maxPrevTokens, risk scores high.
//     A dev with many prior launches and no successful migrations is a
//     strong serial pump-and-dump signal (the $RIBBIT pattern: 29 tokens,
//     0 migrations, 0 golden gems).
//
//  2. No social links: if SocialLinksKnown is true and HasSocialLinks is
//     false, the token has no Twitter/Telegram/website — a weaker signal
//     by itself but meaningful in combination with other risk factors.
//
// When both *Known bits are false the detector returns Unknown=true and
// contributes 0 — the orchestrator profile's UnknownFactor applies.
//
// Parameters:
//
//	maxPrevTokens  — launches at or above this count earn full serial-launcher
//	                 risk (0 disables; canonical default: 5)
//	noSocialRisk   — fixed score contribution when social links are absent
//	                 and known (0 disables; canonical default: 0.4)
//
// Pure function — no RPC, no state.
func DetectDevReputation(
	in contracts.MarketDataDTO,
	maxPrevTokens int32,
	noSocialRisk float64,
) DevReputationResult {
	if maxPrevTokens <= 0 {
		maxPrevTokens = 5
	}
	if noSocialRisk <= 0 {
		noSocialRisk = 0.0
	}

	knownCount := 0
	flags := []string{}
	signals := []float64{}

	// ── Serial-launcher signal ────────────────────────────────────────────
	if in.CreatorPrevTokenCountKnown {
		knownCount++
		launchScore := 0.0
		if in.CreatorPrevTokenCount >= maxPrevTokens {
			// Scale: at maxPrevTokens → 0.5; at 2×maxPrevTokens → 1.0 (capped).
			// This prevents a step-function hard penalty while still pushing
			// the aggregate risk score meaningfully above the REJECT threshold.
			ratio := float64(in.CreatorPrevTokenCount) / float64(maxPrevTokens)
			launchScore = clampFloat(0.5*ratio, 0, 1)
			flags = append(flags, "DEV_SERIAL_LAUNCHER")
		}
		signals = append(signals, launchScore)
	}

	// ── No-social-links signal ────────────────────────────────────────────
	if in.SocialLinksKnown {
		knownCount++
		socialScore := 0.0
		if !in.HasSocialLinks {
			socialScore = noSocialRisk
			flags = append(flags, "DEV_NO_SOCIAL_LINKS")
		}
		signals = append(signals, socialScore)
	}

	if knownCount == 0 {
		return DevReputationResult{Unknown: true, UnknownFlag: "dq_unknown_dev_reputation"}
	}

	// Average across known signals.
	total := 0.0
	for _, s := range signals {
		total += s
	}
	score := 0.0
	if len(signals) > 0 {
		score = total / float64(len(signals))
	}

	return DevReputationResult{
		Score: clampFloat(score, 0, 1),
		Flags: flags,
	}
}
