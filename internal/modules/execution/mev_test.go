package execution

import (
	"testing"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// ── PickRoute ─────────────────────────────────────────────────────────────────

func TestPickRoute_BelowThreshold_ReturnsPublic(t *testing.T) {
	// Arrange
	alloc := contracts.AllocationDTO{SizeUsd: 100}
	lat := contracts.LatencyProfileDTO{ExpectedP95Ms: 200}
	cfg := config.MEVConfig{
		PrivateSizeThresholdUsd: 500,
		PreferredPrivate:        "flashbots",
		FrontRunWindowMs:        500,
	}

	// Act
	route := PickRoute(alloc, lat, cfg)

	// Assert
	if route != "public" {
		t.Errorf("expected public route, got %q", route)
	}
}

func TestPickRoute_AboveThreshold_ReturnsPrivate(t *testing.T) {
	alloc := contracts.AllocationDTO{SizeUsd: 1000}
	lat := contracts.LatencyProfileDTO{ExpectedP95Ms: 100}
	cfg := config.MEVConfig{
		PrivateSizeThresholdUsd: 500,
		PreferredPrivate:        "flashbots",
		FrontRunWindowMs:        500,
	}

	route := PickRoute(alloc, lat, cfg)
	if route != "flashbots" {
		t.Errorf("expected flashbots, got %q", route)
	}
}

func TestPickRoute_HighLatency_RoutesPrivate(t *testing.T) {
	// Arrange — latency P95 exceeds front-run window
	alloc := contracts.AllocationDTO{SizeUsd: 50}
	lat := contracts.LatencyProfileDTO{ExpectedP95Ms: 1000}
	cfg := config.MEVConfig{
		PrivateSizeThresholdUsd: 500,
		PreferredPrivate:        "beaverbuild",
		FrontRunWindowMs:        500,
	}

	route := PickRoute(alloc, lat, cfg)
	if route != "beaverbuild" {
		t.Errorf("expected beaverbuild for high latency, got %q", route)
	}
}

func TestPickRoute_ZeroThreshold_DoesNotTriggerSizeRule(t *testing.T) {
	// PrivateSizeThresholdUsd == 0 disables size rule
	alloc := contracts.AllocationDTO{SizeUsd: 99999}
	lat := contracts.LatencyProfileDTO{ExpectedP95Ms: 100}
	cfg := config.MEVConfig{
		PrivateSizeThresholdUsd: 0,
		PreferredPrivate:        "flashbots",
		FrontRunWindowMs:        500,
	}

	route := PickRoute(alloc, lat, cfg)
	if route != "public" {
		t.Errorf("expected public when threshold=0, got %q", route)
	}
}

// ── resolvePrivateRoute ───────────────────────────────────────────────────────

func TestPickRoute_KnownPreferred_Eden(t *testing.T) {
	alloc := contracts.AllocationDTO{SizeUsd: 10000}
	lat := contracts.LatencyProfileDTO{ExpectedP95Ms: 100}
	cfg := config.MEVConfig{
		PrivateSizeThresholdUsd: 500,
		PreferredPrivate:        "eden",
		FrontRunWindowMs:        0,
	}
	if route := PickRoute(alloc, lat, cfg); route != "eden" {
		t.Errorf("expected eden, got %q", route)
	}
}

func TestPickRoute_UnknownPreferred_FallsBackToFlashbots(t *testing.T) {
	alloc := contracts.AllocationDTO{SizeUsd: 10000}
	lat := contracts.LatencyProfileDTO{ExpectedP95Ms: 100}
	cfg := config.MEVConfig{
		PrivateSizeThresholdUsd: 500,
		PreferredPrivate:        "unknown_relay",
	}
	route := PickRoute(alloc, lat, cfg)
	if route != "flashbots" {
		t.Errorf("expected fallback to flashbots for unknown preferred, got %q", route)
	}
}

// ── ComputeSlippageGuard ──────────────────────────────────────────────────────

func TestComputeSlippageGuard_Zero_ReturnsOne(t *testing.T) {
	if got := ComputeSlippageGuard(0); got != 1.0 {
		t.Errorf("expected 1.0 for 0 bps, got %f", got)
	}
}

func TestComputeSlippageGuard_NegativeBps_ReturnsOne(t *testing.T) {
	if got := ComputeSlippageGuard(-10); got != 1.0 {
		t.Errorf("expected 1.0 for negative bps, got %f", got)
	}
}

func TestComputeSlippageGuard_TenThousandBps_ReturnsZero(t *testing.T) {
	if got := ComputeSlippageGuard(10000); got != 0.0 {
		t.Errorf("expected 0.0 for 10000 bps, got %f", got)
	}
}

func TestComputeSlippageGuard_ExceedsTenThousand_ReturnsZero(t *testing.T) {
	if got := ComputeSlippageGuard(20000); got != 0.0 {
		t.Errorf("expected 0.0 for >10000 bps, got %f", got)
	}
}

func TestComputeSlippageGuard_NormalBps_ReturnsExpectedRatio(t *testing.T) {
	// 150 bps = 1.5% → guard = 1 - 0.015 = 0.985
	got := ComputeSlippageGuard(150)
	want := 0.985
	if got < want-0.0001 || got > want+0.0001 {
		t.Errorf("expected ~0.985, got %f", got)
	}
}

// ── ExceedsSlippageGuard ──────────────────────────────────────────────────────

func TestExceedsSlippageGuard_EstimateAboveGuard_ReturnsTrue(t *testing.T) {
	if !ExceedsSlippageGuard(200, 150) {
		t.Error("expected true when estimated > guard")
	}
}

func TestExceedsSlippageGuard_EstimateBelowGuard_ReturnsFalse(t *testing.T) {
	if ExceedsSlippageGuard(100, 150) {
		t.Error("expected false when estimated < guard")
	}
}

func TestExceedsSlippageGuard_Equal_ReturnsFalse(t *testing.T) {
	if ExceedsSlippageGuard(150, 150) {
		t.Error("expected false when estimated == guard (strict greater than)")
	}
}
