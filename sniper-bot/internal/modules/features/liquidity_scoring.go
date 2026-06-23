package features

import (
	"math"
	"strings"
)

const (
	virtualSolReserveLamports = 30_000_000_000
	birthLiquidityScoreCap    = 0.55
)

// shouldUseAbsoluteLiquidityScoring returns true for rescan transports and
// graduation events where z-score collapse on identical virtual reserves
// must not block the fortress alpha path.
func shouldUseAbsoluteLiquidityScoring(snap MarketSnapshot, liqUsd, solPriceUsd float64) bool {
	if strings.HasPrefix(snap.Transport, "rescan_") {
		return true
	}
	if snap.EventTopic == "PumpFunAMMCreatePool" {
		return true
	}
	if !isVirtualBirthLiquidity(liqUsd, solPriceUsd) {
		return true
	}
	return false
}

// isVirtualBirthLiquidity reports whether liquidity matches the pump.fun
// virtual 30 SOL anchor (±5%).
func isVirtualBirthLiquidity(liqUsd, solPriceUsd float64) bool {
	if liqUsd <= 0 || solPriceUsd <= 0 {
		return false
	}
	anchor := (float64(virtualSolReserveLamports) / 1e9) * solPriceUsd
	if anchor <= 0 {
		return false
	}
	diff := math.Abs(liqUsd-anchor) / anchor
	return diff <= 0.05
}

// absoluteLiquidityUnitScore maps USD liquidity to [0,1] deterministically.
// Virtual 30 SOL anchor maps to ~0.52 (below min_liquidity_score 0.55).
func absoluteLiquidityUnitScore(liqUsd, solPriceUsd float64) float64 {
	if liqUsd <= 0 {
		return 0
	}
	anchorUsd := 4500.0
	if solPriceUsd > 0 {
		anchorUsd = (float64(virtualSolReserveLamports) / 1e9) * solPriceUsd
	}
	raw := math.Log1p(liqUsd)
	anchorRaw := math.Log1p(anchorUsd)
	if anchorRaw <= 0 {
		return 0
	}
	ratio := raw / anchorRaw
	score := 0.52 + 0.35*(ratio-1.0)
	return clamp(score, 0, 1)
}

// normalizeLiquidityScore applies fortress rules: birth virtual reserves stay
// capped; rescan/graduation use absolute scoring.
func (m *Module) normalizeLiquidityScore(
	snap MarketSnapshot,
	base BaselineSnapshot,
	rawLiquidity float64,
) NormalizedSignal {
	liqUsd := m.derivedLiquidityUsd(snap)
	if shouldUseAbsoluteLiquidityScoring(snap, liqUsd, m.solEstimatedPriceUsd) {
		score := absoluteLiquidityUnitScore(liqUsd, m.solEstimatedPriceUsd)
		return NormalizedSignal{
			Raw:         rawLiquidity,
			ScoreUnit01: score,
			ColdStart:   true,
			N:           0,
		}
	}
	liqNS := NormalizeSignal(rawLiquidity, base.HistoryFor(SignalLiquidity), m.normalizerCfg)
	if isVirtualBirthLiquidity(liqUsd, m.solEstimatedPriceUsd) && liqNS.ScoreUnit01 > birthLiquidityScoreCap {
		liqNS.ScoreUnit01 = birthLiquidityScoreCap
		liqNS.Score = liqNS.ScoreUnit01*2 - 1
	}
	return liqNS
}
