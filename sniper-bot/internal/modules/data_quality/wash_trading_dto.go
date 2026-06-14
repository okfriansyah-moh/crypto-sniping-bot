package data_quality

import (
	"sort"

	"crypto-sniping-bot/shared/contracts"
)

// WashTradingResult is the structured output of the wash-trading detector.
type WashTradingResult struct {
	Score       float64
	Flags       []string
	Unknown     bool
	UnknownFlag string
}

// DetectWashTradingDTO computes wash-trading risk from upstream telemetry
// fields on MarketDataDTO. Pure function — no state, no RPC.
//
// Sub-signals (each in [0,1], averaged across signals present):
//
//   - tx_to_wallet_ratio : TxCount1m / max(UniqueWallets1m, 1) normalized
//     against a configured cap. High ratios indicate few wallets producing
//     many trades.
//   - low_uniqueness     : 1 if UniqueWallets1m < minUniqueWallets, else 0.
//   - repeat_ratio       : RepeatRatio1m clamped to [0,1].
//   - low_entropy        : 1 - (WalletEntropy / maxExpectedEntropy), clamped.
//
// The legacy DetectWashTrading(volume, holders, age) heuristic remains
// available for callers passing pre-enriched primitives.
func DetectWashTradingDTO(
	in contracts.MarketDataDTO,
	minUniqueWallets int32,
	maxTxPerWalletRatio float64,
	maxExpectedEntropy float64,
	maxRepeatRatio float64,
) WashTradingResult {
	if !in.WashStatsKnown {
		return WashTradingResult{Unknown: true, UnknownFlag: "dq_unknown_wash"}
	}

	if minUniqueWallets <= 0 {
		minUniqueWallets = 5
	}
	if maxTxPerWalletRatio <= 0 {
		maxTxPerWalletRatio = 10.0
	}
	if maxExpectedEntropy <= 0 {
		maxExpectedEntropy = 4.0
	}
	if maxRepeatRatio <= 0 {
		maxRepeatRatio = 0.5
	}

	flags := []string{}

	wallets := in.UniqueWallets1m
	if wallets <= 0 {
		wallets = 1
	}
	ratio := float64(in.TxCount1m) / float64(wallets)
	ratioNorm := ratio / maxTxPerWalletRatio
	if ratioNorm < 0 {
		ratioNorm = 0
	}
	if ratioNorm > 1 {
		ratioNorm = 1
	}

	lowUniqueness := 0.0
	if in.UniqueWallets1m > 0 && in.UniqueWallets1m < minUniqueWallets {
		lowUniqueness = 1.0
		flags = append(flags, "WASH_LOW_UNIQUENESS")
	}

	repeatRatio := in.RepeatRatio1m
	if repeatRatio < 0 {
		repeatRatio = 0
	}
	if repeatRatio > 1 {
		repeatRatio = 1
	}
	if in.RepeatRatio1m > maxRepeatRatio {
		flags = append(flags, "WASH_LOOP_TRADES")
	}

	lowEntropy := 0.0
	if in.WalletEntropy >= 0 {
		lowEntropy = 1.0 - (in.WalletEntropy / maxExpectedEntropy)
		if lowEntropy < 0 {
			lowEntropy = 0
		}
		if lowEntropy > 1 {
			lowEntropy = 1
		}
		if in.WalletEntropy < 1.5 {
			flags = append(flags, "WASH_LOW_ENTROPY")
		}
	}

	score := (ratioNorm + lowUniqueness + repeatRatio + lowEntropy) / 4.0
	if score > 1 {
		score = 1
	}

	sort.Strings(flags)
	return WashTradingResult{Score: score, Flags: flags}
}
