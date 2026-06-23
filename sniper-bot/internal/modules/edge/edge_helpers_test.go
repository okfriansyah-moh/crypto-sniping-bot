package edge

import "testing"

func TestMomentumScoreBounds(t *testing.T) {
	if MomentumScore(0, 0) != 0 {
		t.Fatal("zero in zero out")
	}
	if MomentumScore(1, 1) != 1 {
		t.Fatal("max in max out")
	}
	got := MomentumScore(0.5, 1.0)
	want := 0.6*1.0 + 0.4*0.5
	if got != want {
		t.Fatalf("weight mismatch: %v vs %v", got, want)
	}
}

func TestAdaptiveThresholdRange(t *testing.T) {
	// Mid-congestion → unchanged.
	if AdaptiveThreshold(0.5, 0.5, 0.2) != 0.5 {
		t.Fatal("mid congestion should not shift")
	}
	// High congestion → above base.
	if AdaptiveThreshold(0.5, 1.0, 0.2) <= 0.5 {
		t.Fatal("high congestion should raise threshold")
	}
	// Low congestion → below base.
	if AdaptiveThreshold(0.5, 0.0, 0.2) >= 0.5 {
		t.Fatal("low congestion should lower threshold")
	}
}

func TestNewPoolGate(t *testing.T) {
	if !NewPoolGate(60, 100_000, 30, 50_000) {
		t.Fatal("eligible pool should pass")
	}
	if NewPoolGate(10, 100_000, 30, 50_000) {
		t.Fatal("too young should fail")
	}
	if NewPoolGate(60, 1_000, 30, 50_000) {
		t.Fatal("thin liquidity should fail")
	}
}
