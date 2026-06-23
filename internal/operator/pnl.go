package operator

import (
	"context"
	"fmt"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
)

const defaultPnLLookbackHours = 24

// BuildPnLSummary aggregates realized/unrealized PnL for the dashboard PnL view.
// lookbackHours is clamped to [1, 168]. Mirrors cmd/telegram.go buildPnlFn semantics.
func BuildPnLSummary(ctx context.Context, db database.Adapter, lookbackHours int) (*contracts.PnLSummaryDTO, error) {
	lookbackHours = database.CapDQWindowHours(lookbackHours)
	if lookbackHours <= 0 {
		lookbackHours = defaultPnLLookbackHours
	}

	drawdown, err := db.ComputeDrawdown(ctx, lookbackHours)
	if err != nil {
		return nil, fmt.Errorf("compute drawdown: %w", err)
	}

	open, err := db.GetOpenPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("get open positions: %w", err)
	}

	closed, err := db.GetClosedPositions(ctx, lookbackHours*3600)
	if err != nil {
		return nil, fmt.Errorf("get closed positions: %w", err)
	}

	now := time.Now().UTC()
	var openEntryUsd, openUnrealizedUsd float64
	var stuck int32
	for _, p := range open {
		openEntryUsd += p.EntrySizeUsd
		openUnrealizedUsd += unrealizedUsd(p)
		if positionAge(p.OpenedAt, now) > stuckPositionThreshold {
			stuck++
		}
	}

	realized, wins, losses := summarizeClosed(closed)

	return &contracts.PnLSummaryDTO{
		LookbackHours:    lookbackHours,
		RealizedPnLUsd:   realized,
		UnrealizedPnLUsd: openUnrealizedUsd,
		OpenExposureUsd:  openEntryUsd,
		DrawdownPct:      drawdown,
		Wins:             wins,
		Losses:           losses,
		WinRatePct:       winRatePct(wins, losses),
		OpenPositions:    int32(len(open)),
		StuckPositions:   stuck,
	}, nil
}
