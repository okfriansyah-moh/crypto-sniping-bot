// Package validation implements Layer 5: Edge Validation.
// Consumes EdgeDTO and emits ValidatedEdgeDTO.
// Pure function: no DB, no side effects.
package validation

import (
"context"
"fmt"
"time"

"crypto-sniping-bot/contracts"
"crypto-sniping-bot/internal/app/config"
)

// Module is the edge validation engine.
type Module struct {
cfg     *config.ValidationConfig
probCfg *config.ProbabilityRuntimeConfig // Phase 9 (§ 9.3) — optional, may be nil.
}

// New returns a new validation Module.
func New(cfg *config.ValidationConfig) *Module {
if cfg == nil {
cfg = &config.ValidationConfig{
PriorProbability: 0.55,
PriorGainBps:     500,
PriorLossBps:     300,
PriorSlippageBps: 100,
EvThresholdBps:   10,
FixedCostsBps:    50,
BuildSubmitP95Ms: 500,
TTLSeconds:       5,
}
}
return &Module{cfg: cfg}
}

// WithProbabilityRuntime attaches the Phase 9 probability runtime config
// (NaN/Inf guards, confidence gate, fallback semantics). Returns the
// receiver to allow fluent wiring at construction sites.
func (m *Module) WithProbabilityRuntime(p *config.ProbabilityRuntimeConfig) *Module {
m.probCfg = p
return m
}

// Process evaluates an EdgeDTO and emits ValidatedEdgeDTO.
// EV gate per docs/implementation_roadmap.md §3.5.
// Phase 2: fixed probability priors, no real model (deferred to Phase 4).
func (m *Module) Process(_ context.Context, in contracts.EdgeDTO) (contracts.ValidatedEdgeDTO, error) {
nowTime := time.Now().UTC()
now := nowTime.Format(time.RFC3339Nano)

rejectReason := ""
latencyGatePassed := true

if in.EdgeType == "" {
rejectReason = "no_edge_detected"
} else {
// EV = P × GainBps - (1-P) × LossBps - FixedCostsBps - SlippageBps
p := m.cfg.PriorProbability
ev := p*float64(m.cfg.PriorGainBps) -
(1-p)*float64(m.cfg.PriorLossBps) -
float64(m.cfg.FixedCostsBps) -
float64(m.cfg.PriorSlippageBps)

if ev < float64(m.cfg.EvThresholdBps) {
rejectReason = fmt.Sprintf("ev_below_threshold:ev=%.1f,threshold=%d", ev, m.cfg.EvThresholdBps)
}

// Latency gate: build+submit P95 must fit in opportunity window.
if in.OpportunityWindowMs > 0 && int32(m.cfg.BuildSubmitP95Ms) > in.OpportunityWindowMs {
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

p := m.cfg.PriorProbability
evBps := int32(p*float64(m.cfg.PriorGainBps) -
(1-p)*float64(m.cfg.PriorLossBps) -
float64(m.cfg.FixedCostsBps) -
float64(m.cfg.PriorSlippageBps))

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
SlippageP95BpsUsed: m.cfg.PriorSlippageBps,
EvThresholdApplied: m.cfg.EvThresholdBps,
RejectReason:       rejectReason,

ExpectedLatencyMs: int32(m.cfg.BuildSubmitP95Ms),
LatencyGatePassed: latencyGatePassed,

ExpiresAt:   expiresAt,
ValidatedAt: now,
}, nil
}
