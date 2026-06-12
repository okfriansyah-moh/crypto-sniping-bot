// Package health exposes operator-facing readiness checks.
package health

import (
	"context"
	"fmt"
	"time"

	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
)

// ShadowGateReader loads shadow-mode trade aggregates from the database.
type ShadowGateReader interface {
	GetShadowGateStats(ctx context.Context, windowSeconds int) (*database.ShadowGateStats, error)
}

// ShadowGateResult is the evaluated live-flip readiness gate.
type ShadowGateResult struct {
	Pass               bool    `json:"pass"`
	TradeCount         int     `json:"trade_count"`
	AggregatePnlBps    float64 `json:"aggregate_pnl_bps"`
	AvgPnlBps          float64 `json:"avg_pnl_bps"`
	MinTrades          int     `json:"min_trades"`
	MinWindowDays      int     `json:"min_window_days"`
	MinAggregatePnlBps float64 `json:"min_aggregate_pnl_bps"`
	ExecutionMode      string  `json:"execution_mode"`
	LiveFlipHint       string  `json:"live_flip_hint"`
}

// ShadowGateEvaluator computes shadow gate metrics for health and Telegram status.
type ShadowGateEvaluator struct {
	reader        ShadowGateReader
	executionMode string
	cfg           config.ShadowGateConfig
}

// NewShadowGateEvaluator returns an evaluator. reader may be nil (returns zero stats).
func NewShadowGateEvaluator(reader ShadowGateReader, executionMode string, cfg config.ShadowGateConfig) *ShadowGateEvaluator {
	if cfg.MinTrades <= 0 {
		cfg = config.DefaultShadowGateConfig()
	}
	return &ShadowGateEvaluator{
		reader:        reader,
		executionMode: executionMode,
		cfg:           cfg,
	}
}

// Evaluate loads stats and applies configured thresholds. No side effects.
func (e *ShadowGateEvaluator) Evaluate(ctx context.Context) (ShadowGateResult, error) {
	windowSec := e.cfg.MinWindowDays * 24 * 3600
	if windowSec <= 0 {
		windowSec = 14 * 24 * 3600
	}

	stats := &database.ShadowGateStats{}
	if e.reader != nil {
		var err error
		stats, err = e.reader.GetShadowGateStats(ctx, windowSec)
		if err != nil {
			return ShadowGateResult{}, fmt.Errorf("shadow gate stats: %w", err)
		}
		if stats == nil {
			stats = &database.ShadowGateStats{}
		}
	}

	result := ShadowGateResult{
		TradeCount:         stats.TradeCount,
		AggregatePnlBps:    roundBps(stats.AggregatePnlBps),
		AvgPnlBps:          roundBps(stats.AvgPnlBps),
		MinTrades:          e.cfg.MinTrades,
		MinWindowDays:      e.cfg.MinWindowDays,
		MinAggregatePnlBps: e.cfg.MinAggregatePnlBps,
		ExecutionMode:      e.executionMode,
		LiveFlipHint:       liveFlipRunbookHint(),
	}

	passTrades := result.TradeCount >= result.MinTrades
	passPnl := aggregatePnlPasses(result.AggregatePnlBps, result.MinAggregatePnlBps)
	result.Pass = passTrades && passPnl
	return result, nil
}

func aggregatePnlPasses(aggregateBps, minBps float64) bool {
	// min_aggregate_pnl_bps: 0 means strictly positive aggregate (production gate).
	if minBps == 0 {
		return aggregateBps > 0
	}
	return aggregateBps >= minBps
}

func roundBps(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}

func liveFlipRunbookHint() string {
	return "Operator action only: set execution.mode: live in config/pipeline.yaml after gate passes. No auto-promotion."
}

// WindowDuration returns the configured lookback as a duration.
func (e *ShadowGateEvaluator) WindowDuration() time.Duration {
	days := e.cfg.MinWindowDays
	if days <= 0 {
		days = config.DefaultShadowGateConfig().MinWindowDays
	}
	return time.Duration(days) * 24 * time.Hour
}
