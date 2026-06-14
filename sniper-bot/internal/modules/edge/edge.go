// Package edge implements Layer 3: Signal & Edge Discovery.
//
// Consumes FeatureDTO and emits EdgeDTO. Pure function: no DB, no
// network, no clocks except those injected via ProcessWithContext.
//
// Taxonomy (canonical — see docs/reference/architecture.md § 3.3 and the
// edge-detection / momentum-detector / signal-normalizer skills):
//
//	NEW_LAUNCH_EDGE — fires on freshly created pools (age <
//	    new_launch_window_seconds) with sufficient liquidity and
//	    contract safety. Strength is a weighted blend of liquidity,
//	    safety, holder-distribution, wallet-entropy.
//
//	MOMENTUM_EDGE   — fires on tokens past the NEW_LAUNCH window when
//	    PriceMomentum exceeds the adaptive threshold (rolling
//	    baseline q-th quantile, falling back to MinPriceMomentum
//	    during cold start) AND VolumeMomentum exceeds its floor.
//	    Strength is a weighted blend of price, volume, tx-velocity.
//
//	NONE            — emitted when neither edge qualifies. Strength
//	    is 0 and RejectReason is populated.
//
// Selection: when both candidates qualify, the highest-strength wins
// (deterministic tiebreaker: NEW_LAUNCH_EDGE).
package edge

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// Module is the edge discovery engine.
type Module struct {
	cfg    *config.EdgeConfig
	logger *slog.Logger
}

// New returns a new edge Module. A nil cfg is replaced with a safe
// default so unit tests don't crash when wired without YAML.
func New(cfg *config.EdgeConfig) *Module {
	if cfg == nil {
		cfg = &config.EdgeConfig{}
	}
	return &Module{cfg: applyDefaults(cfg), logger: slog.Default()}
}

// applyDefaults returns a copy of cfg with zero-valued fields filled in.
// The Module owns the resulting pointer so callers may pass a literal.
func applyDefaults(in *config.EdgeConfig) *config.EdgeConfig {
	out := *in
	if out.MinVelocityScore == 0 {
		out.MinVelocityScore = 0.2
	}
	if out.MinLiquidityScore == 0 {
		out.MinLiquidityScore = 0.3
	}
	if out.MaxAgeSeconds == 0 {
		out.MaxAgeSeconds = 30
	}
	if out.BaseWindowMs == 0 {
		out.BaseWindowMs = 5000
	}
	if out.WindowMomentumFactor == 0 {
		out.WindowMomentumFactor = 0.2
	}
	if out.TTLSeconds == 0 {
		out.TTLSeconds = 8
	}
	if out.NewLaunchWindowSeconds == 0 {
		out.NewLaunchWindowSeconds = 300
	}
	if out.NewLaunchWeightLiquidity == 0 && out.NewLaunchWeightSafety == 0 &&
		out.NewLaunchWeightHolders == 0 && out.NewLaunchWeightEntropy == 0 {
		out.NewLaunchWeightLiquidity = 0.4
		out.NewLaunchWeightSafety = 0.3
		out.NewLaunchWeightHolders = 0.2
		out.NewLaunchWeightEntropy = 0.1
	}
	if out.MomentumWeightPrice == 0 && out.MomentumWeightVolume == 0 &&
		out.MomentumWeightVelocity == 0 {
		out.MomentumWeightPrice = 0.4
		out.MomentumWeightVolume = 0.4
		out.MomentumWeightVelocity = 0.2
	}
	if out.MinPriceMomentum == 0 {
		out.MinPriceMomentum = 0.4
	}
	if out.MinVolumeMomentum == 0 {
		out.MinVolumeMomentum = 0.3
	}
	if out.MomentumQuantile == 0 {
		out.MomentumQuantile = 0.7
	}
	if out.BaselineMinSamples == 0 {
		out.BaselineMinSamples = 30
	}
	if out.BaselineMaxLen == 0 {
		out.BaselineMaxLen = 256
	}
	if out.ModelVersion == "" {
		out.ModelVersion = "edge-v1"
	}
	return &out
}

// Process is the legacy entry point used by tests and code paths that do
// not yet inject a baseline. It evaluates the edge with an empty rolling
// baseline (cold-start path) and the wall clock.
func (m *Module) Process(ctx context.Context, in contracts.FeatureDTO) (contracts.EdgeDTO, error) {
	return m.ProcessWithContext(ctx, in, BaselineSnapshot{}, 0, time.Now().UTC())
}

// ProcessWithContext is the deterministic, baseline-aware entry point.
// It evaluates the canonical edge taxonomy and returns the highest-
// strength candidate, or NONE when none qualify.
//
// Pure function: same (in, baseline, now, cfg) → same EdgeDTO bytes.
func (m *Module) ProcessWithContext(
	_ context.Context,
	in contracts.FeatureDTO,
	baseline BaselineSnapshot,
	edgeStrengthMin float64,
	now time.Time,
) (contracts.EdgeDTO, error) {
	now = now.UTC()
	detectedAt := now.Format(time.RFC3339Nano)

	// Adaptive PriceMomentum threshold derived from rolling baseline.
	threshold, _ := momentumThreshold(baseline.HistoryFor(SignalPriceMomentum), m.cfg)

	newLaunch, hasNewLaunch := m.detectNewLaunch(in)
	momentum, hasMomentum := m.detectMomentum(in, threshold)

	// Selection rule: highest strength wins; NEW_LAUNCH_EDGE breaks ties
	// (deterministic — favours fresh-pool discovery).
	chosen := edgeCandidate{
		edgeType:     contracts.EdgeTypeNone,
		rejectReason: "no_qualifying_edge",
	}
	switch {
	case hasNewLaunch && hasMomentum:
		if momentum.strength > newLaunch.strength {
			chosen = momentum
		} else {
			chosen = newLaunch
		}
	case hasNewLaunch:
		chosen = newLaunch
	case hasMomentum:
		chosen = momentum
	}

	// MomentumScore is always derivable from PriceMomentum and
	// VolumeMomentum — exposed even when no edge fires so downstream
	// observers see continuous signal evolution.
	momentumScore := MomentumScore(in.PriceMomentum, in.VolumeMomentum)

	// P7 — Bottom detection: analyse recent price slots to confirm a
	// V-shape recovery.  Runs only when configured and price data is
	// available.  In shadow mode the score is recorded but NOT used as a
	// gate; this allows shadow validation before enabling as a real filter.
	var bottomScore float64
	var bottomSlotsAnalysed int32

	if m.cfg.BottomDetection.Enabled && len(in.RecentPricesUsd) > 0 {
		pslots := make([]PriceSlot, len(in.RecentPricesUsd))
		for i, p := range in.RecentPricesUsd {
			pslots[i] = PriceSlot{PriceUsd: p, SlotIndex: i}
		}
		sig := AnalyzeBottom(pslots, m.cfg.BottomDetection.MaxSlots)
		bottomScore = sig.BottomDetectionScore
		bottomSlotsAnalysed = int32(sig.SlotsAnalyzed)

		// Apply as a hard gate only when NOT in shadow mode and MinScore > 0.
		minScoreGate := m.cfg.BottomDetection.MinScore > 0
		if !m.cfg.BottomDetection.ShadowMode && minScoreGate &&
			chosen.edgeType != contracts.EdgeTypeNone &&
			bottomScore < m.cfg.BottomDetection.MinScore {
			chosen = edgeCandidate{
				edgeType:     contracts.EdgeTypeNone,
				rejectReason: "bottom_not_confirmed",
			}
		}
	}

	chosen = applyModeStrengthFloor(chosen, edgeStrengthMin)

	// Opportunity window scales with momentum_score regardless of
	// edge type — preserves legacy semantics expected by Layer 5.
	opportunityWindowMs := int32(
		float64(m.cfg.BaseWindowMs) * (1 + m.cfg.WindowMomentumFactor*momentumScore),
	)

	expiresAt := now.Add(
		time.Duration(m.cfg.TTLSeconds) * time.Second,
	).Format(time.RFC3339Nano)

	eventID := contracts.ContentIDFromString(
		fmt.Sprintf("edge:%s:%s:%s", in.EventID, chosen.edgeType, m.cfg.ModelVersion),
	)

	finalConfidence := applyNarrativeMultiplier(chosen.confidence, in.NarrativeKnown, in.NarrativeScore)
	if in.NarrativeKnown {
		m.logger.Info("edge_narrative_multiplier",
			"token", in.TokenAddress,
			"base_confidence", chosen.confidence,
			"final_confidence", finalConfidence,
			"narrative_score", in.NarrativeScore,
			"delta_pct", (finalConfidence-chosen.confidence)*100,
			"edge_type", chosen.edgeType,
		)
	}

	return contracts.EdgeDTO{
		EventID:       eventID,
		TraceID:       in.TraceID,
		CorrelationID: in.CorrelationID,
		CausationID:   in.EventID,
		VersionID:     in.VersionID,

		TokenLifecycleID: in.TokenLifecycleID,
		TokenAddress:     in.TokenAddress,

		EdgeType:            chosen.edgeType,
		EdgeStrength:        chosen.strength,
		EdgeConfidence:      finalConfidence,
		MomentumScore:       momentumScore,
		ThresholdApplied:    chosen.threshold,
		OpportunityWindowMs: opportunityWindowMs,

		ExpiresAt:  expiresAt,
		DetectedAt: detectedAt,

		EdgeModelVersionID: m.cfg.ModelVersion,
		RejectReason:       chosen.rejectReason,

		// P7 bottom detection (zero when subsystem is disabled or inactive).
		BottomDetectionScore: bottomScore,
		SlotWindowSize:       bottomSlotsAnalysed,
	}, nil
}

// applyModeStrengthFloor rejects a qualifying edge whose strength is below
// the operational-mode floor from config/priority.yaml (docs/plans/2026-06-10-profit-restoration-plan.md Task 3).
func applyModeStrengthFloor(chosen edgeCandidate, edgeStrengthMin float64) edgeCandidate {
	if chosen.edgeType == contracts.EdgeTypeNone || edgeStrengthMin <= 0 {
		return chosen
	}
	if chosen.strength >= edgeStrengthMin {
		return chosen
	}
	return edgeCandidate{
		edgeType:     contracts.EdgeTypeNone,
		rejectReason: fmt.Sprintf("edge_strength_below_floor:strength=%.4f,floor=%.4f", chosen.strength, edgeStrengthMin),
	}
}

// edgeCandidate is the internal struct populated by detect* helpers.
type edgeCandidate struct {
	edgeType     string
	strength     float64
	confidence   float64
	threshold    float64
	rejectReason string
}

// detectNewLaunch evaluates the NEW_LAUNCH_EDGE gates and returns
// (candidate, true) on qualification.
func (m *Module) detectNewLaunch(in contracts.FeatureDTO) (edgeCandidate, bool) {
	// Age gate: TokenAgeSecondsRaw=0 is treated as "unknown" and allowed
	// (defensive: missing age must not silently disqualify); a
	// strictly-positive age must be below the configured window.
	if in.TokenAgeSecondsRaw > 0 && in.TokenAgeSecondsRaw >= m.cfg.NewLaunchWindowSeconds {
		return edgeCandidate{}, false
	}
	if in.LiquidityScore < m.cfg.MinLiquidityScore {
		return edgeCandidate{}, false
	}
	if in.TxVelocityScore < m.cfg.MinVelocityScore {
		return edgeCandidate{}, false
	}
	if in.ContractSafety < m.cfg.MinContractSafety {
		return edgeCandidate{}, false
	}

	strength := clamp01(
		m.cfg.NewLaunchWeightLiquidity*in.LiquidityScore +
			m.cfg.NewLaunchWeightSafety*in.ContractSafety +
			m.cfg.NewLaunchWeightHolders*in.HolderDistribution +
			m.cfg.NewLaunchWeightEntropy*in.WalletEntropy,
	)

	confidence := minNonZero(
		in.Confidence.LiquidityScore,
		in.Confidence.ContractSafety,
		in.Confidence.HolderDistribution,
		in.Confidence.WalletEntropy,
	)

	return edgeCandidate{
		edgeType:   contracts.EdgeTypeNewLaunch,
		strength:   strength,
		confidence: confidence,
		threshold:  m.cfg.MinLiquidityScore,
	}, true
}

// detectMomentum evaluates the MOMENTUM_EDGE gates against the supplied
// adaptive PriceMomentum threshold.
func (m *Module) detectMomentum(in contracts.FeatureDTO, threshold float64) (edgeCandidate, bool) {
	// Age gate: must be past the NEW_LAUNCH window. Unknown age (=0) is
	// excluded from MOMENTUM_EDGE to avoid double-counting fresh pools.
	if in.TokenAgeSecondsRaw < m.cfg.NewLaunchWindowSeconds {
		return edgeCandidate{}, false
	}
	if in.PriceMomentum < threshold {
		return edgeCandidate{}, false
	}
	if in.VolumeMomentum < m.cfg.MinVolumeMomentum {
		return edgeCandidate{}, false
	}

	strength := clamp01(
		m.cfg.MomentumWeightPrice*in.PriceMomentum +
			m.cfg.MomentumWeightVolume*in.VolumeMomentum +
			m.cfg.MomentumWeightVelocity*in.TxVelocityScore,
	)

	confidence := minNonZero(
		in.Confidence.PriceMomentum,
		in.Confidence.VolumeMomentum,
		in.Confidence.TxVelocityScore,
	)

	return edgeCandidate{
		edgeType:   contracts.EdgeTypeMomentum,
		strength:   strength,
		confidence: confidence,
		threshold:  threshold,
	}, true
}

// momentumThreshold returns the adaptive PriceMomentum threshold derived
// from the rolling-window quantile, falling back to MinPriceMomentum
// during cold start (history shorter than BaselineMinSamples). The
// returned value is clamped to [MinPriceMomentum, 1.0].
//
// The bool return reports whether the adaptive (not cold-start) path was
// taken — useful for observability tests.
func momentumThreshold(history []float64, cfg *config.EdgeConfig) (float64, bool) {
	if len(history) < cfg.BaselineMinSamples {
		return cfg.MinPriceMomentum, false
	}
	q := quantile(history, cfg.MomentumQuantile)
	if q < cfg.MinPriceMomentum {
		q = cfg.MinPriceMomentum
	}
	if q > 1 {
		q = 1
	}
	return q, true
}

// minNonZero returns the smallest strictly-positive value among args.
// Zero arguments are skipped (treated as "no signal"). When ALL args are
// zero, returns 0 — the caller may decide to treat that as low confidence.
//
// This implements the "min of relevant feature confidences" rule: a
// feature with confidence=0 typically means the upstream extractor had
// no data, so excluding it from the min prevents a single missing input
// from collapsing edge_confidence to zero.
func minNonZero(values ...float64) float64 {
	out := 0.0
	for _, v := range values {
		if v <= 0 {
			continue
		}
		if out == 0 || v < out {
			out = v
		}
	}
	return out
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// applyNarrativeMultiplier scales EdgeConfidence by ±10% based on the
// AI narrative score (0–10, midpoint 5). Only applied when NarrativeKnown=true.
// Δ = (score - 5) × 0.02 → range [-0.10, +0.10].
func applyNarrativeMultiplier(confidence float64, known bool, score float64) float64 {
	if !known {
		return confidence
	}
	delta := (score - 5.0) * 0.02
	return clamp01(confidence * (1.0 + delta))
}

// minFloat is retained for backward compatibility with callers and tests
// that import it. New code should use minNonZero.
func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
