// Package selection implements Layer 6: Selection Engine.
// Consumes ValidatedEdgeDTO and emits SelectionOutputDTO.
// Pure function: no DB, no side effects.
package selection

import (
	"context"
	"fmt"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// Module is the selection engine.
type Module struct {
	cfg *config.SelectionConfig
}

// New returns a new selection Module.
func New(cfg *config.SelectionConfig) *Module {
	if cfg == nil {
		cfg = &config.SelectionConfig{MaxOpenPositions: 1}
	}
	return &Module{cfg: cfg}
}

// Process evaluates a ValidatedEdgeDTO and emits SelectionOutputDTO.
// Phase 2: single-position concurrency gate — max 1 concurrent position.
func (m *Module) Process(
	_ context.Context,
	in contracts.ValidatedEdgeDTO,
	openCount int,
) (contracts.SelectionOutputDTO, error) {
	// SelectedAt is derived from the upstream ValidatedAt for deterministic replay.
	// Same ValidatedEdgeDTO always produces identical SelectionOutputDTO content.
	selectedAt := in.ValidatedAt

	selected := false
	rejectReason := ""

	if in.Decision != "ACCEPT" {
		rejectReason = "edge_not_validated:" + in.RejectReason
	} else if openCount >= m.cfg.MaxOpenPositions {
		rejectReason = fmt.Sprintf("max_open_positions_reached:%d", openCount)
	} else {
		selected = true
	}

	// CombinedScore: probability × EV signal; used for ranking in Phase 3+.
	combinedScore := 0.0
	if selected {
		combinedScore = in.ProbabilityUsed * float64(in.ExpectedValueBps) / 1000.0
	}

	eventID := contracts.ContentIDFromString(fmt.Sprintf("sel:%s:%v", in.EventID, selected))

	return contracts.SelectionOutputDTO{
		EventID:       eventID,
		TraceID:       in.TraceID,
		CorrelationID: in.CorrelationID,
		CausationID:   in.EventID,
		VersionID:     in.VersionID,

		TokenLifecycleID: in.TokenLifecycleID,
		TokenAddress:     in.TokenAddress,

		Selected:        selected,
		Rank:            1,
		CombinedScore:   combinedScore,
		DiversityBucket: "default",
		IsExploration:   false,
		RejectReason:    rejectReason,
		SelectedAt:      selectedAt,
	}, nil
}
