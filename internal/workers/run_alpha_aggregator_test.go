package workers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/modules/execution_quality"
)

// alphaAggStub embeds workersStubAdapter behavior with focused overrides
// for the two methods exercised by the alpha aggregator.
type alphaAggStub struct {
	stubAdapter
	mu        sync.Mutex
	samples   map[string][]database.FillSample
	upserts   map[string]struct{ alpha, ep, er float64; n int }
	upsertErr error
}

func (a *alphaAggStub) GetRealizedFillSamples(_ context.Context, _ int) (map[string][]database.FillSample, error) {
	return a.samples, nil
}

func (a *alphaAggStub) UpsertSlippageAlpha(_ context.Context, market string, alpha, ep, er float64, n int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.upsertErr != nil {
		return a.upsertErr
	}
	if a.upserts == nil {
		a.upserts = make(map[string]struct{ alpha, ep, er float64; n int })
	}
	a.upserts[market] = struct{ alpha, ep, er float64; n int }{alpha, ep, er, n}
	return nil
}

func discardLoggerAlpha() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestAlphaAggregator_BelowMinSampleCount_PreservesExisting verifies the
// aggregator skips Upsert for markets below MinSampleCount, leaving any
// prior calibration row intact.
func TestAlphaAggregator_BelowMinSampleCount_PreservesExisting(t *testing.T) {
	now := time.Now().UTC()
	stub := &alphaAggStub{
		samples: map[string][]database.FillSample{
			"eth": {
				{PredictedBps: 100, RealizedBps: 120, At: now.Add(-30 * time.Second)},
				{PredictedBps: 100, RealizedBps: 130, At: now.Add(-60 * time.Second)},
				{PredictedBps: 100, RealizedBps: 110, At: now.Add(-90 * time.Second)},
				{PredictedBps: 100, RealizedBps: 140, At: now.Add(-120 * time.Second)},
				{PredictedBps: 100, RealizedBps: 150, At: now.Add(-150 * time.Second)},
			},
		},
	}
	cfg := execution_quality.AlphaAggregatorConfig{
		MinSampleCount:    30, // 5 < 30 → skipped
		AlphaMin:          0.5,
		AlphaMax:          2.0,
		EwmaHalflifeSec:   3600,
		UpdateIntervalSec: 300,
	}

	if err := runAlphaAggregatorOnce(context.Background(), stub, cfg, 14400, discardLoggerAlpha()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(stub.upserts) != 0 {
		t.Fatalf("expected no upserts (below MinSampleCount), got %v", stub.upserts)
	}
}

// TestAlphaAggregator_AboveMinSampleCount_Upserts verifies that with enough
// samples the aggregator persists per-market α.
func TestAlphaAggregator_AboveMinSampleCount_Upserts(t *testing.T) {
	now := time.Now().UTC()
	var samples []database.FillSample
	for i := 0; i < 10; i++ {
		samples = append(samples, database.FillSample{
			PredictedBps: 100, RealizedBps: 150,
			At: now.Add(-time.Duration(i) * time.Second),
		})
	}
	stub := &alphaAggStub{
		samples: map[string][]database.FillSample{"eth": samples},
	}
	cfg := execution_quality.AlphaAggregatorConfig{
		MinSampleCount:    5,
		AlphaMin:          0.5,
		AlphaMax:          2.0,
		EwmaHalflifeSec:   3600,
		UpdateIntervalSec: 300,
	}
	if err := runAlphaAggregatorOnce(context.Background(), stub, cfg, 14400, discardLoggerAlpha()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, ok := stub.upserts["eth"]
	if !ok {
		t.Fatalf("expected eth upsert, got %v", stub.upserts)
	}
	if got.n != 10 {
		t.Fatalf("expected n=10, got %d", got.n)
	}
	if got.alpha <= 1.0 || got.alpha > 2.0 {
		t.Fatalf("expected α in (1.0, 2.0], got %v", got.alpha)
	}
}

// TestAlphaAggregator_GetSamplesError_Propagates verifies adapter errors abort the cycle.
func TestAlphaAggregator_GetSamplesError_Propagates(t *testing.T) {
	stub := &errSamplesStub{}
	cfg := execution_quality.DefaultAlphaAggregatorConfig()
	err := runAlphaAggregatorOnce(context.Background(), stub, cfg, 14400, discardLoggerAlpha())
	if err == nil {
		t.Fatal("expected error")
	}
}

type errSamplesStub struct{ stubAdapter }

func (e *errSamplesStub) GetRealizedFillSamples(_ context.Context, _ int) (map[string][]database.FillSample, error) {
	return nil, errors.New("boom")
}
