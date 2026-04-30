// Phase 10 (Reference-Repo Improvements / Task C) — adaptive priority fee.
//
// AdaptivePriorityFeeWei scales the RPC-suggested priority fee by a
// latency-error multiplier, bounded by configured min/max multipliers.
//
// The function is pure and deterministic — given the same inputs it
// always returns the same fee. The decision to invoke "static" vs
// "adaptive" mode lives in the caller (the execution worker, which
// reads recent ExecutionResultDTO latencies). This keeps the module
// itself replay-safe.
//
// Inputs:
//
//	suggestedWei   — the wei value returned by EVMClient.GetGasPrice()
//	latencyErrPct  — observed latency error vs target as a fraction;
//	                 0.0 means on-target, +0.5 means 50 % over budget,
//	                 -0.2 means 20 % under (under-target reduces fee).
//	cfg            — bounds and mode toggle.
//
// Returns the recommended priority fee in wei (never nil).

package execution

import (
	"math/big"

	"crypto-sniping-bot/internal/app/config"
)

// AdaptivePriorityFeeWei applies the priority-fee policy to a base wei
// suggestion. When cfg is nil or cfg.PriorityFee.Mode != "adaptive",
// the input is returned unchanged.
func AdaptivePriorityFeeWei(suggestedWei *big.Int, latencyErrPct float64, cfg *config.ExecutionConfig) *big.Int {
	if suggestedWei == nil {
		return big.NewInt(0)
	}
	if cfg == nil || cfg.PriorityFee.Mode != "adaptive" {
		return new(big.Int).Set(suggestedWei)
	}

	multiplier := 1.0 + latencyErrPct
	minM := cfg.PriorityFee.MinMultiplier
	maxM := cfg.PriorityFee.MaxMultiplier
	if minM > 0 && multiplier < minM {
		multiplier = minM
	}
	if maxM > 0 && multiplier > maxM {
		multiplier = maxM
	}
	if multiplier <= 0 {
		multiplier = 1.0 // never push to zero gas
	}

	// Compute (suggested * multiplier) using big.Float for precision,
	// then convert back to big.Int (truncated). This avoids overflow on
	// large gas prices (gwei → wei is 1e9; a 30 gwei price is well
	// within int64, but defensive math is cheap).
	bf := new(big.Float).SetInt(suggestedWei)
	bf = bf.Mul(bf, big.NewFloat(multiplier))
	out, _ := bf.Int(nil)
	if out == nil || out.Sign() <= 0 {
		return new(big.Int).Set(suggestedWei)
	}
	return out
}
