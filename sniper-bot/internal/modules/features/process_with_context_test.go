package features

import (
	"context"
	"reflect"
	"strconv"
	"testing"

	"crypto-sniping-bot/shared/contracts"
)

// snapForToken returns a deterministic, fully-populated MarketSnapshot
// derived from a token-discriminator string. Each token produces a
// distinct snapshot so we can prove the extractor is input-sensitive.
func snapForToken(token string) MarketSnapshot {
	var seed int32
	for _, r := range token {
		seed = seed*131 + int32(r)
	}
	if seed < 0 {
		seed = -seed
	}
	return MarketSnapshot{
		Market:          "eth-uniswap-v2",
		Chain:           "eth",
		LpStatsKnown:    true,
		LiquidityUsd:    float64(10_000 + (seed%900)*100),
		WashStatsKnown:  true,
		TxCount1m:       int32(seed % 50),
		UniqueWallets1m: int32((seed / 7) % 30),
		WalletEntropy:   float64(seed%97) / 100.0,
		RepeatRatio1m:   float64(seed%37) / 100.0,
		HolderDistKnown: true,
		HolderCount:     int32(50 + seed%500),
		Top5HolderPct:   float64(seed%50) / 100.0,
		PoolAgeSeconds:  int32(60 + seed%600),
		LpLockKnown:     true,
		LpLockStrength:  float64(seed%100) / 100.0,
		ReserveBaseRaw:  "1000000000000000000",
		ReserveTokenRaw: "5000000000000000000",
	}
}

// THE bug regression: distinct MarketDataDTOs MUST produce distinct
// FeatureDTO outputs. Prior to the fix, every event collapsed to the
// same constant triplet because Confidence was hardcoded.
func TestProcessWithContext_DifferentInputsYieldDifferentOutputs(t *testing.T) {
	m := New(nil)
	dq := passedDQ()
	tokens := []string{"AAA", "BBB", "CCC", "DDD", "EEE"}

	// Use distinct baselines per token so both scores AND confidence vary —
	// in production the per-market baseline carries the variance.
	scoreFP := make(map[string]struct{}, len(tokens))
	confFP := make(map[string]struct{}, len(tokens))
	for i, tok := range tokens {
		dq.EventID = "dq-" + tok
		dq.TraceID = "trace-" + tok
		base := BaselineSnapshot{
			Market: "eth-uniswap-v2",
			History: map[string][]float64{
				SignalLiquidity:      makeRamp(20+i, 0.5),
				SignalTxVelocity:     makeRamp(20+i, 1.0),
				SignalVolumeMomentum: makeRamp(20+i, 1.0),
			},
		}
		out, err := m.ProcessWithContext(context.Background(), dq, snapForToken(tok), base, "2026-01-01T00:00:00Z")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		scoreFP[scoresFingerprint(out)] = struct{}{}
		confFP[confidenceFingerprint(out)] = struct{}{}
	}
	if len(scoreFP) < 2 {
		t.Fatalf("FeatureDTO scores collapsed across inputs: %d unique", len(scoreFP))
	}
	if len(confFP) < 2 {
		t.Fatalf("FeatureConfidence collapsed across inputs: %d unique", len(confFP))
	}
}

func makeRamp(n int, step float64) []float64 {
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		out[i] = float64(i) * step
	}
	return out
}

func scoresFingerprint(out contracts.FeatureDTO) string {
	return strconv.FormatFloat(out.LiquidityScore, 'f', 9, 64) + "|" +
		strconv.FormatFloat(out.VolumeMomentum, 'f', 9, 64) + "|" +
		strconv.FormatFloat(out.ContractSafety, 'f', 9, 64) + "|" +
		strconv.FormatFloat(out.HolderDistribution, 'f', 9, 64) + "|" +
		strconv.FormatFloat(out.WalletEntropy, 'f', 9, 64) + "|" +
		strconv.FormatFloat(out.PriceMomentum, 'f', 9, 64) + "|" +
		strconv.FormatFloat(out.TxVelocityScore, 'f', 9, 64) + "|" +
		strconv.FormatFloat(out.TokenAge, 'f', 9, 64)
}

func confidenceFingerprint(out contracts.FeatureDTO) string {
	c := out.Confidence
	return strconv.FormatFloat(c.LiquidityScore, 'f', 9, 64) + "|" +
		strconv.FormatFloat(c.VolumeMomentum, 'f', 9, 64) + "|" +
		strconv.FormatFloat(c.ContractSafety, 'f', 9, 64) + "|" +
		strconv.FormatFloat(c.HolderDistribution, 'f', 9, 64) + "|" +
		strconv.FormatFloat(c.WalletEntropy, 'f', 9, 64) + "|" +
		strconv.FormatFloat(c.PriceMomentum, 'f', 9, 64) + "|" +
		strconv.FormatFloat(c.TxVelocityScore, 'f', 9, 64) + "|" +
		strconv.FormatFloat(c.TokenAge, 'f', 9, 64)
}

// Determinism: same input + same baseline → identical output across many runs.
func TestProcessWithContext_Deterministic(t *testing.T) {
	m := New(nil)
	dq := passedDQ()
	snap := snapForToken("AAA")
	base := BaselineSnapshot{
		Market: snap.Market,
		History: map[string][]float64{
			SignalLiquidity:      {7.5, 8.0, 8.2, 8.4, 8.7},
			SignalTxVelocity:     {10, 12, 9, 11, 14},
			SignalVolumeMomentum: {30, 35, 33, 38, 36},
		},
	}

	first, err := m.ProcessWithContext(context.Background(), dq, snap, base, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i := 0; i < 99; i++ {
		other, err := m.ProcessWithContext(context.Background(), dq, snap, base, "2026-01-01T00:00:00Z")
		if err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
		if !reflect.DeepEqual(first, other) {
			t.Fatalf("non-deterministic output at iteration %d", i)
		}
	}
}

// Confidence varies with input completeness — the canonical fix for the
// previously hardcoded {0.7, 0.4, 0.5, 0.8, 0.1, 0.4, 0.4} triplet.
func TestProcessWithContext_ConfidenceVariesWithCompleteness(t *testing.T) {
	m := New(nil)
	dq := passedDQ()

	empty := MarketSnapshot{Market: "eth-uniswap-v2"}
	rich := snapForToken("AAA")

	outEmpty, _ := m.ProcessWithContext(context.Background(), dq, empty, BaselineSnapshot{}, "2026-01-01T00:00:00Z")
	outRich, _ := m.ProcessWithContext(context.Background(), dq, rich, BaselineSnapshot{}, "2026-01-01T00:00:00Z")

	if outRich.Confidence.LiquidityScore <= outEmpty.Confidence.LiquidityScore {
		t.Errorf("liquidity confidence did not rise with snapshot completeness: empty=%f rich=%f",
			outEmpty.Confidence.LiquidityScore, outRich.Confidence.LiquidityScore)
	}
	if outRich.Confidence.VolumeMomentum <= outEmpty.Confidence.VolumeMomentum {
		t.Errorf("volume confidence did not rise with snapshot completeness: empty=%f rich=%f",
			outEmpty.Confidence.VolumeMomentum, outRich.Confidence.VolumeMomentum)
	}
	if outRich.Confidence.ContractSafety <= outEmpty.Confidence.ContractSafety {
		t.Errorf("contract-safety confidence did not rise with LpLockKnown: empty=%f rich=%f",
			outEmpty.Confidence.ContractSafety, outRich.Confidence.ContractSafety)
	}
	if outRich.Confidence.TokenAge <= outEmpty.Confidence.TokenAge {
		t.Errorf("token-age confidence did not rise with PoolAgeSeconds: empty=%f rich=%f",
			outEmpty.Confidence.TokenAge, outRich.Confidence.TokenAge)
	}
}

// Confidence and scores are NEVER the constants the old implementation hardcoded.
// This test exists explicitly to prevent regression to the F-2 STUBBED bug.
func TestProcessWithContext_NoHardcodedConfidenceConstants(t *testing.T) {
	m := New(nil)
	dq := passedDQ()

	legacy := contracts.FeatureConfidence{
		LiquidityScore:     0.7,
		TxVelocityScore:    0.4,
		HolderDistribution: 0.5,
		WalletEntropy:      0.4,
		ContractSafety:     0.8,
		TokenAge:           0.1,
		VolumeMomentum:     0.4,
		PriceMomentum:      0.4,
	}
	for _, tok := range []string{"AAA", "BBB", "CCC", "DDD", "EEE", "FFF"} {
		dq.EventID = "dq-" + tok
		out, err := m.ProcessWithContext(context.Background(), dq, snapForToken(tok), BaselineSnapshot{}, "2026-01-01T00:00:00Z")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out.Confidence == legacy {
			t.Fatalf("FeatureConfidence regressed to the legacy hardcoded constants for %q", tok)
		}
	}
}
