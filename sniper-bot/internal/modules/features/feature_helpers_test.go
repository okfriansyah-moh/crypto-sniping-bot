package features

import (
	"math"
	"testing"
)

func TestHolderDistributionScoreMonotonic(t *testing.T) {
	prev := -1.0
	for _, n := range []int64{1, 5, 50, 100, 500, 5000} {
		s := HolderDistributionScore(n)
		if s < 0 || s > 1 {
			t.Fatalf("out of [0,1]: %v", s)
		}
		if s < prev {
			t.Fatalf("non-monotonic at n=%d: prev=%v cur=%v", n, prev, s)
		}
		prev = s
	}
	if HolderDistributionScore(1) != 0 {
		t.Fatal("single holder should score 0")
	}
}

func TestWalletEntropyScore(t *testing.T) {
	// Uniform distribution → 1
	if got := WalletEntropyScore([]float64{1, 1, 1, 1}); math.Abs(got-1.0) > 1e-9 {
		t.Fatalf("uniform expected 1, got %v", got)
	}
	// Single dominant holder → near 0
	if got := WalletEntropyScore([]float64{1000, 1, 1, 1}); got > 0.3 {
		t.Fatalf("concentrated expected near 0, got %v", got)
	}
	if WalletEntropyScore([]float64{1}) != 0 {
		t.Fatal("single holder should be 0")
	}
}

func TestDriftZScore(t *testing.T) {
	if z := DriftZScore(110, 100, 5); math.Abs(z-2.0) > 1e-9 {
		t.Fatalf("expected 2.0 got %v", z)
	}
	if DriftZScore(100, 100, 0) != 0 {
		t.Fatal("zero stddev should yield 0")
	}
	if DriftZScore(math.NaN(), 100, 5) != 0 {
		t.Fatal("NaN current should yield 0")
	}
}
