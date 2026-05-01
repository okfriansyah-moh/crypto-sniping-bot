package models

// Layer 4 slippage model — CPMM (constant-product) closed-form with a
// learned per-market α coefficient and a volatility-driven tail term.
//
// Fixes F-3 (STUBBED): the previous bucket-lookup model emitted identical
// (p50, p95) for any token landing in the same (liquidity, size) bucket,
// effectively producing constant slippage across distinct trace_ids and
// causing EV miscalibration.
//
// Algorithm (deterministic, bounded):
//
//   B            = max(LiquidityUsdRaw / 2, ε)        // base-side reserve
//   base_bps     = α · (size / (B + size)) · 10_000   // CPMM closed-form
//   p50_bps      = clip(base_bps, 0, MaxSlippageBps)
//   σ_proxy      = clip((1 - LiquidityScore) + |2·PriceMomentum - 1| · 0.5, 0, 1)
//   p95_bps      = clip(p50_bps · (1 + Z95 · σ_proxy) + TailBps, 0, MaxSlippageBps)
//
// Where:
//   * α is read from a SlippageAlphaProvider (default 1.0). The
//     execution-quality-analyzer is expected to update α from realized
//     fills (skill: execution-quality-analyzer). Until that aggregator is
//     wired, the adapter returns 1.0.
//   * Z95 ≈ 1.65 is the standard-normal 95th-percentile multiplier.
//   * TailBps adds a small fixed premium so p95 > p50 even for very low σ.
//
// Properties (enforced by tests):
//   * Deterministic: same input + same α ⇒ identical (p50, p95, eventID).
//   * Bounded: both bps ∈ [0, MaxSlippageBps].
//   * Monotonic in size: ∂p50/∂size > 0 with reserves fixed.
//   * p95 ≥ p50 for all inputs.
//   * α=1.0 leaves base_bps unchanged; α=2.0 doubles base_bps.

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"crypto-sniping-bot/contracts"
)

// ErrInvalidSlippageInput indicates a non-finite numeric input (NaN/±Inf)
// reached the model. Callers MUST reject the candidate.
var ErrInvalidSlippageInput = errors.New("models: invalid slippage input")

// SlippageAlphaProvider resolves the per-market α calibration coefficient.
// Implementations live outside the module — typically the database adapter
// or a future learning-engine cache. Returning 1.0 means "no calibration
// applied" and is the safe default before realized-fill data is available.
type SlippageAlphaProvider interface {
	GetSlippageAlpha(ctx context.Context, market string) (float64, error)
}

// SlippageBucket is retained for backward-compatible YAML loading only.
// The CPMM model does NOT consult buckets; this type stays so existing
// pipeline.yaml files continue to parse during the transition.
type SlippageBucket struct {
	LiquidityMaxUsd float64 `yaml:"liquidity_max_usd"`
	SizeMaxUsd      float64 `yaml:"size_max_usd"`
	P50Bps          int32   `yaml:"p50_bps"`
	P95Bps          int32   `yaml:"p95_bps"`
}

// SlippageConfig holds the CPMM model parameters and bounds.
type SlippageConfig struct {
	MaxSlippageBps int32   `yaml:"max_slippage_bps"` // hard upper bound (default 5000 = 50%)
	VolatilityZ    float64 `yaml:"volatility_z"`     // p95 z-score multiplier (default 1.65)
	TailBps        int32   `yaml:"tail_bps"`         // fixed p95 premium (default 25 bps)
	MinReserveUsd  float64 `yaml:"min_reserve_usd"`  // floor for base reserve B (default 1.0)
	DefaultAlpha   float64 `yaml:"default_alpha"`    // α used when provider is nil/errors (default 1.0)
	MaxAlpha       float64 `yaml:"max_alpha"`        // hard cap on α (default 5.0)
	ModelVersionID string  `yaml:"model_version_id"` // populated on every DTO

	// Retained for backward-compatible YAML loading; unused.
	Buckets        []SlippageBucket `yaml:"buckets"`
	FallbackP50Bps int32            `yaml:"fallback_p50_bps"`
	FallbackP95Bps int32            `yaml:"fallback_p95_bps"`
}

// DefaultSlippageConfig returns conservative defaults for the CPMM model.
func DefaultSlippageConfig() SlippageConfig {
	return SlippageConfig{
		MaxSlippageBps: 5000,
		VolatilityZ:    1.65,
		TailBps:        25,
		MinReserveUsd:  1.0,
		DefaultAlpha:   1.0,
		MaxAlpha:       5.0,
		ModelVersionID: "cpmm-alpha-v1",
	}
}

// SlippageModel emits CPMM-derived slippage estimates with α calibration.
type SlippageModel struct {
	cfg   SlippageConfig
	alpha SlippageAlphaProvider // optional; nil ⇒ DefaultAlpha
}

// NewSlippageModel constructs a SlippageModel that uses cfg.DefaultAlpha
// (no per-market calibration).
func NewSlippageModel(cfg SlippageConfig) *SlippageModel {
	return &SlippageModel{cfg: withSlippageDefaults(cfg)}
}

// NewSlippageModelWithAlpha constructs a SlippageModel that resolves α via
// the given provider on every Estimate call. Provider errors fall back to
// cfg.DefaultAlpha — the model never returns an error solely because α
// resolution failed.
func NewSlippageModelWithAlpha(cfg SlippageConfig, alpha SlippageAlphaProvider) *SlippageModel {
	return &SlippageModel{cfg: withSlippageDefaults(cfg), alpha: alpha}
}

func withSlippageDefaults(cfg SlippageConfig) SlippageConfig {
	d := DefaultSlippageConfig()
	if cfg.MaxSlippageBps <= 0 {
		cfg.MaxSlippageBps = d.MaxSlippageBps
	}
	if cfg.VolatilityZ <= 0 {
		cfg.VolatilityZ = d.VolatilityZ
	}
	if cfg.TailBps < 0 {
		cfg.TailBps = d.TailBps
	}
	if cfg.MinReserveUsd <= 0 {
		cfg.MinReserveUsd = d.MinReserveUsd
	}
	if cfg.DefaultAlpha <= 0 {
		cfg.DefaultAlpha = d.DefaultAlpha
	}
	if cfg.MaxAlpha <= 0 {
		cfg.MaxAlpha = d.MaxAlpha
	}
	if cfg.ModelVersionID == "" {
		cfg.ModelVersionID = d.ModelVersionID
	}
	return cfg
}

// Estimate returns a SlippageEstimateDTO for the given (feature, proposed
// size in USD). Deterministic apart from EstimatedAt timestamp.
func (m *SlippageModel) Estimate(
	ctx context.Context,
	feature contracts.FeatureDTO,
	proposedSizeUsd float64,
) (contracts.SlippageEstimateDTO, error) {
	return m.estimate(ctx, feature, proposedSizeUsd, "")
}

// EstimateForMarket is identical to Estimate but threads the α-resolution
// market key explicitly (e.g. "eth-uniswap-v2"). Use this when the caller
// knows the market.
func (m *SlippageModel) EstimateForMarket(
	ctx context.Context,
	feature contracts.FeatureDTO,
	proposedSizeUsd float64,
	market string,
) (contracts.SlippageEstimateDTO, error) {
	return m.estimate(ctx, feature, proposedSizeUsd, market)
}

func (m *SlippageModel) estimate(
	ctx context.Context,
	feature contracts.FeatureDTO,
	proposedSizeUsd float64,
	market string,
) (contracts.SlippageEstimateDTO, error) {
	if !isFinite(feature.LiquidityUsdRaw) ||
		!isFinite(feature.LiquidityScore) ||
		!isFinite(feature.PriceMomentum) ||
		!isFinite(feature.Confidence.LiquidityScore) ||
		!isFinite(proposedSizeUsd) {
		return contracts.SlippageEstimateDTO{}, ErrInvalidSlippageInput
	}

	size := math.Max(0, proposedSizeUsd)
	liq := math.Max(0, feature.LiquidityUsdRaw)

	// CPMM: treat half the USD-denominated TVL as the base-side reserve.
	base := math.Max(liq*0.5, m.cfg.MinReserveUsd)

	// Resolve α (per-market). Provider failures degrade to DefaultAlpha.
	alpha := m.cfg.DefaultAlpha
	if m.alpha != nil {
		if a, err := m.alpha.GetSlippageAlpha(ctx, market); err == nil && isFinite(a) && a > 0 {
			alpha = a
		}
	}
	if alpha > m.cfg.MaxAlpha {
		alpha = m.cfg.MaxAlpha
	}

	denom := base + size
	if denom <= 0 {
		denom = m.cfg.MinReserveUsd
	}
	baseBps := alpha * (size / denom) * 10_000.0
	p50f := clipFloat(baseBps, 0, float64(m.cfg.MaxSlippageBps))

	// Volatility proxy: low liquidity score and momentum extremes widen p95.
	liqScore := clipFloat(feature.LiquidityScore, 0, 1)
	pmDeviation := math.Abs(2.0*clipFloat(feature.PriceMomentum, 0, 1) - 1.0)
	sigma := clipFloat((1.0-liqScore)+pmDeviation*0.5, 0, 1)

	p95f := p50f*(1.0+m.cfg.VolatilityZ*sigma) + float64(m.cfg.TailBps)
	p95f = clipFloat(p95f, p50f, float64(m.cfg.MaxSlippageBps))

	p50 := int32(math.Round(p50f))
	p95 := int32(math.Round(p95f))
	if p95 < p50 {
		p95 = p50
	}

	confidence := slippageConfidence(feature, liq)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	// F-SEC-03: α is a runtime calibration parameter, NOT part of the
	// content-addressable identity. Including it in the eventID hash would
	// produce drift the moment the execution-quality-analyzer adjusts α —
	// the same (feature, size) emission would acquire a new eventID without
	// any change to its observable behaviour. Identity = (feature.EventID,
	// quantized size). α attribution lives in ModelVersionID
	// ("cpmm-alpha-v1") and the persisted slippage row.
	eventID := contracts.ContentIDFromString(fmt.Sprintf(
		"slip:%s:%d",
		feature.EventID, int64(size*100),
	))

	return contracts.SlippageEstimateDTO{
		EventID:          eventID,
		TraceID:          feature.TraceID,
		CorrelationID:    feature.CorrelationID,
		CausationID:      feature.EventID,
		VersionID:        feature.VersionID,
		TokenLifecycleID: feature.TokenLifecycleID,
		ExpectedP50Bps:   p50,
		ExpectedP95Bps:   p95,
		ModelVersionID:   m.cfg.ModelVersionID,
		EstimatedAt:      now,
		Confidence:       confidence,
	}, nil
}

// ModelVersionID returns the configured slippage model version.
func (m *SlippageModel) ModelVersionID() string { return m.cfg.ModelVersionID }

// slippageConfidence derives [0,1] confidence from the depth of inputs.
func slippageConfidence(f contracts.FeatureDTO, liq float64) float64 {
	depth := 0.0
	switch {
	case liq <= 0:
		depth = 0.1
	case liq < 25_000:
		depth = 0.4
	case liq < 100_000:
		depth = 0.7
	default:
		depth = 1.0
	}
	featConf := 1.0
	if f.Confidence.LiquidityScore > 0 {
		featConf = clipFloat(f.Confidence.LiquidityScore, 0, 1)
	}
	return clipFloat(depth*featConf, 0, 1)
}

func clipFloat(v, lo, hi float64) float64 {
	// F-SEC-02: NaN/±Inf MUST NOT propagate through Confidence/bounded
	// fields. Treat them as missing and return the lower bound — every
	// caller of clipFloat passes lo=0 for confidence-style values, so this
	// degrades the score without poisoning downstream consumers.
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func isFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
