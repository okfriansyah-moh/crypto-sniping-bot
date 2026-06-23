package database

import (
	"context"

	"crypto-sniping-bot/shared/contracts"
)

// ProbePendingQueueStub provides no-op probe-pending adapter methods.
// Embed this in test doubles when the full database.Adapter is required.
type ProbePendingQueueStub struct{}

func (ProbePendingQueueStub) GetLatestMarketDataForToken(_ context.Context, _, _ string) (*contracts.MarketDataDTO, error) {
	return nil, nil
}
func (ProbePendingQueueStub) EnqueueProbePending(_ context.Context, _ ProbePendingEnqueue) error {
	return nil
}
func (ProbePendingQueueStub) ClaimDueProbePending(_ context.Context, _ int) ([]ProbePendingRow, error) {
	return nil, nil
}
func (ProbePendingQueueStub) CompleteProbePending(_ context.Context, _ string) error { return nil }
func (ProbePendingQueueStub) FailProbePending(_ context.Context, _, _ string, _ int) error {
	return nil
}
func (ProbePendingQueueStub) ExpireStaleProbePending(_ context.Context, _ int) (int64, error) {
	return 0, nil
}
func (ProbePendingQueueStub) ExpireStaleProbePendingRows(_ context.Context, _ int) ([]ProbePendingRow, error) {
	return nil, nil
}
func (ProbePendingQueueStub) GetProbePendingStats(_ context.Context) (*ProbePendingStats, error) {
	return &ProbePendingStats{}, nil
}
