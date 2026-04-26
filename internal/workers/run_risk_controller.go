package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/resource_control"
)

// systemTraceID is the fixed trace ID for system-originated events (SHA256("system")[:16]).
const systemTraceID = "9f86d081884c7d65" // SHA256("system")[:8] in hex

// RunRiskController runs the periodic risk controller that monitors drawdown and transitions
// the system mode between BALANCED, DEGRADED, and HALTED.
// It runs every cfg.Risk.CheckIntervalSeconds until ctx is cancelled.
func RunRiskController(ctx context.Context, adapter database.Adapter, cfg *config.Config, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}

	interval := time.Duration(cfg.Risk.CheckIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 30 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if err := runRiskCheck(ctx, adapter, cfg, logger); err != nil {
				logger.Error("risk_controller_check_failed", "error", err)
				// Non-fatal: log and continue.
			}
		}
	}
}

// runRiskCheck performs a single drawdown check and updates system state if mode changed.
func runRiskCheck(ctx context.Context, adapter database.Adapter, cfg *config.Config, logger *slog.Logger) error {
	state, err := adapter.GetSystemState(ctx)
	if err != nil {
		return fmt.Errorf("risk_controller: get system state: %w", err)
	}

	dd, err := computeDrawdown(ctx, adapter, cfg.Risk.DrawdownWindowHours)
	if err != nil {
		return fmt.Errorf("risk_controller: compute drawdown: %w", err)
	}

	result := resource_control.EvaluateMode(
		state.Mode,
		dd,
		cfg.Risk.DegradedDrawdownPct,
		cfg.Risk.HaltDrawdownPct,
		cfg.Risk.ResumeDrawdownPct,
	)

	if result.Mode == state.Mode {
		return nil // no transition needed
	}

	sv, svErr := adapter.GetActiveStrategyVersion(ctx)
	versionID := ""
	if svErr == nil && sv != nil {
		versionID = sv.StrategyVersionID
	}

	newState := contracts.SystemStateDTO{
		Mode:                 result.Mode,
		DrawdownPct:          dd,
		DrawdownWindowHours:  int32(cfg.Risk.DrawdownWindowHours),
		OpenPositions:        state.OpenPositions,
		TotalExposureUsd:     state.TotalExposureUsd,
		ActiveStrategyID:     state.ActiveStrategyID,
		ShadowStrategyID:     state.ShadowStrategyID,
		LastTransitionReason: result.Reason,
		UpdatedAt:            time.Now().UTC().Format(time.RFC3339Nano),
		VersionID:            versionID,
	}

	if _, err := adapter.UpsertSystemState(ctx, newState, 0); err != nil {
		return fmt.Errorf("risk_controller: upsert system state: %w", err)
	}

	// Emit system_event for the mode transition.
	payload, marshalErr := json.Marshal(map[string]interface{}{
		"prev_mode":    state.Mode,
		"new_mode":     result.Mode,
		"drawdown_pct": dd,
		"trigger":      "risk_controller",
		"reason":       result.Reason,
	})
	if marshalErr != nil {
		return fmt.Errorf("risk_controller: marshal event payload: %w", marshalErr)
	}

	eventID := contracts.ContentIDFromString(fmt.Sprintf("sys-mode:%s:%s:%f", state.Mode, result.Mode, dd))
	evt := database.Event{
		EventID:       eventID,
		EventType:     "system_event",
		Payload:       payload,
		TraceID:       systemTraceID,
		CorrelationID: systemTraceID,
		VersionID:     versionID,
	}

	if err := adapter.InsertEvent(ctx, evt); err != nil {
		logger.Warn("risk_controller_event_insert_failed", "error", err)
	}

	logger.Info("risk_controller_mode_transition",
		"prev_mode", state.Mode,
		"new_mode", result.Mode,
		"drawdown_pct", dd,
		"reason", result.Reason,
	)
	return nil
}

// computeDrawdown returns the drawdown fraction over the last windowHours.
// drawdown = ABS(sum of losses) / max(sum of peak exposure, 1)
// Returns a value in [0.0, ∞) — callers compare against thresholds.
func computeDrawdown(ctx context.Context, adapter database.Adapter, windowHours int) (float64, error) {
	if windowHours <= 0 {
		windowHours = 24
	}
	return adapter.ComputeDrawdown(ctx, windowHours)
}
