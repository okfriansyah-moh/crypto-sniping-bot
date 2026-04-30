// Phase 9 (Profitability Restoration § 9.4) — dynamic capital allocation.
//
// ProcessWithEstimates produces an AllocationDTO whose SizeUsd is
// proportional to the active edge:
//
//	size_raw    = base × score × P × confidence × kelly_fraction
//	size_mode   = size_raw × mode_multiplier × cohort_multiplier
//	size_final  = clamp(min_size, max_size, exploration_band(size_mode))
//
// All knobs come from CapitalConfig (mirrors config/capital.yaml).
// Reject reasons surfaced in AllocationDTO.RejectReason:
//   - "missing_probability"      — prob nil, on_missing_probability=reject
//   - "negative_kelly"           — kelly fraction ≤ 0, reject_negative=true
//   - "low_aggregate_confidence" — feat.Confidence aggregate below threshold
//   - "size_below_min"           — final size < min_size_usd
//
// Backward-compat: when m.cfg.UseDynamicSizing is false (or cfg nil),
// delegates to legacy Process for fixed sizing.
package capital

import (
	"context"
	"fmt"
	"math"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// ProcessWithEstimates produces an AllocationDTO with Phase 9 dynamic sizing.
// `mode` is the current operational mode (STRICT|BALANCED|EXPLORATION).
// `prob` and `feat` may be nil; missing-prob handling is governed by
// cfg.FailurePolicy.OnMissingProbability.
func (m *Module) ProcessWithEstimates(
	ctx context.Context,
	in contracts.SelectionOutputDTO,
	prob *contracts.ProbabilityEstimateDTO,
	feat *contracts.FeatureDTO,
	mode string,
	chain string,
	portfolioUsd float64,
) (contracts.AllocationDTO, error) {
	// Legacy / disabled path — preserve old behavior.
	if m.cfg == nil || !m.cfg.UseDynamicSizing {
		return m.Process(ctx, in, chain)
	}

	nowTime := time.Now().UTC()
	now := nowTime.Format(time.RFC3339Nano)

	if !in.Selected {
		return rejectedAllocation(in, chain, in.RejectReason, now), nil
	}

	// ── Probability resolution ────────────────────────────────────
	p, probReject := resolveProbability(prob, m.cfg.FailurePolicy)
	if probReject != "" {
		return rejectedAllocation(in, chain, probReject, now), nil
	}

	// ── Aggregate confidence gate ─────────────────────────────────
	// Phase 9 audit fix: when MinAggregateConfidence > 0, treat aggConf==0
	// (nil features or all-zero/invalid components) as below-threshold.
	// Previously aggConf==0 bypassed the gate and was later promoted to
	// 1.0, which sized trades at full confidence on missing features.
	aggConf := aggregateConfidence(feat)
	if m.cfg.MinAggregateConfidence > 0 && aggConf < m.cfg.MinAggregateConfidence {
		return rejectedAllocation(in, chain, "low_aggregate_confidence", now), nil
	}

	// ── Kelly fraction ────────────────────────────────────────────
	kellyCap := KellyCapForMode(mode, m.cfg.Kelly)
	f := KellyFraction(p, m.cfg.Kelly, kellyCap)
	if m.cfg.Kelly.RejectNegative && f <= 0 {
		return rejectedAllocation(in, chain, "negative_kelly", now), nil
	}
	if f < 0 {
		f = 0
	}

	// ── Raw edge-proportional size ────────────────────────────────
	// Security (Phase 9 audit M3): explicitly reject NaN/Inf
	// CombinedScore — clampUnit silently flattens these to 0, which
	// would then be coerced to 1.0 below (fail-open: max favorable
	// sizing on malformed upstream input).
	if math.IsNaN(in.CombinedScore) || math.IsInf(in.CombinedScore, 0) {
		return rejectedAllocation(in, chain, "invalid_score", now), nil
	}
	score := clampUnit(in.CombinedScore)
	if score == 0 {
		// Legitimate zero score may indicate a legacy caller that does
		// not populate CombinedScore — treat as 1.0 to avoid zeroing
		// every allocation. NaN/Inf are rejected above; only finite
		// values reach this branch.
		score = 1.0
	}
	conf := aggConf
	if conf == 0 {
		conf = 1.0
	}
	base := m.cfg.BaseSizeUsd
	if base <= 0 {
		base = m.cfg.FixedEntrySizeUsd
	}
	sizeRaw := base * score * p * conf * f

	// ── Mode + cohort multipliers ─────────────────────────────────
	// Security (Phase 9 audit M2): honor failure policy when the
	// configured ModeMultipliers map does not contain the active
	// mode. Default behavior remains fail-soft (BALANCED fallback)
	// unless OnModeLookupStale="reject" is set.
	modeMult, modeFallback := ModeMultiplier(mode, m.cfg)
	if modeFallback && m.cfg.FailurePolicy.OnModeLookupStale == "reject" {
		return rejectedAllocation(in, chain, "mode_lookup_stale", now), nil
	}
	cohortMult := CohortMultiplier("default", m.cfg)
	sizeUsd := sizeRaw * modeMult * cohortMult

	// ── Exploration band ──────────────────────────────────────────
	sizeUsd = ExplorationBand(sizeUsd, mode, portfolioUsd, m.cfg)

	// ── Min / Max clamping ────────────────────────────────────────
	if m.cfg.MaxSizeUsd > 0 && sizeUsd > m.cfg.MaxSizeUsd {
		sizeUsd = m.cfg.MaxSizeUsd
	}
	if m.cfg.MinSizeUsd > 0 && sizeUsd < m.cfg.MinSizeUsd {
		return rejectedAllocation(in, chain, "size_below_min", now), nil
	}
	if math.IsNaN(sizeUsd) || math.IsInf(sizeUsd, 0) || sizeUsd <= 0 {
		return rejectedAllocation(in, chain, "size_invalid", now), nil
	}

	expiresAt := nowTime.Add(time.Duration(m.cfg.TTLSeconds) * time.Second).Format(time.RFC3339Nano)
	executionID := contracts.ContentIDFromString(in.TraceID + in.VersionID + in.TokenAddress + chain)
	eventID := contracts.ContentIDFromString(fmt.Sprintf("alloc:%s", in.EventID))

	return contracts.AllocationDTO{
		EventID:       eventID,
		TraceID:       in.TraceID,
		CorrelationID: in.CorrelationID,
		CausationID:   in.EventID,
		VersionID:     in.VersionID,

		TokenLifecycleID: in.TokenLifecycleID,
		TokenAddress:     in.TokenAddress,
		Chain:            chain,

		ExecutionID:    executionID,
		SizeUsd:        sizeUsd,
		SizeBaseRaw:    "0",
		MaxSlippageBps: 200,
		WalletAddress:  m.cfg.WalletAddress,
		WalletShard:    0,

		Rejected:     false,
		RejectReason: "",
		CohortID:     "default",

		ExpiresAt:   expiresAt,
		AllocatedAt: now,
	}, nil
}

func rejectedAllocation(in contracts.SelectionOutputDTO, chain, reason, now string) contracts.AllocationDTO {
	eventID := contracts.ContentIDFromString(fmt.Sprintf("alloc-reject:%s:%s", in.EventID, reason))
	return contracts.AllocationDTO{
		EventID:       eventID,
		TraceID:       in.TraceID,
		CorrelationID: in.CorrelationID,
		CausationID:   in.EventID,
		VersionID:     in.VersionID,

		TokenLifecycleID: in.TokenLifecycleID,
		TokenAddress:     in.TokenAddress,
		Chain:            chain,

		ExecutionID:  "",
		SizeUsd:      0,
		SizeBaseRaw:  "0",
		Rejected:     true,
		RejectReason: reason,
		AllocatedAt:  now,
	}
}

// resolveProbability reads probability per failure policy. Returns
// (effectiveP, rejectReason). When rejectReason != "", caller must reject.
// The fallback prior is sourced from FailurePolicy.FallbackPriorProbability
// (mirrors capital.yaml) — not hardcoded — and a missing/invalid value
// causes a reject rather than a silent default.
func resolveProbability(prob *contracts.ProbabilityEstimateDTO, fp config.CapitalFailurePolicyConfig) (float64, string) {
	if prob == nil {
		switch fp.OnMissingProbability {
		case "fallback_prior":
			fp.FallbackPriorProbability = clampProbability(fp.FallbackPriorProbability)
			if fp.FallbackPriorProbability <= 0 || fp.FallbackPriorProbability >= 1 {
				return 0, "missing_fallback_prior"
			}
			return fp.FallbackPriorProbability, ""
		case "reject", "":
			return 0, "missing_probability"
		}
		return 0, "missing_probability"
	}
	p := prob.Probability
	if math.IsNaN(p) || math.IsInf(p, 0) || p <= 0 || p >= 1 {
		return 0, "invalid_probability"
	}
	return p, ""
}

// clampProbability returns 0 for NaN/Inf and otherwise the input unchanged.
// Used to reject malformed configured priors at the boundary.
func clampProbability(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	return v
}

// aggregateConfidence collapses FeatureConfidence into a single [0,1] value
// using a min-aggregate (the most pessimistic component).
func aggregateConfidence(feat *contracts.FeatureDTO) float64 {
	if feat == nil {
		return 0
	}
	c := feat.Confidence
	values := []float64{
		c.LiquidityScore, c.TxVelocityScore, c.HolderDistribution,
		c.WalletEntropy, c.ContractSafety, c.TokenAge,
		c.VolumeMomentum, c.PriceMomentum,
	}
	min := math.Inf(1)
	any := false
	for _, v := range values {
		// Security (Phase 9 audit M1): explicitly skip NaN/Inf —
		// `v <= 0` returns false for NaN, which would otherwise
		// poison `min` with non-finite values and propagate Inf
		// into size_raw. Defense-in-depth before the final
		// IsNaN/IsInf size check.
		if math.IsNaN(v) || math.IsInf(v, 0) {
			continue
		}
		if v <= 0 {
			continue
		}
		any = true
		if v < min {
			min = v
		}
	}
	if !any {
		return 0
	}
	return min
}

func clampUnit(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
