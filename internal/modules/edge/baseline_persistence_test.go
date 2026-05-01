package edge

import (
	"testing"
)

func TestBaselineStore_Hydrate_PopulatesFromMap(t *testing.T) {
	s := NewBaselineStore(4)
	s.Hydrate(map[string]map[string][]float64{
		"global": {
			SignalPriceMomentum:  {0.1, 0.2, 0.3},
			SignalVolumeMomentum: {0.5, 0.6, 0.7, 0.8, 0.9}, // > maxLen
		},
	})
	prices := s.Snapshot("global").HistoryFor(SignalPriceMomentum)
	if len(prices) != 3 || prices[0] != 0.1 || prices[2] != 0.3 {
		t.Errorf("price history wrong after hydrate: %v", prices)
	}
	vols := s.Snapshot("global").HistoryFor(SignalVolumeMomentum)
	if len(vols) != 4 {
		t.Errorf("expected bounded to maxLen=4, got %d", len(vols))
	}
	if vols[0] != 0.6 || vols[3] != 0.9 {
		t.Errorf("expected trailing window [0.6..0.9], got %v", vols)
	}
	if keys := s.DirtyKeys(); len(keys) != 0 {
		t.Errorf("Hydrate must not mark dirty, got %v", keys)
	}
}

func TestBaselineStore_DirtyTracking_TracksAppends(t *testing.T) {
	s := NewBaselineStore(8)
	s.AppendBatch("global", map[string]float64{
		SignalPriceMomentum:  0.4,
		SignalVolumeMomentum: 0.6,
	})
	keys := s.DirtyKeys()
	if len(keys) != 2 {
		t.Fatalf("expected 2 dirty keys, got %d", len(keys))
	}
	// AppendBatch should mark BOTH signals.
	seen := map[string]bool{}
	for _, k := range keys {
		seen[k.Signal] = true
	}
	if !seen[SignalPriceMomentum] || !seen[SignalVolumeMomentum] {
		t.Errorf("expected both signals dirty, got %v", keys)
	}
}

func TestBaselineStore_DirtyTracking_MarkCleanRemovesOnly(t *testing.T) {
	s := NewBaselineStore(8)
	s.Append("global", SignalPriceMomentum, 0.4)
	s.Append("global", SignalVolumeMomentum, 0.6)
	s.MarkClean("global", SignalPriceMomentum)

	keys := s.DirtyKeys()
	if len(keys) != 1 || keys[0].Signal != SignalVolumeMomentum {
		t.Errorf("expected only volume dirty, got %v", keys)
	}
}

func TestBaselineStore_DirtyTracking_ClearsAfterReset(t *testing.T) {
	s := NewBaselineStore(8)
	s.Append("global", SignalPriceMomentum, 0.4)
	s.ClearDirty()
	if keys := s.DirtyKeys(); len(keys) != 0 {
		t.Errorf("expected empty after ClearDirty, got %v", keys)
	}
}

func TestBaselineStore_Values_ReturnsDefensiveCopy(t *testing.T) {
	s := NewBaselineStore(8)
	s.Append("global", SignalPriceMomentum, 0.4)
	got := s.Values("global", SignalPriceMomentum)
	if len(got) != 1 || got[0] != 0.4 {
		t.Errorf("Values wrong: %v", got)
	}
	got[0] = 99
	again := s.Values("global", SignalPriceMomentum)
	if again[0] != 0.4 {
		t.Errorf("Values must be defensive copy")
	}
}
