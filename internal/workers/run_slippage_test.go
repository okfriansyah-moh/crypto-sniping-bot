package workers

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
)

// recordingAlphaAdapter wraps stubAdapter and records the market argument
// passed to GetSlippageAlpha so we can assert the slippage worker threads
// FeatureDTO.Market through to the per-market α aggregator.
type recordingAlphaAdapter struct {
	stubAdapter
	mu      sync.Mutex
	markets []string
}

func (r *recordingAlphaAdapter) GetSlippageAlpha(_ context.Context, market string) (float64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.markets = append(r.markets, market)
	return 1.0, nil
}

// TestSlippage_UsesMarketKey is the B7 follow-up regression: the worker
// MUST forward FeatureDTO.Market into the slippage model so per-market α
// resolution succeeds. Pre-fix, the worker called Estimate(...) with no
// market and every call collapsed to the DefaultAlpha cold-start path.
func TestSlippage_UsesMarketKey(t *testing.T) {
	adapter := &recordingAlphaAdapter{}
	cfg := &config.Config{}
	cfg.Capital.FixedEntrySizeUsd = 50.0

	w := NewSlippageWorker(adapter, cfg, nil)

	feat := contracts.FeatureDTO{
		EventID:          "feat-1",
		TraceID:          "trace-1",
		CorrelationID:    "corr-1",
		VersionID:        "v1",
		TokenLifecycleID: "lc-1",
		TokenAddress:     "0xabc",
		Market:           "eth-uniswap-v2",
		Chain:            "eth",
		LiquidityScore:   0.5,
		LiquidityUsdRaw:  100_000,
		PriceMomentum:    0.5,
		Confidence:       contracts.FeatureConfidence{LiquidityScore: 0.9},
		ExtractedAt:      "2026-01-01T00:00:00Z",
	}
	payload, err := json.Marshal(feat)
	if err != nil {
		t.Fatalf("marshal feature: %v", err)
	}
	evt := &database.Event{
		EventID:       "evt-feat-1",
		EventType:     "feature_event",
		Payload:       payload,
		TraceID:       feat.TraceID,
		CorrelationID: feat.CorrelationID,
		VersionID:     feat.VersionID,
	}

	if _, err := w.Process(context.Background(), evt); err != nil {
		t.Fatalf("Process returned error: %v", err)
	}

	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	if len(adapter.markets) == 0 {
		t.Fatal("GetSlippageAlpha was not called")
	}
	got := adapter.markets[0]
	if got != "eth-uniswap-v2" {
		t.Fatalf("slippage worker passed market=%q, want %q", got, "eth-uniswap-v2")
	}
}
