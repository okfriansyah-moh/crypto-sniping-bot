// Package models implements Layer 4: Probability, Slippage, and Latency models.
// All three models are pure functions of their inputs (deterministic).
// No DB access, no shared mutable state outside the in-memory rolling latency buffer.
//
// See docs/implementation_roadmap.md § Phase 4 and docs/architecture.md § 3.4.
package models

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"crypto-sniping-bot/contracts"
)

// Probability errors.
var (
	// ErrInvalidProbability indicates the model produced an out-of-range value
	// (NaN, ±Inf, or outside (0.0, 1.0)). Caller MUST reject the candidate.
	ErrInvalidProbability = errors.New("models: probability out of range")
)

// LogisticConfig holds the fixed coefficients for the Phase 4 logistic regression.
// Coefficients are loaded from config.models.probability.* (YAML).
// For Phase 4 these are fixed; Phase 5 introduces learning-driven updates.
type LogisticConfig struct {
	Bias                float64 `yaml:"bias"`
	WLiquidityScore     float64 `yaml:"w_liquidity_score"`
	WTxVelocityScore    float64 `yaml:"w_tx_velocity_score"`
	WHolderDistribution float64 `yaml:"w_holder_distribution"`
	WWalletEntropy      float64 `yaml:"w_wallet_entropy"`
	WContractSafety     float64 `yaml:"w_contract_safety"`
	WTokenAge           float64 `yaml:"w_token_age"`
	WVolumeMomentum     float64 `yaml:"w_volume_momentum"`
	WPriceMomentum      float64 `yaml:"w_price_momentum"`
	ModelVersionID      string  `yaml:"model_version_id"`
	BrierCalibration    float64 `yaml:"brier_calibration"`
	MinProbability      float64 `yaml:"min_probability"`
	MaxProbability      float64 `yaml:"max_probability"`
}

// DefaultLogisticConfig returns the conservative Phase 4 default model.
func DefaultLogisticConfig() LogisticConfig {
	return LogisticConfig{
		Bias:                -1.0,
		WLiquidityScore:     1.6,
		WTxVelocityScore:    0.9,
		WHolderDistribution: 0.5,
		WWalletEntropy:      0.4,
		WContractSafety:     1.4,
		WTokenAge:           -0.3,
		WVolumeMomentum:     0.8,
		WPriceMomentum:      0.6,
		ModelVersionID:      "logreg-phase4-v1",
		BrierCalibration:    0.22,
		// Per probability-modeling skill: clip to [0.05, 0.95] — never 0 or 1.
		// Tight extremes destabilize the EV gate and inflate Kelly sizing.
		MinProbability: 0.05,
		MaxProbability: 0.95,
	}
}

// ProbabilityModel scores a FeatureDTO into a calibrated success probability.
type ProbabilityModel struct {
	cfg LogisticConfig
}

// NewProbabilityModel constructs a logistic-regression probability model.
func NewProbabilityModel(cfg LogisticConfig) *ProbabilityModel {
	if cfg.MaxProbability <= cfg.MinProbability {
		cfg.MinProbability = 1e-6
		cfg.MaxProbability = 1.0 - 1e-6
	}
	return &ProbabilityModel{cfg: cfg}
}

// Predict scores a FeatureDTO and emits a ProbabilityEstimateDTO.
// Deterministic: identical features always produce identical Probability.
// Returns ErrInvalidProbability when the linear combination yields NaN/±Inf.
func (m *ProbabilityModel) Predict(_ context.Context, in contracts.FeatureDTO) (contracts.ProbabilityEstimateDTO, error) {
	c := m.cfg
	z := c.Bias +
		c.WLiquidityScore*in.LiquidityScore +
		c.WTxVelocityScore*in.TxVelocityScore +
		c.WHolderDistribution*in.HolderDistribution +
		c.WWalletEntropy*in.WalletEntropy +
		c.WContractSafety*in.ContractSafety +
		c.WTokenAge*in.TokenAge +
		c.WVolumeMomentum*in.VolumeMomentum +
		c.WPriceMomentum*in.PriceMomentum

	if math.IsNaN(z) || math.IsInf(z, 0) {
		return contracts.ProbabilityEstimateDTO{}, ErrInvalidProbability
	}
	p := sigmoid(z)
	if p < c.MinProbability {
		p = c.MinProbability
	}
	if p > c.MaxProbability {
		p = c.MaxProbability
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	eventID := contracts.ContentIDFromString(fmt.Sprintf("prob:%s", in.EventID))

	return contracts.ProbabilityEstimateDTO{
		EventID:          eventID,
		TraceID:          in.TraceID,
		CorrelationID:    in.CorrelationID,
		CausationID:      in.EventID,
		VersionID:        in.VersionID,
		TokenLifecycleID: in.TokenLifecycleID,
		Probability:      p,
		Calibration:      c.BrierCalibration,
		ModelVersionID:   c.ModelVersionID,
		Confidence:       minFeatureConfidence(in.Confidence),
		CalibrationBin:   probabilityDecile(p),
		EstimatedAt:      now,
	}, nil
}

// minFeatureConfidence returns the minimum confidence over the eight features
// that feed the logistic model. Per probability-modeling skill: "low FeatureConfidence
// in → low ProbabilityEstimate confidence out". Skips zero-valued slots so an
// optional/absent feature does not collapse the score (a confidence of exactly 0
// signals "not provided", not "fully uncertain"). If no slot is positive, returns 0.
func minFeatureConfidence(fc contracts.FeatureConfidence) float64 {
	candidates := [...]float64{
		fc.LiquidityScore,
		fc.TxVelocityScore,
		fc.HolderDistribution,
		fc.WalletEntropy,
		fc.ContractSafety,
		fc.TokenAge,
		fc.VolumeMomentum,
		fc.PriceMomentum,
	}
	min := -1.0
	for _, v := range candidates {
		// F-SEC-02: skip NaN/±Inf in addition to non-positive — same
		// "treat as missing" rule. Without this guard a single NaN slot
		// poisoned the returned Confidence and propagated into
		// ProbabilityEstimateDTO.Confidence.
		if v <= 0 || math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		if min < 0 || v < min {
			min = v
		}
	}
	if min < 0 {
		return 0
	}
	if min > 1.0 {
		min = 1.0
	}
	return min
}

// probabilityDecile maps p ∈ [0,1] → bin in [0..9]. p=1.0 maps to 9.
// Used by the learning engine to bucket predictions for calibration/Brier tracking.
func probabilityDecile(p float64) int32 {
	if p <= 0 {
		return 0
	}
	bin := int32(math.Floor(p * 10))
	if bin > 9 {
		bin = 9
	}
	if bin < 0 {
		bin = 0
	}
	return bin
}

// ModelVersionID returns the configured probability model version.
func (m *ProbabilityModel) ModelVersionID() string { return m.cfg.ModelVersionID }

// sigmoid computes the logistic function 1/(1+e^-z) in a numerically-stable way.
func sigmoid(z float64) float64 {
	if z >= 0 {
		ez := math.Exp(-z)
		return 1.0 / (1.0 + ez)
	}
	ez := math.Exp(z)
	return ez / (1.0 + ez)
}
