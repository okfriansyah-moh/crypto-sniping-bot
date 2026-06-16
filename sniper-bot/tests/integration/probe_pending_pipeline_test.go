// Package integration — probe pending queue pipeline tests.
package integration

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/sniper-bot/internal/modules/probes"
	"crypto-sniping-bot/sniper-bot/internal/workers"
)

type stubCreditProbe struct{}

func (stubCreditProbe) Name() string { return "solana_holder_dist" }
func (stubCreditProbe) Probe(ctx context.Context, in contracts.MarketDataDTO) (contracts.MarketDataDTO, error) {
	return in, nil
}

type probePendingRecorder struct {
	memAdapter
	mu           sync.Mutex
	pending      []database.ProbePendingEnqueue
	claimed      []database.ProbePendingRow
	marketData   []contracts.MarketDataDTO
	insertEvents []database.Event
}

func newProbePendingRecorder() *probePendingRecorder {
	r := &probePendingRecorder{}
	r.memAdapter.runs = make(map[string]*database.PipelineRun)
	return r
}

func (r *probePendingRecorder) EnqueueProbePending(_ context.Context, req database.ProbePendingEnqueue) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending = append(r.pending, req)
	return nil
}

func (r *probePendingRecorder) ClaimDueProbePending(_ context.Context, limit int) ([]database.ProbePendingRow, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if limit <= 0 {
		limit = 10
	}
	now := time.Now().UTC()
	var out []database.ProbePendingRow
	remaining := make([]database.ProbePendingEnqueue, 0, len(r.pending))
	for _, p := range r.pending {
		if len(out) < limit && !p.DueAt.After(now) {
			out = append(out, database.ProbePendingRow{
				PendingID:     p.PendingID,
				SourceEventID: p.SourceEventID,
				TokenAddress:  p.TokenAddress,
				Chain:         p.Chain,
				Market:        p.Market,
				Priority:      p.Priority,
				Payload:       p.Payload,
				EnqueuedAt:    p.EnqueuedAt,
				DueAt:         p.DueAt,
			})
		} else {
			remaining = append(remaining, p)
		}
	}
	r.pending = remaining
	return out, nil
}

func (r *probePendingRecorder) CompleteProbePending(_ context.Context, _ string) error { return nil }
func (r *probePendingRecorder) FailProbePending(_ context.Context, _, _ string, _ int) error {
	return nil
}
func (r *probePendingRecorder) ExpireStaleProbePending(_ context.Context, _ int) (int64, error) {
	return 0, nil
}
func (r *probePendingRecorder) GetProbePendingStats(_ context.Context) (*database.ProbePendingStats, error) {
	return &database.ProbePendingStats{}, nil
}
func (r *probePendingRecorder) GetLatestMarketDataForToken(_ context.Context, _, _ string) (*contracts.MarketDataDTO, error) {
	return nil, nil
}
func (r *probePendingRecorder) InsertMarketData(_ context.Context, dto contracts.MarketDataDTO) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.marketData = append(r.marketData, dto)
	return nil
}

func (r *probePendingRecorder) InsertEvent(_ context.Context, evt database.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.insertEvents {
		if existing.EventID == evt.EventID {
			return nil
		}
	}
	r.insertEvents = append(r.insertEvents, evt)
	return nil
}

func TestProbePending_RateLimitedTokenEnqueuesWithoutEnrichedEvent(t *testing.T) {
	rec := newProbePendingRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	probeWorker := workers.NewMarketProbesWorker(rec, []probes.MarketProbe{stubCreditProbe{}}, logger).
		WithProbeBudget(config.ProbesConfig{
			MaxProbeCreditsPerHour: 1,
			ProbeCreditCosts: map[string]int{
				"solana_holder_dist": 5,
			},
			PendingQueue: config.ProbePendingQueueConfig{Enabled: true},
		})

	payload, _ := json.Marshal(contracts.MarketDataDTO{
		EventID:      "src-evt-1",
		TokenAddress: "tokenA",
		Chain:        "solana",
		Market:       "solana-pumpfun",
		Transport:    "websocket",
	})
	evt := &database.Event{
		EventID:   "src-evt-1",
		EventType: "market_data_event",
		Payload:   payload,
	}

	out, err := probeWorker.Process(context.Background(), evt)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if out != nil {
		t.Fatal("rate-limited token must not emit market_data_enriched")
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.pending) != 1 {
		t.Fatalf("want 1 pending row, got %d", len(rec.pending))
	}
}

func TestProbePending_DrainReEmitsMarketDataEvent(t *testing.T) {
	rec := newProbePendingRecorder()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	rec.pending = []database.ProbePendingEnqueue{{
		PendingID:     "pend-1",
		SourceEventID: "src-1",
		TokenAddress:  "tokenB",
		Chain:         "solana",
		Market:        "solana-pumpfun",
		Payload: contracts.MarketDataDTO{
			EventID:      "src-1",
			TokenAddress: "tokenB",
			Chain:        "solana",
			Market:       "solana-pumpfun",
			VersionID:    "v1",
		},
		DueAt: time.Now().UTC().Add(-time.Minute),
	}}

	cfg := &config.Config{
		Probes: config.ProbesConfig{
			PendingQueue: config.ProbePendingQueueConfig{
				Enabled:              true,
				DrainIntervalSeconds: 1,
				DrainBatchSize:         1,
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	_ = workers.RunProbePendingWorker(ctx, rec, cfg, logger)

	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.insertEvents) == 0 {
		t.Fatal("expected re-emitted market_data_event after drain")
	}
	if rec.insertEvents[0].EventType != "market_data_event" {
		t.Fatalf("event type = %q, want market_data_event", rec.insertEvents[0].EventType)
	}
}
