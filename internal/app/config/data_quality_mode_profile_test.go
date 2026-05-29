// Tests for DataQualityModeProfile per-mode serial-launcher fields (Task 11).
// These assert that all four new fields default to their zero values, which
// preserves STRICT/BALANCED behaviour when the YAML does not set them.
package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"crypto-sniping-bot/internal/app/config"
)

// TestModeProfile_NewFieldsZeroByDefault verifies that a DataQualityModeProfile
// decoded from YAML containing only the original four keys has zero values for
// the four new serial-launcher fields introduced in Task 11.
// Zero values are the safe sentinel: MaxCreatorPrevTokenCount=0 means
// "use global threshold" and all gate fields disabled.
func TestModeProfile_NewFieldsZeroByDefault(t *testing.T) {
	yamlInput := `
reject_above: 0.7
risky_pass_above: 0.4
unknown_factor: 0.5
min_token_age_seconds: 900
`
	var profile config.DataQualityModeProfile
	if err := yaml.Unmarshal([]byte(yamlInput), &profile); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}

	// Existing fields must decode correctly.
	if profile.RejectAbove != 0.7 {
		t.Errorf("RejectAbove: want 0.7, got %v", profile.RejectAbove)
	}
	if profile.MinTokenAgeSeconds != 900 {
		t.Errorf("MinTokenAgeSeconds: want 900, got %v", profile.MinTokenAgeSeconds)
	}

	// New fields must be zero (safe defaults).
	if profile.MaxCreatorPrevTokenCount != 0 {
		t.Errorf("MaxCreatorPrevTokenCount: want 0 (use-global sentinel), got %v", profile.MaxCreatorPrevTokenCount)
	}
	if profile.SerialLauncherRequiresSocialLinks {
		t.Error("SerialLauncherRequiresSocialLinks: want false, got true")
	}
	if profile.SerialLauncherMaxRiskScore != 0.0 {
		t.Errorf("SerialLauncherMaxRiskScore: want 0.0, got %v", profile.SerialLauncherMaxRiskScore)
	}
	if profile.SerialLauncherMinHolderCount != 0 {
		t.Errorf("SerialLauncherMinHolderCount: want 0, got %v", profile.SerialLauncherMinHolderCount)
	}
}

// TestModeProfile_StrictModeKeepsGlobalThreshold verifies the STRICT mode
// invariant: MaxCreatorPrevTokenCount must be 0 (use-global sentinel).
// A non-zero value on STRICT would silently raise the threshold, breaking
// the architecture invariant that STRICT/BALANCED always hard-reject serial launchers.
func TestModeProfile_StrictModeKeepsGlobalThreshold(t *testing.T) {
	strictYAML := `
reject_above: 0.9
risky_pass_above: 0.7
unknown_factor: 0.5
min_token_age_seconds: 900
max_creator_prev_token_count: 0
serial_launcher_requires_social_links: false
serial_launcher_max_risk_score: 0.0
serial_launcher_min_holder_count: 0
`
	var profile config.DataQualityModeProfile
	if err := yaml.Unmarshal([]byte(strictYAML), &profile); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}

	if profile.MaxCreatorPrevTokenCount != 0 {
		t.Errorf("STRICT MaxCreatorPrevTokenCount must be 0, got %v", profile.MaxCreatorPrevTokenCount)
	}
	if profile.SerialLauncherRequiresSocialLinks {
		t.Error("STRICT SerialLauncherRequiresSocialLinks must be false")
	}
}

// TestModeProfile_ExplorationModeCanOverrideThreshold verifies that the
// EXPLORATION mode profile correctly decodes exploration-mode override values.
// EXPLORATION: MaxCreatorPrevTokenCount=5, SerialLauncherMaxRiskScore=0.40,
// SerialLauncherMinHolderCount=50, SerialLauncherRequiresSocialLinks=true.
func TestModeProfile_ExplorationModeCanOverrideThreshold(t *testing.T) {
	explorationYAML := `
reject_above: 0.85
risky_pass_above: 0.5
unknown_factor: 0.0
min_token_age_seconds: 0
max_creator_prev_token_count: 5
serial_launcher_requires_social_links: true
serial_launcher_max_risk_score: 0.40
serial_launcher_min_holder_count: 50
`
	var profile config.DataQualityModeProfile
	if err := yaml.Unmarshal([]byte(explorationYAML), &profile); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}

	if profile.MaxCreatorPrevTokenCount != 5 {
		t.Errorf("EXPLORATION MaxCreatorPrevTokenCount: want 5, got %v", profile.MaxCreatorPrevTokenCount)
	}
	if !profile.SerialLauncherRequiresSocialLinks {
		t.Error("EXPLORATION SerialLauncherRequiresSocialLinks: want true, got false")
	}
	if profile.SerialLauncherMaxRiskScore != 0.40 {
		t.Errorf("EXPLORATION SerialLauncherMaxRiskScore: want 0.40, got %v", profile.SerialLauncherMaxRiskScore)
	}
	if profile.SerialLauncherMinHolderCount != 50 {
		t.Errorf("EXPLORATION SerialLauncherMinHolderCount: want 50, got %v", profile.SerialLauncherMinHolderCount)
	}
}

// TestModeProfile_VeryExplorationModeValues verifies VERY_EXPLORATION values:
// MaxCreatorPrevTokenCount=10, SerialLauncherMaxRiskScore=0.45,
// SerialLauncherMinHolderCount=25.
func TestModeProfile_VeryExplorationModeValues(t *testing.T) {
	veryExplorationYAML := `
reject_above: 0.90
risky_pass_above: 0.55
unknown_factor: 0.0
min_token_age_seconds: 0
max_creator_prev_token_count: 10
serial_launcher_requires_social_links: true
serial_launcher_max_risk_score: 0.45
serial_launcher_min_holder_count: 25
`
	var profile config.DataQualityModeProfile
	if err := yaml.Unmarshal([]byte(veryExplorationYAML), &profile); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}

	if profile.MaxCreatorPrevTokenCount != 10 {
		t.Errorf("VERY_EXPLORATION MaxCreatorPrevTokenCount: want 10, got %v", profile.MaxCreatorPrevTokenCount)
	}
	if profile.SerialLauncherMaxRiskScore != 0.45 {
		t.Errorf("VERY_EXPLORATION SerialLauncherMaxRiskScore: want 0.45, got %v", profile.SerialLauncherMaxRiskScore)
	}
	if profile.SerialLauncherMinHolderCount != 25 {
		t.Errorf("VERY_EXPLORATION SerialLauncherMinHolderCount: want 25, got %v", profile.SerialLauncherMinHolderCount)
	}
}

// TestModeProfile_RoundTripThroughRuntimeConfig verifies that the new fields
// survive a full YAML → DataQualityRuntimeConfig → ModeProfiles map round-trip.
// This mirrors how the actual config loader reads data_quality.yaml at startup.
func TestModeProfile_RoundTripThroughRuntimeConfig(t *testing.T) {
	fullYAML := `
detector_timeout_ms: 500
total_budget_ms: 3000
pass_threshold: 0.35
reject_threshold: 0.65
mode_profiles:
  strict:
    reject_above: 0.9
    risky_pass_above: 0.7
    unknown_factor: 0.5
    min_token_age_seconds: 900
    max_creator_prev_token_count: 0
    serial_launcher_requires_social_links: false
    serial_launcher_max_risk_score: 0.0
    serial_launcher_min_holder_count: 0
  exploration:
    reject_above: 0.85
    risky_pass_above: 0.5
    unknown_factor: 0.0
    min_token_age_seconds: 0
    max_creator_prev_token_count: 5
    serial_launcher_requires_social_links: true
    serial_launcher_max_risk_score: 0.40
    serial_launcher_min_holder_count: 50
`
	var cfg config.DataQualityRuntimeConfig
	if err := yaml.Unmarshal([]byte(fullYAML), &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}

	strict, ok := cfg.ModeProfiles["strict"]
	if !ok {
		t.Fatal("strict mode profile missing")
	}
	if strict.MaxCreatorPrevTokenCount != 0 {
		t.Errorf("strict MaxCreatorPrevTokenCount: want 0, got %v", strict.MaxCreatorPrevTokenCount)
	}

	exploration, ok := cfg.ModeProfiles["exploration"]
	if !ok {
		t.Fatal("exploration mode profile missing")
	}
	if exploration.MaxCreatorPrevTokenCount != 5 {
		t.Errorf("exploration MaxCreatorPrevTokenCount: want 5, got %v", exploration.MaxCreatorPrevTokenCount)
	}
	if !exploration.SerialLauncherRequiresSocialLinks {
		t.Error("exploration SerialLauncherRequiresSocialLinks: want true")
	}
	if exploration.SerialLauncherMaxRiskScore != 0.40 {
		t.Errorf("exploration SerialLauncherMaxRiskScore: want 0.40, got %v", exploration.SerialLauncherMaxRiskScore)
	}
	if exploration.SerialLauncherMinHolderCount != 50 {
		t.Errorf("exploration SerialLauncherMinHolderCount: want 50, got %v", exploration.SerialLauncherMinHolderCount)
	}
}

// TestModeProfile_YAMLTagNamesMatchFieldNames verifies the yaml struct tags
// match the expected snake_case names used in config/data_quality.yaml.
// Uses a temp file write → yaml.Unmarshal round-trip to catch tag mismatches.
func TestModeProfile_YAMLTagNamesMatchFieldNames(t *testing.T) {
	// Build a raw map using the exact YAML key names that Tasks 12+ will write.
	rawYAML := strings.Join([]string{
		"max_creator_prev_token_count: 7",
		"serial_launcher_requires_social_links: true",
		"serial_launcher_max_risk_score: 0.38",
		"serial_launcher_min_holder_count: 42",
	}, "\n")

	dir := t.TempDir()
	path := filepath.Join(dir, "profile.yaml")
	if err := os.WriteFile(path, []byte(rawYAML), 0o600); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read temp: %v", err)
	}

	var p config.DataQualityModeProfile
	if err := yaml.Unmarshal(data, &p); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if p.MaxCreatorPrevTokenCount != 7 {
		t.Errorf("yaml tag max_creator_prev_token_count: want 7, got %v", p.MaxCreatorPrevTokenCount)
	}
	if !p.SerialLauncherRequiresSocialLinks {
		t.Error("yaml tag serial_launcher_requires_social_links: want true")
	}
	if p.SerialLauncherMaxRiskScore != 0.38 {
		t.Errorf("yaml tag serial_launcher_max_risk_score: want 0.38, got %v", p.SerialLauncherMaxRiskScore)
	}
	if p.SerialLauncherMinHolderCount != 42 {
		t.Errorf("yaml tag serial_launcher_min_holder_count: want 42, got %v", p.SerialLauncherMinHolderCount)
	}
}
