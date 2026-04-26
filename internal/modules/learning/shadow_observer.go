package learning

import (
	"context"
	"fmt"
	"strconv"
)

// PriceClient is the minimal interface for fetching current token prices.
// Injected at construction time so the module is testable without real RPC.
type PriceClient interface {
	GetTokenPrice(ctx context.Context, tokenAddress, chain string) (string, error)
}

// ShadowObserver computes the return achieved by a rejected token since rejection.
// Returns (observedReturnPct, true, nil) when the observation window has elapsed
// and a price could be fetched. Returns (0, false, nil) when still waiting.
type ShadowObserver struct {
	priceClient PriceClient
	chain       string
}

// NewShadowObserver creates a ShadowObserver for the given chain.
func NewShadowObserver(priceClient PriceClient, chain string) *ShadowObserver {
	return &ShadowObserver{priceClient: priceClient, chain: chain}
}

// Observe fetches the current price for tokenAddress and returns the return
// relative to entryPriceStr (price at rejection time).
// complete=true means the observation window has ended and a price was obtained.
func (o *ShadowObserver) Observe(
	ctx context.Context,
	tokenAddress string,
	rejectionPriceStr string,
) (observedReturnPct float64, complete bool, err error) {
	if o.priceClient == nil {
		return 0, false, nil
	}

	currentPriceStr, err := o.priceClient.GetTokenPrice(ctx, tokenAddress, o.chain)
	if err != nil {
		return 0, false, fmt.Errorf("shadow observer: get price: %w", err)
	}
	if currentPriceStr == "" {
		return 0, false, nil
	}

	currentPrice, err := strconv.ParseFloat(currentPriceStr, 64)
	if err != nil || currentPrice == 0 {
		return 0, false, nil
	}

	rejectionPrice, err := strconv.ParseFloat(rejectionPriceStr, 64)
	if err != nil || rejectionPrice == 0 {
		// No baseline price — treat as complete with 0 return (safe fallback = TN).
		return 0, true, nil
	}

	returnPct := (currentPrice - rejectionPrice) / rejectionPrice
	return returnPct, true, nil
}
