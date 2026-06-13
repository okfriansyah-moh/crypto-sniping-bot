// Package selection implements Layer 6: Selection Engine.
// Consumes ValidatedEdgeDTO and emits SelectionOutputDTO.
// Pure function: no DB, no side effects.
package selection

import (
	"context"

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

// Process evaluates a single ValidatedEdgeDTO (legacy/tests). Prefer ProcessBatch
// from the selection worker for Top-K ranking across concurrent candidates.
func (m *Module) Process(
	ctx context.Context,
	in contracts.ValidatedEdgeDTO,
	openCount int,
) (contracts.SelectionOutputDTO, error) {
	thresholds := config.ModeThresholds{
		MaxPositions:     m.effectiveMaxPositions(config.ModeThresholds{}),
		ExploreBudgetPct: 0,
	}
	outs, err := m.ProcessBatch(ctx, []BatchItem{{Edge: in}}, openCount, thresholds, nil)
	if err != nil || len(outs) == 0 {
		return contracts.SelectionOutputDTO{}, err
	}
	return outs[0], nil
}

// ProcessBatch ranks ACCEPT edges via greedy Top-K (docs/plans/2026-06-10-profit-restoration-plan.md §7.3 Task 4).
func (m *Module) ProcessBatch(
	_ context.Context,
	items []BatchItem,
	openCount int,
	thresholds config.ModeThresholds,
	openByCreator map[string]int32,
) ([]contracts.SelectionOutputDTO, error) {
	if len(items) == 0 {
		return nil, nil
	}
	maxPositions := m.effectiveMaxPositions(thresholds)
	if thresholds.MaxPositions <= 0 {
		thresholds.MaxPositions = maxPositions
	}
	topK := m.effectiveTopK(thresholds)
	return PickTopK(items, openCount, thresholds, topK, int32(m.cfg.MaxPositionsPerCreator), openByCreator), nil
}

func (m *Module) effectiveTopK(thresholds config.ModeThresholds) int {
	if m.cfg.TopK > 0 {
		return m.cfg.TopK
	}
	if thresholds.MaxPositions > 0 {
		return thresholds.MaxPositions
	}
	if m.cfg.MaxOpenPositions > 0 {
		return m.cfg.MaxOpenPositions
	}
	return 1
}

func (m *Module) effectiveMaxPositions(thresholds config.ModeThresholds) int {
	if thresholds.MaxPositions > 0 {
		return thresholds.MaxPositions
	}
	if m.cfg.MaxOpenPositions > 0 {
		return m.cfg.MaxOpenPositions
	}
	return 1
}
