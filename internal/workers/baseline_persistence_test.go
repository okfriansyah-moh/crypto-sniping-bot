package workers

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/edge"
	"crypto-sniping-bot/internal/modules/features"
)

// ── Hydrate-on-startup ────────────────────────────────────────────────────────

// loadingStubAdapter wraps stubAdapter and returns a canned snapshot from
// LoadBaselines, so we can verify hydrate-on-startup populates the worker's
// in-memory store.
type loadingStubAdapter struct {
	*stubAdapter
	loaded   map[string]map[string][]float64
	loadedBy atomic.Value // last `module` arg observed
	loadErr  error
}

func (a *loadingStubAdapter) LoadBaselines(_ context.Context, module string) (map[string]map[string][]float64, error) {
	a.loadedBy.Store(module)
	if a.loadErr != nil {
		return nil, a.loadErr
	}
	return a.loaded, nil
}

func TestRunFeatures_HydratesOnStartup(t *testing.T) {
	adapter := &loadingStubAdapter{
		stubAdapter: &stubAdapter{},
		loaded: map[string]map[string][]float64{
			"eth-uniswap-v2": {
				"liquidity_size": {1, 2, 3},
				"tx_velocity":    {7},
			},
		},
	}
	w := NewFeaturesWorker(adapter, minConfig(), nil)
	w.HydrateBaselines(context.Background())

	if got, ok := adapter.loadedBy.Load().(string); !ok || got != baselineModuleFeatures {
		t.Fatalf("LoadBaselines not invoked with correct module, got %v", adapter.loadedBy.Load())
	}
	got := w.baseline.Snapshot("eth-uniswap-v2").HistoryFor("liquidity_size")
	if len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Errorf("expected [1,2,3] in features baseline, got %v", got)
	}
}

func TestRunFeatures_HydrateLoadFailure_ContinuesWithEmpty(t *testing.T) {
	adapter := &loadingStubAdapter{
		stubAdapter: &stubAdapter{},
		loadErr:     errors.New("boom"),
	}
	w := NewFeaturesWorker(adapter, minConfig(), nil)
	// Must NOT panic; baselines remain empty.
	w.HydrateBaselines(context.Background())

	if got := w.baseline.Snapshot("any").HistoryFor("any"); got != nil {
		t.Errorf("expected empty baseline on load failure, got %v", got)
	}
}

func TestRunEdge_HydratesOnStartup(t *testing.T) {
	adapter := &loadingStubAdapter{
		stubAdapter: &stubAdapter{},
		loaded: map[string]map[string][]float64{
			"global": {
				edge.SignalPriceMomentum: {0.1, 0.2, 0.3},
			},
		},
	}
	w := NewEdgeWorker(adapter, minConfig(), nil)
	w.HydrateBaselines(context.Background())

	if got, ok := adapter.loadedBy.Load().(string); !ok || got != baselineModuleEdge {
		t.Fatalf("LoadBaselines not invoked with correct module, got %v", adapter.loadedBy.Load())
	}
	got := w.baseline.Snapshot("global").HistoryFor(edge.SignalPriceMomentum)
	if len(got) != 3 || got[2] != 0.3 {
		t.Errorf("expected [0.1,0.2,0.3] in edge baseline, got %v", got)
	}
}

// ── Flush cycle ───────────────────────────────────────────────────────────────

// recordingSaveAdapter records every SaveBaseline call so the test can assert
// on the (module, market, signal, values) tuples written.
type recordingSaveAdapter struct {
	*stubAdapter
	mu     atomic.Pointer[saveLog]
	saveFn func(module, market, signal string, values []float64) error
}

type saveCall struct {
	Module string
	Market string
	Signal string
	Values []float64
}

type saveLog struct {
	calls []saveCall
}

func (a *recordingSaveAdapter) SaveBaseline(_ context.Context, module, market, signal string, values []float64) error {
	if a.saveFn != nil {
		if err := a.saveFn(module, market, signal, values); err != nil {
			return err
		}
	}
	for {
		old := a.mu.Load()
		var next saveLog
		if old != nil {
			next.calls = append(next.calls, old.calls...)
		}
		next.calls = append(next.calls, saveCall{Module: module, Market: market, Signal: signal, Values: append([]float64(nil), values...)})
		if a.mu.CompareAndSwap(old, &next) {
			return nil
		}
	}
}

func (a *recordingSaveAdapter) calls() []saveCall {
	v := a.mu.Load()
	if v == nil {
		return nil
	}
	return v.calls
}

func TestFlushBaselineCycle_FeaturesPersistsDirtyKeys(t *testing.T) {
	adapter := &recordingSaveAdapter{stubAdapter: &stubAdapter{}}
	w := NewFeaturesWorker(adapter, minConfig(), nil)

	// Append values — these mark dirty.
	w.baseline.AppendBatch("eth-uniswap-v2", map[string]float64{
		"liquidity_size": 1.0,
		"tx_velocity":    7.0,
	})
	w.baseline.Append("bsc-pancake-v2", "liquidity_size", 9.0)

	flusher := &featuresBaselineFlusher{adapter: adapter, store: w.baseline}
	writes, deferred := flushBaselineCycle(
		context.Background(), w.logger, baselineModuleFeatures,
		100, // generous max
		flusher,
	)
	if writes != 3 || deferred != 0 {
		t.Fatalf("expected 3 writes, 0 deferred, got writes=%d deferred=%d", writes, deferred)
	}

	calls := adapter.calls()
	if len(calls) != 3 {
		t.Fatalf("expected 3 SaveBaseline calls, got %d", len(calls))
	}
	for _, c := range calls {
		if c.Module != baselineModuleFeatures {
			t.Errorf("wrong module: %s", c.Module)
		}
	}

	// After successful flush, dirty set is empty.
	if keys := w.baseline.DirtyKeys(); len(keys) != 0 {
		t.Errorf("expected dirty cleared after flush, got %v", keys)
	}

	// Re-flush is a no-op (no dirty keys).
	writes2, _ := flushBaselineCycle(
		context.Background(), w.logger, baselineModuleFeatures,
		100, flusher,
	)
	if writes2 != 0 {
		t.Errorf("expected 0 writes on second cycle, got %d", writes2)
	}
}

func TestFlushBaselineCycle_Throttles_DeferredKeysStayDirty(t *testing.T) {
	adapter := &recordingSaveAdapter{stubAdapter: &stubAdapter{}}
	w := NewEdgeWorker(adapter, minConfig(), nil)

	// 3 dirty keys, max writes = 2.
	w.baseline.Append("m1", "sig_a", 1)
	w.baseline.Append("m1", "sig_b", 2)
	w.baseline.Append("m2", "sig_a", 3)

	flusher := &edgeBaselineFlusher{adapter: adapter, store: w.baseline}
	writes, deferred := flushBaselineCycle(
		context.Background(), w.logger, baselineModuleEdge,
		2, flusher,
	)
	if writes != 2 || deferred != 1 {
		t.Fatalf("expected 2 writes, 1 deferred, got writes=%d deferred=%d", writes, deferred)
	}

	// One key should still be dirty for the next cycle (deterministic
	// sort: third key in lexical order is (m2, sig_a)).
	remaining := w.baseline.DirtyKeys()
	if len(remaining) != 1 {
		t.Fatalf("expected 1 deferred dirty key, got %v", remaining)
	}
	if remaining[0].Market != "m2" || remaining[0].Signal != "sig_a" {
		t.Errorf("expected (m2, sig_a) to remain dirty, got %v", remaining[0])
	}
}

func TestFlushBaselineCycle_SaveErrorKeepsDirty(t *testing.T) {
	adapter := &recordingSaveAdapter{
		stubAdapter: &stubAdapter{},
		saveFn: func(_, market, signal string, _ []float64) error {
			if market == "m1" && signal == "sig_a" {
				return errors.New("transient db error")
			}
			return nil
		},
	}
	w := NewFeaturesWorker(adapter, minConfig(), nil)
	w.baseline.Append("m1", "sig_a", 1)
	w.baseline.Append("m1", "sig_b", 2)

	flusher := &featuresBaselineFlusher{adapter: adapter, store: w.baseline}
	writes, _ := flushBaselineCycle(
		context.Background(), w.logger, baselineModuleFeatures,
		100, flusher,
	)
	if writes != 1 {
		t.Errorf("expected 1 successful write, got %d", writes)
	}
	// Failed key stays dirty for retry next cycle.
	remaining := w.baseline.DirtyKeys()
	if len(remaining) != 1 || remaining[0].Signal != "sig_a" {
		t.Errorf("expected sig_a still dirty, got %v", remaining)
	}
}

func TestFlushBaselineCycle_NoDirtyKeys_IsNoop(t *testing.T) {
	adapter := &recordingSaveAdapter{stubAdapter: &stubAdapter{}}
	w := NewFeaturesWorker(adapter, minConfig(), nil)
	flusher := &featuresBaselineFlusher{adapter: adapter, store: w.baseline}
	writes, deferred := flushBaselineCycle(
		context.Background(), w.logger, baselineModuleFeatures,
		100, flusher,
	)
	if writes != 0 || deferred != 0 {
		t.Errorf("expected zero work, got writes=%d deferred=%d", writes, deferred)
	}
	if len(adapter.calls()) != 0 {
		t.Errorf("expected no SaveBaseline calls, got %d", len(adapter.calls()))
	}
}

// ── Worker construction reads new config keys ─────────────────────────────────

func TestNewFeaturesWorker_ReadsBaselineFlushConfig(t *testing.T) {
	cfg := minConfig()
	cfg.Feature = config.FeatureRuntimeConfig{
		BaselineFlushIntervalSec: 60,
		BaselineFlushMaxWrites:   42,
	}
	w := NewFeaturesWorker(&stubAdapter{}, cfg, nil)
	if w.flushIntervalSec != 60 {
		t.Errorf("expected flushIntervalSec=60, got %d", w.flushIntervalSec)
	}
	if w.flushMaxWrites != 42 {
		t.Errorf("expected flushMaxWrites=42, got %d", w.flushMaxWrites)
	}
}

func TestNewEdgeWorker_ReadsBaselineFlushConfig(t *testing.T) {
	cfg := minConfig()
	cfg.Edge.BaselineFlushIntervalSec = 60
	cfg.Edge.BaselineFlushMaxWrites = 42
	w := NewEdgeWorker(&stubAdapter{}, cfg, nil)
	if w.flushIntervalSec != 60 {
		t.Errorf("expected flushIntervalSec=60, got %d", w.flushIntervalSec)
	}
	if w.flushMaxWrites != 42 {
		t.Errorf("expected flushMaxWrites=42, got %d", w.flushMaxWrites)
	}
}

// Compile-time guard: features.BaselineKey and edge.BaselineKey both
// produce the same shape so the worker-level baselineFlushKey can carry
// them through the shared flusher loop.
var _ = features.BaselineKey{}
var _ = edge.BaselineKey{}
