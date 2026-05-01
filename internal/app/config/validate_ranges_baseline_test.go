package config

import "testing"

// Residual-risk #1 — baseline persistence validator tests.

func TestValidateRanges_FeatureBaselineFlushInterval_BelowMin_Errors(t *testing.T) {
	cfg := &Config{
		Validation: ValidationConfig{JoinTimeoutMs: 200, JoinPollIntervalMs: 50},
		Feature:    FeatureRuntimeConfig{BaselineFlushIntervalSec: 1},
	}
	logger, _ := captureLogger()
	if err := cfg.validateRanges(logger); err == nil {
		t.Fatal("expected hard error for feature.baseline_flush_interval_sec=1; got nil")
	}
}

func TestValidateRanges_FeatureBaselineFlushMaxWrites_BelowMin_Errors(t *testing.T) {
	cfg := &Config{
		Validation: ValidationConfig{JoinTimeoutMs: 200, JoinPollIntervalMs: 50},
		Feature:    FeatureRuntimeConfig{BaselineFlushMaxWrites: -3},
	}
	logger, _ := captureLogger()
	if err := cfg.validateRanges(logger); err == nil {
		t.Fatal("expected hard error for feature.baseline_flush_max_writes=-3; got nil")
	}
}

func TestValidateRanges_EdgeBaselineFlushInterval_BelowMin_Errors(t *testing.T) {
	cfg := &Config{
		Validation: ValidationConfig{JoinTimeoutMs: 200, JoinPollIntervalMs: 50},
		Edge:       EdgeConfig{BaselineFlushIntervalSec: 4},
	}
	logger, _ := captureLogger()
	if err := cfg.validateRanges(logger); err == nil {
		t.Fatal("expected hard error for edge.baseline_flush_interval_sec=4; got nil")
	}
}

func TestValidateRanges_BaselineFlush_ZeroAllowed(t *testing.T) {
	// Zero means "use default" — must not error.
	cfg := &Config{
		Validation: ValidationConfig{JoinTimeoutMs: 200, JoinPollIntervalMs: 50},
		Feature:    FeatureRuntimeConfig{BaselineFlushIntervalSec: 0, BaselineFlushMaxWrites: 0},
		Edge:       EdgeConfig{BaselineFlushIntervalSec: 0, BaselineFlushMaxWrites: 0},
	}
	logger, _ := captureLogger()
	if err := cfg.validateRanges(logger); err != nil {
		t.Fatalf("zero (=default) must be permitted; got %v", err)
	}
}

func TestValidateRanges_BaselineFlush_AtOrAboveMin_OK(t *testing.T) {
	cfg := &Config{
		Validation: ValidationConfig{JoinTimeoutMs: 200, JoinPollIntervalMs: 50},
		Feature:    FeatureRuntimeConfig{BaselineFlushIntervalSec: 5, BaselineFlushMaxWrites: 1},
		Edge:       EdgeConfig{BaselineFlushIntervalSec: 60, BaselineFlushMaxWrites: 100},
	}
	logger, _ := captureLogger()
	if err := cfg.validateRanges(logger); err != nil {
		t.Fatalf("valid flush config must not error; got %v", err)
	}
}
