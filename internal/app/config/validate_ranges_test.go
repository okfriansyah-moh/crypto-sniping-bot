package config

// F-SEC-06 regression tests for bounded-range YAML startup validation.

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

// captureLogger wires a slog.Logger that writes JSON to a bytes.Buffer
// so tests can assert that warnings were emitted.
func captureLogger() (*slog.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	h := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(h), buf
}

// TestValidateRanges_SoftViolations_EmitWarning verifies that out-of-band
// values trigger structured warnings without aborting startup.
func TestValidateRanges_SoftViolations_EmitWarning(t *testing.T) {
	cfg := &Config{
		Validation: ValidationConfig{
			JoinTimeoutMs:      9_000, // > 5000 → warn
			JoinPollIntervalMs: 2_000, // > 1000 → warn
		},
		Feature: FeatureRuntimeConfig{
			Stability: FeatureStabilityConfig{MinConsistency: 1.5}, // > 1 → warn
		},
		DataQualityRuntime: DataQualityRuntimeConfig{
			ModeProfiles: map[string]DataQualityModeProfile{
				"strict": {RejectAbove: 1.4, UnknownFactor: 2.5}, // both warn
			},
			RiskWeights: DataQualityRiskWeights{Honeypot: 1.5}, // warn
		},
	}

	logger, buf := captureLogger()
	if err := cfg.validateRanges(logger); err != nil {
		t.Fatalf("soft violations must not error; got %v", err)
	}
	out := buf.String()
	for _, want := range []string{
		"feature.stability.min_consistency",
		"data_quality.mode_profiles.strict.unknown_factor",
		"data_quality.mode_profiles.strict.reject_above",
		"data_quality.risk_weights.honeypot",
		"validation.join_timeout_ms",
		"validation.join_poll_interval_ms",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("expected warning for %q in log output; got %s", want, out)
		}
	}
	_ = context.Background() // imported for any future ctx-aware check
}

// TestValidateRanges_HardViolation_NegativeUnknownFactor returns error.
func TestValidateRanges_HardViolation_NegativeUnknownFactor(t *testing.T) {
	cfg := &Config{
		DataQualityRuntime: DataQualityRuntimeConfig{
			ModeProfiles: map[string]DataQualityModeProfile{
				"strict": {UnknownFactor: -0.1},
			},
		},
	}
	logger, _ := captureLogger()
	if err := cfg.validateRanges(logger); err == nil {
		t.Fatal("expected error for negative unknown_factor; got nil")
	}
}

// TestValidateRanges_HardViolation_NegativeJoinTimeout returns error.
func TestValidateRanges_HardViolation_NegativeJoinTimeout(t *testing.T) {
	cfg := &Config{
		Validation: ValidationConfig{
			JoinTimeoutMs:      -1,
			JoinPollIntervalMs: 50,
		},
	}
	logger, _ := captureLogger()
	if err := cfg.validateRanges(logger); err == nil {
		t.Fatal("expected error for negative join_timeout_ms; got nil")
	}
}

// TestValidateRanges_HardViolation_ZeroPollInterval returns error when
// the join wait is enabled (timeout > 0).
func TestValidateRanges_HardViolation_ZeroPollInterval(t *testing.T) {
	cfg := &Config{
		Validation: ValidationConfig{
			JoinTimeoutMs:      500,
			JoinPollIntervalMs: 0,
		},
	}
	logger, _ := captureLogger()
	if err := cfg.validateRanges(logger); err == nil {
		t.Fatal("expected error for zero poll interval with active join wait; got nil")
	}
}

// TestValidateRanges_DisabledJoinWait_AllowsZero verifies the carve-out
// for join_timeout_ms=0 + join_poll_interval_ms=0 (legacy single-shot).
func TestValidateRanges_DisabledJoinWait_AllowsZero(t *testing.T) {
	cfg := &Config{
		Validation: ValidationConfig{
			JoinTimeoutMs:      0,
			JoinPollIntervalMs: 0,
		},
	}
	logger, _ := captureLogger()
	if err := cfg.validateRanges(logger); err != nil {
		t.Fatalf("legacy single-shot should be permitted; got %v", err)
	}
}

// TestValidateRanges_NoOpOnCleanConfig — happy path.
func TestValidateRanges_NoOpOnCleanConfig(t *testing.T) {
	cfg := &Config{
		Validation: ValidationConfig{JoinTimeoutMs: 200, JoinPollIntervalMs: 50},
		Feature: FeatureRuntimeConfig{
			Stability: FeatureStabilityConfig{MinConsistency: 0.6},
		},
		DataQualityRuntime: DataQualityRuntimeConfig{
			ModeProfiles: map[string]DataQualityModeProfile{
				"strict":      {RejectAbove: 0.30, UnknownFactor: 0.5},
				"balanced":    {RejectAbove: 0.50, UnknownFactor: 0.0},
				"exploration": {RejectAbove: 0.65, UnknownFactor: 0.0},
			},
			RiskWeights: DataQualityRiskWeights{
				Honeypot: 0.30, TaxAnomaly: 0.20, RugAuthority: 0.20,
				LpLockMissing: 0.15, WashTrading: 0.10, ContractUnverified: 0.05,
			},
		},
	}
	logger, buf := captureLogger()
	if err := cfg.validateRanges(logger); err != nil {
		t.Fatalf("clean config must not error; got %v", err)
	}
	if strings.Contains(buf.String(), "config_range_warning") {
		t.Errorf("clean config must not emit warnings; got %s", buf.String())
	}
}
