package workers

import (
	"context"
	"encoding/json"
	"errors"
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

	// stateVersion tracks the CAS version returned by UpsertSystemState.
	// Starting at 0 means "no prior version" (create or unconditional first write).
	// After each successful upsert, the returned version is stored here so subsequent
	// writes use proper optimistic locking and cannot silently overwrite concurrent changes.
	var stateVersion int64

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			newVersion, err := runRiskCheck(ctx, adapter, cfg, logger, stateVersion)
			if err != nil {
				if errors.Is(err, database.ErrStaleState) {
					// CAS conflict: another writer updated the state concurrently.
					// Do NOT reset stateVersion to 0 — once the persisted version is
					// non-zero, zeroing the local cache causes repeated ErrStaleState
					// failures on every tick until a successful read resynchronises.
					// Keep the last known version and surface the need for resync.
					logger.Warn("risk_controller_cas_conflict_resync_required", "state_version", stateVersion)
				} else {
					logger.Error("risk_controller_check_failed", "error", err)
				}
				// Non-fatal: log and continue.
				continue
			}
			if newVersion > 0 {
				stateVersion = newVersion
			}
		}
	}
}

// runRiskCheck performs a single drawdown check and updates system state if mode changed.
// stateVersion is the CAS version from the last successful UpsertSystemState call (0 = initial).
// Returns the new state version after a successful upsert, or 0 if no update was needed.
func runRiskCheck(ctx context.Context, adapter database.Adapter, cfg *config.Config, logger *slog.Logger, stateVersion int64) (int64, error) {
	state, err := adapter.GetSystemState(ctx)
	if err != nil {
		return 0, fmt.Errorf("risk_controller: get system state: %w", err)
	}

	dd, err := computeDrawdown(ctx, adapter, cfg.Risk.DrawdownWindowHours)
	if err != nil {
		return 0, fmt.Errorf("risk_controller: compute drawdown: %w", err)
	}

	result := resource_control.EvaluateMode(
		state.Mode,
		dd,
		cfg.Risk.DegradedDrawdownPct,
		cfg.Risk.HaltDrawdownPct,
		cfg.Risk.ResumeDrawdownPct,
	)

	if result.Mode == state.Mode {
		return 0, nil // no transition needed
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

	newVersion, err := adapter.UpsertSystemState(ctx, newState, stateVersion)
	if err != nil {
		return 0, fmt.Errorf("risk_controller: upsert system state: %w", err)
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
		return 0, fmt.Errorf("risk_controller: marshal event payload: %w", marshalErr)
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
	return newVersion, nil
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
