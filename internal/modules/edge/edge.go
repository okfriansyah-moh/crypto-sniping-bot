// Package edge implements Layer 3: Signal & Edge Discovery.
// Consumes FeatureDTO and emits EdgeDTO.
// Pure function: no DB, no side effects.
package edge

import (
	"context"
	"fmt"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// Module is the edge discovery engine.
type Module struct {
	cfg *config.EdgeConfig
}

// New returns a new edge Module.
func New(cfg *config.EdgeConfig) *Module {
	if cfg == nil {
		cfg = &config.EdgeConfig{
			MinVelocityScore:     0.2,
			MinLiquidityScore:    0.3,
			MaxAgeSeconds:        30,
			BaseWindowMs:         5000,
			WindowMomentumFactor: 0.2,
			TTLSeconds:           8,
		}
	}
	return &Module{cfg: cfg}
}

// Process evaluates a FeatureDTO and emits EdgeDTO.
// Detects NEW_LAUNCH_EDGE pattern per docs/implementation_roadmap.md §3.3.
// Phase 2: single edge type, fixed window; Phase 3 adds adaptive thresholds.
func (m *Module) Process(_ context.Context, in contracts.FeatureDTO) (contracts.EdgeDTO, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	edgeType := ""
	edgeStrength := 0.0
	edgeConfidence := 0.0
	momentumScore := 0.0

	// NEW_LAUNCH_EDGE fires when liquidity and velocity scores exceed minimums.
	if in.LiquidityScore >= m.cfg.MinLiquidityScore &&
		in.TxVelocityScore >= m.cfg.MinVelocityScore {
		edgeType = "NEW_LAUNCH_EDGE"

		// EdgeStrength: weighted combination of available signals.
		edgeStrength = in.LiquidityScore*0.5 + in.ContractSafety*0.3 + in.VolumeMomentum*0.2

		// EdgeConfidence: minimum of per-feature confidences that drove the decision.
		edgeConfidence = minFloat(in.Confidence.LiquidityScore, in.Confidence.ContractSafety)

		// MomentumScore: direct from feature.
		momentumScore = in.VolumeMomentum
	}

	// Opportunity window: base_ms * (1 + momentum_factor * momentum_score).
	opportunityWindowMs := int32(
		float64(m.cfg.BaseWindowMs) * (1 + m.cfg.WindowMomentumFactor*momentumScore),
	)

	// ExpiresAt: now + TTL.
	expiresAt := time.Now().UTC().Add(
		time.Duration(m.cfg.TTLSeconds) * time.Second,
	).Format(time.RFC3339Nano)

	eventID := contracts.ContentIDFromString(fmt.Sprintf("edge:%s:%s", in.EventID, edgeType))

	return contracts.EdgeDTO{
		EventID:       eventID,
		TraceID:       in.TraceID,
		CorrelationID: in.CorrelationID,
		CausationID:   in.EventID,
		VersionID:     in.VersionID,

		TokenLifecycleID: in.TokenLifecycleID,
		TokenAddress:     in.TokenAddress,

		EdgeType:            edgeType,
		EdgeStrength:        edgeStrength,
		EdgeConfidence:      edgeConfidence,
		MomentumScore:       momentumScore,
		ThresholdApplied:    m.cfg.MinLiquidityScore,
		OpportunityWindowMs: opportunityWindowMs,

		ExpiresAt:  expiresAt,
		DetectedAt: now,
	}, nil
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
