package models

// Phase 4 slippage model: empirical p50/p95 by (liquidity bucket, size bucket).
// Buckets are defined in config; missing buckets fall back to conservative priors.

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"crypto-sniping-bot/contracts"
)

// SlippageBucket is a single (liquidity, size) bucket calibration entry.
// LiquidityMaxUsd and SizeMaxUsd are the *upper* bound of the bucket.
type SlippageBucket struct {
	LiquidityMaxUsd float64 `yaml:"liquidity_max_usd"`
	SizeMaxUsd      float64 `yaml:"size_max_usd"`
	P50Bps          int32   `yaml:"p50_bps"`
	P95Bps          int32   `yaml:"p95_bps"`
}

// SlippageConfig holds the model's bucket grid plus fallback priors.
type SlippageConfig struct {
	Buckets        []SlippageBucket `yaml:"buckets"`
	FallbackP50Bps int32            `yaml:"fallback_p50_bps"`
	FallbackP95Bps int32            `yaml:"fallback_p95_bps"`
	ModelVersionID string           `yaml:"model_version_id"`
}

// DefaultSlippageConfig returns a conservative bucket grid.
func DefaultSlippageConfig() SlippageConfig {
	return SlippageConfig{
		Buckets: []SlippageBucket{
			{LiquidityMaxUsd: 25_000, SizeMaxUsd: 50, P50Bps: 250, P95Bps: 800},
			{LiquidityMaxUsd: 25_000, SizeMaxUsd: 200, P50Bps: 500, P95Bps: 1500},
			{LiquidityMaxUsd: 100_000, SizeMaxUsd: 50, P50Bps: 80, P95Bps: 250},
			{LiquidityMaxUsd: 100_000, SizeMaxUsd: 200, P50Bps: 150, P95Bps: 500},
			{LiquidityMaxUsd: 500_000, SizeMaxUsd: 50, P50Bps: 30, P95Bps: 90},
			{LiquidityMaxUsd: 500_000, SizeMaxUsd: 200, P50Bps: 60, P95Bps: 180},
			{LiquidityMaxUsd: math.MaxFloat64, SizeMaxUsd: 50, P50Bps: 15, P95Bps: 50},
			{LiquidityMaxUsd: math.MaxFloat64, SizeMaxUsd: math.MaxFloat64, P50Bps: 40, P95Bps: 120},
		},
		FallbackP50Bps: 200,
		FallbackP95Bps: 600,
		ModelVersionID: "slip-phase4-v1",
	}
}

// SlippageModel emits an empirically-bucketed slippage estimate.
type SlippageModel struct {
	cfg SlippageConfig
}

// NewSlippageModel constructs a SlippageModel with deterministically-sorted buckets.
func NewSlippageModel(cfg SlippageConfig) *SlippageModel {
	sort.SliceStable(cfg.Buckets, func(i, j int) bool {
		if cfg.Buckets[i].LiquidityMaxUsd != cfg.Buckets[j].LiquidityMaxUsd {
			return cfg.Buckets[i].LiquidityMaxUsd < cfg.Buckets[j].LiquidityMaxUsd
		}
		return cfg.Buckets[i].SizeMaxUsd < cfg.Buckets[j].SizeMaxUsd
	})
	return &SlippageModel{cfg: cfg}
}

// Estimate returns a SlippageEstimateDTO for the given (feature, proposed size).
// Deterministic apart from EstimatedAt timestamp.
func (m *SlippageModel) Estimate(
	_ context.Context,
	feature contracts.FeatureDTO,
	proposedSizeUsd float64,
) (contracts.SlippageEstimateDTO, error) {
	liq := math.Max(0, feature.LiquidityUsdRaw)
	size := math.Max(0, proposedSizeUsd)
	p50, p95 := m.cfg.FallbackP50Bps, m.cfg.FallbackP95Bps
	for _, b := range m.cfg.Buckets {
		if liq <= b.LiquidityMaxUsd && size <= b.SizeMaxUsd {
			p50, p95 = b.P50Bps, b.P95Bps
			break
		}
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	eventID := contracts.ContentIDFromString(fmt.Sprintf("slip:%s:%d", feature.EventID, int64(size*100)))

	return contracts.SlippageEstimateDTO{
		EventID:          eventID,
		TraceID:          feature.TraceID,
		CorrelationID:    feature.CorrelationID,
		CausationID:      feature.EventID,
		VersionID:        feature.VersionID,
		TokenLifecycleID: feature.TokenLifecycleID,
		ExpectedP50Bps:   p50,
		ExpectedP95Bps:   p95,
		ModelVersionID:   m.cfg.ModelVersionID,
		EstimatedAt:      now,
	}, nil
}

// ModelVersionID returns the configured slippage model version.
func (m *SlippageModel) ModelVersionID() string { return m.cfg.ModelVersionID }
