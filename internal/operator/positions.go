package operator

import (
	"context"
	"fmt"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
)

// BuildPositionRows returns open positions for the dashboard table view.
// chain filters post-query; empty or "all" returns every chain.
func BuildPositionRows(ctx context.Context, db database.Adapter, chain string) ([]contracts.PositionRowDTO, error) {
	chain = normalizeChainFilter(chain)

	open, err := db.GetOpenPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("get open positions: %w", err)
	}

	now := time.Now().UTC()
	rows := make([]contracts.PositionRowDTO, 0, len(open))
	for _, p := range open {
		if !chainMatches(chain, p.Chain) {
			continue
		}
		entryUsd, _ := parseDecimal(p.EntryPrice)
		currentUsd, _ := parseDecimal(p.CurrentPrice)
		rows = append(rows, contracts.PositionRowDTO{
			PositionID:        p.PositionID,
			TokenAddress:      p.TokenAddress,
			Chain:             p.Chain,
			Market:            "",
			EntryPriceUsd:     entryUsd,
			CurrentPriceUsd:   currentUsd,
			PnLPct:            unrealizedPct(p),
			SizeUsd:           p.EntrySizeUsd,
			AgeSeconds:        int64(positionAge(p.OpenedAt, now).Seconds()),
			TraceID:           p.TraceID,
			StrategyVersionID: p.VersionID,
		})
	}
	return rows, nil
}

// ListOpenPositions returns open positions with optional post-query chain filter.
// Used by Telegram /positions formatting (full PositionStateDTO fields).
func ListOpenPositions(ctx context.Context, db database.Adapter, chain string) ([]contracts.PositionStateDTO, error) {
	chain = normalizeChainFilter(chain)

	open, err := db.GetOpenPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("get open positions: %w", err)
	}
	if chain == "" {
		return open, nil
	}
	filtered := make([]contracts.PositionStateDTO, 0, len(open))
	for _, p := range open {
		if chainMatches(chain, p.Chain) {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}
