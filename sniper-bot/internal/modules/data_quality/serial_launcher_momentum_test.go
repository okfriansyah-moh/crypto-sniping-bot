package data_quality

import (
	"testing"

	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/shared/contracts"
)

func TestEvaluateMomentumOverride_DisabledByDefault(t *testing.T) {
	in := contracts.MarketDataDTO{
		HolderDistKnown:          true,
		HolderCount:              200,
		LiquidityUsd:             20000,
		Top5HolderPct:            0.4,
		SolanaAuthoritiesKnown:     true,
		MintAuthorityRenounced:   true,
		FreezeAuthorityRenounced: true,
	}
	cfg := config.SerialLauncherMomentumOverride{Enabled: false}
	if evaluateMomentumOverride(in, cfg) {
		t.Fatal("expected override false when disabled")
	}
}

func TestEvaluateMomentumOverride_PassesStrongMomentum(t *testing.T) {
	in := contracts.MarketDataDTO{
		HolderDistKnown:          true,
		HolderCount:              150,
		LiquidityUsd:             15000,
		Top5HolderPct:            0.35,
		SolanaAuthoritiesKnown:     true,
		MintAuthorityRenounced:   true,
		FreezeAuthorityRenounced: true,
	}
	cfg := config.SerialLauncherMomentumOverride{
		Enabled:                     true,
		MinHolderCount:              100,
		MinLiquidityUsd:             10000,
		MaxTop5HolderPct:            0.60,
		RequireAuthoritiesRenounced: true,
	}
	if !evaluateMomentumOverride(in, cfg) {
		t.Fatal("expected momentum override pass")
	}
}

func TestEffectiveSerialLauncherProfile_ShadowPipelineProof(t *testing.T) {
	profile := config.DataQualityModeProfile{
		SerialLauncherRequiresSocialLinks: true,
		SerialLauncherMinHolderCount:      25,
		ShadowPipelineProof:               true,
	}
	got := effectiveSerialLauncherProfile(profile, true)
	if got.SerialLauncherRequiresSocialLinks {
		t.Error("shadow_pipeline_proof should disable social links requirement")
	}
	if got.SerialLauncherMinHolderCount != 0 {
		t.Errorf("shadow_pipeline_proof should zero holder floor, got %d", got.SerialLauncherMinHolderCount)
	}
}
