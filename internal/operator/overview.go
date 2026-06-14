package operator

import (
	"context"
	"fmt"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
)

const winRateLookbackHours = 168 // 7d — overview KPI per mockup

// BuildOverview assembles overview KPIs for GET /api/v1/overview.
// Logic mirrors cmd/telegram.go buildStatusFn + buildPnlFn fields used on the mockup overview grid.
func BuildOverview(
	ctx context.Context,
	db database.Adapter,
	cfg *config.Config,
	startTime time.Time,
) (*contracts.OverviewResponseDTO, error) {
	state, err := db.GetSystemState(ctx)
	if err != nil {
		return nil, fmt.Errorf("get system state: %w", err)
	}
	if state == nil {
		return nil, fmt.Errorf("system state not initialized")
	}

	strategyID := state.ActiveStrategyID
	if sv, svErr := db.GetActiveStrategyVersion(ctx); svErr == nil && sv != nil && sv.StrategyVersionID != "" {
		strategyID = sv.StrategyVersionID
	}

	executionMode := ""
	maxExposure := 0.0
	if cfg != nil {
		executionMode = cfg.Execution.Mode
		maxExposure = cfg.Capital.MaxTotalExposureUsd
	}

	pnlToday, err := BuildPnLSummary(ctx, db, defaultPnLLookbackHours)
	if err != nil {
		return nil, err
	}

	closed7d, err := db.GetClosedPositions(ctx, winRateLookbackHours*3600)
	if err != nil {
		return nil, fmt.Errorf("get closed positions 7d: %w", err)
	}
	_, wins7d, losses7d := summarizeClosed(closed7d)
	closedTrades7d := wins7d + losses7d

	shadowGate, err := EvaluateShadowGate(ctx, NewShadowGateEvaluator(db, cfg))
	if err != nil {
		shadowGate = nil // fail-open — matches Telegram /status when evaluator errors
	}

	out := &contracts.OverviewResponseDTO{
		Mode:              state.Mode,
		ExecutionMode:     executionMode,
		DrawdownPct:       state.DrawdownPct,
		OpenPositions:     state.OpenPositions,
		TotalExposureUsd:  state.TotalExposureUsd,
		MaxExposureUsd:    maxExposure,
		PnLTodayUsd:       pnlToday.RealizedPnLUsd,
		PnLTodayWins:      pnlToday.Wins,
		PnLTodayLosses:    pnlToday.Losses,
		WinRate7d:         winRatePct(wins7d, losses7d),
		ClosedTrades7d:    closedTrades7d,
		ShadowGate:        shadowGate,
		ChainStatuses:     []contracts.ChainStatusDTO{},
		StrategyVersionID: strategyID,
		UpdatedAt:         state.UpdatedAt,
	}

	if halted, haltReason, hErr := db.IsSystemHalted(ctx); hErr == nil && halted {
		out.AlertBanner = &contracts.AlertBannerDTO{
			Severity: "bad",
			Message:  fmt.Sprintf("Kill switch active: %s", haltReason),
			Code:     "KILL_SWITCH",
		}
	}

	_ = startTime // reserved for uptime display in Task 6 parity / future overview field

	return out, nil
}
