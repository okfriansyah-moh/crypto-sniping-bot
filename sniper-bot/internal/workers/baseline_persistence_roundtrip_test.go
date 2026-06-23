package workers

import (
	"context"
	"sort"
	"sync"
	"testing"
)

// memBaselineAdapter is an in-memory adapter that exercises the
// SaveBaseline / LoadBaselines contract — second save with the same
// (module, market, signal) key MUST overwrite. Mirrors the postgres
// implementation's `INSERT ... ON CONFLICT DO UPDATE SET values=$4`.
type memBaselineAdapter struct {
	*stubAdapter
	mu   sync.Mutex
	rows map[string]map[string]map[string][]float64 // module → market → signal → values
}

func newMemBaselineAdapter() *memBaselineAdapter {
	return &memBaselineAdapter{
		stubAdapter: &stubAdapter{},
		rows:        make(map[string]map[string]map[string][]float64),
	}
}

func (a *memBaselineAdapter) SaveBaseline(_ context.Context, module, market, signal string, values []float64) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	byMarket, ok := a.rows[module]
	if !ok {
		byMarket = make(map[string]map[string][]float64)
		a.rows[module] = byMarket
	}
	bySignal, ok := byMarket[market]
	if !ok {
		bySignal = make(map[string][]float64)
		byMarket[market] = bySignal
	}
	bySignal[signal] = append([]float64(nil), values...)
	return nil
}

func (a *memBaselineAdapter) LoadBaselines(_ context.Context, module string) (map[string]map[string][]float64, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make(map[string]map[string][]float64)
	src := a.rows[module]
	for market, bySig := range src {
		dst := make(map[string][]float64, len(bySig))
		for sig, vals := range bySig {
			dst[sig] = append([]float64(nil), vals...)
		}
		out[market] = dst
	}
	return out, nil
}

// TestSaveBaseline_OnConflictUpdates: second save with same key overwrites.
func TestSaveBaseline_OnConflictUpdates(t *testing.T) {
	a := newMemBaselineAdapter()
	ctx := context.Background()

	if err := a.SaveBaseline(ctx, "features", "eth-uniswap-v2", "tx_velocity", []float64{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveBaseline(ctx, "features", "eth-uniswap-v2", "tx_velocity", []float64{9, 9}); err != nil {
		t.Fatal(err)
	}
	loaded, err := a.LoadBaselines(ctx, "features")
	if err != nil {
		t.Fatal(err)
	}
	got := loaded["eth-uniswap-v2"]["tx_velocity"]
	if len(got) != 2 || got[0] != 9 || got[1] != 9 {
		t.Errorf("expected [9,9] after upsert, got %v", got)
	}
}

// TestLoadBaselines_PerModuleScope: rows under one module never bleed
// into another module's snapshot.
func TestLoadBaselines_PerModuleScope(t *testing.T) {
	a := newMemBaselineAdapter()
	ctx := context.Background()
	_ = a.SaveBaseline(ctx, "features", "m", "sig_f", []float64{1})
	_ = a.SaveBaseline(ctx, "edge", "m", "sig_e", []float64{2})

	feat, _ := a.LoadBaselines(ctx, "features")
	if _, has := feat["m"]["sig_e"]; has {
		t.Error("edge signal leaked into features module load")
	}
	if got := feat["m"]["sig_f"]; len(got) != 1 || got[0] != 1 {
		t.Errorf("features sig_f wrong: %v", got)
	}

	edgeRows, _ := a.LoadBaselines(ctx, "edge")
	if _, has := edgeRows["m"]["sig_f"]; has {
		t.Error("features signal leaked into edge module load")
	}
}

// TestFeaturesFlusher_RoundTrip: full Append → flush → Hydrate path.
func TestFeaturesFlusher_RoundTrip(t *testing.T) {
	a := newMemBaselineAdapter()
	ctx := context.Background()

	w1 := NewFeaturesWorker(a, minConfig(), nil)
	w1.baseline.AppendBatch("eth-uniswap-v2", map[string]float64{
		"tx_velocity":    1,
		"liquidity_size": 100,
	})
	w1.baseline.Append("eth-uniswap-v2", "tx_velocity", 2)

	flusher1 := &featuresBaselineFlusher{adapter: a, store: w1.baseline}
	if writes, _ := flushBaselineCycle(ctx, w1.logger, baselineModuleFeatures, 100, flusher1); writes != 2 {
		t.Fatalf("expected 2 writes, got %d", writes)
	}

	// Restart: new worker hydrates from DB.
	w2 := NewFeaturesWorker(a, minConfig(), nil)
	w2.HydrateBaselines(ctx)

	got := w2.baseline.Snapshot("eth-uniswap-v2")
	if vel := got.HistoryFor("tx_velocity"); len(vel) != 2 || vel[0] != 1 || vel[1] != 2 {
		t.Errorf("tx_velocity after rehydrate wrong: %v", vel)
	}
	if liq := got.HistoryFor("liquidity_size"); len(liq) != 1 || liq[0] != 100 {
		t.Errorf("liquidity_size after rehydrate wrong: %v", liq)
	}

	// Hydrated entries are NOT dirty — no spurious writes on next flush.
	if keys := w2.baseline.DirtyKeys(); len(keys) != 0 {
		t.Errorf("hydrated worker should have no dirty keys, got %v", keys)
	}
}

// TestEdgeFlusher_RoundTrip mirrors the features round-trip for the edge
// worker so both module discriminators are exercised.
func TestEdgeFlusher_RoundTrip(t *testing.T) {
	a := newMemBaselineAdapter()
	ctx := context.Background()

	w1 := NewEdgeWorker(a, minConfig(), nil)
	w1.baseline.AppendBatch("global", map[string]float64{
		"price_momentum":  0.1,
		"volume_momentum": 0.2,
	})

	flusher1 := &edgeBaselineFlusher{adapter: a, store: w1.baseline}
	if writes, _ := flushBaselineCycle(ctx, w1.logger, baselineModuleEdge, 100, flusher1); writes != 2 {
		t.Fatalf("expected 2 writes, got %d", writes)
	}

	w2 := NewEdgeWorker(a, minConfig(), nil)
	w2.HydrateBaselines(ctx)
	got := w2.baseline.Snapshot("global")

	signals := []string{"price_momentum", "volume_momentum"}
	sort.Strings(signals)
	for _, sig := range signals {
		v := got.HistoryFor(sig)
		if len(v) != 1 {
			t.Errorf("rehydrated %s wrong: %v", sig, v)
		}
	}
}
