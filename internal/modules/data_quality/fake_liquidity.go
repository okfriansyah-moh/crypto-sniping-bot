package data_quality

import (
	"sort"

	"crypto-sniping-bot/contracts"
)

// FakeLiquidityResult is the structured output of the fake-liquidity detector.
type FakeLiquidityResult struct {
	Score       float64
	Flags       []string
	Unknown     bool
	UnknownFlag string
}

// DetectFakeLiquidity scores fake-liquidity risk from already-collected
// telemetry — LP add/remove churn, single-LP concentration, USD depth.
//
// Inputs are pre-aggregated by an upstream worker (this module makes no
// RPC calls). When LpStatsKnown is false, the detector emits
// `dq_unknown_fake_liquidity` and contributes 0 — the orchestrator's
// per-profile UnknownFactor decides how to degrade.
//
// Sub-signals (each clamped to [0,1], averaged across signals present):
//
//	churn          : 1.0 if LpChurnDetected, else 0
//	concentration  : SingleLpProviderPct (already in [0,1])
//	thin_liquidity : 1.0 when LiquidityUsd < minLiquidityUsd, scaled linearly
func DetectFakeLiquidity(in contracts.MarketDataDTO, minLiquidityUsd float64) FakeLiquidityResult {
	if !in.LpStatsKnown {
		return FakeLiquidityResult{Unknown: true, UnknownFlag: "dq_unknown_fake_liquidity"}
	}

	if minLiquidityUsd <= 0 {
		minLiquidityUsd = 5000.0 // EXPLORATION-grade floor from skill
	}

	flags := []string{}
	signals := []float64{}

	// LP churn (add → remove inside the configured window).
	churn := 0.0
	if in.LpChurnDetected {
		churn = 1.0
		flags = append(flags, "LP_CHURN")
	}
	signals = append(signals, churn)

	// Single-LP-provider concentration.
	concentration := in.SingleLpProviderPct
	if concentration < 0 {
		concentration = 0
	}
	if concentration > 1 {
		concentration = 1
	}
	if concentration >= 0.90 {
		flags = append(flags, "SINGLE_LP_PROVIDER")
	}
	signals = append(signals, concentration)

	// Thin-liquidity (USD depth below floor).
	thin := 0.0
	if in.LiquidityUsd > 0 && in.LiquidityUsd < minLiquidityUsd {
		thin = 1.0 - (in.LiquidityUsd / minLiquidityUsd)
		if thin < 0 {
			thin = 0
		}
		if thin > 1 {
			thin = 1
		}
		flags = append(flags, "LOW_LIQUIDITY")
	}
	signals = append(signals, thin)

	score := 0.0
	for _, s := range signals {
		score += s
	}
	if len(signals) > 0 {
		score /= float64(len(signals))
	}
	if score > 1 {
		score = 1
	}

	sort.Strings(flags)
	return FakeLiquidityResult{Score: score, Flags: flags}
}
