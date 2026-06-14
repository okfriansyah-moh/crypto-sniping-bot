package workers

import (
	"testing"
	"time"
)

// ── nextUpgrade ───────────────────────────────────────────────────────────────

func TestNextUpgrade_Strict_ReturnsBalanced(t *testing.T) {
	// Arrange / Act
	next, ok := nextUpgrade(modeStrict)

	// Assert
	if !ok {
		t.Fatal("nextUpgrade(STRICT): want ok=true")
	}
	if next != modeBalanced {
		t.Errorf("nextUpgrade(STRICT): want %q, got %q", modeBalanced, next)
	}
}

func TestNextUpgrade_Balanced_ReturnsExploration(t *testing.T) {
	// Arrange / Act
	next, ok := nextUpgrade(modeBalanced)

	// Assert
	if !ok {
		t.Fatal("nextUpgrade(BALANCED): want ok=true")
	}
	if next != modeExploration {
		t.Errorf("nextUpgrade(BALANCED): want %q, got %q", modeExploration, next)
	}
}

func TestNextUpgrade_Exploration_ReturnsVeryExploration(t *testing.T) {
	// Arrange / Act: EXPLORATION upgrades to VERY_EXPLORATION.
	next, ok := nextUpgrade(modeExploration)

	// Assert
	if !ok {
		t.Fatal("nextUpgrade(EXPLORATION): want ok=true")
	}
	if next != modeVeryExploration {
		t.Errorf("nextUpgrade(EXPLORATION): want %q, got %q", modeVeryExploration, next)
	}
}

func TestNextUpgrade_VeryExploration_ReturnsFalse(t *testing.T) {
	// Arrange / Act: VERY_EXPLORATION is the highest mode; no further upgrade.
	_, ok := nextUpgrade(modeVeryExploration)

	// Assert
	if ok {
		t.Error("nextUpgrade(VERY_EXPLORATION): want ok=false (no higher mode)")
	}
}

func TestNextUpgrade_Unknown_ReturnsFalse(t *testing.T) {
	// Arrange / Act
	_, ok := nextUpgrade("DEGRADED")

	// Assert
	if ok {
		t.Error("nextUpgrade(DEGRADED): want ok=false")
	}
}

func TestNextUpgrade_Empty_ReturnsFalse(t *testing.T) {
	// Arrange / Act
	_, ok := nextUpgrade("")

	// Assert
	if ok {
		t.Error("nextUpgrade(''): want ok=false")
	}
}

// ── nextDowngrade ─────────────────────────────────────────────────────────────

func TestNextDowngrade_VeryExploration_ReturnsExploration(t *testing.T) {
	// Arrange / Act: VERY_EXPLORATION downgrades to EXPLORATION.
	next, ok := nextDowngrade(modeVeryExploration)

	// Assert
	if !ok {
		t.Fatal("nextDowngrade(VERY_EXPLORATION): want ok=true")
	}
	if next != modeExploration {
		t.Errorf("nextDowngrade(VERY_EXPLORATION): want %q, got %q", modeExploration, next)
	}
}

func TestNextDowngrade_Exploration_ReturnsBalanced(t *testing.T) {
	// Arrange / Act
	next, ok := nextDowngrade(modeExploration)

	// Assert
	if !ok {
		t.Fatal("nextDowngrade(EXPLORATION): want ok=true")
	}
	if next != modeBalanced {
		t.Errorf("nextDowngrade(EXPLORATION): want %q, got %q", modeBalanced, next)
	}
}

func TestNextDowngrade_Balanced_ReturnsStrict(t *testing.T) {
	// Arrange / Act
	next, ok := nextDowngrade(modeBalanced)

	// Assert
	if !ok {
		t.Fatal("nextDowngrade(BALANCED): want ok=true")
	}
	if next != modeStrict {
		t.Errorf("nextDowngrade(BALANCED): want %q, got %q", modeStrict, next)
	}
}

func TestNextDowngrade_Strict_ReturnsFalse(t *testing.T) {
	// Arrange / Act: STRICT is the most conservative; no further downgrade.
	_, ok := nextDowngrade(modeStrict)

	// Assert
	if ok {
		t.Error("nextDowngrade(STRICT): want ok=false (no lower mode)")
	}
}

func TestNextDowngrade_Unknown_ReturnsFalse(t *testing.T) {
	// Arrange / Act
	_, ok := nextDowngrade("UNKNOWN_MODE")

	// Assert
	if ok {
		t.Error("nextDowngrade(UNKNOWN_MODE): want ok=false")
	}
}

// ── computeAdaptiveWindowID ───────────────────────────────────────────────────

func TestComputeAdaptiveWindowID_Deterministic(t *testing.T) {
	// Arrange: two calls with the same time and windowSec must produce the same ID.
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	windowSec := 3600

	// Act
	id1 := computeAdaptiveWindowID(now, windowSec)
	id2 := computeAdaptiveWindowID(now, windowSec)

	// Assert
	if id1 != id2 {
		t.Errorf("computeAdaptiveWindowID non-deterministic: %q vs %q", id1, id2)
	}
}

func TestComputeAdaptiveWindowID_SameHour_SameID(t *testing.T) {
	// Arrange: two timestamps within the same 1-hour window must produce the same ID.
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 1, 1, 12, 59, 59, 0, time.UTC)

	// Act
	id0 := computeAdaptiveWindowID(t0, 3600)
	id1 := computeAdaptiveWindowID(t1, 3600)

	// Assert
	if id0 != id1 {
		t.Errorf("expected same window ID for same hour: %q vs %q", id0, id1)
	}
}

func TestComputeAdaptiveWindowID_DifferentHour_DifferentID(t *testing.T) {
	// Arrange: two timestamps in adjacent hours must produce different IDs.
	t0 := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	t1 := time.Date(2026, 1, 1, 13, 0, 0, 0, time.UTC)

	// Act
	id0 := computeAdaptiveWindowID(t0, 3600)
	id1 := computeAdaptiveWindowID(t1, 3600)

	// Assert
	if id0 == id1 {
		t.Errorf("expected different IDs for different hours, both got %q", id0)
	}
}

func TestComputeAdaptiveWindowID_ZeroWindowSec_FallsBackTo3600(t *testing.T) {
	// Arrange: zero windowSec must not panic and must behave as 3600.
	now := time.Date(2026, 1, 1, 12, 30, 0, 0, time.UTC)

	// Act: both calls should produce the same result (fallback to 3600).
	id0 := computeAdaptiveWindowID(now, 0)
	id1 := computeAdaptiveWindowID(now, 3600)

	// Assert
	if id0 != id1 {
		t.Errorf("zero windowSec fallback mismatch: %q vs %q", id0, id1)
	}
}

func TestComputeAdaptiveWindowID_Length_Is16Chars(t *testing.T) {
	// Arrange / Act
	id := computeAdaptiveWindowID(time.Now(), 3600)

	// Assert: ContentIDFromString returns 16 hex chars.
	if len(id) != 16 {
		t.Errorf("ID length: want 16, got %d (%q)", len(id), id)
	}
}
