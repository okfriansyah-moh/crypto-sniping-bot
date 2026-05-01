package data_quality

import "crypto-sniping-bot/contracts"

// HoneypotResult is the structured output of the honeypot detector.
type HoneypotResult struct {
	Score       float64  // [0,1]
	Flags       []string // diagnostic codes
	HardReject  bool     // SELL_BLOCKED → forced REJECT regardless of score
	Unknown     bool     // upstream simulation result missing
	UnknownFlag string   // "dq_unknown_honeypot" when Unknown
}

// Hard-reject sentinel flags. Their presence forces Decision = REJECT
// regardless of the aggregated RiskScore (per the data-quality-engine skill:
// "A token you cannot sell is a guaranteed loss").
const (
	FlagHoneypotSellFail = "HONEYPOT_SELL_FAIL"
	FlagSellBlocked      = "SELL_BLOCKED"
	FlagHoneypotBuyFail  = "HONEYPOT_BUY_FAIL"
)

// DetectHoneypot evaluates buy/sell simulation outcomes and effective tax
// from the upstream MarketDataDTO. Pure function — no RPC, no state.
//
// Inputs (from MarketDataDTO):
//   - HoneypotSimKnown:  whether upstream populated BuySimSuccess / SellSimSuccess
//   - BuySimSuccess:     callStatic buy result (if known)
//   - SellSimSuccess:    callStatic sell result (if known)
//
// Outputs:
//   - Sell simulation failed             → score=1.0, hardReject=true (FlagHoneypotSellFail + FlagSellBlocked)
//   - Buy simulation failed              → score=1.0, hardReject=true (FlagHoneypotBuyFail)
//   - Both succeed                       → score=0
//   - Unknown (HoneypotSimKnown==false)  → score=0, Unknown=true, dq_unknown_honeypot
func DetectHoneypot(in contracts.MarketDataDTO) HoneypotResult {
	if !in.HoneypotSimKnown {
		return HoneypotResult{Unknown: true, UnknownFlag: "dq_unknown_honeypot"}
	}
	if !in.SellSimSuccess {
		return HoneypotResult{
			Score:      1.0,
			Flags:      []string{FlagHoneypotSellFail, FlagSellBlocked},
			HardReject: true,
		}
	}
	if !in.BuySimSuccess {
		return HoneypotResult{
			Score:      1.0,
			Flags:      []string{FlagHoneypotBuyFail},
			HardReject: true,
		}
	}
	return HoneypotResult{}
}
