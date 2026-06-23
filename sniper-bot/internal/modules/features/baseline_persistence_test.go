package features

import (
	"testing"
)

// Hydrate populates the store from a persisted snapshot, bounded by maxLen.
func TestBaselineStore_Hydrate_PopulatesFromMap(t *testing.T) {
	s := NewBaselineStore(4)
	src := map[string]map[string][]float64{
		"eth-uniswap-v2": {
			"liquidity_size": {1, 2, 3},
			"tx_velocity":    {7},
		},
		"bsc-pancake-v2": {
			"liquidity_size": {9, 9, 9, 9, 9}, // longer than maxLen=4
		},
	}
	s.Hydrate(src)

	got := s.Snapshot("eth-uniswap-v2").HistoryFor("liquidity_size")
	if len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Errorf("eth liquidity wrong: %v", got)
	}
	if got := s.Snapshot("eth-uniswap-v2").HistoryFor("tx_velocity"); len(got) != 1 || got[0] != 7 {
		t.Errorf("eth tx_velocity wrong: %v", got)
	}
	bsc := s.Snapshot("bsc-pancake-v2").HistoryFor("liquidity_size")
	if len(bsc) != 4 {
		t.Errorf("bsc should be bounded to maxLen=4, got %d", len(bsc))
	}

	// Hydrate must NOT mark entries dirty — there is nothing to write back.
	if keys := s.DirtyKeys(); len(keys) != 0 {
		t.Errorf("Hydrate should not mark dirty, got %v", keys)
	}
}

func TestBaselineStore_Hydrate_EmptySnapshotIsNoop(t *testing.T) {
	s := NewBaselineStore(4)
	s.Append("m", "sig", 1)
	s.Hydrate(nil)
	s.Hydrate(map[string]map[string][]float64{})
	if got := s.Snapshot("m").HistoryFor("sig"); len(got) != 1 || got[0] != 1 {
		t.Errorf("existing data must be preserved, got %v", got)
	}
}

func TestBaselineStore_DirtyTracking_TracksAppends(t *testing.T) {
	s := NewBaselineStore(8)
	s.Append("m1", "sig_a", 1)
	s.Append("m1", "sig_b", 2)
	s.AppendBatch("m2", map[string]float64{"sig_a": 3})

	got := s.DirtyKeys()
	if len(got) != 3 {
		t.Fatalf("expected 3 dirty keys, got %d: %v", len(got), got)
	}
	// Sorted by (Market, Signal).
	want := []BaselineKey{
		{"m1", "sig_a"}, {"m1", "sig_b"}, {"m2", "sig_a"},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("idx %d: got %v want %v", i, got[i], want[i])
		}
	}
}

func TestBaselineStore_DirtyTracking_MarkCleanRemovesOnly(t *testing.T) {
	s := NewBaselineStore(8)
	s.Append("m1", "sig_a", 1)
	s.Append("m1", "sig_b", 2)
	s.MarkClean("m1", "sig_a")

	keys := s.DirtyKeys()
	if len(keys) != 1 || keys[0] != (BaselineKey{"m1", "sig_b"}) {
		t.Errorf("expected only sig_b dirty, got %v", keys)
	}
}

func TestBaselineStore_DirtyTracking_ClearsAfterReset(t *testing.T) {
	s := NewBaselineStore(8)
	s.Append("m", "sig", 1)
	s.AppendBatch("m", map[string]float64{"other": 2})
	s.ClearDirty()
	if keys := s.DirtyKeys(); len(keys) != 0 {
		t.Errorf("expected empty after ClearDirty, got %v", keys)
	}

	// Subsequent Append re-marks dirty.
	s.Append("m", "sig", 99)
	if keys := s.DirtyKeys(); len(keys) != 1 {
		t.Errorf("expected 1 dirty after re-append, got %v", keys)
	}
}

func TestBaselineStore_Values_ReturnsDefensiveCopy(t *testing.T) {
	s := NewBaselineStore(8)
	s.Append("m", "sig", 1)
	s.Append("m", "sig", 2)
	got := s.Values("m", "sig")
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Errorf("Values wrong: %v", got)
	}
	got[0] = 99 // mutating the copy must not affect the store
	again := s.Values("m", "sig")
	if again[0] != 1 {
		t.Errorf("Values should return defensive copy, got %v", again)
	}

	if got := s.Values("missing", "sig"); got != nil {
		t.Errorf("expected nil for missing market, got %v", got)
	}
}
