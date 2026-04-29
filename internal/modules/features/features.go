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
// Phase 9 (§ 9.2) — Replaces the original 0.5 placeholders with
// deterministic derivations based on DQ inputs already available.
// True on-chain Sync-event aggregation requires Phase 9.5 RPC plumbing
// and is deferred; until then derived signals carry low confidence so the
// learning engine treats them as cold-start (per stub_guard).
func (m *Module) Process(_ context.Context, in contracts.DataQualityDTO) (contracts.FeatureDTO, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	// LiquidityScore [0,1]: derived from DQ risk score inversion.
	liquidityScore := clamp(1.0-in.RiskScore, 0.0, 1.0)

	// ── TxVelocityScore [0,1] ─────────────────────────────────────
	// Heuristic: low-risk + LP-locked + verified contracts correlate
	// with healthier swap activity at launch. Confidence is low until
	// the Sync-event ring buffer ships in Phase 9.5.
	txVelocityScore := clamp(0.6*liquidityScore+0.4*lpHealthScore(in), 0.0, 1.0)

	// HolderDistribution [0,1]: derived from raw LP holder count.
	holderDistribution := HolderDistributionScore(int64(in.LpHolderCount))

	// ── WalletEntropy [0,1] ───────────────────────────────────────
	// Derived from holder count (more unique LP holders → higher entropy).
	// Smooth saturation 1 - 1/(1+n/10): n=0→0, n=10→0.5, n=100→~0.91.
	walletEntropy := holderEntropyProxy(in.LpHolderCount)
	if in.IsWashTrading {
		walletEntropy *= 0.3 // wash-trading flag suppresses the score
	}

	// ContractSafety [0,1]: derived from DQ flags.
	contractSafety := computeContractSafety(in)

	// TokenAge [0,1]: cold-start until upstream MarketDataDTO is plumbed
	// through. Score 0 (most conservative) signals "unknown age".
	tokenAge := 0.0

	// ── VolumeMomentum [0,1] ──────────────────────────────────────
	// Heuristic: scaled liquidity score, suppressed by fake-liquidity flag.
	volumeMomentum := clamp(0.5*liquidityScore+0.5*lpHealthScore(in), 0.0, 1.0)
	if in.IsFakeLiquidity {
		volumeMomentum *= 0.2
	}

	// ── PriceMomentum [0,1] ───────────────────────────────────────
	// Phase 9 cold-start derivation: combine contract safety with LP health
	// as a stand-in until real Δprice is available.
	priceMomentum := clamp(0.4*contractSafety+0.6*lpHealthScore(in), 0.0, 1.0)
	if in.IsHoneypot || in.IsTaxAnomaly {
		priceMomentum *= 0.1
	}

	// Per-feature confidence — Phase 9 cold-start values.
	// Low values on derived signals signal to learning to treat as cold-start.
	confidence := contracts.FeatureConfidence{
		LiquidityScore:     0.7,
		TxVelocityScore:    0.4, // derived, not measured
		HolderDistribution: 0.5,
		WalletEntropy:      0.4, // derived from holder count proxy
		ContractSafety:     0.8,
		TokenAge:           0.1, // unknown until MarketDataDTO plumbed
		VolumeMomentum:     0.4, // derived, not measured
		PriceMomentum:      0.4, // derived, not measured
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

// lpHealthScore aggregates LP-related DQ flags into [0,1].
// Locked + verified + non-rug = 1.0; opposites pull toward 0.
func lpHealthScore(in contracts.DataQualityDTO) float64 {
	score := 1.0
	if !in.LpLocked {
		score -= 0.4
	}
	if in.IsRugRisk {
		score -= 0.4
	}
	if !in.ContractVerified {
		score -= 0.2
	}
	if score < 0 {
		score = 0
	}
	return score
}

// holderEntropyProxy maps LP holder count to a [0,1] entropy proxy.
// Smooth saturation: 1 - 1/(1 + n/scale) where scale=10.
func holderEntropyProxy(n int32) float64 {
	if n <= 0 {
		return 0
	}
	scale := 10.0
	return 1.0 - 1.0/(1.0+float64(n)/scale)
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
