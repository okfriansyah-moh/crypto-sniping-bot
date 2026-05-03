package capital

import (
	"math"
	"testing"
)

// ── clampProbability ──────────────────────────────────────────────────────────

func TestClampProbability_NaN_ReturnsZero(t *testing.T) {
	// Arrange / Act / Assert
	if got := clampProbability(math.NaN()); got != 0 {
		t.Errorf("clampProbability(NaN): want 0, got %v", got)
	}
}

func TestClampProbability_PosInf_ReturnsZero(t *testing.T) {
	// Arrange / Act / Assert
	if got := clampProbability(math.Inf(1)); got != 0 {
		t.Errorf("clampProbability(+Inf): want 0, got %v", got)
	}
}

func TestClampProbability_NegInf_ReturnsZero(t *testing.T) {
	// Arrange / Act / Assert
	if got := clampProbability(math.Inf(-1)); got != 0 {
		t.Errorf("clampProbability(-Inf): want 0, got %v", got)
	}
}

func TestClampProbability_ValidValue_PassesThrough(t *testing.T) {
	// Arrange
	cases := []float64{0.0, 0.5, 1.0, -1.5, 42.0}
	for _, v := range cases {
		// Act
		got := clampProbability(v)
		// Assert: finite, non-NaN values must pass through unchanged.
		if got != v {
			t.Errorf("clampProbability(%v): want %v, got %v", v, v, got)
		}
	}
}

// ── clampUnit ─────────────────────────────────────────────────────────────────

func TestClampUnit_NaN_ReturnsZero(t *testing.T) {
	// Arrange / Act / Assert
	if got := clampUnit(math.NaN()); got != 0 {
		t.Errorf("clampUnit(NaN): want 0, got %v", got)
	}
}

func TestClampUnit_PosInf_ReturnsZero(t *testing.T) {
	// Arrange / Act / Assert
	if got := clampUnit(math.Inf(1)); got != 0 {
		t.Errorf("clampUnit(+Inf): want 0, got %v", got)
	}
}

func TestClampUnit_NegInf_ReturnsZero(t *testing.T) {
	// Arrange / Act / Assert
	if got := clampUnit(math.Inf(-1)); got != 0 {
		t.Errorf("clampUnit(-Inf): want 0, got %v", got)
	}
}

func TestClampUnit_BelowZero_ReturnsZero(t *testing.T) {
	// Arrange / Act / Assert
	if got := clampUnit(-0.5); got != 0 {
		t.Errorf("clampUnit(-0.5): want 0, got %v", got)
	}
}

func TestClampUnit_AboveOne_ReturnsOne(t *testing.T) {
	// Arrange / Act / Assert
	if got := clampUnit(1.5); got != 1 {
		t.Errorf("clampUnit(1.5): want 1, got %v", got)
	}
}

func TestClampUnit_ExactlyZero_ReturnsZero(t *testing.T) {
	// Arrange / Act / Assert
	if got := clampUnit(0); got != 0 {
		t.Errorf("clampUnit(0): want 0, got %v", got)
	}
}

func TestClampUnit_ExactlyOne_ReturnsOne(t *testing.T) {
	// Arrange / Act / Assert
	if got := clampUnit(1); got != 1 {
		t.Errorf("clampUnit(1): want 1, got %v", got)
	}
}

func TestClampUnit_MidValue_PassesThrough(t *testing.T) {
	// Arrange / Act / Assert
	if got := clampUnit(0.7); math.Abs(got-0.7) > 1e-12 {
		t.Errorf("clampUnit(0.7): want 0.7, got %v", got)
	}
}
