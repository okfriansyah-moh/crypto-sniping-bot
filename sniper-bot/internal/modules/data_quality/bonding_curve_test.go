package data_quality

import (
	"context"
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// TestDataQuality_BondingCurveProgressGate verifies Task F: when
// MaxBondingCurveProgressBps > 0 and MarketDataDTO carries a curve
// progress beyond the cap, the event is rejected with the dedicated
// reason.
func TestDataQuality_BondingCurveProgressGate(t *testing.T) {
	rt := &config.DataQualityRuntimeConfig{
		Thresholds: config.DataQualityDetectorThresholds{
			MaxBondingCurveProgressBps: 5000, // reject when > 50%
		},
	}
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := contracts.MarketDataDTO{
		EventID:                 "ev-curve",
		TraceID:                 "tr",
		VersionID:               "v",
		Chain:                   "solana",
		EventTopic:              "PoolCreated",
		TokenAddress:            "PUMP1111111111111111111111111111111111111",
		ReserveBaseRaw:          "1000000000000000000",
		ReserveTokenRaw:         "1000",
		BondingCurveProgressBps: 7500, // 75 % — above cap.
	}

	out, err := m.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	found := false
	for _, r := range out.RejectReasons {
		if r == "bonding_curve_too_advanced" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("RejectReasons missing bonding_curve_too_advanced: %v", out.RejectReasons)
	}
	if out.Decision != "REJECT" {
		t.Errorf("Decision = %q, want REJECT", out.Decision)
	}
}

// TestDataQuality_BondingCurveDisabledWhenZero verifies the gate is a
// no-op when MaxBondingCurveProgressBps is 0 (default).
func TestDataQuality_BondingCurveDisabledWhenZero(t *testing.T) {
	rt := &config.DataQualityRuntimeConfig{} // MaxBondingCurveProgressBps == 0
	m := New(DefaultConfig(nil), nil).WithRuntimeConfig(rt)

	in := contracts.MarketDataDTO{
		EventID:                 "ev-curve-2",
		TraceID:                 "tr",
		VersionID:               "v",
		Chain:                   "solana",
		EventTopic:              "PoolCreated",
		TokenAddress:            "PUMP2222222222222222222222222222222222222",
		ReserveBaseRaw:          "1000000000000000000",
		ReserveTokenRaw:         "1000",
		BondingCurveProgressBps: 9999,
	}

	out, _ := m.Process(context.Background(), in)
	for _, r := range out.RejectReasons {
		if r == "bonding_curve_too_advanced" {
			t.Fatalf("gate fired despite cfg=0: %v", out.RejectReasons)
		}
	}
}
