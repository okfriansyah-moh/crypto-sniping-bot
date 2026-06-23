package health_test

import (
	"context"
	"testing"

	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/health"
)

type stubShadowGateReader struct {
	stats *database.ShadowGateStats
	err   error
}

func (s *stubShadowGateReader) GetShadowGateStats(_ context.Context, _ int) (*database.ShadowGateStats, error) {
	return s.stats, s.err
}

func TestEvaluateShadowGate_Pass(t *testing.T) {
	reader := &stubShadowGateReader{stats: &database.ShadowGateStats{
		TradeCount: 30, AggregatePnlBps: 120, AvgPnlBps: 4,
	}}
	ev := health.NewShadowGateEvaluator(reader, "shadow", config.DefaultShadowGateConfig())
	result, err := ev.Evaluate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !result.Pass {
		t.Fatalf("expected pass, got %+v", result)
	}
}

func TestEvaluateShadowGate_FailInsufficientTrades(t *testing.T) {
	reader := &stubShadowGateReader{stats: &database.ShadowGateStats{
		TradeCount: 10, AggregatePnlBps: 500,
	}}
	ev := health.NewShadowGateEvaluator(reader, "shadow", config.DefaultShadowGateConfig())
	result, err := ev.Evaluate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Pass {
		t.Fatal("expected fail on trade count")
	}
}

func TestEvaluateShadowGate_FailNonPositiveAggregate(t *testing.T) {
	reader := &stubShadowGateReader{stats: &database.ShadowGateStats{
		TradeCount: 40, AggregatePnlBps: 0,
	}}
	ev := health.NewShadowGateEvaluator(reader, "shadow", config.DefaultShadowGateConfig())
	result, err := ev.Evaluate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Pass {
		t.Fatal("expected fail when aggregate pnl is zero with min_aggregate_pnl_bps=0")
	}
}
