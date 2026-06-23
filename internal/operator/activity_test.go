package operator_test

import (
	"context"
	"testing"

	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/operator"
)

func TestBuildActivityFeed_ChainFilterAndSummary(t *testing.T) {
	t.Parallel()
	stub := &overviewStubDB{
		recent: []database.RecentEventRow{
			{
				EventID:      "e1",
				EventType:    "position_opened",
				Chain:        "solana",
				TokenAddress: "So11111111111111111111111111111111111111112",
				TraceID:      "t1",
				CreatedAt:    "2026-06-13T12:04:18Z",
			},
			{
				EventID:      "e2",
				EventType:    "dq_decision",
				Chain:        "eth",
				TokenAddress: "0xabc",
				CreatedAt:    "2026-06-13T12:03:55Z",
			},
		},
	}

	got, err := operator.BuildActivityFeed(context.Background(), stub, "solana", 50)
	if err != nil {
		t.Fatalf("BuildActivityFeed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1", len(got))
	}
	if got[0].Summary == "" || got[0].EventType != "position_opened" {
		t.Fatalf("unexpected row: %+v", got[0])
	}
}
