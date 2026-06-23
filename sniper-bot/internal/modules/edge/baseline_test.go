package edge

import (
	"math"
	"testing"
)

func TestBaselineStore_AppendAndSnapshot(t *testing.T) {
	s := NewBaselineStore(4)
	s.Append("eth", SignalPriceMomentum, 0.1)
	s.Append("eth", SignalPriceMomentum, 0.2)
	s.Append("eth", SignalVolumeMomentum, 0.5)

	snap := s.Snapshot("eth")
	prices := snap.HistoryFor(SignalPriceMomentum)
	if len(prices) != 2 || prices[0] != 0.1 || prices[1] != 0.2 {
		t.Errorf("price history wrong: %v", prices)
	}
	vols := snap.HistoryFor(SignalVolumeMomentum)
	if len(vols) != 1 || vols[0] != 0.5 {
		t.Errorf("volume history wrong: %v", vols)
	}
}

func TestBaselineStore_RingBufferEvictsOldest(t *testing.T) {
	s := NewBaselineStore(3)
	for i := 0; i < 10; i++ {
		s.Append("eth", SignalPriceMomentum, float64(i))
	}
	snap := s.Snapshot("eth")
	got := snap.HistoryFor(SignalPriceMomentum)
	if len(got) != 3 {
		t.Fatalf("expected ring buffer len 3, got %d", len(got))
	}
	if got[0] != 7 || got[1] != 8 || got[2] != 9 {
		t.Errorf("expected last 3 values, got %v", got)
	}
}

func TestBaselineStore_AppendBatch(t *testing.T) {
	s := NewBaselineStore(0) // defaults to 256
	s.AppendBatch("eth", map[string]float64{
		SignalPriceMomentum:  0.7,
		SignalVolumeMomentum: 0.6,
	})
	snap := s.Snapshot("eth")
	if snap.HistoryFor(SignalPriceMomentum)[0] != 0.7 {
		t.Error("price not appended")
	}
	if snap.HistoryFor(SignalVolumeMomentum)[0] != 0.6 {
		t.Error("volume not appended")
	}
}

func TestBaselineStore_EmptyMarketSafe(t *testing.T) {
	s := NewBaselineStore(8)
	snap := s.Snapshot("nonexistent")
	if got := snap.HistoryFor("anything"); got != nil {
		t.Errorf("expected nil for missing signal, got %v", got)
	}
}

func TestBaselineStore_EmptyKeysIgnored(t *testing.T) {
	s := NewBaselineStore(4)
	s.Append("", SignalPriceMomentum, 1) // ignored
	s.Append("eth", "", 1)               // ignored
	if got := s.Snapshot("eth").HistoryFor(SignalPriceMomentum); len(got) != 0 {
		t.Errorf("expected empty history, got %v", got)
	}
}

func TestBaselineStore_SnapshotIsImmutable(t *testing.T) {
	s := NewBaselineStore(8)
	s.Append("eth", SignalPriceMomentum, 0.5)
	snap := s.Snapshot("eth")
	snap.HistoryFor(SignalPriceMomentum)[0] = 99.9 // mutate the copy
	again := s.Snapshot("eth")
	if again.HistoryFor(SignalPriceMomentum)[0] != 0.5 {
		t.Error("snapshot must be a defensive copy")
	}
}

// ── quantile ─────────────────────────────────────────────────────────────────

func TestQuantile_EmptyAndSingle(t *testing.T) {
	if quantile(nil, 0.5) != 0 {
		t.Error("nil → 0")
	}
	if quantile([]float64{}, 0.5) != 0 {
		t.Error("empty → 0")
	}
	if quantile([]float64{0.42}, 0.9) != 0.42 {
		t.Error("single → value")
	}
}

func TestQuantile_Sorted(t *testing.T) {
	vals := []float64{0, 0.25, 0.5, 0.75, 1.0}
	if got := quantile(vals, 0); got != 0 {
		t.Errorf("q0 expected 0, got %f", got)
	}
	if got := quantile(vals, 1); got != 1 {
		t.Errorf("q1 expected 1, got %f", got)
	}
	if got := quantile(vals, 0.5); math.Abs(got-0.5) > 1e-9 {
		t.Errorf("q0.5 expected 0.5, got %f", got)
	}
}

func TestQuantile_DoesNotMutateInput(t *testing.T) {
	vals := []float64{3, 1, 2}
	_ = quantile(vals, 0.5)
	if vals[0] != 3 || vals[1] != 1 || vals[2] != 2 {
		t.Errorf("input must not be mutated, got %v", vals)
	}
}

func TestQuantile_ClampsQuantile(t *testing.T) {
	vals := []float64{0, 1, 2}
	if got := quantile(vals, -1); got != 0 {
		t.Errorf("q<0 should clamp to q=0, got %f", got)
	}
	if got := quantile(vals, 99); got != 2 {
		t.Errorf("q>1 should clamp to q=1, got %f", got)
	}
}
