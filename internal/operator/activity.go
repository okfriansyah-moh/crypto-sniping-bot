package operator

import (
	"context"
	"fmt"
	"strings"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
)

// BuildActivityFeed returns recent event bus rows for the dashboard activity view.
func BuildActivityFeed(
	ctx context.Context,
	db database.Adapter,
	chain string,
	limit int,
) ([]contracts.ActivityEventDTO, error) {
	chain = normalizeChainFilter(chain)
	limit = database.CapRecentEventsLimit(limit)

	rows, err := db.ListRecentEvents(ctx, chain, limit)
	if err != nil {
		return nil, fmt.Errorf("list recent events: %w", err)
	}

	out := make([]contracts.ActivityEventDTO, 0, len(rows))
	for _, row := range rows {
		out = append(out, contracts.ActivityEventDTO{
			EventID:      row.EventID,
			EventType:    row.EventType,
			Chain:        row.Chain,
			TokenAddress: row.TokenAddress,
			Summary:      activityEventSummary(row),
			TraceID:      row.TraceID,
			CreatedAt:    row.CreatedAt,
		})
	}
	return out, nil
}

func activityEventSummary(row database.RecentEventRow) string {
	label := strings.ReplaceAll(strings.TrimSpace(row.EventType), "_", " ")
	if label == "" {
		label = "event"
	}
	if row.TokenAddress != "" {
		return label + " · " + shortTokenAddress(row.TokenAddress)
	}
	return label
}

func shortTokenAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	if len(addr) <= 12 {
		return addr
	}
	return addr[:4] + "…" + addr[len(addr)-4:]
}
