package execution

import (
	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// PickRoute returns the mempool routing path for a trade.
// Returns one of: "public" | "flashbots" | "beaverbuild" | "eden".
//
// Routing rules:
//  1. If alloc.SizeUsd >= cfg.PrivateSizeThresholdUsd → use cfg.PreferredPrivate
//  2. If slippage estimate indicates likely front-run (high p95) → use preferred private
//  3. Otherwise → "public"
func PickRoute(alloc contracts.AllocationDTO, lat contracts.LatencyProfileDTO, cfg config.MEVConfig) string {
	// Rule 1: size threshold.
	if cfg.PrivateSizeThresholdUsd > 0 && alloc.SizeUsd >= cfg.PrivateSizeThresholdUsd {
		return resolvePrivateRoute(cfg.PreferredPrivate)
	}

	// Rule 2: high-latency indicates network congestion / front-run risk.
	// Use latency p95 as a proxy — if p95 > front_run_window_ms, route private.
	if cfg.FrontRunWindowMs > 0 && lat.ExpectedP95Ms > int32(cfg.FrontRunWindowMs) {
		return resolvePrivateRoute(cfg.PreferredPrivate)
	}

	return "public"
}

// resolvePrivateRoute maps the config string to a validated route value.
// Falls back to "flashbots" for unknown values.
func resolvePrivateRoute(preferred string) string {
	switch preferred {
	case "flashbots", "beaverbuild", "eden":
		return preferred
	default:
		return "flashbots"
	}
}

// ComputeSlippageGuard returns the amountOutMin guard as a fraction of expected output.
// amountOutMin = expectedOut * (1 - guardBps / 10000)
// Returns a value in [0.0, 1.0] representing the minimum acceptable output ratio.
func ComputeSlippageGuard(guardBps int32) float64 {
	if guardBps <= 0 {
		return 1.0
	}
	if guardBps >= 10000 {
		return 0.0
	}
	return 1.0 - float64(guardBps)/10000.0
}

// ExceedsSlippageGuard returns true when the estimated P95 slippage exceeds the guard.
// If true, the trade should be rejected pre-submission with Status=rejected, Reason=slippage_guard.
func ExceedsSlippageGuard(estimatedP95Bps, guardBps int32) bool {
	return estimatedP95Bps > guardBps
}
