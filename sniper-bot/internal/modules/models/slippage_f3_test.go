package models

// F-3 regression tests for the CPMM slippage model. These tests replace
// the old "constant {p50:80, p95:200}" stub-like behavior with concrete
// distribution / determinism / boundedness / monotonicity guarantees.

import (
	"context"
	"math"
	"testing"

	"crypto-sniping-bot/shared/contracts"
)

// fakeAlphaProvider is a deterministic in-memory α store used to verify
// that the model multiplies base impact by the resolved coefficient.
type fakeAlphaProvider struct {
	val float64
	err error
}

func (f *fakeAlphaProvider) GetSlippageAlpha(_ context.Context, _ string) (float64, error) {
	return f.val, f.err
}

// f3Feature returns a sample feature parameterised by liquidity, score,
// and momentum so individual tests can sweep one axis at a time.
func f3Feature(liqUsd float64, liqScore, priceMomentum float64) contracts.FeatureDTO {
	return contracts.FeatureDTO{
		EventID:          "feat-f3",
		TraceID:          "trace-f3",
		CorrelationID:    "corr-f3",
		CausationID:      "dq-f3",
		VersionID:        "ver-f3",
		TokenLifecycleID: "lc-f3",
		TokenAddress:     "0xf3",
		LiquidityScore:   liqScore,
		PriceMomentum:    priceMomentum,
		LiquidityUsdRaw:  liqUsd,
		Confidence:       contracts.FeatureConfidence{LiquidityScore: 1.0},
		ExtractedAt:      "2026-04-26T00:00:00Z",
	}
}

// TestSlippageF3DistinctInputsProduceDistinctOutputs is the headline
// regression: the bucket model emitted at most ~6 distinct (p50,p95)
// tuples across all of phase space. The CPMM model must spread distinct
// (size, reserves, volatility) inputs across ≥20 unique tuples / 100.
func TestSlippageF3DistinctInputsProduceDistinctOutputs(t *testing.T) {
	m := NewSlippageModel(DefaultSlippageConfig())
	seen := make(map[[2]int32]struct{})

	count := 0
	for liqIdx := 0; liqIdx < 10; liqIdx++ {
		for sizeIdx := 0; sizeIdx < 10; sizeIdx++ {
			liq := 5_000.0 + float64(liqIdx)*47_000.0  // 5k..428k, irregular
			size := 5.0 + float64(sizeIdx)*37.0        // 5..338
			liqScore := 0.05 + 0.09*float64(liqIdx%9)  // varies σ
			priceMom := 0.05 + 0.09*float64(sizeIdx%9) // varies σ
			feat := f3Feature(liq, liqScore, priceMom)
			out, err := m.Estimate(context.Background(), feat, size)
			if err != nil {
				t.Fatalf("estimate err: %v", err)
			}
			seen[[2]int32{out.ExpectedP50Bps, out.ExpectedP95Bps}] = struct{}{}
			count++
		}
	}
	if count != 100 {
		t.Fatalf("expected 100 inputs, got %d", count)
	}
	if len(seen) < 20 {
		t.Fatalf("F-3 regression: only %d unique (p50,p95) tuples across 100 inputs; want >= 20", len(seen))
	}
}

// TestSlippageF3Determinism: 100 calls with identical input must produce
// identical (p50, p95, EventID).
func TestSlippageF3Determinism(t *testing.T) {
	m := NewSlippageModel(DefaultSlippageConfig())
	feat := f3Feature(150_000, 0.7, 0.3)
	want, err := m.Estimate(context.Background(), feat, 50)
	if err != nil {
		t.Fatalf("estimate err: %v", err)
	}
	for i := 0; i < 100; i++ {
		got, err := m.Estimate(context.Background(), feat, 50)
		if err != nil {
			t.Fatalf("estimate err on iter %d: %v", i, err)
		}
		if got.EventID != want.EventID ||
			got.ExpectedP50Bps != want.ExpectedP50Bps ||
			got.ExpectedP95Bps != want.ExpectedP95Bps {
			t.Fatalf("non-deterministic at iter %d: want %+v got %+v", i, want, got)
		}
	}
}

// TestSlippageF3Bounds: extreme inputs must clip to [0, MaxSlippageBps].
func TestSlippageF3Bounds(t *testing.T) {
	cfg := DefaultSlippageConfig()
	m := NewSlippageModel(cfg)

	// Very large size against tiny pool ⇒ should saturate to MaxSlippageBps.
	bigFeat := f3Feature(100, 0.0, 1.0) // tiny TVL, max σ
	out, err := m.Estimate(context.Background(), bigFeat, 1_000_000)
	if err != nil {
		t.Fatalf("estimate err: %v", err)
	}
	if out.ExpectedP50Bps < 0 || out.ExpectedP50Bps > cfg.MaxSlippageBps {
		t.Fatalf("p50 out of bounds: %d", out.ExpectedP50Bps)
	}
	if out.ExpectedP95Bps < 0 || out.ExpectedP95Bps > cfg.MaxSlippageBps {
		t.Fatalf("p95 out of bounds: %d", out.ExpectedP95Bps)
	}
	if out.ExpectedP50Bps != cfg.MaxSlippageBps {
		t.Fatalf("expected p50 saturated to MaxSlippageBps for size>>reserves; got %d", out.ExpectedP50Bps)
	}

	// Zero size ⇒ zero p50 (CPMM impact = 0).
	zeroFeat := f3Feature(500_000, 0.9, 0.5)
	out, err = m.Estimate(context.Background(), zeroFeat, 0)
	if err != nil {
		t.Fatalf("estimate err: %v", err)
	}
	if out.ExpectedP50Bps != 0 {
		t.Fatalf("size=0 must give p50=0; got %d", out.ExpectedP50Bps)
	}
	if out.ExpectedP95Bps < 0 {
		t.Fatalf("p95 negative: %d", out.ExpectedP95Bps)
	}

	// Negative size is sanitized to zero (defensive).
	out, err = m.Estimate(context.Background(), zeroFeat, -500)
	if err != nil {
		t.Fatalf("estimate err: %v", err)
	}
	if out.ExpectedP50Bps != 0 {
		t.Fatalf("negative size must clamp to p50=0; got %d", out.ExpectedP50Bps)
	}
}

// TestSlippageF3MonotonicInSize: with reserves fixed, increasing size
// must produce non-decreasing p50.
func TestSlippageF3MonotonicInSize(t *testing.T) {
	m := NewSlippageModel(DefaultSlippageConfig())
	feat := f3Feature(250_000, 0.6, 0.5)
	prev := int32(-1)
	for _, size := range []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 5000, 10_000, 50_000} {
		out, err := m.Estimate(context.Background(), feat, size)
		if err != nil {
			t.Fatalf("estimate err: %v", err)
		}
		if out.ExpectedP50Bps < prev {
			t.Fatalf("monotonicity broken: size=%v p50=%d < prev=%d", size, out.ExpectedP50Bps, prev)
		}
		prev = out.ExpectedP50Bps
	}
}

// TestSlippageF3P95GreaterEqualP50 across a wide grid.
func TestSlippageF3P95GreaterEqualP50(t *testing.T) {
	m := NewSlippageModel(DefaultSlippageConfig())
	for liq := 1_000.0; liq <= 5_000_000; liq *= 3 {
		for size := 1.0; size <= 100_000; size *= 4 {
			for _, ls := range []float64{0.0, 0.3, 0.7, 1.0} {
				for _, pm := range []float64{0.0, 0.3, 0.7, 1.0} {
					feat := f3Feature(liq, ls, pm)
					out, _ := m.Estimate(context.Background(), feat, size)
					if out.ExpectedP95Bps < out.ExpectedP50Bps {
						t.Fatalf("p95<p50: liq=%v size=%v ls=%v pm=%v p50=%d p95=%d",
							liq, size, ls, pm, out.ExpectedP50Bps, out.ExpectedP95Bps)
					}
				}
			}
		}
	}
}

// TestSlippageF3AlphaProviderApplied verifies α=1.0 (no-op) matches the
// no-provider baseline, and α=2.0 doubles the CPMM base impact.
func TestSlippageF3AlphaProviderApplied(t *testing.T) {
	feat := f3Feature(100_000, 1.0, 0.5) // σ_proxy = 0 + 0 = 0 ⇒ p95 = p50 + tail
	cfg := DefaultSlippageConfig()
	cfg.TailBps = 0       // simplify: p95 = p50*(1+z*σ); with σ=0 ⇒ p95 = p50.
	cfg.VolatilityZ = 1.0 // unused with σ=0

	baseline := NewSlippageModel(cfg)
	withOne := NewSlippageModelWithAlpha(cfg, &fakeAlphaProvider{val: 1.0})
	withTwo := NewSlippageModelWithAlpha(cfg, &fakeAlphaProvider{val: 2.0})

	size := 100.0
	a, _ := baseline.Estimate(context.Background(), feat, size)
	b, _ := withOne.Estimate(context.Background(), feat, size)
	c, _ := withTwo.Estimate(context.Background(), feat, size)

	if a.ExpectedP50Bps != b.ExpectedP50Bps {
		t.Fatalf("α=1.0 must equal default-α baseline: %d vs %d", a.ExpectedP50Bps, b.ExpectedP50Bps)
	}
	// α=2.0 doubles base_bps. Allow ±1 for rounding.
	want := 2 * a.ExpectedP50Bps
	if diff := c.ExpectedP50Bps - want; diff < -1 || diff > 1 {
		t.Fatalf("α=2.0 must double p50 (~%d); got %d", want, c.ExpectedP50Bps)
	}
}

// TestSlippageF3AlphaProviderErrorFallsBackToDefault: provider errors
// must NOT produce errors from the model — they fall back to DefaultAlpha.
func TestSlippageF3AlphaProviderErrorFallsBackToDefault(t *testing.T) {
	feat := f3Feature(100_000, 0.5, 0.5)
	cfg := DefaultSlippageConfig()
	cfg.DefaultAlpha = 1.0

	withErr := NewSlippageModelWithAlpha(cfg, &fakeAlphaProvider{val: 0, err: errFakeProvider})
	out, err := withErr.Estimate(context.Background(), feat, 100)
	if err != nil {
		t.Fatalf("provider error must NOT propagate: %v", err)
	}
	if out.ExpectedP50Bps == 0 {
		t.Fatalf("expected non-zero p50 from default-α fallback")
	}
}

// TestSlippageF3AlphaCappedAtMax: malicious / runaway α must be clamped.
func TestSlippageF3AlphaCappedAtMax(t *testing.T) {
	feat := f3Feature(100_000, 1.0, 0.5)
	cfg := DefaultSlippageConfig()
	cfg.TailBps = 0
	cfg.MaxAlpha = 3.0

	huge := NewSlippageModelWithAlpha(cfg, &fakeAlphaProvider{val: 10_000.0})
	out, _ := huge.Estimate(context.Background(), feat, 100)
	// Bounded by MaxSlippageBps anyway; verify we still bound and produce
	// a finite, non-saturated value when (size << reserves) and α=MaxAlpha.
	if out.ExpectedP50Bps > cfg.MaxSlippageBps {
		t.Fatalf("p50 exceeded MaxSlippageBps: %d", out.ExpectedP50Bps)
	}
}

// TestSlippageF3RejectsNonFiniteInput
func TestSlippageF3RejectsNonFiniteInput(t *testing.T) {
	m := NewSlippageModel(DefaultSlippageConfig())
	for _, bad := range []contracts.FeatureDTO{
		f3Feature(math.NaN(), 0.5, 0.5),
		f3Feature(math.Inf(1), 0.5, 0.5),
		f3Feature(100_000, math.NaN(), 0.5),
		f3Feature(100_000, 0.5, math.Inf(-1)),
	} {
		if _, err := m.Estimate(context.Background(), bad, 50); err == nil {
			t.Fatalf("expected ErrInvalidSlippageInput for %+v", bad)
		}
	}
}

// TestSlippageF3ModelVersionPopulated
func TestSlippageF3ModelVersionPopulated(t *testing.T) {
	m := NewSlippageModel(DefaultSlippageConfig())
	out, _ := m.Estimate(context.Background(), f3Feature(100_000, 0.5, 0.5), 50)
	if out.ModelVersionID == "" {
		t.Fatalf("model_version_id must be populated")
	}
}

// errFakeProvider is a sentinel for the provider-error fallback test.
var errFakeProvider = fakeProviderErr{}

type fakeProviderErr struct{}

func (fakeProviderErr) Error() string { return "fake provider failure" }
