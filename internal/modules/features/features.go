// Package features implements Layer 2: Feature Extraction.
// Consumes DataQualityDTO and emits FeatureDTO.
// Pure function: no DB, no side effects.
package features

import (
	"context"
	"fmt"
	"math"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// Module is the feature extraction engine.
type Module struct{}

// New returns a new features Module.
func New(_ *config.Config) *Module {
	return &Module{}
}

// Process computes the canonical feature vector from a DataQualityDTO.
// Phase 2: simplified heuristic computation (no ML, no historical data).
// Deterministic: same input → same output.
func (m *Module) Process(_ context.Context, in contracts.DataQualityDTO) (contracts.FeatureDTO, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	// LiquidityScore [0,1]: derived from DQ risk score inversion.
	liquidityScore := clamp(1.0-in.RiskScore, 0.0, 1.0)

	// TxVelocityScore [0,1]: Phase 2 stub — moderate positive signal.
	txVelocityScore := 0.5

	// HolderDistribution [0,1]: derived from raw LP holder count (Phase 4).
	holderDistribution := HolderDistributionScore(int64(in.LpHolderCount))

	// WalletEntropy [0,1]: Phase 2 stub.
	walletEntropy := 0.5

	// ContractSafety [0,1]: derived from DQ flags.
	contractSafety := computeContractSafety(in)

	// TokenAge [0,1]: Phase 2 stub — new tokens score 0 (unknown age, conservative).
	tokenAge := 0.0

	// VolumeMomentum [0,1]: Phase 2 stub — moderate positive signal.
	volumeMomentum := 0.5

	// PriceMomentum [0,1]: Phase 2 stub.
	priceMomentum := 0.5

	// Per-feature confidence: Phase 2 uses fixed moderate confidence.
	confidence := contracts.FeatureConfidence{
		LiquidityScore:     0.7,
		TxVelocityScore:    0.3, // low: stub value
		HolderDistribution: 0.3,
		WalletEntropy:      0.3,
		ContractSafety:     0.8,
		TokenAge:           0.1, // very low: stub
		VolumeMomentum:     0.3,
		PriceMomentum:      0.3,
	}

	eventID := contracts.ContentIDFromString(fmt.Sprintf("feat:%s", in.EventID))

	return contracts.FeatureDTO{
		EventID:       eventID,
		TraceID:       in.TraceID,
		CorrelationID: in.CorrelationID,
		CausationID:   in.EventID,
		VersionID:     in.VersionID,

		TokenLifecycleID: in.TokenLifecycleID,
		TokenAddress:     in.TokenAddress,

		LiquidityScore:     liquidityScore,
		TxVelocityScore:    txVelocityScore,
		HolderDistribution: holderDistribution,
		WalletEntropy:      walletEntropy,
		ContractSafety:     contractSafety,
		TokenAge:           tokenAge,
		VolumeMomentum:     volumeMomentum,
		PriceMomentum:      priceMomentum,

		LiquidityUsdRaw:    0,
		TxVelocity30sRaw:   0,
		HolderCountRaw:     int64(in.LpHolderCount),
		TokenAgeSecondsRaw: 0,

		Confidence:  confidence,
		ExtractedAt: now,
	}, nil
}

// computeContractSafety derives a safety score from DQ flags.
func computeContractSafety(in contracts.DataQualityDTO) float64 {
	score := 1.0
	if in.IsHoneypot {
		score -= 0.5
	}
	if in.IsRugRisk {
		score -= 0.3
	}
	if in.IsFakeLiquidity {
		score -= 0.2
	}
	if !in.ContractVerified {
		score -= 0.1
	}
	return math.Max(0.0, score)
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
