package operator_test

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

type overviewStubDB struct {
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
	dq          *database.DQBreakdown
	recent      []database.RecentEventRow
}

func (s *overviewStubDB) GetSystemState(context.Context) (*contracts.SystemStateDTO, error) {
	return s.state, nil
}

func (s *overviewStubDB) GetActiveStrategyVersion(context.Context) (*database.StrategyVersion, error) {
	return s.strategy, nil
}

func (s *overviewStubDB) IsSystemHalted(context.Context) (bool, string, error) {
	return s.halted, s.haltReason, nil
}

func (s *overviewStubDB) ComputeDrawdown(context.Context, int) (float64, error) {
	return s.drawdown, nil
}

func (s *overviewStubDB) GetOpenPositions(context.Context) ([]contracts.PositionStateDTO, error) {
	return s.open, nil
}

func (s *overviewStubDB) GetClosedPositions(context.Context, int) ([]contracts.PositionStateDTO, error) {
	return s.closed, nil
}

func (s *overviewStubDB) GetShadowGateStats(context.Context, int) (*database.ShadowGateStats, error) {
	if s.shadowStats == nil {
		return &database.ShadowGateStats{}, nil
	}
	return s.shadowStats, nil
}

func (s *overviewStubDB) GetPipelineStats(context.Context, int) (*database.PipelineStats, error) {
	if s.pipeline == nil {
		return &database.PipelineStats{}, nil
	}
	return s.pipeline, nil
}

func (s *overviewStubDB) GetDQBreakdown(context.Context, int, string) (*database.DQBreakdown, error) {
	return s.dq, nil
}

func (s *overviewStubDB) ListRecentEvents(_ context.Context, chain string, limit int) ([]database.RecentEventRow, error) {
	limit = database.CapRecentEventsLimit(limit)
	rows := s.recent
	if chain != "" {
		filtered := make([]database.RecentEventRow, 0, len(rows))
		for _, r := range rows {
			if strings.EqualFold(r.Chain, chain) {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	return rows, nil
}

func TestBuildOverview_PopulatesCoreKPIs(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	stub := &overviewStubDB{
		state: &contracts.SystemStateDTO{
			Mode:             "BALANCED",
			DrawdownPct:      0.03,
			OpenPositions:    2,
			TotalExposureUsd: 15,
			ActiveStrategyID: "abc123def4567890",
			UpdatedAt:        now.Format(time.RFC3339Nano),
		},
		strategy: &database.StrategyVersion{StrategyVersionID: "ver1111222233334444"},
		closed: []contracts.PositionStateDTO{
			{PnlUsd: 5.0},
			{PnlUsd: -2.0},
			{PnlUsd: 3.0},
		},
		drawdown: 0.03,
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
		Capital: config.CapitalConfig{MaxTotalExposureUsd: 500},
	}

	got, err := operator.BuildOverview(context.Background(), stub, cfg, now.Add(-2*time.Hour))
	if err != nil {
		t.Fatalf("BuildOverview: %v", err)
	}

	if got.Mode != "BALANCED" {
		t.Errorf("Mode = %q, want BALANCED", got.Mode)
	}
	if got.ExecutionMode != "shadow" {
		t.Errorf("ExecutionMode = %q, want shadow", got.ExecutionMode)
	}
	if got.MaxExposureUsd != 500 {
		t.Errorf("MaxExposureUsd = %v, want 500", got.MaxExposureUsd)
	}
	if got.StrategyVersionID != "ver1111222233334444" {
		t.Errorf("StrategyVersionID = %q", got.StrategyVersionID)
	}
	if got.PnLTodayUsd != 6.0 {
		t.Errorf("PnLTodayUsd = %v, want 6", got.PnLTodayUsd)
	}
	if got.PnLTodayWins != 2 || got.PnLTodayLosses != 1 {
		t.Errorf("wins/losses = %d/%d, want 2/1", got.PnLTodayWins, got.PnLTodayLosses)
	}
	if got.ShadowGate == nil || !got.ShadowGate.Pass {
		t.Fatal("expected shadow gate pass")
	}
}

func TestBuildOverview_KillSwitchBanner(t *testing.T) {
	t.Parallel()
	stub := &overviewStubDB{
		state: &contracts.SystemStateDTO{
			Mode:      "HALTED",
			UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
		halted:     true,
		haltReason: "operator /kill",
	}
	cfg := &config.Config{Execution: config.ExecutionConfig{Mode: "shadow"}}

	got, err := operator.BuildOverview(context.Background(), stub, cfg, time.Now())
	if err != nil {
		t.Fatalf("BuildOverview: %v", err)
	}
	if got.AlertBanner == nil || got.AlertBanner.Code != "KILL_SWITCH" {
		t.Fatalf("AlertBanner = %+v, want KILL_SWITCH", got.AlertBanner)
	}
}

func TestBuildOverview_UninitializedState(t *testing.T) {
	t.Parallel()
	stub := &overviewStubDB{state: nil}
	_, err := operator.BuildOverview(context.Background(), stub, nil, time.Now())
	if err == nil {
		t.Fatal("expected error for nil system state")
	}
}

func TestBuildPnLSummary_WinRate(t *testing.T) {
	t.Parallel()
	stub := &overviewStubDB{
		closed: []contracts.PositionStateDTO{
			{PnlUsd: 1},
			{PnlUsd: 1},
			{PnlUsd: -1},
		},
		drawdown: 0.05,
	}
	got, err := operator.BuildPnLSummary(context.Background(), stub, 24)
	if err != nil {
		t.Fatalf("BuildPnLSummary: %v", err)
	}
	if got.WinRatePct < 66.6 || got.WinRatePct > 66.7 {
		t.Errorf("WinRatePct = %v, want ~66.7", got.WinRatePct)
	}
	if got.RealizedPnLUsd != 1 {
		t.Errorf("RealizedPnLUsd = %v, want 1", got.RealizedPnLUsd)
	}
}
