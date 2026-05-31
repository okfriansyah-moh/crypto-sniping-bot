// Tests for Task 16 — config/data_quality.yaml market-cap and volume threshold keys.
// These tests load the actual config/data_quality.yaml file and assert:
//   - The three new threshold keys are commented out by default (fields parse as zero).
//   - All pre-existing threshold fields are undisturbed (additive-only invariant).
//
// Per PRODUCTION_GATE_ANALYSIS § 10 Change 3 and §7.11 of the implementation plan:
// the three keys MUST remain commented out until shadow-mode data confirms the
// graduation-token market-cap distribution; enabling them prematurely would reject
// freshly-graduated tokens whose market cap immediately exceeds the $20k ceiling.
package config_test

import (
	"testing"
)

// TestDataQualityYAML_MarketCapThresholdsAreCommentedOutByDefault loads
// config/data_quality.yaml and asserts that all three market-cap / volume
// threshold keys are absent from the parsed YAML (commented out → zero value).
//
// Guard contract from §7.11:
//
//	if MinMarketCapUsd > 0 && in.MarketCapUsd > 0 && in.MarketCapUsd < MinMarketCapUsd { reject }
//
// With zero values the condition is always false, so no token is ever rejected
// by these filters until an operator explicitly enables them after shadow tuning.
func TestDataQualityYAML_MarketCapThresholdsAreCommentedOutByDefault(t *testing.T) {
	cfg := loadDataQualityYAML(t)
	got := cfg.Thresholds

	if got.MinMarketCapUsd != 0.0 {
		t.Errorf("MinMarketCapUsd: want 0.0 (commented out, filter disabled), got %v — "+
			"uncomment only after shadow-mode calibration (PRODUCTION_GATE_ANALYSIS § 10 Change 3)",
			got.MinMarketCapUsd)
	}
	if got.MaxMarketCapUsd != 0.0 {
		t.Errorf("MaxMarketCapUsd: want 0.0 (commented out, filter disabled), got %v — "+
			"pump.fun graduation tokens may immediately exceed any cap; tune in shadow mode first",
			got.MaxMarketCapUsd)
	}
	if got.MinVolumeUsd1h != 0.0 {
		t.Errorf("MinVolumeUsd1h: want 0.0 (commented out, filter disabled), got %v — "+
			"uncomment only after shadow-mode calibration (PRODUCTION_GATE_ANALYSIS § 10 Change 3)",
			got.MinVolumeUsd1h)
	}
}

// TestDataQualityYAML_ExistingThresholdsUnchangedByTask16 verifies the
// additive-only invariant: Task 16 adds no active keys, so all pre-existing
// threshold values in config/data_quality.yaml must remain at their canonical
// defaults after the task-16 edit.
func TestDataQualityYAML_ExistingThresholdsUnchangedByTask16(t *testing.T) {
	cfg := loadDataQualityYAML(t)
	got := cfg.Thresholds

	cases := []struct {
		name string
		got  interface{}
		want interface{}
	}{
		{"HoneypotRatioDeviationMax", got.HoneypotRatioDeviationMax, 0.30},
		{"TaxTotalMaxBps", got.TaxTotalMaxBps, 1000},
		{"WashUniqueRatioMin", got.WashUniqueRatioMin, 0.30},
		{"LpLockMinDays", got.LpLockMinDays, 30},
		{"MinLiquidityUsd", got.MinLiquidityUsd, 3000.0},
		{"MaxCreatorPrevTokenCount", got.MaxCreatorPrevTokenCount, 1},
		{"MinTokenAgeSeconds", got.MinTokenAgeSeconds, 900},
		{"MinHolderCount", got.MinHolderCount, 50},
		{"AICopyPasteDescMinNarrativeScore", got.AICopyPasteDescMinNarrativeScore, 6.0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Compare as float64 to handle int/float differences uniformly.
			var gotF, wantF float64
			switch v := tc.got.(type) {
			case float64:
				gotF = v
			case int:
				gotF = float64(v)
			case int32:
				gotF = float64(v)
			case int64:
				gotF = float64(v)
			}
			switch v := tc.want.(type) {
			case float64:
				wantF = v
			case int:
				wantF = float64(v)
			case int32:
				wantF = float64(v)
			case int64:
				wantF = float64(v)
			}
			if gotF != wantF {
				t.Errorf("%s: want %v, got %v — Task 16 must not modify existing keys", tc.name, wantF, gotF)
			}
		})
	}
}

// TestDataQualityYAML_MandatoryRejectsStillEnabled verifies that the three
// mandatory structural hard-rejects are still enabled in the config after the
// Task 16 edit (additive-only invariant applies to boolean flags too).
func TestDataQualityYAML_MandatoryRejectsStillEnabled(t *testing.T) {
	cfg := loadDataQualityYAML(t)
	got := cfg.Thresholds

	if !got.RejectNoSocialLinks {
		t.Error("RejectNoSocialLinks must be true — mandatory criterion per copilot-instructions § Security Invariants")
	}
	if !got.RejectUnknownSocialLinks {
		t.Error("RejectUnknownSocialLinks must be true — mandatory criterion per copilot-instructions § Security Invariants")
	}
	if !got.RejectUnknownTotalSupply {
		t.Error("RejectUnknownTotalSupply must be true — mandatory criterion per copilot-instructions § Security Invariants")
	}
	if !got.RejectUnknownCreatorCount {
		t.Error("RejectUnknownCreatorCount must be true — mandatory criterion per copilot-instructions § Security Invariants")
	}
}
