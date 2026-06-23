package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"crypto-sniping-bot/internal/app/config"
)

func TestResolveModeThresholds_Balanced_ReturnsYAMLValues(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Priority = config.PriorityConfig{
		Modes: config.PriorityModesConfig{
			Balanced: config.ModeThresholdProfile{
				ExploreBudgetPct: 2.0,
				EdgeStrengthMin:  0.60,
				EvThresholdBps:   100,
				MaxPositions:     15,
			},
		},
	}

	got := cfg.ResolveModeThresholds("BALANCED")

	if got.Mode != "BALANCED" {
		t.Fatalf("Mode: want BALANCED, got %q", got.Mode)
	}
	if got.EvThresholdBps != 100 {
		t.Fatalf("EvThresholdBps: want 100, got %d", got.EvThresholdBps)
	}
	if got.EdgeStrengthMin != 0.60 {
		t.Fatalf("EdgeStrengthMin: want 0.60, got %v", got.EdgeStrengthMin)
	}
	if got.MaxPositions != 15 {
		t.Fatalf("MaxPositions: want 15, got %d", got.MaxPositions)
	}
}

func TestResolveModeThresholds_VeryExploration_NormalizesYAMLKey(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Priority = config.PriorityConfig{
		Modes: config.PriorityModesConfig{
			VeryExploration: config.ModeThresholdProfile{
				ExploreBudgetPct: 8.0,
				EdgeStrengthMin:  0.30,
				EvThresholdBps:   30,
				MaxPositions:     25,
			},
		},
	}

	got := cfg.ResolveModeThresholds("very_exploration")

	if got.Mode != "VERY_EXPLORATION" {
		t.Fatalf("Mode: want VERY_EXPLORATION, got %q", got.Mode)
	}
	if got.EvThresholdBps != 30 {
		t.Fatalf("EvThresholdBps: want 30, got %d", got.EvThresholdBps)
	}
}

func TestResolveModeThresholds_UnknownMode_FailClosedToStrict(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Priority = config.PriorityConfig{
		Modes: config.PriorityModesConfig{
			Strict: config.ModeThresholdProfile{
				ExploreBudgetPct: 1.0,
				EdgeStrengthMin:  0.75,
				EvThresholdBps:   150,
				MaxPositions:     5,
			},
			Balanced: config.ModeThresholdProfile{
				ExploreBudgetPct: 2.0,
				EdgeStrengthMin:  0.60,
				EvThresholdBps:   100,
				MaxPositions:     15,
			},
		},
	}

	got := cfg.ResolveModeThresholds("UNKNOWN_MODE")

	if got.Mode != "STRICT" {
		t.Fatalf("Mode: want STRICT fail-closed, got %q", got.Mode)
	}
	if got.EvThresholdBps != 150 {
		t.Fatalf("EvThresholdBps: want 150 (strict), got %d", got.EvThresholdBps)
	}
}

func TestResolveModeThresholds_NilConfig_ReturnsCanonicalStrict(t *testing.T) {
	var cfg *config.Config
	got := cfg.ResolveModeThresholds("BALANCED")
	if got.Mode != "STRICT" || got.EvThresholdBps != 150 {
		t.Fatalf("nil config should fail-closed to canonical strict, got %+v", got)
	}
}

func TestResolveActiveModeThresholds_UsesActiveMode(t *testing.T) {
	cfg := minimalValidConfig()
	cfg.Priority = config.PriorityConfig{
		ActiveMode: "exploration",
		Modes: config.PriorityModesConfig{
			Exploration: config.ModeThresholdProfile{
				ExploreBudgetPct: 5.0,
				EdgeStrengthMin:  0.45,
				EvThresholdBps:   60,
				MaxPositions:     20,
			},
		},
	}

	got := cfg.ResolveActiveModeThresholds()
	if got.Mode != "EXPLORATION" || got.EvThresholdBps != 60 {
		t.Fatalf("unexpected active-mode thresholds: %+v", got)
	}
}

func TestLoad_PriorityYAML_MergesModes(t *testing.T) {
	repoRoot, err := findRepoRoot()
	if err != nil {
		t.Skipf("repo root not found: %v", err)
	}
	cfgPath := filepath.Join(repoRoot, "shared", "config", "pipeline.yaml")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	got := cfg.ResolveModeThresholds("BALANCED")
	if got.EvThresholdBps != 100 {
		t.Fatalf("balanced ev_threshold_bps: want 100 from priority.yaml, got %d", got.EvThresholdBps)
	}
	if got.MaxPositions != 15 {
		t.Fatalf("balanced max_positions: want 15, got %d", got.MaxPositions)
	}

	strict := cfg.ResolveModeThresholds("STRICT")
	if strict.EvThresholdBps != 150 {
		t.Fatalf("strict ev_threshold_bps: want 150, got %d", strict.EvThresholdBps)
	}
}

func findRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir := cwd
	for {
		if _, statErr := os.Stat(filepath.Join(dir, "shared", "config", "priority.yaml")); statErr == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", os.ErrNotExist
		}
		dir = parent
	}
}
