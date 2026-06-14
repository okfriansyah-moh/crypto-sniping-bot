package edge

// NewPoolGate enforces the time/liquidity guard for the NEW_LAUNCH_EDGE
// pattern.  Pools younger than minAgeSeconds OR thinner than minLiquidityUsd
// are rejected to prevent sniping ghost pools.
func NewPoolGate(poolAgeSeconds int32, liquidityUsd float64, minAgeSeconds int32, minLiquidityUsd float64) bool {
	if poolAgeSeconds < minAgeSeconds {
		return false
	}
	if liquidityUsd < minLiquidityUsd {
		return false
	}
	return true
}
