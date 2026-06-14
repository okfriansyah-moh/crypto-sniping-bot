// Package features implements Layer 2: Feature Extraction.
// Pure function: accepts immutable DTOs and snapshots, returns FeatureDTO.
// No DB, no network, no clocks affecting computation (only the emit timestamp).
//
// The extractor produces a normalized feature vector via two-stage signal
// normalization (per .github/skills/signal-normalizer/SKILL.md) and a
// directional-consistency stability gate (per
// .github/skills/feature-stability-checker/SKILL.md). Per-feature
// FeatureConfidence is derived from input completeness and baseline sample
// count — never a constant.
package features

import (
	"context"
	"fmt"
	"math"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// Canonical signal names — used as keys for both BaselineSnapshot history
// and stability tracking. They match the FeatureDTO field semantics.
const (
	SignalLiquidity      = "liquidity_size"
	SignalTxVelocity     = "tx_velocity"
	SignalHolderDist     = "holder_distribution"
	SignalWalletEntropy  = "wallet_entropy"
	SignalContractSafety = "contract_safety"
	SignalTokenAge       = "token_age"
	SignalVolumeMomentum = "volume_momentum"
	SignalPriceMomentum  = "price_momentum"
)

// Module is the feature extraction engine.
type Module struct {
	normalizerCfg NormalizerConfig
	stabilityCfg  StabilityConfig
	tokenAgeCfg   ageConfig
}

type ageConfig struct {
	tooNewSec    int
	sweetMinSec  int
	sweetMaxSec  int
	confidenceTg float64
}

// New returns a new features Module wired against the runtime config.
// A nil config falls back to skill defaults so unit tests remain trivial.
func New(cfg *config.Config) *Module {
	m := &Module{
		normalizerCfg: DefaultNormalizerConfig(),
		stabilityCfg:  DefaultStabilityConfig(),
		tokenAgeCfg: ageConfig{
			tooNewSec:    30,
			sweetMinSec:  30,
			sweetMaxSec:  300,
			confidenceTg: 0.95,
		},
	}
	if cfg == nil {
		return m
	}
	if k := cfg.Feature.Normalization.SigmoidSteepness; k > 0 {
		m.normalizerCfg.SigmoidK = k
	}
	if n := cfg.Feature.Normalization.ZScoreMinSamples; n > 0 {
		m.normalizerCfg.MinSamples = n
	}
	if s := cfg.Feature.Stability; s.MinBars > 0 {
		m.stabilityCfg = StabilityConfig{
			MinConsistency: s.MinConsistency,
			MinBars:        s.MinBars,
			Lookback:       s.LookbackBars,
		}
	}
	if a := cfg.Feature.TokenAge; a.SweetSpotMaxSec > 0 {
		m.tokenAgeCfg = ageConfig{
			tooNewSec:    a.TooNewThresholdSec,
			sweetMinSec:  a.SweetSpotMinSec,
			sweetMaxSec:  a.SweetSpotMaxSec,
			confidenceTg: a.ConfidenceTarget,
		}
	}
	return m
}

// Process is the back-compat entry point used by callers that have only the
// upstream DataQualityDTO. Equivalent to ProcessWithContext with an empty
// MarketSnapshot and BaselineSnapshot — features that depend on upstream
// MarketDataDTO data degrade to cold-start with low confidence rather than
// emitting constants.
func (m *Module) Process(ctx context.Context, dq contracts.DataQualityDTO) (contracts.FeatureDTO, error) {
	return m.ProcessWithContext(ctx, dq, MarketSnapshot{}, BaselineSnapshot{}, "")
}

// ProcessWithContext is the production entry point. The worker fetches the
// originating MarketDataDTO, builds a MarketSnapshot, snapshots the rolling
// baselines for the market, and calls this method.
//
// extractedAt is the ISO 8601 timestamp injected by the worker — never read
// from the system clock inside the module so replay stays bit-for-bit
// deterministic.
func (m *Module) ProcessWithContext(
	_ context.Context,
	dq contracts.DataQualityDTO,
	snap MarketSnapshot,
	base BaselineSnapshot,
	extractedAt string,
) (contracts.FeatureDTO, error) {
	if extractedAt == "" {
		// Fall back to the upstream deterministic timestamp.
		extractedAt = dq.EvaluatedAt
	}

	// ── Compute raw signals ───────────────────────────────────────
	rawLiquidity, liquidityKnown := rawLiquiditySize(snap)
	rawTxVel, txVelKnown := rawTxVelocity(snap)
	rawHolderDist, holderDistKnown := rawHolderDistribution(dq, snap)
	rawWalletEntropy, walletEntropyKnown := rawWalletEntropy(dq, snap)
	rawTokenAgeSec, tokenAgeKnown := rawTokenAge(snap)
	rawPriceVal, priceKnown := rawPrice(snap)
	rawVolume, volumeKnown := rawVolumeMomentum(snap)

	// ── Normalize via two-stage pipeline ──────────────────────────
	liqNS := NormalizeSignal(rawLiquidity, base.HistoryFor(SignalLiquidity), m.normalizerCfg)
	txNS := NormalizeSignal(rawTxVel, base.HistoryFor(SignalTxVelocity), m.normalizerCfg)
	hdNS := NormalizeSignal(rawHolderDist, base.HistoryFor(SignalHolderDist), m.normalizerCfg)
	weNS := NormalizeSignal(rawWalletEntropy, base.HistoryFor(SignalWalletEntropy), m.normalizerCfg)
	volNS := NormalizeSignal(rawVolume, base.HistoryFor(SignalVolumeMomentum), m.normalizerCfg)

	// Price momentum: feed log return relative to the mean of the price
	// history when available; otherwise fall back to 0 so the cold-start
	// path produces a centered (≈0) momentum.
	rawPriceMomentum := 0.0
	if priceKnown {
		priceHist := base.HistoryFor(SignalPriceMomentum)
		if len(priceHist) > 0 {
			var sum float64
			for _, v := range priceHist {
				sum += v
			}
			mean := sum / float64(len(priceHist))
			if mean > 0 && rawPriceVal > 0 {
				rawPriceMomentum = math.Log(rawPriceVal / mean)
			}
		}
	}
	pmNS := NormalizeSignal(rawPriceMomentum, base.HistoryFor(SignalPriceMomentum), m.normalizerCfg)

	// Contract safety and token age have natural bounded domains and are
	// scored directly (no z-score normalization).
	contractSafety := computeContractSafety(dq, snap)
	tokenAgeScore := scoreTokenAge(rawTokenAgeSec, m.tokenAgeCfg)

	// ── Map normalized signals to FeatureDTO [0,1] fields ─────────
	liquidityScore := liqNS.ScoreUnit01
	txVelocityScore := txNS.ScoreUnit01
	holderDistribution := hdNS.ScoreUnit01
	walletEntropyScore := weNS.ScoreUnit01
	volumeMomentum := volNS.ScoreUnit01
	priceMomentum := pmNS.ScoreUnit01

	// Suppression by safety flags is applied AFTER normalization so it
	// cannot be hidden by a friendly baseline.
	if dq.IsWashTrading {
		walletEntropyScore *= 0.3
	}
	if dq.IsFakeLiquidity {
		volumeMomentum *= 0.2
	}
	if dq.IsHoneypot || dq.IsTaxAnomaly {
		priceMomentum *= 0.1
	}

	// ── Per-feature confidence (skill-derived, never constant) ────
	conf := contracts.FeatureConfidence{
		LiquidityScore:     deriveConfidence(liquidityKnown, liqNS),
		TxVelocityScore:    deriveConfidence(txVelKnown, txNS),
		HolderDistribution: deriveConfidence(holderDistKnown, hdNS),
		WalletEntropy:      deriveConfidence(walletEntropyKnown, weNS),
		ContractSafety:     deriveContractSafetyConfidence(dq, snap),
		TokenAge:           deriveTokenAgeConfidence(tokenAgeKnown, m.tokenAgeCfg.confidenceTg),
		VolumeMomentum:     deriveConfidence(volumeKnown, volNS),
		PriceMomentum:      deriveConfidence(priceKnown, pmNS),
	}

	// ── Stability gate (per feature-stability-checker skill) ──────
	conf = m.applyStabilityGate(conf, base)

	// ── Assemble DTO ─────────────────────────────────────────────
	eventID := contracts.ContentIDFromString(fmt.Sprintf("feat:%s", dq.EventID))

	return contracts.FeatureDTO{
		EventID:       eventID,
		TraceID:       dq.TraceID,
		CorrelationID: dq.CorrelationID,
		CausationID:   dq.EventID,
		VersionID:     dq.VersionID,

		TokenLifecycleID: dq.TokenLifecycleID,
		TokenAddress:     dq.TokenAddress,

		Market: snap.Market,
		Chain:  snap.Chain,

		LiquidityScore:     liquidityScore,
		TxVelocityScore:    txVelocityScore,
		HolderDistribution: holderDistribution,
		WalletEntropy:      walletEntropyScore,
		ContractSafety:     contractSafety,
		TokenAge:           tokenAgeScore,
		VolumeMomentum:     volumeMomentum,
		PriceMomentum:      priceMomentum,

		LiquidityUsdRaw:    snap.LiquidityUsd,
		TxVelocity30sRaw:   float64(snap.TxCount1m),
		HolderCountRaw:     int64(holderCountRaw(dq, snap)),
		TokenAgeSecondsRaw: int64(snap.PoolAgeSeconds),

		NarrativeScore: snap.NarrativeScore,
		NarrativeKnown: snap.NarrativeKnown,

		Confidence:  conf,
		ExtractedAt: extractedAt,
	}, nil
}

// applyStabilityGate computes per-signal directional consistency from the
// baseline history, zeroes the confidence of unstable signals, and
// redistributes the freed weight proportionally across stable signals so
// the aggregate trust budget is preserved. Cold-start (n < MinBars) is
// treated as stable so early trades are not blocked.
func (m *Module) applyStabilityGate(
	conf contracts.FeatureConfidence,
	base BaselineSnapshot,
) contracts.FeatureConfidence {
	weights := map[string]float64{
		SignalLiquidity:      conf.LiquidityScore,
		SignalTxVelocity:     conf.TxVelocityScore,
		SignalHolderDist:     conf.HolderDistribution,
		SignalWalletEntropy:  conf.WalletEntropy,
		SignalContractSafety: conf.ContractSafety,
		SignalTokenAge:       conf.TokenAge,
		SignalVolumeMomentum: conf.VolumeMomentum,
		SignalPriceMomentum:  conf.PriceMomentum,
	}
	stable := make(map[string]bool, len(weights))
	for sig := range weights {
		switch sig {
		case SignalContractSafety, SignalTokenAge:
			// Derived directly from input flags / age — not z-score
			// normalized, so the gate doesn't apply.
			stable[sig] = true
			continue
		}
		hist := base.HistoryFor(sig)
		stable[sig] = CheckFeatureStability(sig, hist, m.stabilityCfg).Stable
	}
	redistributed := RedistributeWeights(weights, stable)

	clip := func(v float64) float64 {
		if v < 0 {
			return 0
		}
		if v > 1 {
			return 1
		}
		return v
	}
	return contracts.FeatureConfidence{
		LiquidityScore:     clip(redistributed[SignalLiquidity]),
		TxVelocityScore:    clip(redistributed[SignalTxVelocity]),
		HolderDistribution: clip(redistributed[SignalHolderDist]),
		WalletEntropy:      clip(redistributed[SignalWalletEntropy]),
		ContractSafety:     clip(redistributed[SignalContractSafety]),
		TokenAge:           clip(redistributed[SignalTokenAge]),
		VolumeMomentum:     clip(redistributed[SignalVolumeMomentum]),
		PriceMomentum:      clip(redistributed[SignalPriceMomentum]),
	}
}

// RawSignalsForBaseline returns the raw signal values the worker should
// append to the BaselineStore after a successful extraction. Keeping this
// derivation here (rather than in the worker) ensures the baseline reflects
// the same raw inputs the module just consumed.
func (m *Module) RawSignalsForBaseline(dq contracts.DataQualityDTO, snap MarketSnapshot) map[string]float64 {
	out := make(map[string]float64, 8)
	if v, ok := rawLiquiditySize(snap); ok {
		out[SignalLiquidity] = v
	}
	if v, ok := rawTxVelocity(snap); ok {
		out[SignalTxVelocity] = v
	}
	if v, ok := rawHolderDistribution(dq, snap); ok {
		out[SignalHolderDist] = v
	}
	if v, ok := rawWalletEntropy(dq, snap); ok {
		out[SignalWalletEntropy] = v
	}
	if v, ok := rawVolumeMomentum(snap); ok {
		out[SignalVolumeMomentum] = v
	}
	if v, ok := rawPrice(snap); ok {
		out[SignalPriceMomentum] = v
	}
	return out
}

// ── Raw signal extractors ────────────────────────────────────────
// Each returns (raw_value, known). When known is false, downstream
// confidence is materially reduced.

func rawLiquiditySize(s MarketSnapshot) (float64, bool) {
	// Use any positive LiquidityUsd regardless of LpStatsKnown.
	// LpStatsKnown=false only means the Pyth SOL/USD feed was unavailable;
	// the on-chain SOL reserve was still read. Blocking on LpStatsKnown
	// causes sigmoid(0)=0.5 < MinLiquidityScore=0.55 → edge_strength=0
	// for every token when the Pyth cache expires after 60 s.
	if s.LiquidityUsd <= 0 {
		return 0, false
	}
	return math.Log1p(s.LiquidityUsd), true
}

func rawTxVelocity(s MarketSnapshot) (float64, bool) {
	if !s.WashStatsKnown {
		return 0, false
	}
	return float64(s.TxCount1m), true
}

func rawHolderDistribution(dq contracts.DataQualityDTO, s MarketSnapshot) (float64, bool) {
	if s.HolderDistKnown && s.HolderCount > 0 {
		// Higher entropy = lower top5 share. Map to a score-like raw.
		return math.Log1p(float64(s.HolderCount)) - 4.0*s.Top5HolderPct, true
	}
	if dq.LpHolderCount > 0 {
		return math.Log1p(float64(dq.LpHolderCount)), true
	}
	return 0, false
}

func rawWalletEntropy(dq contracts.DataQualityDTO, s MarketSnapshot) (float64, bool) {
	if s.WashStatsKnown {
		return s.WalletEntropy, true
	}
	if dq.LpHolderCount > 0 {
		return holderEntropyProxy(dq.LpHolderCount), true
	}
	return 0, false
}

func rawTokenAge(s MarketSnapshot) (int, bool) {
	if s.PoolAgeSeconds > 0 {
		return int(s.PoolAgeSeconds), true
	}
	return 0, false
}

func rawPrice(s MarketSnapshot) (float64, bool) {
	p := PriceFromReserves(s.ReserveBaseRaw, s.ReserveTokenRaw)
	if p <= 0 {
		return 0, false
	}
	return p, true
}

func rawVolumeMomentum(s MarketSnapshot) (float64, bool) {
	if !s.WashStatsKnown {
		return 0, false
	}
	// Combine throughput with unique participation so wash-padded counts
	// don't dominate the raw signal before normalization.
	return float64(s.TxCount1m) + 2.0*float64(s.UniqueWallets1m), true
}

func holderCountRaw(dq contracts.DataQualityDTO, s MarketSnapshot) int32 {
	if s.HolderDistKnown && s.HolderCount > 0 {
		return s.HolderCount
	}
	return dq.LpHolderCount
}

// scoreTokenAge implements the piecewise FeatureRuntimeConfig scoring:
//
//	age == 0                       → 0 (unknown)
//	0 < age < tooNewSec            → 0.2 (just-born, untrusted)
//	tooNewSec ≤ age ≤ sweetMaxSec  → 1.0 (sweet spot)
//	age > sweetMaxSec              → linearly decays toward 0.6
func scoreTokenAge(age int, cfg ageConfig) float64 {
	if age <= 0 {
		return 0
	}
	if age < cfg.tooNewSec {
		return 0.2
	}
	if age <= cfg.sweetMaxSec {
		return 1.0
	}
	if cfg.sweetMaxSec <= 0 {
		return 0.6
	}
	ratio := float64(age-cfg.sweetMaxSec) / float64(3*cfg.sweetMaxSec)
	if ratio > 1 {
		ratio = 1
	}
	return 1.0 - 0.4*ratio
}

// ── Confidence derivation (skill-driven, NEVER constant) ─────────

// deriveConfidence combines:
//   - input known flag (0.4 weight)
//   - baseline coverage proportional to N/MinSamples (0.5 weight)
//   - sigmoid hygiene = 0.1 if not clamped/floored else 0.06
//
// Output is in [0, 1] and continuously varies with each input change.
//
// Special case: when known=false AND ns.N==0, the feature was completely
// absent (not computable) for this token. Return 0.0 so that minFeatureConfidence
// treats it as "not provided" and excludes it from the minimum — preventing a
// single cold-start feature from collapsing the model confidence to 0.10.
func deriveConfidence(known bool, ns NormalizedSignal) float64 {
	// Cold-start absent feature: no data at all — treat as missing, not low-confidence.
	if !known && ns.N == 0 {
		return 0.0
	}
	base := 0.0
	if known {
		base = 0.4
	}
	// Coverage rewards baseline depth proportionally.
	coverage := 0.0
	target := 20 // mirrors NormalizerConfig.MinSamples skill default
	if ns.N >= target {
		coverage = 0.5
	} else if ns.N > 0 {
		coverage = 0.5 * float64(ns.N) / float64(target)
	}
	hygiene := 0.1
	if ns.Clamped || ns.SigmaFloor {
		hygiene = 0.06
	}
	c := base + coverage + hygiene
	if c < 0 {
		return 0
	}
	if c > 1 {
		return 1
	}
	return c
}

func deriveContractSafetyConfidence(dq contracts.DataQualityDTO, s MarketSnapshot) float64 {
	c := 0.4 // DQ flags are always populated by Layer 1
	if dq.ContractVerified {
		c += 0.15
	}
	if dq.LpLocked {
		c += 0.15
	}
	if s.LpLockKnown {
		c += 0.15
	}
	if dq.HoneypotScore > 0 || dq.RugScore > 0 || dq.WashScore > 0 || dq.FakeLiqScore > 0 || dq.TaxScore > 0 {
		c += 0.15
	}
	if c > 1 {
		return 1
	}
	return c
}

func deriveTokenAgeConfidence(known bool, target float64) float64 {
	if !known {
		return 0.1
	}
	if target <= 0 || target > 1 {
		return 0.95
	}
	return target
}

// ── Helpers retained from the prior implementation ──────────────

func computeContractSafety(dq contracts.DataQualityDTO, s MarketSnapshot) float64 {
	score := 1.0
	if dq.IsHoneypot {
		score -= 0.5
	}
	if dq.IsRugRisk {
		score -= 0.3
	}
	if dq.IsFakeLiquidity {
		score -= 0.2
	}
	if !dq.ContractVerified {
		score -= 0.1
	}
	if s.LpLockKnown {
		score += 0.05 * s.LpLockStrength
	}
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score
}

// holderEntropyProxy maps an LP holder count to a [0,1] entropy proxy.
// Smooth saturation: 1 - 1/(1 + n/scale).
func holderEntropyProxy(n int32) float64 {
	if n <= 0 {
		return 0
	}
	scale := 10.0
	return 1.0 - 1.0/(1.0+float64(n)/scale)
}

// clamp is exported via test helper expectations.
func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
