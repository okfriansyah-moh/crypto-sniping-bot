package models

// Phase 4 latency model: rolling p50/p95 per chain over a configurable window.
// Periodic worker calls Profile() to emit a LatencyProfileDTO snapshot.

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"crypto-sniping-bot/contracts"
)

// LatencyConfig holds the rolling-window and fallback priors.
type LatencyConfig struct {
	WindowSeconds int32 `yaml:"window_seconds"`
	MinSamples    int   `yaml:"min_samples"`
	FallbackP50Ms int32 `yaml:"fallback_p50_ms"`
	FallbackP95Ms int32 `yaml:"fallback_p95_ms"`
}

// DefaultLatencyConfig returns conservative defaults.
func DefaultLatencyConfig() LatencyConfig {
	return LatencyConfig{
		WindowSeconds: 300,
		MinSamples:    8,
		FallbackP50Ms: 250,
		FallbackP95Ms: 900,
	}
}

// latencySample is a single observation in the rolling window.
type latencySample struct {
	at    time.Time
	durMs int64
}

// LatencyModel maintains a per-chain rolling buffer of RPC latency samples
// and emits LatencyProfileDTO snapshots on demand.
//
// Concurrency: all methods are safe for concurrent use.
type LatencyModel struct {
	cfg     LatencyConfig
	mu      sync.Mutex
	samples map[string][]latencySample
	now     func() time.Time
}

// NewLatencyModel constructs a LatencyModel with empty per-chain buffers.
func NewLatencyModel(cfg LatencyConfig) *LatencyModel {
	if cfg.WindowSeconds <= 0 {
		cfg.WindowSeconds = 300
	}
	return &LatencyModel{
		cfg:     cfg,
		samples: make(map[string][]latencySample),
		now:     func() time.Time { return time.Now().UTC() },
	}
}

// Record stores a single latency observation for the given chain.
// Out-of-window entries are evicted lazily on every call.
func (m *LatencyModel) Record(chain string, dur time.Duration) {
	if chain == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	cutoff := now.Add(-time.Duration(m.cfg.WindowSeconds) * time.Second)
	buf := m.samples[chain]
	keep := buf[:0]
	for _, s := range buf {
		if s.at.After(cutoff) {
			keep = append(keep, s)
		}
	}
	keep = append(keep, latencySample{at: now, durMs: dur.Milliseconds()})
	m.samples[chain] = keep
}

// Profile returns a LatencyProfileDTO snapshot for the given chain.
// Falls back to configured priors when fewer than MinSamples are available.
// Deterministic given the current sample set.
func (m *LatencyModel) Profile(_ context.Context, chain string) (contracts.LatencyProfileDTO, error) {
	m.mu.Lock()
	now := m.now()
	cutoff := now.Add(-time.Duration(m.cfg.WindowSeconds) * time.Second)
	buf := m.samples[chain]
	keep := buf[:0]
	for _, s := range buf {
		if s.at.After(cutoff) {
			keep = append(keep, s)
		}
	}
	m.samples[chain] = keep
	durs := make([]int64, len(keep))
	for i, s := range keep {
		durs[i] = s.durMs
	}
	m.mu.Unlock()

	var p50, p95 int32
	if len(durs) < m.cfg.MinSamples {
		p50 = m.cfg.FallbackP50Ms
		p95 = m.cfg.FallbackP95Ms
	} else {
		sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
		p50 = int32(durs[percentileIndex(len(durs), 0.50)])
		p95 = int32(durs[percentileIndex(len(durs), 0.95)])
	}

	// windowEpoch = floor(now / windowSize). Stable per-window; provides idempotent insertion.
	windowEpoch := now.Unix() / int64(m.cfg.WindowSeconds)
	traceSrc := fmt.Sprintf("latency:%s:%d", chain, windowEpoch)
	traceID := contracts.ContentIDFromString(traceSrc)
	eventID := contracts.ContentIDFromString("evt:" + traceSrc)

	return contracts.LatencyProfileDTO{
		EventID:           eventID,
		TraceID:           traceID,
		CorrelationID:     traceID,
		CausationID:       "",
		VersionID:         "",
		Chain:             chain,
		ExpectedP50Ms:     p50,
		ExpectedP95Ms:     p95,
		WindowSizeSeconds: m.cfg.WindowSeconds,
		EstimatedAt:       now.Format(time.RFC3339Nano),
	}, nil
}

// percentileIndex returns a deterministic index into a sorted slice for q∈[0,1].
func percentileIndex(n int, q float64) int {
	if n <= 0 {
		return 0
	}
	idx := int(float64(n-1) * q)
	if idx < 0 {
		return 0
	}
	if idx >= n {
		return n - 1
	}
	return idx
}
