package features

import (
	"sync"
	"testing"
)

func TestBaselineStore_AppendAndSnapshot(t *testing.T) {
	s := NewBaselineStore(5)
	s.Append("eth-uniswap-v2", "liquidity_size", 1.0)
	s.Append("eth-uniswap-v2", "liquidity_size", 2.0)
	s.Append("eth-uniswap-v2", "tx_velocity", 7.0)
	snap := s.Snapshot("eth-uniswap-v2")
	if got := snap.HistoryFor("liquidity_size"); len(got) != 2 || got[0] != 1.0 || got[1] != 2.0 {
		t.Errorf("liquidity history wrong: %v", got)
	}
	if got := snap.HistoryFor("tx_velocity"); len(got) != 1 || got[0] != 7.0 {
		t.Errorf("tx_velocity history wrong: %v", got)
	}
}

func TestBaselineStore_RingBufferEviction(t *testing.T) {
	s := NewBaselineStore(3)
	for i := 1; i <= 6; i++ {
		s.Append("m", "sig", float64(i))
	}
	got := s.Snapshot("m").HistoryFor("sig")
	if len(got) != 3 {
		t.Fatalf("expected window=3, got len=%d", len(got))
	}
	if got[0] != 4 || got[1] != 5 || got[2] != 6 {
		t.Errorf("expected [4,5,6], got %v", got)
	}
}

func TestBaselineStore_SnapshotIsIsolated(t *testing.T) {
	s := NewBaselineStore(10)
	s.Append("m", "sig", 1)
	snap := s.Snapshot("m")
	// Mutating the returned slice must not affect future snapshots.
	snap.HistoryFor("sig")[0] = 99
	again := s.Snapshot("m").HistoryFor("sig")
	if again[0] != 1 {
		t.Errorf("snapshot should be isolated, got %v", again)
	}
}

func TestBaselineStore_ConcurrentAppendIsSafe(t *testing.T) {
	s := NewBaselineStore(1000)
	var wg sync.WaitGroup
	for w := 0; w < 8; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				s.Append("m", "sig", float64(i))
			}
		}()
	}
	wg.Wait()
	if got := len(s.Snapshot("m").HistoryFor("sig")); got != 800 {
		t.Errorf("expected 800 samples after concurrent append, got %d", got)
	}
}

func TestBaselineStore_AppendBatchEqualsAppend(t *testing.T) {
	a := NewBaselineStore(10)
	a.Append("m", "x", 1)
	a.Append("m", "y", 2)

	b := NewBaselineStore(10)
	b.AppendBatch("m", map[string]float64{"x": 1, "y": 2})

	if got := a.Snapshot("m").HistoryFor("x"); len(got) != 1 || got[0] != 1 {
		t.Fatalf("a.x wrong: %v", got)
	}
	if got := b.Snapshot("m").HistoryFor("x"); len(got) != 1 || got[0] != 1 {
		t.Fatalf("b.x wrong: %v", got)
	}
}
