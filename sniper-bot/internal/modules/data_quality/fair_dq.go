package data_quality

import (
	"strings"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
)

func isFairExplorationMode(mode string) bool {
	switch strings.ToUpper(strings.TrimSpace(mode)) {
	case "EXPLORATION", "VERY_EXPLORATION":
		return true
	default:
		return false
	}
}

// applyFairUnknownEvaluation converts unknown_* structural rejects into SKIP
// with probe_partial flags for EXPLORATION+ modes.
// Returns skip=true when the token should receive buildSkipResult immediately.
func applyFairUnknownEvaluation(
	mode string,
	in contracts.MarketDataDTO,
	isNewLaunch bool,
	flags []string,
	rejectReasons []string,
	profile config.DataQualityModeProfile,
) (contracts.MarketDataDTO, []string, []string, bool) {
	if !isFairExplorationMode(mode) {
		return in, flags, rejectReasons, false
	}

	var partialFlags []string
	var confirmed []string

	for _, reason := range rejectReasons {
		switch reason {
		case "unknown_social_links":
			partialFlags = append(partialFlags, contracts.FlagProbePartialSocial)
		case "unknown_creator_count":
			partialFlags = append(partialFlags, contracts.FlagProbePartialCreator)
		case "unknown_holder_count":
			if !isNewLaunch {
				partialFlags = append(partialFlags, contracts.FlagProbePartialHolder)
			} else {
				confirmed = append(confirmed, reason)
			}
		case "unknown_total_supply":
			partialFlags = append(partialFlags, contracts.FlagProbePartialSupply)
		default:
			confirmed = append(confirmed, reason)
		}
	}

	if len(partialFlags) == 0 {
		return in, flags, rejectReasons, false
	}

	flags = append(flags, partialFlags...)

	// Confirmed disqualifiers still REJECT — fair path only applies to unknown-only cases.
	if len(confirmed) > 0 {
		return in, flags, confirmed, false
	}

	if shouldProbePartialRiskyPass(in, flags, profile) {
		flags = append(flags, contracts.FlagProbePartialMonitored)
		return in, flags, nil, false
	}

	return in, flags, nil, true
}

// applyNoSocialMonitoring removes no_social_links reject when monitoring applies.
func applyNoSocialMonitoring(
	mode string,
	in contracts.MarketDataDTO,
	rejectReasons []string,
	flags []string,
	mon config.NoSocialMonitoringConfig,
	thresholds config.DataQualityDetectorThresholds,
) ([]string, []string) {
	if !mon.Enabled || !isFairExplorationMode(mode) {
		return rejectReasons, flags
	}
	if !in.SocialLinksKnown || in.HasSocialLinks {
		return rejectReasons, flags
	}
	if !mandatoryKnownExceptSocial(in, mon, thresholds) {
		return rejectReasons, flags
	}

	var remaining []string
	for _, r := range rejectReasons {
		if r != "no_social_links" {
			remaining = append(remaining, r)
		}
	}
	flags = append(flags, contracts.FlagNoSocialMonitored)
	return remaining, flags
}

func mandatoryKnownExceptSocial(
	in contracts.MarketDataDTO,
	mon config.NoSocialMonitoringConfig,
	thresholds config.DataQualityDetectorThresholds,
) bool {
	if !mon.RequireAllOtherMandatoryKnown {
		return true
	}
	maxSupply := mon.MaxTotalSupply
	if maxSupply <= 0 {
		maxSupply = thresholds.MaxTotalSupply
	}
	minHolders := mon.MinHolderCount
	if minHolders <= 0 {
		minHolders = thresholds.MinHolderCount
	}
	if in.CreatorAddress != "" && !in.CreatorPrevTokenCountKnown {
		return false
	}
	if maxSupply > 0 && !in.TotalSupplyKnown {
		return false
	}
	if minHolders > 0 && in.EventTopic != "PumpFunCreate" && !in.HolderDistKnown {
		return false
	}
	return true
}

func shouldProbePartialRiskyPass(in contracts.MarketDataDTO, flags []string, profile config.DataQualityModeProfile) bool {
	_ = profile
	if containsString(flags, contracts.FlagProbePartialMonitored) {
		return false
	}
	if in.LpStatsKnown && in.LiquidityUsd >= 5000 {
		return true
	}
	if in.SolanaAuthoritiesKnown && in.MintAuthorityRenounced && in.FreezeAuthorityRenounced {
		return true
	}
	return false
}
