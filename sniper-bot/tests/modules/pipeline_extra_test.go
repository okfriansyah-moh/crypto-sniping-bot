// Package modules_test — additional coverage for uncovered pipeline paths.
// Tests here complement tests/modules/pipeline_test.go with edge cases
// not covered in the primary test file.
package modules_test

import (
	"context"
	"math"
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/sniper-bot/internal/modules/capital"
	"crypto-sniping-bot/sniper-bot/internal/modules/data_quality"
	"crypto-sniping-bot/sniper-bot/internal/modules/edge"
	"crypto-sniping-bot/sniper-bot/internal/modules/features"
	"crypto-sniping-bot/sniper-bot/internal/modules/validation"
)

// ── data_quality: uncovered paths ────────────────────────────────────────────

func TestDataQuality_RejectsMissingTokenAddress(t *testing.T) {
	// Arrange
	in := contracts.MarketDataDTO{
		EventID:        "evt-no-addr",
		TraceID:        "trace-no-addr",
		CorrelationID:  "corr-no-addr",
		ReserveBaseRaw: "5000000000000000000",
		Chain:          "eth",
		VersionID:      "v1",
		TokenAddress:   "", // missing
	}
	mod := data_quality.New(data_quality.DefaultConfig(nil), nil)

	// Act
	dto, err := mod.Process(context.Background(), in)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.Decision != "REJECT" {
		t.Errorf("expected REJECT for missing token address, got %q", dto.Decision)
	}
	found := false
	for _, r := range dto.RejectReasons {
		if r == "missing_token_address" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'missing_token_address' in reject reasons, got %v", dto.RejectReasons)
	}
}

func TestDataQuality_RejectsInsufficientLiquidity(t *testing.T) {
	// Arrange: reserve below the minimum threshold (0.001 ETH = 1e15 wei)
	in := contracts.MarketDataDTO{
		EventID:        "evt-low-liq",
		TraceID:        "trace-low-liq",
		CorrelationID:  "corr-low-liq",
		TokenAddress:   "0xabc",
		Chain:          "eth",
		VersionID:      "v1",
		ReserveBaseRaw: "100000000000000", // 0.0001 ETH — below 0.001 ETH minimum
	}
	mod := data_quality.New(data_quality.DefaultConfig(nil), nil)

	// Act
	dto, err := mod.Process(context.Background(), in)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.Decision != "REJECT" {
		t.Errorf("expected REJECT for insufficient liquidity, got %q", dto.Decision)
	}
	if !dto.IsFakeLiquidity {
		t.Error("expected IsFakeLiquidity=true for insufficient reserves")
	}
}

func TestDataQuality_RejectsSetsRejectReasonsInSortedOrder(t *testing.T) {
	// Arrange: trigger multiple reject reasons
	in := contracts.MarketDataDTO{
		EventID:        "evt-multi",
		TraceID:        "trace-multi",
		CorrelationID:  "corr-multi",
		TokenAddress:   "",  // triggers missing_token_address
		ReserveBaseRaw: "0", // triggers missing_reserves
		Chain:          "eth",
		VersionID:      "v1",
		Reorged:        true, // triggers reorged_event
	}
	mod := data_quality.New(data_quality.DefaultConfig(nil), nil)

	// Act
	dto, err := mod.Process(context.Background(), in)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.Decision != "REJECT" {
		t.Errorf("expected REJECT, got %q", dto.Decision)
	}
	// Verify sorted order (determinism).
	for i := 1; i < len(dto.RejectReasons); i++ {
		if dto.RejectReasons[i] < dto.RejectReasons[i-1] {
			t.Errorf("RejectReasons not sorted: %v", dto.RejectReasons)
		}
	}
}

func TestDataQuality_RiskScoreClamped(t *testing.T) {
	// Arrange: many failures → risk score should not exceed 1.0
	in := contracts.MarketDataDTO{
		EventID:        "evt-high-risk",
		TraceID:        "trace-high-risk",
		CorrelationID:  "corr-high-risk",
		TokenAddress:   "",
		ReserveBaseRaw: "0",
		Reorged:        true,
		Chain:          "eth",
		VersionID:      "v1",
	}
	mod := data_quality.New(data_quality.DefaultConfig(nil), nil)

	// Act
	dto, err := mod.Process(context.Background(), in)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.RiskScore < 0 || dto.RiskScore > 1.0 {
		t.Errorf("RiskScore out of [0,1]: %v", dto.RiskScore)
	}
}

// ── features: uncovered paths ─────────────────────────────────────────────────

func TestFeatures_RugRiskReducesSafety(t *testing.T) {
	// Arrange
	in := contracts.DataQualityDTO{
		EventID:          "dq-rug",
		TraceID:          "trace-rug",
		CorrelationID:    "corr-rug",
		TokenLifecycleID: "lc-rug",
		TokenAddress:     "0xrug",
		Chain:            "eth",
		VersionID:        "v1",
		Decision:         "PASS",
		RiskScore:        0.1,
		IsRugRisk:        true,
	}
	mod := features.New(nil)

	// Act
	dto, err := mod.Process(context.Background(), in)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.ContractSafety >= 1.0 {
		t.Errorf("rug risk should reduce ContractSafety; got %v", dto.ContractSafety)
	}
}

func TestFeatures_FakeLiquidityReducesSafety(t *testing.T) {
	// Arrange
	in := contracts.DataQualityDTO{
		EventID:          "dq-fakeliq",
		TraceID:          "trace-fakeliq",
		CorrelationID:    "corr-fakeliq",
		TokenLifecycleID: "lc-fakeliq",
		TokenAddress:     "0xfakeliq",
		Chain:            "eth",
		VersionID:        "v1",
		Decision:         "PASS",
		RiskScore:        0.1,
		IsFakeLiquidity:  true,
	}
	mod := features.New(nil)

	// Act
	dto, err := mod.Process(context.Background(), in)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.ContractSafety >= 1.0 {
		t.Errorf("fake liquidity should reduce ContractSafety; got %v", dto.ContractSafety)
	}
}

func TestFeatures_AllFlagsFlooredAtZero(t *testing.T) {
	// Arrange: all negative flags set simultaneously → safety should floor at 0
	in := contracts.DataQualityDTO{
		EventID:          "dq-all-bad",
		TraceID:          "trace-all-bad",
		CorrelationID:    "corr-all-bad",
		TokenLifecycleID: "lc-all-bad",
		TokenAddress:     "0xallbad",
		Chain:            "eth",
		VersionID:        "v1",
		Decision:         "REJECT",
		RiskScore:        1.0,
		IsHoneypot:       true,
		IsRugRisk:        true,
		IsFakeLiquidity:  true,
		ContractVerified: false,
	}
	mod := features.New(nil)

	// Act
	dto, err := mod.Process(context.Background(), in)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.ContractSafety < 0 {
		t.Errorf("ContractSafety should floor at 0, got %v", dto.ContractSafety)
	}
	if dto.LiquidityScore < 0 || dto.LiquidityScore > 1 {
		t.Errorf("LiquidityScore out of [0,1]: %v", dto.LiquidityScore)
	}
}

func TestFeatures_HolderCountPropagated(t *testing.T) {
	// Arrange
	in := contracts.DataQualityDTO{
		EventID:          "dq-holders",
		TraceID:          "trace-holders",
		CorrelationID:    "corr-holders",
		TokenLifecycleID: "lc-holders",
		TokenAddress:     "0xholders",
		Chain:            "eth",
		VersionID:        "v1",
		Decision:         "PASS",
		RiskScore:        0.0,
		LpHolderCount:    42,
	}
	mod := features.New(nil)

	// Act
	dto, err := mod.Process(context.Background(), in)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.HolderCountRaw != 42 {
		t.Errorf("expected HolderCountRaw=42, got %d", dto.HolderCountRaw)
	}
}

func TestFeatures_Deterministic(t *testing.T) {
	// Arrange
	in := contracts.DataQualityDTO{
		EventID:          "dq-det",
		TraceID:          "trace-det",
		CorrelationID:    "corr-det",
		TokenLifecycleID: "lc-det",
		TokenAddress:     "0xdet",
		Chain:            "eth",
		VersionID:        "v1",
		Decision:         "PASS",
		RiskScore:        0.2,
	}
	mod := features.New(nil)

	// Act
	dto1, _ := mod.Process(context.Background(), in)
	dto2, _ := mod.Process(context.Background(), in)

	// Assert: EventID is content-addressable → must be equal
	if dto1.EventID != dto2.EventID {
		t.Errorf("non-deterministic EventID: %q vs %q", dto1.EventID, dto2.EventID)
	}
	if dto1.LiquidityScore != dto2.LiquidityScore {
		t.Errorf("non-deterministic LiquidityScore: %v vs %v", dto1.LiquidityScore, dto2.LiquidityScore)
	}
	if dto1.ContractSafety != dto2.ContractSafety {
		t.Errorf("non-deterministic ContractSafety: %v vs %v", dto1.ContractSafety, dto2.ContractSafety)
	}
}

// ── edge: uncovered paths ──────────────────────────────────────────────────────

func TestEdge_OpportunityWindowScalesWithMomentum(t *testing.T) {
	// Arrange: high momentum should widen the opportunity window
	cfg := &config.EdgeConfig{
		MinLiquidityScore:    0.3,
		MinVelocityScore:     0.2,
		BaseWindowMs:         5000,
		WindowMomentumFactor: 0.5, // 50% factor
		TTLSeconds:           8,
	}
	in := contracts.FeatureDTO{
		EventID:          "feat-hi-mom",
		TraceID:          "trace-hi-mom",
		CorrelationID:    "corr-hi-mom",
		TokenLifecycleID: "lc-hi-mom",
		TokenAddress:     "0xhimom",
		VersionID:        "v1",
		LiquidityScore:   0.8,
		TxVelocityScore:  0.6,
		ContractSafety:   0.9,
		VolumeMomentum:   0.8, // high momentum → wider window
		Confidence: contracts.FeatureConfidence{
			LiquidityScore: 0.8,
			ContractSafety: 0.9,
		},
	}
	mod := edge.New(cfg)

	// Act
	dto, err := mod.Process(context.Background(), in)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.OpportunityWindowMs <= cfg.BaseWindowMs {
		t.Errorf("expected window > %d for high momentum, got %d",
			cfg.BaseWindowMs, dto.OpportunityWindowMs)
	}
}

func TestEdge_EdgeStrengthComponents(t *testing.T) {
	// Arrange: known inputs for verifiable NEW_LAUNCH_EDGE strength
	// (per F-4 fix). Weights: liquidity 0.4, safety 0.3, holders 0.2,
	// entropy 0.1.
	cfg := &config.EdgeConfig{
		MinLiquidityScore:        0.0,
		MinVelocityScore:         0.0,
		BaseWindowMs:             5000,
		WindowMomentumFactor:     0.0,
		TTLSeconds:               8,
		NewLaunchWeightLiquidity: 0.4,
		NewLaunchWeightSafety:    0.3,
		NewLaunchWeightHolders:   0.2,
		NewLaunchWeightEntropy:   0.1,
	}
	in := contracts.FeatureDTO{
		EventID:            "feat-strength",
		TraceID:            "trace-strength",
		CorrelationID:      "corr-strength",
		TokenLifecycleID:   "lc-strength",
		TokenAddress:       "0xstrength",
		VersionID:          "v1",
		LiquidityScore:     1.0,
		TxVelocityScore:    1.0,
		ContractSafety:     1.0,
		HolderDistribution: 1.0,
		WalletEntropy:      1.0,
		Confidence: contracts.FeatureConfidence{
			LiquidityScore: 0.8,
			ContractSafety: 0.9,
		},
	}
	mod := edge.New(cfg)

	// Act
	dto, err := mod.Process(context.Background(), in)

	// Assert: EdgeStrength = 1.0*0.4 + 1.0*0.3 + 1.0*0.2 + 1.0*0.1 = 1.0
	// (allow IEEE-754 epsilon)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	const expected = 1.0
	if math.Abs(dto.EdgeStrength-expected) > 1e-9 {
		t.Errorf("expected EdgeStrength=%v, got %v", expected, dto.EdgeStrength)
	}
}

func TestEdge_ConfidenceIsMinOfComponents(t *testing.T) {
	// Arrange
	cfg := &config.EdgeConfig{
		MinLiquidityScore: 0.0,
		MinVelocityScore:  0.0,
		BaseWindowMs:      5000,
		TTLSeconds:        8,
	}
	in := contracts.FeatureDTO{
		EventID:         "feat-conf",
		TokenAddress:    "0xconf",
		VersionID:       "v1",
		LiquidityScore:  0.8,
		TxVelocityScore: 0.6,
		Confidence: contracts.FeatureConfidence{
			LiquidityScore: 0.7, // min of these two
			ContractSafety: 0.9,
		},
	}
	mod := edge.New(cfg)

	// Act
	dto, err := mod.Process(context.Background(), in)

	// Assert: EdgeConfidence = min(0.7, 0.9) = 0.7
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.EdgeConfidence != 0.7 {
		t.Errorf("expected EdgeConfidence=0.7 (min), got %v", dto.EdgeConfidence)
	}
}

func TestEdge_Deterministic(t *testing.T) {
	// Arrange
	cfg := &config.EdgeConfig{
		MinLiquidityScore: 0.3,
		MinVelocityScore:  0.2,
		BaseWindowMs:      5000,
		TTLSeconds:        8,
	}
	in := contracts.FeatureDTO{
		EventID:         "feat-det2",
		TokenAddress:    "0xdet2",
		VersionID:       "v1",
		LiquidityScore:  0.8,
		TxVelocityScore: 0.6,
		Confidence: contracts.FeatureConfidence{
			LiquidityScore: 0.8,
			ContractSafety: 0.9,
		},
	}
	mod := edge.New(cfg)

	// Act
	dto1, _ := mod.Process(context.Background(), in)
	dto2, _ := mod.Process(context.Background(), in)

	// Assert: EventID is content-addressable → must match
	if dto1.EventID != dto2.EventID {
		t.Errorf("non-deterministic EventID: %q vs %q", dto1.EventID, dto2.EventID)
	}
}

// ── validation: EV below threshold ───────────────────────────────────────────

func TestValidation_EVBelowThreshold_Rejects(t *testing.T) {
	// Arrange: very high EV threshold that real EV cannot reach
	cfg := &config.ValidationConfig{
		PriorProbability: 0.5,
		PriorGainBps:     100,
		PriorLossBps:     100,
		PriorSlippageBps: 100,
		EvThresholdBps:   99999, // unreachably high threshold
		FixedCostsBps:    50,
		BuildSubmitP95Ms: 100,
		TTLSeconds:       5,
	}
	in := contracts.EdgeDTO{
		EventID:             "edge-low-ev",
		TraceID:             "trace-low-ev",
		CorrelationID:       "corr-low-ev",
		TokenLifecycleID:    "lc-low-ev",
		TokenAddress:        "0xlowev",
		VersionID:           "v1",
		EdgeType:            "NEW_LAUNCH_EDGE",
		OpportunityWindowMs: 8000,
	}
	mod := validation.New(cfg)

	// Act
	dto, err := mod.Process(context.Background(), in)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.Decision != "REJECT" {
		t.Errorf("expected REJECT for EV below threshold, got %q", dto.Decision)
	}
	if dto.RejectReason == "" {
		t.Error("RejectReason must be set on REJECT")
	}
}

func TestValidation_EVThresholdApplied_MatchesConfig(t *testing.T) {
	// Arrange
	cfg := &config.ValidationConfig{
		PriorProbability: 0.6,
		PriorGainBps:     600,
		PriorLossBps:     300,
		PriorSlippageBps: 100,
		EvThresholdBps:   10,
		FixedCostsBps:    50,
		BuildSubmitP95Ms: 500,
		TTLSeconds:       5,
	}
	in := contracts.EdgeDTO{
		EventID:             "edge-ev-check",
		TokenAddress:        "0xevcheck",
		VersionID:           "v1",
		EdgeType:            "NEW_LAUNCH_EDGE",
		OpportunityWindowMs: 8000,
	}
	mod := validation.New(cfg)

	// Act
	dto, err := mod.Process(context.Background(), in)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.EvThresholdApplied != cfg.EvThresholdBps {
		t.Errorf("expected EvThresholdApplied=%d, got %d", cfg.EvThresholdBps, dto.EvThresholdApplied)
	}
	if dto.ProbabilityUsed != cfg.PriorProbability {
		t.Errorf("expected ProbabilityUsed=%v, got %v", cfg.PriorProbability, dto.ProbabilityUsed)
	}
}

// ── capital: max size capping ─────────────────────────────────────────────────

func TestCapital_CapsAtMaxSizeUsd(t *testing.T) {
	// Arrange: fixed entry > max → should be capped at max
	cfg := &config.CapitalConfig{
		FixedEntrySizeUsd: 999.0, // exceeds max
		MaxSizeUsd:        50.0,  // hard cap
		TTLSeconds:        3,
		WalletAddress:     "0xwallet",
	}
	in := contracts.SelectionOutputDTO{
		EventID:          "sel-cap",
		TraceID:          "trace-cap",
		CorrelationID:    "corr-cap",
		TokenLifecycleID: "lc-cap",
		TokenAddress:     "0xcap",
		VersionID:        "v1",
		Selected:         true,
		Rank:             1,
	}
	mod := capital.New(cfg)

	// Act
	dto, err := mod.Process(context.Background(), in, "eth")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.SizeUsd != cfg.MaxSizeUsd {
		t.Errorf("expected SizeUsd capped at %v, got %v", cfg.MaxSizeUsd, dto.SizeUsd)
	}
}

func TestCapital_SlippageBpsIsSet(t *testing.T) {
	// Arrange
	cfg := &config.CapitalConfig{
		FixedEntrySizeUsd: 10.0,
		MaxSizeUsd:        100.0,
		TTLSeconds:        3,
		WalletAddress:     "0xwallet",
	}
	in := contracts.SelectionOutputDTO{
		EventID:          "sel-slip",
		TraceID:          "trace-slip",
		CorrelationID:    "corr-slip",
		TokenLifecycleID: "lc-slip",
		TokenAddress:     "0xslip",
		VersionID:        "v1",
		Selected:         true,
		Rank:             1,
	}
	mod := capital.New(cfg)

	// Act
	dto, err := mod.Process(context.Background(), in, "eth")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.MaxSlippageBps == 0 {
		t.Error("expected MaxSlippageBps to be set (non-zero)")
	}
}
