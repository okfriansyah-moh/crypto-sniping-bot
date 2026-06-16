package data_quality

import (
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/shared/contracts"
)

// evaluateMomentumOverride returns true when on-chain momentum signals are strong
// enough to allow the serial-launcher RISKY_PASS path without social links.
func evaluateMomentumOverride(in contracts.MarketDataDTO, cfg config.SerialLauncherMomentumOverride) bool {
	if !cfg.Enabled {
		return false
	}
	if !in.HolderDistKnown || in.HolderCount < cfg.MinHolderCount {
		return false
	}
	if cfg.MinLiquidityUsd > 0 && in.LiquidityUsd < cfg.MinLiquidityUsd {
		return false
	}
	if cfg.MaxTop5HolderPct > 0 && in.Top5HolderPct > cfg.MaxTop5HolderPct {
		return false
	}
	if cfg.RequireAuthoritiesRenounced {
		if !in.SolanaAuthoritiesKnown {
			return false
		}
		if !in.MintAuthorityRenounced || !in.FreezeAuthorityRenounced {
			return false
		}
	}
	if cfg.RequireNarrative {
		if !in.NarrativeKnown || in.NarrativeScore < cfg.MinNarrativeScore {
			return false
		}
	}
	return true
}

// effectiveSerialLauncherProfile applies shadow pipeline proof relaxation when enabled.
func effectiveSerialLauncherProfile(profile config.DataQualityModeProfile) config.DataQualityModeProfile {
	if !profile.ShadowPipelineProof {
		return profile
	}
	relaxed := profile
	relaxed.SerialLauncherRequiresSocialLinks = false
	relaxed.SerialLauncherMinHolderCount = 0
	return relaxed
}
