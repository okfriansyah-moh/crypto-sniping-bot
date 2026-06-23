package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/operator"
)

type telegramParityStub struct {
	database.Adapter
	state       *contracts.SystemStateDTO
	strategy    *database.StrategyVersion
	halted      bool
	haltReason  string
	open        []contracts.PositionStateDTO
	closed      []contracts.PositionStateDTO
	drawdown    float64
	shadowStats *database.ShadowGateStats
	pipeline    *database.PipelineStats
	adaptiveDQ  struct {
		total int
		rug   int
		err   error
	}
}

func (s *telegramParityStub) GetSystemState(context.Context) (*contracts.SystemStateDTO, error) {
	return s.state, nil
}

func (s *telegramParityStub) GetActiveStrategyVersion(context.Context) (*database.StrategyVersion, error) {
	return s.strategy, nil
}

func (s *telegramParityStub) IsSystemHalted(context.Context) (bool, string, error) {
	return s.halted, s.haltReason, nil
}

func (s *telegramParityStub) ComputeDrawdown(context.Context, int) (float64, error) {
	return s.drawdown, nil
}

func (s *telegramParityStub) GetOpenPositions(context.Context) ([]contracts.PositionStateDTO, error) {
	return s.open, nil
}

func (s *telegramParityStub) GetClosedPositions(context.Context, int) ([]contracts.PositionStateDTO, error) {
	return s.closed, nil
}

func (s *telegramParityStub) GetShadowGateStats(context.Context, int) (*database.ShadowGateStats, error) {
	if s.shadowStats == nil {
		return &database.ShadowGateStats{}, nil
	}
	return s.shadowStats, nil
}

func (s *telegramParityStub) GetPipelineStats(context.Context, int) (*database.PipelineStats, error) {
	if s.pipeline == nil {
		return &database.PipelineStats{}, nil
	}
	return s.pipeline, nil
}

func (s *telegramParityStub) GetDQBreakdown(context.Context, int, string) (*database.DQBreakdown, error) {
	return &database.DQBreakdown{}, nil
}

func (s *telegramParityStub) GetAdaptiveDQStats(context.Context, int) (int, int, error) {
	return s.adaptiveDQ.total, s.adaptiveDQ.rug, s.adaptiveDQ.err
}

func TestTelegramParity_StatusAndPnL(t *testing.T) {
	t.Parallel()
	start := time.Date(2026, 6, 13, 8, 0, 0, 0, time.UTC)
	stub := &telegramParityStub{
		state: &contracts.SystemStateDTO{
			Mode:             "BALANCED",
			DrawdownPct:      0.04,
			OpenPositions:    1,
			TotalExposureUsd: 5,
			UpdatedAt:        "2026-06-13T12:00:00Z",
		},
		strategy: &database.StrategyVersion{StrategyVersionID: "strat_v1"},
		closed: []contracts.PositionStateDTO{
			{PnlUsd: 2},
			{PnlUsd: -1},
		},
		drawdown: 0.04,
		shadowStats: &database.ShadowGateStats{
			TradeCount:      35,
			AggregatePnlBps: 120,
			AvgPnlBps:       3.4,
		},
	}
	cfg := &config.Config{
		Execution: config.ExecutionConfig{
			Mode: "shadow",
			ShadowGate: config.ShadowGateConfig{
				MinTrades:          30,
				MinWindowDays:      14,
				MinAggregatePnlBps: 0,
			},
		},
	}

	ctx := context.Background()
	statusFn := buildStatusFn(stub, cfg, start)
	status, err := statusFn(ctx)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if !containsAll(status,
		"<b>Status</b>",
		"Mode: <code>BALANCED</code>",
		"Strategy: <code>strat_v1</code>",
		"<b>Shadow live-flip gate</b>",
	) {
		t.Fatalf("status output missing expected fields:\n%s", status)
	}

	pnlFn := buildPnlFn(stub)
	pnl, err := pnlFn(ctx)
	if err != nil {
		t.Fatalf("pnl: %v", err)
	}
	if !containsAll(pnl,
		"<b>PnL Summary (24h)</b>",
		"Realized: <code>$+1.00</code>",
		"Drawdown: <code>4.00%</code>",
	) {
		t.Fatalf("pnl output missing expected fields:\n%s", pnl)
	}
}

func TestTelegramParity_PositionsPipelineDQ(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	stub := &telegramParityStub{
		open: []contracts.PositionStateDTO{
			{
				PositionID:   "pos1",
				TokenAddress: "So11111111111111111111111111111111111111112",
				Chain:        "solana",
				EntryPrice:   "1.0",
				CurrentPrice: "2.0",
				EntrySizeUsd: 10,
				OpenedAt:     time.Now().UTC().Add(-30 * time.Minute).Format(time.RFC3339),
			},
		},
		pipeline: &database.PipelineStats{
			Detected:  100,
			DQPassed:  10,
			Evaluated: 2,
			Rejected:  50,
			Recent: []database.RecentToken{
				{TokenAddress: "Abcd1234efgh5678ijkl", Symbol: "TEST", State: "EVALUATED", Chain: "solana"},
			},
		},
		adaptiveDQ: struct {
			total int
			rug   int
			err   error
		}{total: 20, rug: 3},
	}

	positions, err := buildPositionsFn(stub)(ctx)
	if err != nil {
		t.Fatalf("positions: %v", err)
	}
	if !containsAll(positions, "<b>Open Positions (1)</b>", "So11111111111111111111111111111111111111112", "+100.00%") {
		t.Fatalf("positions output:\n%s", positions)
	}

	pipeline, err := buildPipelineFn(stub)(ctx)
	if err != nil {
		t.Fatalf("pipeline: %v", err)
	}
	if !containsAll(pipeline, "DETECTED     <code>   100</code>", "DQ_PASSED    <code>    10</code>", "<b>Recent tokens:</b>") {
		t.Fatalf("pipeline output:\n%s", pipeline)
	}

	dq, err := buildDqFn(stub)(ctx, 24)
	if err != nil {
		t.Fatalf("dq: %v", err)
	}
	if !containsAll(dq,
		"<b>Data Quality  (last 24h)</b>",
		"<b>Adaptive DQ decisions:</b>",
		"Rug rate:         <code>15.0%</code>",
		"<b>Funnel gate (DQ):</b>",
		"Verdict: ✅ Healthy",
	) {
		t.Fatalf("dq output:\n%s", dq)
	}
}

func TestTelegramParity_FormattersMatchOperatorData(t *testing.T) {
	t.Parallel()
	report, err := operator.BuildDQTelegramReport(context.Background(), &telegramParityStub{
		adaptiveDQ: struct {
			total int
			rug   int
			err   error
		}{total: 10, rug: 1},
		pipeline: &database.PipelineStats{Detected: 5, DQPassed: 1, Rejected: 2},
	}, 24)
	if err != nil {
		t.Fatal(err)
	}
	got := formatTelegramDQ(report)
	if !containsAll(got, "Pass rate:        <code>20.0%</code>", "Reject rate:      <code>40.0%</code>") {
		t.Fatalf("formatTelegramDQ:\n%s", got)
	}
}

func containsAll(haystack string, needles ...string) bool {
	for _, n := range needles {
		if !strings.Contains(haystack, n) {
			return false
		}
	}
	return true
}
