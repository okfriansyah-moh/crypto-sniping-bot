package learning

import "testing"

// TestClamp_* exercises the clamp helper directly from within the package
// because it is unexported and its boundary branches are not reachable
// through the public API (applyBoundedDelta always produces delta == ±bound).

func TestClamp_ValueBelowLo_ReturnsLo(t *testing.T) {
	if got := clamp(-5.0, -2.0, 2.0); got != -2.0 {
		t.Errorf("expected -2.0, got %.4f", got)
	}
}

func TestClamp_ValueAboveHi_ReturnsHi(t *testing.T) {
	if got := clamp(5.0, -2.0, 2.0); got != 2.0 {
		t.Errorf("expected 2.0, got %.4f", got)
	}
}

func TestClamp_ValueInRange_ReturnsValue(t *testing.T) {
	if got := clamp(1.0, -2.0, 2.0); got != 1.0 {
		t.Errorf("expected 1.0, got %.4f", got)
	}
}

func TestClamp_ValueAtLo_ReturnsLo(t *testing.T) {
	if got := clamp(-2.0, -2.0, 2.0); got != -2.0 {
		t.Errorf("expected -2.0 (at boundary), got %.4f", got)
	}
}

func TestClamp_ValueAtHi_ReturnsHi(t *testing.T) {
	if got := clamp(2.0, -2.0, 2.0); got != 2.0 {
		t.Errorf("expected 2.0 (at boundary), got %.4f", got)
	}
}

// TestFamilyKeys_* ensures each family name returns the expected key slice.

func TestFamilyKeys_Thresholds(t *testing.T) {
	keys := familyKeys("thresholds")
	if len(keys) == 0 {
		t.Error("expected non-empty keys for thresholds family")
	}
}

func TestFamilyKeys_Weights(t *testing.T) {
	keys := familyKeys("weights")
	if len(keys) == 0 {
		t.Error("expected non-empty keys for weights family")
	}
}

func TestFamilyKeys_CohortMults(t *testing.T) {
	keys := familyKeys("cohort_mults")
	if len(keys) == 0 {
		t.Error("expected non-empty keys for cohort_mults family")
	}
}

func TestFamilyKeys_Unknown_ReturnsNil(t *testing.T) {
	keys := familyKeys("no_such_family")
	if keys != nil {
		t.Errorf("expected nil keys for unknown family, got %v", keys)
	}
}
