package edge

import (
	"context"
	"testing"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
)

func TestDetectGraduation_PumpFunAMMCreatePool(t *testing.T) {
	cfg := &config.EdgeConfig{
		MinLiquidityScore:         0.55,
		MinGraduationLiquidityUsd: 3000,
	}
	m := New(cfg)

	in := contracts.FeatureDTO{
		EventTopic:     "PumpFunAMMCreatePool",
		LiquidityUsdRaw: 5000,
		LiquidityScore:  0.62,
		ContractSafety:  0.7,
		HolderDistribution: 0.5,
		Confidence: contracts.FeatureConfidence{
			LiquidityScore:     0.8,
			ContractSafety:     0.8,
			HolderDistribution: 0.6,
		},
	}

	out, err := m.ProcessWithContext(context.Background(), in, BaselineSnapshot{}, 0.58, time.Unix(0, 0).UTC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.EdgeType != contracts.EdgeTypeGraduation {
		t.Fatalf("expected GRADUATION_EDGE, got %q reject=%q", out.EdgeType, out.RejectReason)
	}
	if out.EdgeStrength < 0.58 {
		t.Fatalf("expected strength >= 0.58, got %f", out.EdgeStrength)
	}
}

func TestDetectGraduation_BirthTopicRejected(t *testing.T) {
	cfg := &config.EdgeConfig{MinLiquidityScore: 0.55}
	m := New(cfg)

	in := contracts.FeatureDTO{
		EventTopic:     "PumpFunCreate",
		LiquidityUsdRaw: 4500,
		LiquidityScore:  0.52,
		ContractSafety:  0.7,
		TxVelocityScore: 0.5,
		Confidence: contracts.FeatureConfidence{
			LiquidityScore: 0.5,
			ContractSafety: 0.8,
		},
	}

	out, _ := m.ProcessWithContext(context.Background(), in, BaselineSnapshot{}, 0.58, time.Unix(0, 0).UTC())
	if out.EdgeType == contracts.EdgeTypeGraduation {
		t.Fatal("birth PumpFunCreate must not emit GRADUATION_EDGE")
	}
}
