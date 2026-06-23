package operator

import (
	"context"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
)

// BuildProbePendingStats maps adapter queue metrics to the dashboard DTO.
func BuildProbePendingStats(ctx context.Context, db database.Adapter) (*contracts.ProbePendingStatsDTO, error) {
	stats, err := db.GetProbePendingStats(ctx)
	if err != nil {
		return nil, err
	}
	if stats == nil {
		return &contracts.ProbePendingStatsDTO{}, nil
	}
	return &contracts.ProbePendingStatsDTO{
		PendingCount: stats.PendingCount,
		DueNow:       stats.DueNow,
		Expired24h:   stats.Expired24h,
		Deferred24h:  stats.Deferred24h,
	}, nil
}
