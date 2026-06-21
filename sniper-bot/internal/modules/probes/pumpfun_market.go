package probes

import "strings"

// isPumpfunBondingCurveMarket is true for solana-pumpfun (bonding curve), not AMM graduates.
func isPumpfunBondingCurveMarket(market string) bool {
	m := strings.ToLower(strings.TrimSpace(market))
	return m == "solana-pumpfun"
}

// isPumpfunAMMMarket is true for graduated pump.fun AMM pools.
func isPumpfunAMMMarket(market string) bool {
	return strings.EqualFold(strings.TrimSpace(market), "solana-pumpfun-amm")
}

// isPumpfunFamilyMarket matches any pump.fun market (bonding curve or AMM).
func isPumpfunFamilyMarket(market string) bool {
	m := strings.ToLower(strings.TrimSpace(market))
	return strings.HasPrefix(m, "solana-pumpfun")
}
