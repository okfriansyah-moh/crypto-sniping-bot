package data_quality

import "crypto-sniping-bot/internal/app/config"

// Default per-detector risk-score contributions — used only when no
// runtime weights are configured. Mirrors the legacy hardcoded values.
var defaultRiskWeights = config.DataQualityRiskWeights{
	Honeypot:           0.40,
	TaxAnomaly:         0.10,
	RugAuthority:       0.20,
	LpLockMissing:      0.00,
	WashTrading:        0.15,
	ContractUnverified: 0.00,
}

// AggregateRiskScore combines structured detector flags into a [0,1] risk
// score using per-detector weights from `weights` (mirrors
// config/data_quality.yaml `risk_weights`). When weights is nil or all-zero,
// falls back to defaultRiskWeights so existing callers keep their semantics.
func AggregateRiskScore(
	rejectCount int,
	totalChecks int,
	isHoneypot, isFakeLiquidity, isWashTrading, isRugRisk, isTaxAnomaly bool,
	weights *config.DataQualityRiskWeights,
) float64 {
	w := defaultRiskWeights
	if weights != nil && !isZeroWeights(*weights) {
		w = *weights
	}

	base := 0.0
	if totalChecks > 0 {
		base = float64(rejectCount) / float64(totalChecks)
	}
	if isHoneypot {
		base += w.Honeypot
	}
	if isFakeLiquidity {
		// FakeLiquidity is not a separate weight in YAML; treat as a
		// fixed structural risk contribution to preserve prior behavior.
		base += 0.20
	}
	if isWashTrading {
		base += w.WashTrading
	}
	if isRugRisk {
		base += w.RugAuthority
	}
	if isTaxAnomaly {
		base += w.TaxAnomaly
	}
	if base < 0 {
		return 0
	}
	if base > 1 {
		return 1
	}
	return base
}

func isZeroWeights(w config.DataQualityRiskWeights) bool {
	return w.Honeypot == 0 && w.TaxAnomaly == 0 && w.RugAuthority == 0 &&
		w.LpLockMissing == 0 && w.WashTrading == 0 && w.ContractUnverified == 0
}
