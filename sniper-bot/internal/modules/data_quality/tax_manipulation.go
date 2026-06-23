package data_quality

import (
	"sort"

	"crypto-sniping-bot/shared/contracts"
)

// TaxResult is the structured output of the tax-manipulation detector.
type TaxResult struct {
	Score       float64
	Flags       []string
	Unknown     bool
	UnknownFlag string
}

// DetectTaxManipulation evaluates buy/sell tax magnitude, divergence between
// initial and current tax, dynamic-tax markers, and presence of a blacklist
// function on the contract. Pure function over MarketDataDTO.
//
// Sub-signals (each in [0,1], averaged across signals present):
//
//   - excessive_tax  : (buy+sell) / totalCapBps, clamped.
//   - tax_divergence : 1 if current != initial tax (proxy for upgrade trap).
//   - dynamic_tax    : 1 if TaxIsDynamic, else 0.
//   - blacklist      : 1 if BlacklistFunctionPresent, else 0.
//
// Single-side gates: any side > maxSideBps appends `EXCESSIVE_TAX`.
func DetectTaxManipulation(
	in contracts.MarketDataDTO,
	maxBuyBps, maxSellBps, totalCapBps int32,
) TaxResult {
	if !in.TaxKnown {
		return TaxResult{Unknown: true, UnknownFlag: "dq_unknown_tax"}
	}

	if maxBuyBps <= 0 {
		maxBuyBps = 800
	}
	if maxSellBps <= 0 {
		maxSellBps = 1000
	}
	if totalCapBps <= 0 {
		totalCapBps = maxBuyBps + maxSellBps
	}

	flags := []string{}
	signals := []float64{}

	if in.BuyTaxBps > maxBuyBps || in.SellTaxBps > maxSellBps {
		flags = append(flags, "EXCESSIVE_TAX")
	}

	totalRatio := float64(in.BuyTaxBps+in.SellTaxBps) / float64(totalCapBps)
	if totalRatio < 0 {
		totalRatio = 0
	}
	if totalRatio > 1 {
		totalRatio = 1
	}
	signals = append(signals, totalRatio)

	if in.InitialBuyTaxBps != 0 || in.InitialSellTaxBps != 0 {
		divergence := 0.0
		if in.InitialBuyTaxBps != 0 && in.InitialBuyTaxBps != in.BuyTaxBps {
			divergence = 1.0
			flags = append(flags, "TAX_CHANGED")
		}
		if in.InitialSellTaxBps != 0 && in.InitialSellTaxBps != in.SellTaxBps {
			divergence = 1.0
			flags = append(flags, "TAX_CHANGED")
		}
		signals = append(signals, divergence)
	}

	if in.TaxIsDynamic {
		signals = append(signals, 1.0)
		flags = append(flags, "DYNAMIC_TAX")
	}

	if in.BlacklistFunctionPresent {
		signals = append(signals, 1.0)
		flags = append(flags, "BLACKLIST_FN")
	}

	score := 0.0
	for _, s := range signals {
		score += s
	}
	if len(signals) > 0 {
		score /= float64(len(signals))
	}
	if score > 1 {
		score = 1
	}

	// Deduplicate (TAX_CHANGED can be added twice).
	flags = dedupSorted(flags)
	sort.Strings(flags)
	return TaxResult{Score: score, Flags: flags}
}

func dedupSorted(in []string) []string {
	if len(in) <= 1 {
		return in
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
