// Tests for Task 12 — data_quality.yaml per-mode serial-launcher profile values.
// These tests load the actual config/data_quality.yaml file and assert the
// canonical values for each operational mode are present and correct.
//
// Canonical values per docs/plans/2026-05-29-production-gate-hardening-plan.md §7.9:
//
//	STRICT / BALANCED : max_creator_prev_token_count=0 (sentinel: use global)
//	EXPLORATION       : max_creator_prev_token_count=5, requires_social_links=true,
//	                    max_risk_score=0.40, min_holder_count=50
//	VERY_EXPLORATION  : max_creator_prev_token_count=10, requires_social_links=true,
//	                    max_risk_score=0.45, min_holder_count=25
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"gopkg.in/yaml.v3"

	"crypto-sniping-bot/internal/app/config"
)

// dataQualityFileWrapper mirrors the top-level YAML structure of
// config/data_quality.yaml — the file uses a "data_quality:" root key.
type dataQualityFileWrapper struct {
	DataQuality config.DataQualityRuntimeConfig `yaml:"data_quality"`
}

// loadDataQualityYAML reads config/data_quality.yaml relative to the package
// directory (Go tests run with the package directory as the working dir).
func loadDataQualityYAML(t *testing.T) config.DataQualityRuntimeConfig {
	t.Helper()

	// From internal/app/config/ go up three levels to reach the project root.
	path := filepath.Join("..", "..", "..", "shared", "config", "data_quality.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("loadDataQualityYAML: ReadFile %q: %v", path, err)
	}

	var wrapper dataQualityFileWrapper
	if err := yaml.Unmarshal(data, &wrapper); err != nil {
		t.Fatalf("loadDataQualityYAML: yaml.Unmarshal: %v", err)
	}
	return wrapper.DataQuality
}

// TestDataQualityYAML_AllFourModesPresent verifies that config/data_quality.yaml
// contains all four required mode_profiles keys after Task 12.
func TestDataQualityYAML_AllFourModesPresent(t *testing.T) {
	cfg := loadDataQualityYAML(t)

	required := []string{"strict", "balanced", "exploration", "very_exploration"}
	for _, mode := range required {
		if _, ok := cfg.ModeProfiles[mode]; !ok {
			t.Errorf("mode_profiles[%q] missing from config/data_quality.yaml", mode)
		}
	}
}

// TestDataQualityYAML_StrictModeSerialLauncherSentinel asserts that the STRICT
// mode profile keeps MaxCreatorPrevTokenCount=0 (use-global sentinel), preserving
// the hard-reject behaviour for serial launchers in STRICT mode. Any non-zero
// value would silently raise the global threshold.
func TestDataQualityYAML_StrictModeSerialLauncherSentinel(t *testing.T) {
	cfg := loadDataQualityYAML(t)

	strict, ok := cfg.ModeProfiles["strict"]
	if !ok {
		t.Fatal("strict mode profile missing")
	}

	if strict.MaxCreatorPrevTokenCount != 0 {
		t.Errorf("STRICT MaxCreatorPrevTokenCount: want 0 (use-global sentinel), got %d",
			strict.MaxCreatorPrevTokenCount)
	}
	if strict.SerialLauncherRequiresSocialLinks {
		t.Error("STRICT SerialLauncherRequiresSocialLinks: want false (unused in STRICT)")
	}
	if strict.SerialLauncherMaxRiskScore != 0.0 {
		t.Errorf("STRICT SerialLauncherMaxRiskScore: want 0.0 (unused in STRICT), got %v",
			strict.SerialLauncherMaxRiskScore)
	}
	if strict.SerialLauncherMinHolderCount != 0 {
		t.Errorf("STRICT SerialLauncherMinHolderCount: want 0 (unused in STRICT), got %d",
			strict.SerialLauncherMinHolderCount)
	}
}

// TestDataQualityYAML_BalancedModeSerialLauncherSentinel asserts that the BALANCED
// mode profile keeps MaxCreatorPrevTokenCount=0, preserving the hard-reject
// behaviour for serial launchers in BALANCED mode.
func TestDataQualityYAML_BalancedModeSerialLauncherSentinel(t *testing.T) {
	cfg := loadDataQualityYAML(t)

	balanced, ok := cfg.ModeProfiles["balanced"]
	if !ok {
		t.Fatal("balanced mode profile missing")
	}

	if balanced.MaxCreatorPrevTokenCount != 0 {
		t.Errorf("BALANCED MaxCreatorPrevTokenCount: want 0 (use-global sentinel), got %d",
			balanced.MaxCreatorPrevTokenCount)
	}
	if balanced.SerialLauncherRequiresSocialLinks {
		t.Error("BALANCED SerialLauncherRequiresSocialLinks: want false (unused in BALANCED)")
	}
	if balanced.SerialLauncherMaxRiskScore != 0.0 {
		t.Errorf("BALANCED SerialLauncherMaxRiskScore: want 0.0 (unused in BALANCED), got %v",
			balanced.SerialLauncherMaxRiskScore)
	}
	if balanced.SerialLauncherMinHolderCount != 0 {
		t.Errorf("BALANCED SerialLauncherMinHolderCount: want 0 (unused in BALANCED), got %d",
			balanced.SerialLauncherMinHolderCount)
	}
}

// TestDataQualityYAML_ExplorationModeSerialLauncherValues asserts that the
// EXPLORATION mode profile carries the canonical serial-launcher override values
// that enable conditional RISKY_PASS for serial launchers up to 5 prior tokens.
func TestDataQualityYAML_ExplorationModeSerialLauncherValues(t *testing.T) {
	cfg := loadDataQualityYAML(t)

	exploration, ok := cfg.ModeProfiles["exploration"]
	if !ok {
		t.Fatal("exploration mode profile missing")
	}

	if exploration.MaxCreatorPrevTokenCount != 5 {
		t.Errorf("EXPLORATION MaxCreatorPrevTokenCount: want 5, got %d",
			exploration.MaxCreatorPrevTokenCount)
	}
	if !exploration.SerialLauncherRequiresSocialLinks {
		t.Error("EXPLORATION SerialLauncherRequiresSocialLinks: want true (quality gate)")
	}
	if exploration.SerialLauncherMaxRiskScore != 0.40 {
		t.Errorf("EXPLORATION SerialLauncherMaxRiskScore: want 0.40, got %v",
			exploration.SerialLauncherMaxRiskScore)
	}
	if exploration.SerialLauncherMinHolderCount != 50 {
		t.Errorf("EXPLORATION SerialLauncherMinHolderCount: want 50, got %d",
			exploration.SerialLauncherMinHolderCount)
	}
}

// TestDataQualityYAML_VeryExplorationModeSerialLauncherValues asserts that the
// VERY_EXPLORATION mode profile carries the canonical serial-launcher override
// values — social-links gate enabled, holder floor 25, risk ceiling 0.45.
// Task 28 (2026-06-01): HOTFIX reverted; Task 26 fallback probe restored
// HolderDistKnown coverage to ≥90%, so gates are re-tightened to canonical values.
func TestDataQualityYAML_VeryExplorationModeSerialLauncherValues(t *testing.T) {
	cfg := loadDataQualityYAML(t)

	veryExploration, ok := cfg.ModeProfiles["very_exploration"]
	if !ok {
		t.Fatal("very_exploration mode profile missing")
	}

	if veryExploration.MaxCreatorPrevTokenCount != 10 {
		t.Errorf("VERY_EXPLORATION MaxCreatorPrevTokenCount: want 10, got %d",
			veryExploration.MaxCreatorPrevTokenCount)
	}
	// Task 28: social-links gate re-enabled after Task 26 restored HolderDistKnown ≥90%.
	if !veryExploration.SerialLauncherRequiresSocialLinks {
		t.Error("VERY_EXPLORATION SerialLauncherRequiresSocialLinks: want true (Task 28 re-tighten)")
	}
	if veryExploration.SerialLauncherMaxRiskScore != 0.45 {
		t.Errorf("VERY_EXPLORATION SerialLauncherMaxRiskScore: want 0.45, got %v",
			veryExploration.SerialLauncherMaxRiskScore)
	}
	// Task 28: holder-count floor restored to canonical 25 (was 0 during Task 22 hotfix).
	if veryExploration.SerialLauncherMinHolderCount != 25 {
		t.Errorf("VERY_EXPLORATION SerialLauncherMinHolderCount: want 25 (Task 28 re-tighten), got %d",
			veryExploration.SerialLauncherMinHolderCount)
	}
}

// TestDataQualityYAML_GlobalThresholdUnchanged verifies that the global
// thresholds.max_creator_prev_token_count is still 1 after Task 12.
// Raising the global threshold is forbidden — per-mode overrides are the
// only sanctioned mechanism for relaxing the serial-launcher gate.
func TestDataQualityYAML_GlobalThresholdUnchanged(t *testing.T) {
	cfg := loadDataQualityYAML(t)

	if cfg.Thresholds.MaxCreatorPrevTokenCount != 1 {
		t.Errorf("global thresholds.max_creator_prev_token_count: want 1 (must not be raised), got %d",
			cfg.Thresholds.MaxCreatorPrevTokenCount)
	}
}

// TestDataQualityYAML_ExistingModeFieldsUnchanged verifies that adding the new
// serial-launcher fields to mode_profiles did NOT alter any existing field values.
// This is the append-only invariant verification for config changes.
func TestDataQualityYAML_ExistingModeFieldsUnchanged(t *testing.T) {
	cfg := loadDataQualityYAML(t)

	cases := []struct {
		mode           string
		wantReject     float64
		wantRiskyPass  float64
		wantUnknownFac float64
		wantMinAge     int32
	}{
		{"strict", 0.30, 0.15, 0.5, 0},
		{"balanced", 0.50, 0.25, 0.35, 0},
		{"exploration", 0.65, 0.35, 0.0, -1},
		{"very_exploration", 0.75, 0.45, 0.0, -1},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.mode, func(t *testing.T) {
			profile, ok := cfg.ModeProfiles[tc.mode]
			if !ok {
				t.Fatalf("mode %q not found", tc.mode)
			}
			if profile.RejectAbove != tc.wantReject {
				t.Errorf("reject_above: want %v, got %v", tc.wantReject, profile.RejectAbove)
			}
			if profile.RiskyPassAbove != tc.wantRiskyPass {
				t.Errorf("risky_pass_above: want %v, got %v", tc.wantRiskyPass, profile.RiskyPassAbove)
			}
			if profile.UnknownFactor != tc.wantUnknownFac {
				t.Errorf("unknown_factor: want %v, got %v", tc.wantUnknownFac, profile.UnknownFactor)
			}
			if profile.MinTokenAgeSeconds != tc.wantMinAge {
				t.Errorf("min_token_age_seconds: want %v, got %v", tc.wantMinAge, profile.MinTokenAgeSeconds)
			}
		})
	}
}
