package operator

import (
	"context"
	"fmt"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/health"
)

// NewShadowGateEvaluator returns the shared shadow-gate evaluator used by
// overview and health surfaces. Nil cfg or db yields nil (fail-open skip).
func NewShadowGateEvaluator(db database.Adapter, cfg *config.Config) *health.ShadowGateEvaluator {
	if cfg == nil || db == nil {
		return nil
	}
	return health.NewShadowGateEvaluator(db, cfg.Execution.Mode, cfg.Execution.ShadowGate)
}

// EvaluateShadowGate runs the evaluator when non-nil; nil evaluator returns nil DTO.
func EvaluateShadowGate(ctx context.Context, eval *health.ShadowGateEvaluator) (*contracts.ShadowGateBlockDTO, error) {
	if eval == nil {
		return nil, nil
	}
	result, err := eval.Evaluate(ctx)
	if err != nil {
		return nil, fmt.Errorf("shadow gate evaluate: %w", err)
	}
	return MapShadowGateResult(result), nil
}

// MapShadowGateResult converts health.ShadowGateResult to contracts.ShadowGateBlockDTO.
func MapShadowGateResult(r health.ShadowGateResult) *contracts.ShadowGateBlockDTO {
	dto := &contracts.ShadowGateBlockDTO{
		Pass:               r.Pass,
		Blocked:            !r.Pass,
		TradeCount:         r.TradeCount,
		AggregatePnlBps:    r.AggregatePnlBps,
		AvgPnlBps:          r.AvgPnlBps,
		MinTrades:          r.MinTrades,
		MinWindowDays:      r.MinWindowDays,
		MinAggregatePnlBps: r.MinAggregatePnlBps,
		ExecutionMode:      r.ExecutionMode,
		LiveFlipHint:       r.LiveFlipHint,
	}
	if !r.Pass {
		switch {
		case r.TradeCount < r.MinTrades:
			dto.Reason = fmt.Sprintf("need %d shadow trades, have %d", r.MinTrades, r.TradeCount)
		default:
			dto.Reason = "aggregate shadow PnL below threshold"
		}
	}
	return dto
}
