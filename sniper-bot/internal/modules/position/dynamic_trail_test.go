package position_test

import (
	"testing"

	"crypto-sniping-bot/sniper-bot/internal/modules/position"
)

func TestDynamicTrailCalculator_HighestTierWins(t *testing.T) {
	calc := position.NewDynamicTrailCalculator([]position.DynamicTrailTier{
		{TriggerBps: 10000, TrailBps: 2000}, // 2x → 20% trail
		{TriggerBps: 20000, TrailBps: 1500}, // 3x → 15% trail
		{TriggerBps: 40000, TrailBps: 1000}, // 5x → 10% trail
	})

	cases := []struct {
		gain     int32
		wantBps  int32
		wantDesc string
	}{
		{gain: 50000, wantBps: 1000, wantDesc: "5x+ → 10% tier"},
		{gain: 40000, wantBps: 1000, wantDesc: "exact 5x → 10% tier"},
		{gain: 39999, wantBps: 1500, wantDesc: "just below 5x → 15% tier"},
		{gain: 20000, wantBps: 1500, wantDesc: "exact 3x → 15% tier"},
		{gain: 19999, wantBps: 2000, wantDesc: "just below 3x → 20% tier"},
		{gain: 10000, wantBps: 2000, wantDesc: "exact 2x → 20% tier"},
		{gain: 9999, wantBps: 0, wantDesc: "below all tiers → no trail"},
		{gain: 0, wantBps: 0, wantDesc: "at entry → no trail"},
		{gain: -500, wantBps: 0, wantDesc: "in loss → no trail"},
	}

	for _, tc := range cases {
		got := calc.TrailBpsForGain(tc.gain)
		if got != tc.wantBps {
			t.Errorf("%s: TrailBpsForGain(%d) = %d, want %d",
				tc.wantDesc, tc.gain, got, tc.wantBps)
		}
	}
}

func TestDynamicTrailCalculator_EmptyTiers_AlwaysZero(t *testing.T) {
	calc := position.NewDynamicTrailCalculator(nil)
	if got := calc.TrailBpsForGain(99999); got != 0 {
		t.Errorf("empty tiers: expected 0, got %d", got)
	}
	if calc.Len() != 0 {
		t.Errorf("expected Len=0, got %d", calc.Len())
	}
}

func TestDynamicTrailCalculator_InvalidTiers_Filtered(t *testing.T) {
	calc := position.NewDynamicTrailCalculator([]position.DynamicTrailTier{
		{TriggerBps: 10000, TrailBps: 0},   // TrailBps=0 → invalid, dropped
		{TriggerBps: -1, TrailBps: 2000},   // negative trigger → invalid, dropped
		{TriggerBps: 5000, TrailBps: 1500}, // valid
	})

	if calc.Len() != 1 {
		t.Errorf("expected 1 valid tier, got %d", calc.Len())
	}
	if got := calc.TrailBpsForGain(5000); got != 1500 {
		t.Errorf("expected 1500, got %d", got)
	}
}

func TestDynamicTrailCalculator_UnsortedInput_SortedInternally(t *testing.T) {
	// Tiers provided in random order — calculator must sort descending.
	calc := position.NewDynamicTrailCalculator([]position.DynamicTrailTier{
		{TriggerBps: 5000, TrailBps: 2500},  // 50% gain → 25% trail
		{TriggerBps: 50000, TrailBps: 800},  // 500% gain → 8% trail
		{TriggerBps: 20000, TrailBps: 1200}, // 200% gain → 12% trail
	})

	if got := calc.TrailBpsForGain(60000); got != 800 {
		t.Errorf("600%% gain: expected 800 bps, got %d", got)
	}
	if got := calc.TrailBpsForGain(25000); got != 1200 {
		t.Errorf("250%% gain: expected 1200 bps, got %d", got)
	}
	if got := calc.TrailBpsForGain(7000); got != 2500 {
		t.Errorf("70%% gain: expected 2500 bps, got %d", got)
	}
}

func TestDynamicTrailCalculator_SingleTier(t *testing.T) {
	calc := position.NewDynamicTrailCalculator([]position.DynamicTrailTier{
		{TriggerBps: 0, TrailBps: 3000}, // activates from entry
	})
	// Even at 0 gain this tier should fire since 0 >= 0.
	if got := calc.TrailBpsForGain(0); got != 3000 {
		t.Errorf("expected 3000 at zero gain, got %d", got)
	}
	if got := calc.TrailBpsForGain(-100); got != 0 {
		t.Errorf("in loss: expected 0, got %d", got)
	}
}
