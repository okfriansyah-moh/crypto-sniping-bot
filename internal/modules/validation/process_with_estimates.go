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
//   - missing-model prior fallback ("prob_join_timeout") when prob is nil
//     and use_model_output=true
//
// All inputs other than `in` may be nil; when nil the priors from
// ValidationConfig are used.  The function is pure — no DB, no clock except
// for ExpiresAt/ValidatedAt timestamps which are derived from time.Now().
func (m *Module) ProcessWithEstimates(
	_ context.Context,
	in contracts.EdgeDTO,
	prob *contracts.ProbabilityEstimateDTO,
	slip *contracts.SlippageEstimateDTO,
	lat *contracts.LatencyProfileDTO,
) (contracts.ValidatedEdgeDTO, error) {
	nowTime := time.Now().UTC()
	now := nowTime.Format(time.RFC3339Nano)

	// Fallback reason tags carry diagnostic intent without rejecting.
	var fallbackReasons []string

	p := m.cfg.PriorProbability
	probInvalid := false
	if prob != nil {
		// Phase 9 (§ 9.3) NaN/Inf and range guards.
		if m.probCfg != nil && m.probCfg.RejectNanOrInf &&
			(math.IsNaN(prob.Probability) || math.IsInf(prob.Probability, 0)) {
			probInvalid = true
		} else if m.probCfg != nil && m.probCfg.RejectOutOfRange &&
			(prob.Probability < 0 || prob.Probability > 1) {
			probInvalid = true
		} else if prob.Probability > 0 && prob.Probability < 1 {
			// Confidence gate: low-confidence model output → prior fallback.
			// We use ProbabilityEstimateDTO.Calibration as the model-confidence
			// proxy (Brier-style, [0,1]) since the contract is frozen.
			if m.probCfg != nil && m.probCfg.MinModelConfidence > 0 &&
				prob.Calibration > 0 && prob.Calibration < m.probCfg.MinModelConfidence {
				fallbackReasons = append(fallbackReasons, "low_model_confidence")
			} else {
				p = prob.Probability
			}
		}
	} else if m.probCfg != nil && m.probCfg.UseModelOutput {
		// Model is enabled but no estimate joined in time.
		fallbackReasons = append(fallbackReasons, "prob_join_timeout")
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
	case in.EdgeType == "":
		rejectReason = "no_edge_detected"
	default:
		ev := p*float64(m.cfg.PriorGainBps) -
			(1-p)*float64(m.cfg.PriorLossBps) -
			float64(m.cfg.FixedCostsBps) -
			float64(slipBps)

		if ev < float64(m.cfg.EvThresholdBps) {
			rejectReason = fmt.Sprintf("ev_below_threshold:ev=%.1f,threshold=%d", ev, m.cfg.EvThresholdBps)
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

	// Append fallback diagnostic tags to RejectReason for traceability.
	// This preserves backward-compat (REJECT only when rejectReason != "")
	// while letting downstream observability consume the prefix.
	if len(fallbackReasons) > 0 {
		tag := "fallback:" + strings.Join(fallbackReasons, ",")
		if rejectReason == "" {
			rejectReason = tag
		} else {
			rejectReason = rejectReason + ";" + tag
		}
	}

	evBps := int32(p*float64(m.cfg.PriorGainBps) -
		(1-p)*float64(m.cfg.PriorLossBps) -
		float64(m.cfg.FixedCostsBps) -
		float64(slipBps))

	gainBps := int32(p * float64(m.cfg.PriorGainBps))
	lossBps := int32((1 - p) * float64(m.cfg.PriorLossBps))

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
		EvThresholdApplied: m.cfg.EvThresholdBps,
		RejectReason:       rejectReason,

		ExpectedLatencyMs: latencyP95,
		LatencyGatePassed: latencyGatePassed,

		ExpiresAt:   expiresAt,
		ValidatedAt: now,
	}, nil
}
