package modules_test

import (
	"context"
	"testing"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/sniper-bot/internal/modules/capital"
	"crypto-sniping-bot/sniper-bot/internal/modules/data_quality"
	"crypto-sniping-bot/sniper-bot/internal/modules/edge"
	"crypto-sniping-bot/sniper-bot/internal/modules/features"
	"crypto-sniping-bot/sniper-bot/internal/modules/selection"
	"crypto-sniping-bot/sniper-bot/internal/modules/validation"
)

// ── Fixtures ──────────────────────────────────────────────────────────────────

func marketDataFixture() contracts.MarketDataDTO {
	return contracts.MarketDataDTO{
		EventID:        "test-event-001",
		TraceID:        "trace-001",
		CorrelationID:  "corr-001",
		TokenAddress:   "0xabc123",
		Chain:          "eth",
		ReserveBaseRaw: "5000000000000000000", // 5 ETH
		VersionID:      "v1",
		// Mark dev history as known so DetectDevReputation does not trigger the
		// fail-closed DEV_UNKNOWN_HISTORY path (Score=1.0) that pushes riskScore
		// to the RISKY_PASS boundary. A "good" token has a verified first-time
		// creator with confirmed social presence.
		CreatorPrevTokenCountKnown: true, // first-time creator
		CreatorPrevTokenCount:      0,
		SocialLinksKnown:           true,
		HasSocialLinks:             true,
	}
}

func dqPassFixture() contracts.DataQualityDTO {
	return contracts.DataQualityDTO{
		EventID:          "dq-event-001",
		TraceID:          "trace-001",
		CorrelationID:    "corr-001",
		CausationID:      "test-event-001",
		VersionID:        "v1",
		TokenLifecycleID: "lc-001",
		TokenAddress:     "0xabc123",
		Chain:            "eth",
		Decision:         "PASS",
		RiskScore:        0.1,
		ContractVerified: true,
		LpHolderCount:    3,
	}
}

// ── data_quality ─────────────────────────────────────────────────────────────

func TestDataQuality_PassesGoodToken(t *testing.T) {
	mod := data_quality.New(data_quality.DefaultConfig(nil), nil)
	dto, err := mod.Process(context.Background(), marketDataFixture())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.Decision != "PASS" {
		t.Errorf("expected PASS, got %q (reasons: %v)", dto.Decision, dto.RejectReasons)
	}
	if dto.RiskScore < 0 || dto.RiskScore > 1 {
		t.Errorf("risk score out of range: %v", dto.RiskScore)
	}
	if dto.EventID == "" {
		t.Error("EventID must be set")
	}
	if dto.CausationID != marketDataFixture().EventID {
		t.Errorf("CausationID should be input EventID; got %q", dto.CausationID)
	}
}

func TestDataQuality_RejectsZeroReserve(t *testing.T) {
	in := marketDataFixture()
	in.ReserveBaseRaw = "0"
	mod := data_quality.New(data_quality.DefaultConfig(nil), nil)
	dto, err := mod.Process(context.Background(), in)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.Decision != "REJECT" {
		t.Errorf("expected REJECT for zero reserve, got %q", dto.Decision)
	}
}

func TestDataQuality_RejectsReorgedEvent(t *testing.T) {
	in := marketDataFixture()
	in.Reorged = true
	mod := data_quality.New(data_quality.DefaultConfig(nil), nil)
	dto, _ := mod.Process(context.Background(), in)
	if dto.Decision != "REJECT" {
		t.Errorf("expected REJECT for reorged event, got %q", dto.Decision)
	}
}

func TestDataQuality_Deterministic(t *testing.T) {
	mod := data_quality.New(data_quality.DefaultConfig(nil), nil)
	in := marketDataFixture()
	dto1, _ := mod.Process(context.Background(), in)
	dto2, _ := mod.Process(context.Background(), in)
	if dto1.EventID != dto2.EventID {
		t.Errorf("non-deterministic EventID: %q vs %q", dto1.EventID, dto2.EventID)
	}
	if dto1.Decision != dto2.Decision {
		t.Errorf("non-deterministic Decision: %q vs %q", dto1.Decision, dto2.Decision)
	}
}

// ── features ─────────────────────────────────────────────────────────────────

func TestFeatures_ProducesAllFields(t *testing.T) {
	mod := features.New(nil)
	dto, err := mod.Process(context.Background(), dqPassFixture())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.EventID == "" {
		t.Error("EventID must be set")
	}
	if dto.LiquidityScore < 0 || dto.LiquidityScore > 1 {
		t.Errorf("LiquidityScore out of range: %v", dto.LiquidityScore)
	}
	if dto.ContractSafety < 0 || dto.ContractSafety > 1 {
		t.Errorf("ContractSafety out of range: %v", dto.ContractSafety)
	}
	// LiquidityScore confidence is 0.0 on cold-start (no snapshot data in unit test)
	// — that is correct behaviour after the deriveConfidence fix. Check ContractSafety
	// confidence instead, which is always ≥ 0.4 because DQ flags are always populated.
	if dto.Confidence.ContractSafety <= 0 {
		t.Error("ContractSafety confidence must be > 0")
	}
}

func TestFeatures_HoneypotReducesSafety(t *testing.T) {
	in := dqPassFixture()
	in.IsHoneypot = true
	mod := features.New(nil)
	dto, _ := mod.Process(context.Background(), in)
	if dto.ContractSafety >= 1.0 {
		t.Errorf("honeypot should reduce ContractSafety; got %v", dto.ContractSafety)
	}
}

// ── edge ─────────────────────────────────────────────────────────────────────

func featureDTOFixture() contracts.FeatureDTO {
	return contracts.FeatureDTO{
		EventID:          "feat-001",
		TraceID:          "trace-001",
		CorrelationID:    "corr-001",
		TokenLifecycleID: "lc-001",
		TokenAddress:     "0xabc123",
		VersionID:        "v1",
		LiquidityScore:   0.8,
		TxVelocityScore:  0.6,
		ContractSafety:   0.9,
		VolumeMomentum:   0.7,
		Confidence: contracts.FeatureConfidence{
			LiquidityScore: 0.8,
			ContractSafety: 0.9,
		},
	}
}

func TestEdge_DetectsNewLaunchEdge(t *testing.T) {
	cfg := &config.EdgeConfig{
		MinLiquidityScore:    0.3,
		MinVelocityScore:     0.2,
		BaseWindowMs:         5000,
		WindowMomentumFactor: 0.2,
		TTLSeconds:           8,
	}
	mod := edge.New(cfg)
	dto, err := mod.Process(context.Background(), featureDTOFixture())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.EdgeType != "NEW_LAUNCH_EDGE" {
		t.Errorf("expected NEW_LAUNCH_EDGE, got %q", dto.EdgeType)
	}
	if dto.EdgeStrength <= 0 {
		t.Error("EdgeStrength should be > 0 when edge detected")
	}
	if dto.ExpiresAt == "" {
		t.Error("ExpiresAt should be set")
	}
}

func TestEdge_NoEdgeWhenBelowThreshold(t *testing.T) {
	cfg := &config.EdgeConfig{
		MinLiquidityScore: 0.95, // higher than fixture's 0.8
		MinVelocityScore:  0.95,
		BaseWindowMs:      5000,
		TTLSeconds:        8,
	}
	mod := edge.New(cfg)
	dto, _ := mod.Process(context.Background(), featureDTOFixture())
	if dto.EdgeType != contracts.EdgeTypeNone {
		t.Errorf("expected NONE, got %q", dto.EdgeType)
	}
	if dto.IsEdgeDetected() {
		t.Error("NONE must report IsEdgeDetected()=false")
	}
}

// ── validation ───────────────────────────────────────────────────────────────

func edgeDTOFixture() contracts.EdgeDTO {
	return contracts.EdgeDTO{
		EventID:             "edge-001",
		TraceID:             "trace-001",
		CorrelationID:       "corr-001",
		TokenLifecycleID:    "lc-001",
		TokenAddress:        "0xabc123",
		VersionID:           "v1",
		EdgeType:            "NEW_LAUNCH_EDGE",
		EdgeStrength:        0.8,
		OpportunityWindowMs: 8000,
	}
}

func TestValidation_AcceptsGoodEdge(t *testing.T) {
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
	mod := validation.New(cfg)
	dto, err := mod.Process(context.Background(), edgeDTOFixture())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.Decision != "ACCEPT" {
		t.Errorf("expected ACCEPT, got %q (reason: %s)", dto.Decision, dto.RejectReason)
	}
	if dto.ExpectedValueBps <= 0 {
		t.Errorf("EV should be > 0, got %d", dto.ExpectedValueBps)
	}
}

func TestValidation_RejectsNoEdge(t *testing.T) {
	in := edgeDTOFixture()
	in.EdgeType = ""
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
	mod := validation.New(cfg)
	dto, _ := mod.Process(context.Background(), in)
	if dto.Decision != "REJECT" {
		t.Errorf("expected REJECT for empty edge type, got %q", dto.Decision)
	}
}

func TestValidation_LatencyGate(t *testing.T) {
	in := edgeDTOFixture()
	in.OpportunityWindowMs = 100 // very tight window
	cfg := &config.ValidationConfig{
		PriorProbability: 0.6,
		PriorGainBps:     600,
		PriorLossBps:     300,
		PriorSlippageBps: 100,
		EvThresholdBps:   10,
		FixedCostsBps:    50,
		BuildSubmitP95Ms: 2000, // exceeds window
		TTLSeconds:       5,
	}
	mod := validation.New(cfg)
	dto, _ := mod.Process(context.Background(), in)
	if dto.Decision != "REJECT" {
		t.Errorf("expected REJECT for latency exceeds window, got %q", dto.Decision)
	}
}

// ── selection ────────────────────────────────────────────────────────────────

func validatedEdgeFixture() contracts.ValidatedEdgeDTO {
	return contracts.ValidatedEdgeDTO{
		EventID:          "vedge-001",
		TraceID:          "trace-001",
		CorrelationID:    "corr-001",
		TokenLifecycleID: "lc-001",
		TokenAddress:     "0xabc123",
		VersionID:        "v1",
		Decision:         "ACCEPT",
		ExpectedValueBps: 50,
		ProbabilityUsed:  0.6,
	}
}

func TestSelection_SelectsWhenSlotOpen(t *testing.T) {
	cfg := &config.SelectionConfig{MaxOpenPositions: 1}
	mod := selection.New(cfg)
	dto, err := mod.Process(context.Background(), validatedEdgeFixture(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !dto.Selected {
		t.Errorf("expected Selected=true, got false (reason: %s)", dto.RejectReason)
	}
}

func TestSelection_RejectsWhenFull(t *testing.T) {
	cfg := &config.SelectionConfig{MaxOpenPositions: 1}
	mod := selection.New(cfg)
	dto, _ := mod.Process(context.Background(), validatedEdgeFixture(), 1)
	if dto.Selected {
		t.Error("expected Selected=false when max positions reached")
	}
}

func TestSelection_RejectsWhenEdgeNotAccepted(t *testing.T) {
	in := validatedEdgeFixture()
	in.Decision = "REJECT"
	cfg := &config.SelectionConfig{MaxOpenPositions: 1}
	mod := selection.New(cfg)
	dto, _ := mod.Process(context.Background(), in, 0)
	if dto.Selected {
		t.Error("expected Selected=false when edge not accepted")
	}
}

// ── capital ───────────────────────────────────────────────────────────────────

func selectionFixture() contracts.SelectionOutputDTO {
	return contracts.SelectionOutputDTO{
		EventID:          "sel-001",
		TraceID:          "trace-001",
		CorrelationID:    "corr-001",
		TokenLifecycleID: "lc-001",
		TokenAddress:     "0xabc123",
		VersionID:        "v1",
		Selected:         true,
		Rank:             1,
	}
}

func TestCapital_AllocatesFixedSize(t *testing.T) {
	cfg := &config.CapitalConfig{
		FixedEntrySizeUsd: 10.0,
		MaxSizeUsd:        100.0,
		TTLSeconds:        3,
		WalletAddress:     "0xwallet",
	}
	mod := capital.New(cfg)
	dto, err := mod.Process(context.Background(), selectionFixture(), "eth")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dto.Rejected {
		t.Errorf("expected not rejected, got RejectReason=%q", dto.RejectReason)
	}
	if dto.SizeUsd != 10.0 {
		t.Errorf("expected SizeUsd=10.0, got %v", dto.SizeUsd)
	}
	if dto.ExecutionID == "" {
		t.Error("ExecutionID must be set")
	}
	if dto.ExpiresAt == "" {
		t.Error("ExpiresAt must be set")
	}
}

func TestCapital_SkipsWhenNotSelected(t *testing.T) {
	in := selectionFixture()
	in.Selected = false
	in.RejectReason = "max_open_positions_reached:1"
	cfg := &config.CapitalConfig{
		FixedEntrySizeUsd: 10.0,
		MaxSizeUsd:        100.0,
		TTLSeconds:        3,
	}
	mod := capital.New(cfg)
	dto, _ := mod.Process(context.Background(), in, "eth")
	if !dto.Rejected {
		t.Error("expected Rejected=true when selection not selected")
	}
	if dto.SizeUsd != 0 {
		t.Errorf("expected SizeUsd=0 for rejected allocation, got %v", dto.SizeUsd)
	}
}

func TestCapital_ExecutionIDDeterministic(t *testing.T) {
	cfg := &config.CapitalConfig{
		FixedEntrySizeUsd: 10.0,
		MaxSizeUsd:        100.0,
		TTLSeconds:        3,
		WalletAddress:     "0xwallet",
	}
	mod := capital.New(cfg)
	in := selectionFixture()
	dto1, _ := mod.Process(context.Background(), in, "eth")
	dto2, _ := mod.Process(context.Background(), in, "eth")
	if dto1.ExecutionID != dto2.ExecutionID {
		t.Errorf("ExecutionID not deterministic: %q vs %q", dto1.ExecutionID, dto2.ExecutionID)
	}
}
