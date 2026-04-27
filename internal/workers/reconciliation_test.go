package workers

// reconciliation_test.go — unit tests for ExceedsToleranceBps and reconCfgFromConfig.

import (
	"math/big"
	"testing"

	"crypto-sniping-bot/internal/app/config"
)

// ── ExceedsToleranceBps ───────────────────────────────────────────────────────

func TestExceedsToleranceBps_WithinTolerance(t *testing.T) {
	t.Parallel()
	// db=1000, onchain=999 → diff=1, 1*10000=10000, 1000*50=50000 → 10000 ≤ 50000 → false
	db := big.NewInt(1000)
	onchain := big.NewInt(999)
	if ExceedsToleranceBps(db, onchain, 50) {
		t.Error("expected false (within tolerance), got true")
	}
}

func TestExceedsToleranceBps_ExceedsTolerance(t *testing.T) {
	t.Parallel()
	// db=1000, onchain=900 → diff=100, 100*10000=1_000_000, 1000*50=50000 → exceeds
	db := big.NewInt(1000)
	onchain := big.NewInt(900)
	if !ExceedsToleranceBps(db, onchain, 50) {
		t.Error("expected true (exceeds tolerance), got false")
	}
}

func TestExceedsToleranceBps_ZeroDb_NonZeroOnchain(t *testing.T) {
	t.Parallel()
	// db=0, onchain≠0 → special case: always exceeds
	db := big.NewInt(0)
	onchain := big.NewInt(100)
	if !ExceedsToleranceBps(db, onchain, 50) {
		t.Error("expected true (db=0, onchain≠0)")
	}
}

func TestExceedsToleranceBps_ZeroDb_ZeroOnchain(t *testing.T) {
	t.Parallel()
	db := big.NewInt(0)
	onchain := big.NewInt(0)
	if ExceedsToleranceBps(db, onchain, 50) {
		t.Error("expected false (both zero)")
	}
}

func TestExceedsToleranceBps_ExactlyAtTolerance_NotExceeds(t *testing.T) {
	t.Parallel()
	// db=10000, toleranceBps=100 (1%), diff=100 is exactly at tolerance.
	// diff*10000 = 1_000_000; db*bps = 10000*100 = 1_000_000 → NOT greater → false
	db := big.NewInt(10000)
	onchain := big.NewInt(9900)
	if ExceedsToleranceBps(db, onchain, 100) {
		t.Error("expected false at exact tolerance boundary")
	}
}

func TestExceedsToleranceBps_JustAboveTolerance_Exceeds(t *testing.T) {
	t.Parallel()
	// db=10000, toleranceBps=100, diff=101 → just above 1%
	db := big.NewInt(10000)
	onchain := big.NewInt(9899)
	if !ExceedsToleranceBps(db, onchain, 100) {
		t.Error("expected true just above tolerance")
	}
}

func TestExceedsToleranceBps_OnchainGreaterThanDb(t *testing.T) {
	t.Parallel()
	// Absolute diff matters (not signed).
	db := big.NewInt(900)
	onchain := big.NewInt(1000) // 11% greater
	if !ExceedsToleranceBps(db, onchain, 50) {
		t.Error("expected true when onchain > db by >0.5%")
	}
}

// ── reconCfgFromConfig ────────────────────────────────────────────────────────

func TestReconCfgFromConfig_UsesConfigValues(t *testing.T) {
	t.Parallel()
	h := config.HardeningConfig{
		ReconciliationIntervalMs:   15_000,
		ReconciliationToleranceBps: 100,
	}
	cfg := reconCfgFromConfig(h)
	if cfg.IntervalMs != 15_000 {
		t.Errorf("IntervalMs = %d, want 15000", cfg.IntervalMs)
	}
	if cfg.ToleranceBps != 100 {
		t.Errorf("ToleranceBps = %d, want 100", cfg.ToleranceBps)
	}
}

func TestReconCfgFromConfig_DefaultsApplied_WhenZero(t *testing.T) {
	t.Parallel()
	// Zero values should trigger safe defaults.
	h := config.HardeningConfig{
		ReconciliationIntervalMs:   0,
		ReconciliationToleranceBps: 0,
	}
	cfg := reconCfgFromConfig(h)
	if cfg.IntervalMs <= 0 {
		t.Errorf("IntervalMs should default to positive, got %d", cfg.IntervalMs)
	}
	if cfg.ToleranceBps <= 0 {
		t.Errorf("ToleranceBps should default to positive, got %d", cfg.ToleranceBps)
	}
}

func TestReconCfgFromConfig_NegativeValues_UsesDefaults(t *testing.T) {
	t.Parallel()
	h := config.HardeningConfig{
		ReconciliationIntervalMs:   -1,
		ReconciliationToleranceBps: -5,
	}
	cfg := reconCfgFromConfig(h)
	if cfg.IntervalMs <= 0 {
		t.Errorf("IntervalMs should default to positive for negative input, got %d", cfg.IntervalMs)
	}
	if cfg.ToleranceBps <= 0 {
		t.Errorf("ToleranceBps should default to positive for negative input, got %d", cfg.ToleranceBps)
	}
}
