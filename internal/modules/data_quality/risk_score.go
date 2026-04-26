package data_quality

// AggregateRiskScore combines structured detector flags into a [0,1] risk
// score using fixed weights.  Higher = riskier.
func AggregateRiskScore(
	rejectCount int,
	totalChecks int,
	isHoneypot, isFakeLiquidity, isWashTrading, isRugRisk, isTaxAnomaly bool,
) float64 {
	base := 0.0
	if totalChecks > 0 {
		base = float64(rejectCount) / float64(totalChecks)
	}
	if isHoneypot {
		base += 0.40
	}
	if isFakeLiquidity {
		base += 0.20
	}
	if isWashTrading {
		base += 0.15
	}
	if isRugRisk {
		base += 0.20
	}
	if isTaxAnomaly {
		base += 0.10
	}
	if base < 0 {
		return 0
	}
	if base > 1 {
		return 1
	}
	return base
}
