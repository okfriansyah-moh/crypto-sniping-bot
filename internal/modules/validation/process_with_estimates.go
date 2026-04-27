package validation

import (
	"context"
	"fmt"
	"time"

	"crypto-sniping-bot/contracts"
)

// ProcessWithEstimates is the Phase 4 entry point.  It applies the EV gate
// using whichever model estimates are available (probability, slippage,
// latency) and falls back to the configured priors for any missing model.
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

	p := m.cfg.PriorProbability
	if prob != nil && prob.Probability > 0 && prob.Probability < 1 {
		p = prob.Probability
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

	if in.EdgeType == "" {
		rejectReason = "no_edge_detected"
	} else {
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
