package models

import (
	"context"
	"math"
	"testing"
	"time"

	"crypto-sniping-bot/contracts"
)

func sampleFeature() contracts.FeatureDTO {
	return contracts.FeatureDTO{
		EventID:            "feat-evt-1",
		TraceID:            "trace-1",
		CorrelationID:      "corr-1",
		CausationID:        "dq-evt-1",
		VersionID:          "ver-1",
		TokenLifecycleID:   "tok-1",
		TokenAddress:       "0xabc",
		LiquidityScore:     0.7,
		TxVelocityScore:    0.5,
		HolderDistribution: 0.6,
		WalletEntropy:      0.5,
		ContractSafety:     0.9,
		TokenAge:           0.0,
		VolumeMomentum:     0.4,
		PriceMomentum:      0.3,
		LiquidityUsdRaw:    150_000,
		ExtractedAt:        "2026-04-26T00:00:00Z",
	}
}

// ── Probability ──────────────────────────────────────────────────────────────

func TestProbabilityModelDeterministic(t *testing.T) {
	m := NewProbabilityModel(DefaultLogisticConfig())
	in := sampleFeature()
	out1, err := m.Predict(context.Background(), in)
	if err != nil {
		t.Fatalf("predict err: %v", err)
	}
	out2, err := m.Predict(context.Background(), in)
	if err != nil {
		t.Fatalf("predict err: %v", err)
	}
	if out1.Probability != out2.Probability {
		t.Fatalf("non-deterministic: %v != %v", out1.Probability, out2.Probability)
	}
	if out1.EventID != out2.EventID {
		t.Fatalf("event id not stable: %s vs %s", out1.EventID, out2.EventID)
	}
	if out1.Probability <= 0 || out1.Probability >= 1 {
		t.Fatalf("probability out of (0,1): %v", out1.Probability)
	}
}

func TestProbabilityTracePropagation(t *testing.T) {
	m := NewProbabilityModel(DefaultLogisticConfig())
	in := sampleFeature()
	out, _ := m.Predict(context.Background(), in)
	if out.TraceID != in.TraceID {
		t.Fatalf("trace id not propagated: %s", out.TraceID)
	}
	if out.CausationID != in.EventID {
		t.Fatalf("causation should be feature event: %s", out.CausationID)
	}
	if out.VersionID != in.VersionID {
		t.Fatalf("version id not propagated")
	}
	if out.ModelVersionID == "" {
		t.Fatalf("model version id empty")
	}
}

func TestProbabilityClampsExtremes(t *testing.T) {
	cfg := DefaultLogisticConfig()
	cfg.Bias = 50.0 // forces sigmoid≈1
	m := NewProbabilityModel(cfg)
	in := sampleFeature()
	out, _ := m.Predict(context.Background(), in)
	if out.Probability > cfg.MaxProbability {
		t.Fatalf("probability not clamped to MaxProbability: %v > %v", out.Probability, cfg.MaxProbability)
	}
	if out.Probability != cfg.MaxProbability {
		t.Fatalf("expected probability == MaxProbability for large positive z, got %v", out.Probability)
	}
}

func TestProbabilityRejectsNaN(t *testing.T) {
	m := NewProbabilityModel(DefaultLogisticConfig())
	in := sampleFeature()
	in.LiquidityScore = math.NaN()
	_, err := m.Predict(context.Background(), in)
	if err == nil {
		t.Fatalf("expected ErrInvalidProbability on NaN input")
	}
}

func TestProbabilityMonotonicInLiquidity(t *testing.T) {
	m := NewProbabilityModel(DefaultLogisticConfig())
	low := sampleFeature()
	high := sampleFeature()
	low.LiquidityScore = 0.1
	high.LiquidityScore = 0.9
	pl, _ := m.Predict(context.Background(), low)
	ph, _ := m.Predict(context.Background(), high)
	if pl.Probability >= ph.Probability {
		t.Fatalf("higher liquidity must increase probability: low=%v high=%v", pl.Probability, ph.Probability)
	}
}

// ── Slippage ─────────────────────────────────────────────────────────────────

func TestSlippageBucketLookup(t *testing.T) {
	m := NewSlippageModel(DefaultSlippageConfig())
	feat := sampleFeature()
	feat.LiquidityUsdRaw = 50_000 // → 100k bucket
	out, err := m.Estimate(context.Background(), feat, 50)
	if err != nil {
		t.Fatalf("estimate err: %v", err)
	}
	if out.ExpectedP50Bps == 0 || out.ExpectedP95Bps == 0 {
		t.Fatalf("expected non-zero bps, got %+v", out)
	}
	if out.ExpectedP95Bps < out.ExpectedP50Bps {
		t.Fatalf("p95 must be >= p50: %d vs %d", out.ExpectedP95Bps, out.ExpectedP50Bps)
	}
}

func TestSlippageHigherSizeWorseSlippage(t *testing.T) {
	m := NewSlippageModel(DefaultSlippageConfig())
	feat := sampleFeature()
	feat.LiquidityUsdRaw = 50_000
	small, _ := m.Estimate(context.Background(), feat, 50)
	large, _ := m.Estimate(context.Background(), feat, 200)
	if large.ExpectedP95Bps < small.ExpectedP95Bps {
		t.Fatalf("larger size must produce >= slippage; small=%d large=%d", small.ExpectedP95Bps, large.ExpectedP95Bps)
	}
}

func TestSlippageDeterministic(t *testing.T) {
	m := NewSlippageModel(DefaultSlippageConfig())
	feat := sampleFeature()
	a, _ := m.Estimate(context.Background(), feat, 100)
	b, _ := m.Estimate(context.Background(), feat, 100)
	if a.EventID != b.EventID || a.ExpectedP95Bps != b.ExpectedP95Bps {
		t.Fatalf("non-deterministic")
	}
}

func TestSlippageTracePropagation(t *testing.T) {
	m := NewSlippageModel(DefaultSlippageConfig())
	feat := sampleFeature()
	out, _ := m.Estimate(context.Background(), feat, 50)
	if out.TraceID != feat.TraceID || out.CausationID != feat.EventID {
		t.Fatalf("trace propagation broken: %+v", out)
	}
}

// ── Latency ──────────────────────────────────────────────────────────────────

func TestLatencyFallbackWhenInsufficientSamples(t *testing.T) {
	m := NewLatencyModel(DefaultLatencyConfig())
	out, err := m.Profile(context.Background(), "ethereum")
	if err != nil {
		t.Fatalf("profile err: %v", err)
	}
	cfg := DefaultLatencyConfig()
	if out.ExpectedP50Ms != cfg.FallbackP50Ms || out.ExpectedP95Ms != cfg.FallbackP95Ms {
		t.Fatalf("fallback not applied: %+v", out)
	}
	if out.Chain != "ethereum" {
		t.Fatalf("chain mismatch: %s", out.Chain)
	}
}

func TestLatencyComputesPercentiles(t *testing.T) {
	cfg := DefaultLatencyConfig()
	cfg.MinSamples = 4
	m := NewLatencyModel(cfg)
	for _, ms := range []int{100, 200, 300, 400, 500, 600, 700, 800, 900, 1000} {
		m.Record("ethereum", time.Duration(ms)*time.Millisecond)
	}
	out, _ := m.Profile(context.Background(), "ethereum")
	if out.ExpectedP50Ms <= 0 || out.ExpectedP95Ms < out.ExpectedP50Ms {
		t.Fatalf("invalid percentiles: %+v", out)
	}
	// p95 of 10 sorted samples [100..1000] @ idx (10-1)*0.95=8 → 900.
	if out.ExpectedP95Ms != 900 {
		t.Fatalf("p95 expected 900, got %d", out.ExpectedP95Ms)
	}
}

func TestLatencyEvictsExpiredSamples(t *testing.T) {
	cfg := DefaultLatencyConfig()
	cfg.WindowSeconds = 1
	cfg.MinSamples = 1
	m := NewLatencyModel(cfg)
	m.Record("eth", 50*time.Millisecond)
	// Move clock forward beyond window.
	m.now = func() time.Time { return time.Now().UTC().Add(10 * time.Second) }
	out, _ := m.Profile(context.Background(), "eth")
	if out.ExpectedP50Ms != cfg.FallbackP50Ms {
		t.Fatalf("expired samples should fall back to priors: got %d", out.ExpectedP50Ms)
	}
}

func TestLatencyEventIDStablePerWindow(t *testing.T) {
	cfg := DefaultLatencyConfig()
	cfg.WindowSeconds = 3600
	m := NewLatencyModel(cfg)
	fixed := time.Date(2026, 4, 26, 12, 0, 0, 0, time.UTC)
	m.now = func() time.Time { return fixed }
	a, _ := m.Profile(context.Background(), "eth")
	b, _ := m.Profile(context.Background(), "eth")
	if a.EventID != b.EventID {
		t.Fatalf("event id should be stable within a window: %s vs %s", a.EventID, b.EventID)
	}
}
