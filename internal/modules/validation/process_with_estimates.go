package validation

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"crypto-sniping-bot/contracts"
)

// ProcessWithEstimates is the Phase 4 entry point.  It applies the EV gate
// using whichever model estimates are available (probability, slippage,
// latency) and falls back to the configured priors for any missing model.
//
// Phase 9 (§ 9.3) wiring: when ProbabilityRuntimeConfig is attached via
// WithProbabilityRuntime, the function additionally enforces:
//   - NaN/Inf reject ("invalid_probability") when reject_nan_or_inf=true
//   - out-of-range reject ("invalid_probability") when reject_out_of_range=true
//   - low-confidence prior fallback ("low_model_confidence") when
//     prob.Confidence < min_model_confidence
//   - missing-model REJECT ("probability_unavailable") when prob is nil
//     and use_model_output=true. NOTE: this used to silently substitute
//     PriorProbability and tag "prob_join_timeout" as a fallback, which
//     drove every trade to ev_bps≈-1900 and starved Layers 6–10 in
//     production. Per docs/reference/architecture.md § 3.5 the prior is acceptable
//     ONLY for replay/backtest paths (UseModelOutput=false). When the
//     model is enabled but its estimate has not joined within the
//     ValidationWorker's bounded join window, the correct response is
//     to REJECT with an explicit, distinct reject_reason so the join
//     failure is observable upstream.
//
// All inputs other than `in` may be nil; when nil the priors from
// ValidationConfig are used.  The function is pure — no DB, no clock except
// for ExpiresAt/ValidatedAt timestamps which are derived from time.Now().
func (m *Module) ProcessWithEstimates(
	ctx context.Context,
	in contracts.EdgeDTO,
	prob *contracts.ProbabilityEstimateDTO,
	slip *contracts.SlippageEstimateDTO,
	lat *contracts.LatencyProfileDTO,
) (contracts.ValidatedEdgeDTO, error) {
	return m.ProcessWithEstimatesAt(ctx, in, prob, slip, lat, 0, time.Now().UTC())
}

// ProcessWithEstimatesAt is the deterministic, replay-safe variant of
// ProcessWithEstimates. It accepts an explicit `now` (UTC) which drives
// every timestamp emitted in the resulting ValidatedEdgeDTO
// (ValidatedAt, ExpiresAt). Callers replaying the event log MUST pass
// `evt.OccurredAt` (the bus-recorded creation time) so the function is
// bit-for-bit reproducible across replays — see docs/reference/architecture.md
// § 4.2 (replay must be bit-for-bit deterministic).
func (m *Module) ProcessWithEstimatesAt(
	_ context.Context,
	in contracts.EdgeDTO,
	prob *contracts.ProbabilityEstimateDTO,
	slip *contracts.SlippageEstimateDTO,
	lat *contracts.LatencyProfileDTO,
	evThresholdBps int32,
	now time.Time,
) (contracts.ValidatedEdgeDTO, error) {
	nowTime := now.UTC()
	nowStr := nowTime.Format(time.RFC3339Nano)
	evThreshold := m.effectiveEVThreshold(evThresholdBps)

	// Fallback reason tags carry diagnostic intent without rejecting.
	var fallbackReasons []string

	p := m.cfg.PriorProbability
	probInvalid := false
	probUnavailable := false
	if prob != nil {
		// Phase 9 (§ 9.3) NaN/Inf and range guards.
		switch {
		case m.probCfg != nil && m.probCfg.RejectNanOrInf &&
			(math.IsNaN(prob.Probability) || math.IsInf(prob.Probability, 0)):
			probInvalid = true
		case m.probCfg != nil && m.probCfg.RejectOutOfRange &&
			(math.IsNaN(prob.Probability) || math.IsInf(prob.Probability, 0) ||
				prob.Probability < 0 || prob.Probability > 1):
			probInvalid = true
		case prob.Probability >= 0 && prob.Probability <= 1:
			// F-SEC-01: boundary-inclusive — F-1 already clips probability
			// to [0.05, 0.95], but defense-in-depth here covers any
			// upstream change to the clipping rule. 0.0 and 1.0 are
			// honoured exactly (no silent prior substitution).
			//
			// Confidence gate: low-confidence model output → prior fallback.
			// Use prob.Confidence directly. When Confidence==0 the probability
			// model had no feature-confidence data (cold-start) — this is NOT
			// low confidence and must NOT trigger fallback.
			// NOTE: the former legacy fallback to prob.Calibration (BrierCalibration)
			// was incorrect: Brier score (lower=better) is a model-accuracy metric,
			// not a feature-confidence signal. Using BrierCalibration=0.18 against
			// MinModelConfidence=0.70 caused all cold-start tokens to fall back to
			// prior=0.35 → ev≈-1900 bps → 100% reject.
			conf := prob.Confidence
			if m.probCfg != nil && m.probCfg.MinModelConfidence > 0 &&
				conf > 0 && conf < m.probCfg.MinModelConfidence {
				fallbackReasons = append(fallbackReasons, "low_model_confidence")
			} else {
				p = prob.Probability
			}
		default:
			// F-SEC-01: input is outside [0,1] (NaN, ±Inf, or numeric OOB)
			// AND the strict-reject runtime guard is not configured. The
			// previous behaviour silently substituted PriorProbability with
			// no diagnostic tag — that is the boundary-value silent prior
			// the security review flagged. We now record the substitution
			// as `prob_boundary_value` so the fallback is observable in the
			// emitted ValidatedEdgeDTO.FallbackReasons even when the
			// decision is ACCEPT.
			fallbackReasons = append(fallbackReasons, "prob_boundary_value")
		}
	} else if m.probCfg != nil && m.probCfg.UseModelOutput {
		// Production mode: model is enabled but no estimate joined within
		// the bounded validation join window. Do NOT silently substitute
		// the prior to compute EV — emit an explicit REJECT so the join
		// failure surfaces in metrics and downstream layers are not
		// poisoned by deterministic mass-rejects at large negative bps.
		probUnavailable = true
		// Force ProbabilityUsed to 0 so the emitted DTO honestly reflects
		// "no model input was available" rather than carrying the prior.
		p = 0
	}

	slipBps := m.cfg.PriorSlippageBps
	if slip != nil && slip.ExpectedP95Bps > 0 {
		slipBps = slip.ExpectedP95Bps
	}

	latencyP95 := int32(m.cfg.BuildSubmitP95Ms)
	if lat != nil && lat.ExpectedP95Ms > 0 {
		latencyP95 = lat.ExpectedP95Ms
	}

	rejectReason := ""
	latencyGatePassed := true

	switch {
	case probInvalid:
		rejectReason = "invalid_probability"
	case probUnavailable:
		// Production-mode join failure — explicit and distinct from
		// the legacy "prob_join_timeout" fallback tag.
		rejectReason = "probability_unavailable"
	case in.EdgeType == "":
		rejectReason = "no_edge_detected"
	default:
		ev := p*float64(m.cfg.PriorGainBps) -
			(1-p)*float64(m.cfg.PriorLossBps) -
			float64(m.cfg.FixedCostsBps) -
			float64(slipBps)

		if ev < float64(evThreshold) {
			rejectReason = fmt.Sprintf("ev_below_threshold:ev=%.1f,threshold=%d", ev, evThreshold)
		}

		if in.OpportunityWindowMs > 0 && latencyP95 > in.OpportunityWindowMs {
			latencyGatePassed = false
			if rejectReason == "" {
				rejectReason = "latency_exceeds_window"
			}
		}
	}

	decision := "ACCEPT"
	if rejectReason != "" {
		decision = "REJECT"
		latencyGatePassed = false
	}

	// Append fallback diagnostic tags to RejectReason ONLY when the decision
	// is REJECT — the ValidatedEdgeDTO contract requires RejectReason to be
	// empty on ACCEPT (see contracts/validated_edge.go). Fallback usage on an
	// accepted edge is recorded via FallbackReasons (below) for downstream
	// observability without violating the contract.
	if decision == "REJECT" && len(fallbackReasons) > 0 {
		tag := "fallback:" + strings.Join(fallbackReasons, ",")
		if rejectReason == "" {
			rejectReason = tag
		} else {
			rejectReason = rejectReason + ";" + tag
		}
	}

	// F-SEC-07: clamp the EV float into int32 range before casting.
	// A raw int32() cast on a float exceeding ±2^31 silently wraps to a
	// nonsense value; with adversarial / mis-tuned PriorGainBps this lets
	// rejected trades report ACCEPT-shaped EVs. clipInt32 below pins the
	// result to [MinInt32+1, MaxInt32-1].
	evFloat := p*float64(m.cfg.PriorGainBps) -
		(1-p)*float64(m.cfg.PriorLossBps) -
		float64(m.cfg.FixedCostsBps) -
		float64(slipBps)
	evBps := clipInt32(evFloat)

	gainBps := clipInt32(p * float64(m.cfg.PriorGainBps))
	lossBps := clipInt32((1 - p) * float64(m.cfg.PriorLossBps))

	expiresAt := nowTime.Add(
		time.Duration(m.cfg.TTLSeconds) * time.Second,
	).Format(time.RFC3339Nano)

	eventID := contracts.ContentIDFromString(fmt.Sprintf("validated:%s:%s", in.EventID, decision))

	return contracts.ValidatedEdgeDTO{
		EventID:       eventID,
		TraceID:       in.TraceID,
		CorrelationID: in.CorrelationID,
		CausationID:   in.EventID,
		VersionID:     in.VersionID,

		TokenLifecycleID: in.TokenLifecycleID,
		TokenAddress:     in.TokenAddress,

		Decision:           decision,
		ExpectedValueBps:   evBps,
		ExpectedGainBps:    gainBps,
		ExpectedLossBps:    lossBps,
		FixedCostsBps:      m.cfg.FixedCostsBps,
		ProbabilityUsed:    p,
		SlippageP95BpsUsed: slipBps,
		EvThresholdApplied: evThreshold,
		RejectReason:       rejectReason,

		ExpectedLatencyMs: latencyP95,
		LatencyGatePassed: latencyGatePassed,

		ExpiresAt:       expiresAt,
		ValidatedAt:     nowStr,
		FallbackReasons: fallbackReasons,
	}, nil
}

// effectiveEVThreshold returns the per-call mode threshold when positive,
// otherwise the static ValidationConfig default from pipeline.yaml.
func (m *Module) effectiveEVThreshold(evThresholdBps int32) int32 {
	if evThresholdBps > 0 {
		return evThresholdBps
	}
	return m.cfg.EvThresholdBps
}

// clipInt32 rounds and clamps a float64 into the int32 range, treating
// NaN/\u00b1Inf as 0. Deliberate small duplication of the clipFloat pattern
// in internal/modules/models/slippage.go to avoid coupling the validation
// module to the models package for a one-line helper. F-SEC-07.
func clipInt32(v float64) int32 {
	if math.IsNaN(v) {
		return 0
	}
	const lo = float64(math.MinInt32 + 1)
	const hi = float64(math.MaxInt32 - 1)
	if v > hi || math.IsInf(v, 1) {
		return int32(math.MaxInt32 - 1)
	}
	if v < lo || math.IsInf(v, -1) {
		return int32(math.MinInt32 + 1)
	}
	return int32(math.Round(v))
}
