// Tests for DataQualityDetectorThresholds market-cap and volume fields (Task 15).
// These assert that the three new optional fields default to zero (filter disabled)
// and can round-trip through YAML correctly.
package config_test

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"crypto-sniping-bot/internal/app/config"
)

// TestDetectorThresholds_NewFieldsZeroByDefault verifies that a
// DataQualityDetectorThresholds decoded from YAML that does NOT include the
// three new market-cap/volume keys has zero values for all three fields.
// Zero is the "filter disabled" sentinel — brand-new tokens not yet indexed
// by DEXScreener report 0 for MarketCapUsd and VolumeUsd1h, so zero thresholds
// prevent false rejections at launch time.
func TestDetectorThresholds_NewFieldsZeroByDefault(t *testing.T) {
	yamlInput := `
honeypot_ratio_deviation_max: 0.3
tax_total_max_bps: 1000
max_creator_prev_token_count: 1
reject_no_social_links: true
`
	var thresholds config.DataQualityDetectorThresholds
	if err := yaml.Unmarshal([]byte(yamlInput), &thresholds); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}

	// Existing fields must decode correctly.
	if thresholds.HoneypotRatioDeviationMax != 0.3 {
		t.Errorf("HoneypotRatioDeviationMax: want 0.3, got %v", thresholds.HoneypotRatioDeviationMax)
	}
	if thresholds.MaxCreatorPrevTokenCount != 1 {
		t.Errorf("MaxCreatorPrevTokenCount: want 1, got %v", thresholds.MaxCreatorPrevTokenCount)
	}

	// New fields must default to zero when absent from YAML.
	if thresholds.MinMarketCapUsd != 0.0 {
		t.Errorf("MinMarketCapUsd: want 0.0 (filter disabled), got %v", thresholds.MinMarketCapUsd)
	}
	if thresholds.MaxMarketCapUsd != 0.0 {
		t.Errorf("MaxMarketCapUsd: want 0.0 (filter disabled), got %v", thresholds.MaxMarketCapUsd)
	}
	if thresholds.MinVolumeUsd1h != 0.0 {
		t.Errorf("MinVolumeUsd1h: want 0.0 (filter disabled), got %v", thresholds.MinVolumeUsd1h)
	}
}

// TestDetectorThresholds_YAMLTagNamesMatchFieldNames verifies that the YAML
// tags on the three new fields use the expected snake_case names. The guard
// code in the DQ engine (Task 18) will reference the same YAML keys in
// config/data_quality.yaml, so a mismatch here would cause silent zero values.
func TestDetectorThresholds_YAMLTagNamesMatchFieldNames(t *testing.T) {
	yamlWithAllThree := `
min_market_cap_usd: 3000.0
max_market_cap_usd: 20000.0
min_volume_usd_1h: 100.0
`
	var thresholds config.DataQualityDetectorThresholds
	if err := yaml.Unmarshal([]byte(yamlWithAllThree), &thresholds); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}

	if thresholds.MinMarketCapUsd != 3000.0 {
		t.Errorf("MinMarketCapUsd YAML tag: want 3000.0, got %v", thresholds.MinMarketCapUsd)
	}
	if thresholds.MaxMarketCapUsd != 20000.0 {
		t.Errorf("MaxMarketCapUsd YAML tag: want 20000.0, got %v", thresholds.MaxMarketCapUsd)
	}
	if thresholds.MinVolumeUsd1h != 100.0 {
		t.Errorf("MinVolumeUsd1h YAML tag: want 100.0, got %v", thresholds.MinVolumeUsd1h)
	}
}

// TestDetectorThresholds_RoundTripThroughRuntimeConfig verifies that the three
// new fields survive a full YAML marshal → DataQualityRuntimeConfig unmarshal
// round-trip. This exercises the path that config.Load() would take at runtime.
func TestDetectorThresholds_RoundTripThroughRuntimeConfig(t *testing.T) {
	fullYAML := `
detector_timeout_ms: 500
total_budget_ms: 2000
max_inflight_detectors: 4
pass_threshold: 0.3
reject_threshold: 0.7
thresholds:
  honeypot_ratio_deviation_max: 0.3
  tax_total_max_bps: 1000
  tax_buy_max_bps: 500
  tax_sell_max_bps: 500
  max_creator_prev_token_count: 1
  reject_no_social_links: true
  reject_unknown_social_links: true
  reject_unknown_total_supply: true
  reject_unknown_creator_count: true
  min_market_cap_usd: 3000.0
  max_market_cap_usd: 20000.0
  min_volume_usd_1h: 100.0
`
	var runtimeCfg config.DataQualityRuntimeConfig
	if err := yaml.Unmarshal([]byte(fullYAML), &runtimeCfg); err != nil {
		t.Fatalf("yaml.Unmarshal into DataQualityRuntimeConfig: %v", err)
	}

	got := runtimeCfg.Thresholds

	if got.MinMarketCapUsd != 3000.0 {
		t.Errorf("round-trip MinMarketCapUsd: want 3000.0, got %v", got.MinMarketCapUsd)
	}
	if got.MaxMarketCapUsd != 20000.0 {
		t.Errorf("round-trip MaxMarketCapUsd: want 20000.0, got %v", got.MaxMarketCapUsd)
	}
	if got.MinVolumeUsd1h != 100.0 {
		t.Errorf("round-trip MinVolumeUsd1h: want 100.0, got %v", got.MinVolumeUsd1h)
	}
	// Verify pre-existing field is undisturbed.
	if got.MaxCreatorPrevTokenCount != 1 {
		t.Errorf("round-trip MaxCreatorPrevTokenCount: want 1, got %v", got.MaxCreatorPrevTokenCount)
	}
}

// TestDetectorThresholds_ZeroDisablesFilter documents the contract that callers
// (DQ engine Task 18) MUST honour: a threshold of 0 means "filter disabled",
// and the input value being 0 also means "no data — skip filter".
// This test encodes the expected guard pattern from §7.11:
//
//	if thresholds.MinMarketCapUsd > 0 && in.MarketCapUsd > 0 && in.MarketCapUsd < threshold
//
// by verifying that with a zero threshold the condition evaluates to false.
func TestDetectorThresholds_ZeroDisablesFilter(t *testing.T) {
	cases := []struct {
		name         string
		threshold    float64
		inputValue   float64
		expectReject bool
	}{
		{
			name:         "zero threshold — never rejects",
			threshold:    0.0,
			inputValue:   1.0, // even a low value, threshold=0 means disabled
			expectReject: false,
		},
		{
			name:         "positive threshold, zero input — never rejects (no DEXScreener data)",
			threshold:    3000.0,
			inputValue:   0.0,
			expectReject: false,
		},
		{
			name:         "positive threshold, positive input below threshold — rejects",
			threshold:    3000.0,
			inputValue:   2999.0,
			expectReject: true,
		},
		{
			name:         "positive threshold, input at threshold — does NOT reject (exclusive lower bound)",
			threshold:    3000.0,
			inputValue:   3000.0,
			expectReject: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Encode the §7.11 guard pattern: both threshold > 0 AND input > 0.
			wouldReject := tc.threshold > 0 && tc.inputValue > 0 && tc.inputValue < tc.threshold
			if wouldReject != tc.expectReject {
				t.Errorf("threshold=%.1f input=%.1f: wouldReject=%v, want %v",
					tc.threshold, tc.inputValue, wouldReject, tc.expectReject)
			}
		})
	}
}

// TestDetectorThresholds_ExistingFieldsUnchanged ensures the additive-only
// invariant: no existing DataQualityDetectorThresholds field has been removed
// or renamed. This test decodes a YAML blob that exercises several pre-existing
// fields and asserts their values survive the decode alongside the three new ones.
func TestDetectorThresholds_ExistingFieldsUnchanged(t *testing.T) {
	yamlInput := strings.TrimSpace(`
honeypot_ratio_deviation_max: 0.25
tax_total_max_bps: 800
tax_buy_max_bps: 400
tax_sell_max_bps: 400
wash_unique_ratio_min: 0.6
wash_recent_swaps_window: 20
lp_lock_required: true
lp_lock_min_days: 30
max_bonding_curve_progress_bps: 8000
min_liquidity_usd: 3000.0
max_total_supply: 1000000000.0
max_creator_prev_token_count: 1
no_social_links_risk_score: 0.4
reject_no_social_links: true
reject_unknown_social_links: true
reject_unknown_total_supply: true
reject_unknown_creator_count: true
min_token_age_seconds: 900
min_holder_count: 50
reject_unknown_holder_count: true
ai_copy_paste_desc_min_narrative_score: 6.0
min_market_cap_usd: 5000.0
max_market_cap_usd: 25000.0
min_volume_usd_1h: 200.0
`)
	var thresholds config.DataQualityDetectorThresholds
	if err := yaml.Unmarshal([]byte(yamlInput), &thresholds); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}

	// Pre-existing fields.
	if thresholds.HoneypotRatioDeviationMax != 0.25 {
		t.Errorf("HoneypotRatioDeviationMax: want 0.25, got %v", thresholds.HoneypotRatioDeviationMax)
	}
	if thresholds.TaxTotalMaxBps != 800 {
		t.Errorf("TaxTotalMaxBps: want 800, got %v", thresholds.TaxTotalMaxBps)
	}
	if thresholds.WashUniqueRatioMin != 0.6 {
		t.Errorf("WashUniqueRatioMin: want 0.6, got %v", thresholds.WashUniqueRatioMin)
	}
	if !thresholds.LpLockRequired {
		t.Error("LpLockRequired: want true, got false")
	}
	if thresholds.MaxBondingCurveProgressBps != 8000 {
		t.Errorf("MaxBondingCurveProgressBps: want 8000, got %v", thresholds.MaxBondingCurveProgressBps)
	}
	if thresholds.MinLiquidityUsd != 3000.0 {
		t.Errorf("MinLiquidityUsd: want 3000.0, got %v", thresholds.MinLiquidityUsd)
	}
	if thresholds.MaxTotalSupply != 1_000_000_000.0 {
		t.Errorf("MaxTotalSupply: want 1e9, got %v", thresholds.MaxTotalSupply)
	}
	if thresholds.MaxCreatorPrevTokenCount != 1 {
		t.Errorf("MaxCreatorPrevTokenCount: want 1, got %v", thresholds.MaxCreatorPrevTokenCount)
	}
	if !thresholds.RejectNoSocialLinks {
		t.Error("RejectNoSocialLinks: want true, got false")
	}
	if !thresholds.RejectUnknownSocialLinks {
		t.Error("RejectUnknownSocialLinks: want true, got false")
	}
	if thresholds.MinTokenAgeSeconds != 900 {
		t.Errorf("MinTokenAgeSeconds: want 900, got %v", thresholds.MinTokenAgeSeconds)
	}
	if thresholds.MinHolderCount != 50 {
		t.Errorf("MinHolderCount: want 50, got %v", thresholds.MinHolderCount)
	}
	if thresholds.AICopyPasteDescMinNarrativeScore != 6.0 {
		t.Errorf("AICopyPasteDescMinNarrativeScore: want 6.0, got %v", thresholds.AICopyPasteDescMinNarrativeScore)
	}

	// New Task 15 fields.
	if thresholds.MinMarketCapUsd != 5000.0 {
		t.Errorf("MinMarketCapUsd: want 5000.0, got %v", thresholds.MinMarketCapUsd)
	}
	if thresholds.MaxMarketCapUsd != 25000.0 {
		t.Errorf("MaxMarketCapUsd: want 25000.0, got %v", thresholds.MaxMarketCapUsd)
	}
	if thresholds.MinVolumeUsd1h != 200.0 {
		t.Errorf("MinVolumeUsd1h: want 200.0, got %v", thresholds.MinVolumeUsd1h)
	}
}
