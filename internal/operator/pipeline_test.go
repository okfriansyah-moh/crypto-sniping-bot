package operator_test

import (
	"context"
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/operator"
)

func TestBuildPipelineStats_CumulativeFunnel(t *testing.T) {
	t.Parallel()
	stub := &overviewStubDB{
		pipeline: &database.PipelineStats{
			WindowHours:  24,
			Detected:     100,
			DQPassed:     40,
			FeatureReady: 30,
			EdgeDetected: 20,
			Validated:    15,
			Selected:     10,
			Executed:     8,
			PositionOpen: 5,
			Evaluated:    3,
			Rejected:     50,
			Failed:       2,
		},
	}

	got, err := operator.BuildPipelineStats(context.Background(), stub, 24, "")
	if err != nil {
		t.Fatalf("BuildPipelineStats: %v", err)
	}

	f := got.Funnel
	if f.Selected > f.Validated || f.Validated > f.DQPassed || f.DQPassed > f.Detected {
		t.Fatalf("cumulative funnel violated: %+v", f)
	}
	if f.Detected != 100 || f.Evaluated != 3 {
		t.Fatalf("funnel mapping wrong: %+v", f)
	}
	if len(got.LayerHeartbeats) < 9 {
		t.Fatalf("expected >=9 layer rows, got %d", len(got.LayerHeartbeats))
	}
}

func TestBuildPipelineStats_RescanLayerWhenAvailable(t *testing.T) {
	t.Parallel()
	stub := &pipelineStubDB{
		overviewStubDB: overviewStubDB{
			pipeline: &database.PipelineStats{Detected: 10, DQPassed: 1},
		},
		rescan: &database.RescanPipelineStats{Detected: 42},
	}

	got, err := operator.BuildPipelineStats(context.Background(), stub, 24, "")
	if err != nil {
		t.Fatalf("BuildPipelineStats: %v", err)
	}
	if got.LayerHeartbeats[0].Layer != "L0.5" || got.LayerHeartbeats[0].Count24h != 42 {
		t.Fatalf("rescan layer missing: %+v", got.LayerHeartbeats[0])
	}
}

func TestBuildPositionRows_ChainFilter(t *testing.T) {
	t.Parallel()
	stub := &overviewStubDB{
		open: []contracts.PositionStateDTO{
			{PositionID: "p1", Chain: "solana", TokenAddress: "So111", EntrySizeUsd: 5, OpenedAt: "2026-06-13T10:00:00Z", VersionID: "v1", TraceID: "t1"},
			{PositionID: "p2", Chain: "eth", TokenAddress: "0xabc", EntrySizeUsd: 5, OpenedAt: "2026-06-13T10:00:00Z", VersionID: "v2", TraceID: "t2"},
		},
	}

	got, err := operator.BuildPositionRows(context.Background(), stub, "solana")
	if err != nil {
		t.Fatalf("BuildPositionRows: %v", err)
	}
	if len(got) != 1 || got[0].Chain != "solana" {
		t.Fatalf("got %+v, want single solana row", got)
	}
}

func TestBuildDQBreakdown_MapsAdapterDTO(t *testing.T) {
	t.Parallel()
	stub := &overviewStubDB{
		dq: &database.DQBreakdown{
			WindowHours:    24,
			TotalDecisions: 100,
			PassCount:      4,
			RiskyPassCount: 2,
			RejectCount:    80,
			SkipCount:      14,
			PassRatePct:    6,
			TopRejectReasons: []database.DQRejectReasonCount{
				{Reason: "no_social_links", Count: 40},
			},
		},
	}

	got, err := operator.BuildDQBreakdown(context.Background(), stub, 24, "")
	if err != nil {
		t.Fatalf("BuildDQBreakdown: %v", err)
	}
	if got.TotalDecisions != 100 || got.PassRatePct != 6 {
		t.Fatalf("unexpected breakdown: %+v", got)
	}
	if len(got.TopRejectReasons) != 1 || got.TopRejectReasons[0].Reason != "no_social_links" {
		t.Fatalf("reject reasons: %+v", got.TopRejectReasons)
	}
}

type pipelineStubDB struct {
	overviewStubDB
	rescan *database.RescanPipelineStats
}

func (s *pipelineStubDB) GetRescanPipelineStats(context.Context, int) (*database.RescanPipelineStats, error) {
	return s.rescan, nil
}
