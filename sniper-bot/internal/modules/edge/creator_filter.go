package edge

// Phase 11 (Reference-Repo Improvements R2 — DETECT/EDGE) — pure
// creator-/dev-wallet filters that reject EdgeDTOs with high a-priori
// rug risk.
//
// Heuristics (sourced from m8s-lab, AxisBot, mux):
//   * Dev-buy concentration: creator wallets that buy ≥50 % of supply
//     in the launch window are nearly always rug operators.
//   * Repeat creators: a wallet with prior confirmed rugs has ≥80 %
//     probability of rugging the next launch.
//   * Brand-new dev wallets (age < N seconds since first activity) have
//     no reputation; combine with low-liquidity to reject.
//
// All thresholds come from EdgeConfig (config/pipeline.yaml). Zero
// values disable the filter. The functions are pure: they do not mutate
// the input DTO; the caller (edge.Process) replaces the rejected
// EdgeDTO with EdgeStrength=0 + a reject reason.

import "crypto-sniping-bot/shared/contracts"

// CreatorFilterReason reports the first matching rejection. Empty
// string means "no rejection".
const (
	RejectReasonDevBuyTooHigh    = "creator_dev_buy_too_high"
	RejectReasonCreatorRugRepeat = "creator_rug_repeat"
	RejectReasonDevWalletTooNew  = "dev_wallet_too_new"
)

// CreatorFilterThresholds mirrors the relevant EdgeConfig fields. Kept
// as a small struct so tests don't need full EdgeConfig.
type CreatorFilterThresholds struct {
	MaxDevBuyPctBps        int32
	MaxCreatorRugCount     int32
	MinDevWalletAgeSeconds int64
}

// EvaluateCreatorFilters runs all three creator/dev-wallet checks and
// returns the first matching reason, or "" when the EdgeDTO passes.
// Disabled checks (threshold==0) are skipped.
func EvaluateCreatorFilters(edge contracts.EdgeDTO, t CreatorFilterThresholds) string {
	if t.MaxDevBuyPctBps > 0 && edge.DevBuyPctBps > t.MaxDevBuyPctBps {
		return RejectReasonDevBuyTooHigh
	}
	if t.MaxCreatorRugCount > 0 && edge.CreatorRugCount >= t.MaxCreatorRugCount {
		return RejectReasonCreatorRugRepeat
	}
	if t.MinDevWalletAgeSeconds > 0 &&
		edge.DevWalletAgeSeconds > 0 && // 0 = unknown, do not reject
		edge.DevWalletAgeSeconds < t.MinDevWalletAgeSeconds {
		return RejectReasonDevWalletTooNew
	}
	return ""
}
