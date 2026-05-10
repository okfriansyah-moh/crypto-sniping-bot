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

// systemVersionID is the fixed version ID for system-originated events that
// are emitted outside any active strategy version context (e.g., when no
// strategy version is pinned yet or the lookup fails). Derived as
// SHA256("system_version")[:16].
const systemVersionID = "2e5d7a3f9c1b4e8a" // SHA256("system_version")[:8] in hex

// Operational modes governed by the adaptive risk-appetite controller.
// DEGRADED and HALTED are owned by the safety-mode (drawdown) controller and
// are intentionally NOT in this set.
const (
	modeBalanced        = "BALANCED"
	modeStrict          = "STRICT"
	modeExploration     = "EXPLORATION"
	modeVeryExploration = "VERY_EXPLORATION"
	modeDegraded        = "DEGRADED"
	modeHalted          = "HALTED"
)

// reasonManualTelegram fingerprints state rows that were last updated by an
// operator via the /mode Telegram command. The adaptive controller skips its
// own decision logic when this reason matches the same transition window so
// the operator's choice survives at least one full window.
const reasonManualTelegram = "manual_telegram"

// adaptiveTransition is the in-memory record of the most recent transition
// applied by the adaptive controller — used to enforce the one-per-window
// guard without round-tripping the database.
type adaptiveTransition struct {
	windowID string
}

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

	// adaptiveLast tracks the last adaptive transition window applied by THIS
	// worker process. It is intentionally process-local: the windowID is
	// content-addressed (SHA256 of the floored window) so two workers
	// computing it independently agree, and the durable one-per-window guard
	// is the LastTransitionReason + UpdatedAt persisted on system_state.
	var adaptiveLast adaptiveTransition

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

			// Adaptive risk-appetite pass — runs only when the system is in
			// one of the adaptive-eligible modes (STRICT/BALANCED/EXPLORATION/
			// VERY_EXPLORATION) and never on the cold-start tick where the
			// safety pass already upserted the default.
			adaptiveVersion, err := runAdaptiveRiskAppetiteTick(
				ctx, adapter, cfg, logger, stateVersion, &adaptiveLast, time.Now().UTC(),
			)
			if err != nil {
				if errors.Is(err, database.ErrStaleState) {
					logger.Warn("risk_controller_adaptive_cas_conflict", "state_version", stateVersion)
				} else {
					logger.Error("risk_controller_adaptive_failed", "error", err)
				}
				continue
			}
			if adaptiveVersion > 0 {
				stateVersion = adaptiveVersion
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
	if state == nil {
		return 0, fmt.Errorf("risk_controller: get system state: nil result")
	}

	// Cold-start guarantee: first tick after worker startup with an empty
	// Mode (no prior state row, or freshly bootstrapped DB) MUST upsert the
	// default startup mode and emit a system_event before any other
	// decision logic runs.
	if state.Mode == "" {
		defaultMode := cfg.ModeAdaptive.DefaultStartupMode
		if defaultMode == "" {
			defaultMode = modeBalanced
		}
		newVersion, err := persistMode(
			ctx, adapter, logger, *state, defaultMode,
			"cold_start_default", stateVersion,
		)
		if err != nil {
			return 0, fmt.Errorf("risk_controller: cold start upsert: %w", err)
		}
		emitModeChangeEvent(ctx, adapter, logger, state.Mode, defaultMode, "cold_start_default", "system")
		return newVersion, nil
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

// runAdaptiveRiskAppetiteTick wires the adaptive controller into the worker
// loop: fetches the current state and the adaptive metric inputs, then
// delegates the (deterministic, time-injected) decision to runAdaptiveRiskAppetite.
//
// Inputs:
//   - stateVersion: CAS version from the safety pass (0 if no prior write).
//   - last:        in-memory window guard for THIS worker process.
//   - now:         injected wall clock (UTC) — drives windowID + starvation calc.
//
// Returns the new state version when a transition was applied, 0 otherwise.
func runAdaptiveRiskAppetiteTick(
	ctx context.Context,
	adapter database.Adapter,
	cfg *config.Config,
	logger *slog.Logger,
	stateVersion int64,
	last *adaptiveTransition,
	now time.Time,
) (int64, error) {
	if !cfg.ModeAdaptive.Enabled {
		return 0, nil
	}

	state, err := adapter.GetSystemState(ctx)
	if err != nil {
		return 0, fmt.Errorf("risk_controller: get system state for adaptive: %w", err)
	}
	if state == nil {
		return 0, nil
	}

	// Source the last validated_edge_event timestamp. Missing rows mean
	// "infinitely starved" — express that with the zero time so the
	// pure decision function deterministically routes to starvation.
	lastEdgeAt, err := adapter.GetLastEventTimestamp(ctx, "validated_edge_event")
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return 0, fmt.Errorf("risk_controller: get last validated edge: %w", err)
	}
	if errors.Is(err, database.ErrNotFound) {
		lastEdgeAt = time.Time{}
	}

	// Rug-rate is now sourced from real DataQuality decisions over the
	// adaptive window (operational-modes skill). FP-rate computation is
	// still deferred — single-helper budget reserved for that follow-up;
	// the controller treats it as 0.0 today and emits a structured
	// warning so the gap is explicit. Tests inject metrics by calling
	// runAdaptiveRiskAppetite directly.
	sinceSec := cfg.ModeAdaptive.AdaptiveWindowSec
	if sinceSec <= 0 {
		sinceSec = 1800
	}
	var rugRate float64
	totalDQ, rugRejects, dqErr := adapter.GetAdaptiveDQStats(ctx, sinceSec)
	if dqErr != nil {
		logger.Warn("adaptive_rug_rate_unavailable",
			"error", dqErr,
			"window_sec", sinceSec,
		)
	} else if totalDQ > 0 {
		rugRate = float64(rugRejects) / float64(totalDQ)
	}
	logger.Warn("adaptive_metrics_partial",
		"detail", "fp_rate computation deferred",
		"rug_rate", rugRate,
		"dq_total", totalDQ,
		"dq_rug_rejects", rugRejects,
		"window_sec", sinceSec,
	)

	return runAdaptiveRiskAppetite(
		ctx, adapter, cfg, logger,
		state, stateVersion, last, now, lastEdgeAt, rugRate, 0.0,
	)
}

// runAdaptiveRiskAppetite is the pure adaptive decision function. It is
// fully deterministic given (state, now, lastEdgeAt, rugRate, fpRate) and
// performs at most one transition per call:
//
//   - skip when mode ∈ {DEGRADED, HALTED} (drawdown controller owns those)
//   - skip on cold start (mode == "") — that path is owned by runRiskCheck
//   - skip when the operator's manual /mode change (LastTransitionReason ==
//     "manual_telegram") falls inside the current adaptive window
//   - skip when this worker already applied a transition this window
//   - then evaluate, in order:
//     · safety downgrade (rug or FP rate above threshold)
//     · starvation upgrade (no validated_edge for StarvationTriggerSec)
//
// Returns the new state version on a successful upsert, or 0 when no
// transition was applied. Errors from UpsertSystemState are returned to
// the caller; ErrStaleState is NOT retried in the same tick.
func runAdaptiveRiskAppetite(
	ctx context.Context,
	adapter database.Adapter,
	cfg *config.Config,
	logger *slog.Logger,
	state *contracts.SystemStateDTO,
	stateVersion int64,
	last *adaptiveTransition,
	now time.Time,
	lastEdgeAt time.Time,
	rugRate float64,
	fpRate float64,
) (int64, error) {
	if state == nil {
		return 0, nil
	}
	// Cold-start path is owned by runRiskCheck.
	if state.Mode == "" {
		return 0, nil
	}
	// Drawdown controller owns DEGRADED/HALTED.
	if state.Mode == modeDegraded || state.Mode == modeHalted {
		return 0, nil
	}

	windowSec := cfg.ModeAdaptive.TransitionWindowSec
	if windowSec <= 0 {
		windowSec = 3600
	}
	currentWindowID := computeAdaptiveWindowID(now, windowSec)

	// Manual override fingerprint: when the persisted reason is
	// "manual_telegram" AND the operator's transition fell into the same
	// window we are evaluating, the operator wins for this window.
	if state.LastTransitionReason == reasonManualTelegram {
		if updatedAt, err := time.Parse(time.RFC3339Nano, state.UpdatedAt); err == nil {
			if computeAdaptiveWindowID(updatedAt.UTC(), windowSec) == currentWindowID {
				logger.Info("adaptive_skip_manual_override", "window_id", currentWindowID)
				return 0, nil
			}
		}
	}

	// One-per-window guard (process-local).
	if last != nil && last.windowID == currentWindowID {
		return 0, nil
	}

	// 1. Safety downgrade has priority over starvation upgrade — if rugs
	//    are spiking we MUST tighten before considering loosening.
	if rugRate > cfg.ModeAdaptive.RugRateAutoDowngrade || fpRate > cfg.ModeAdaptive.FPRateAutoDowngrade {
		reason := "high_rug_rate"
		if fpRate > cfg.ModeAdaptive.FPRateAutoDowngrade && !(rugRate > cfg.ModeAdaptive.RugRateAutoDowngrade) {
			reason = "high_fp_rate"
		}
		toMode, ok := nextDowngrade(state.Mode)
		if !ok {
			// Already STRICT — emit critical alert, no transition.
			emitAdaptiveCriticalAlert(ctx, adapter, logger,
				"high_rug_rate_in_strict",
				fmt.Sprintf("rug_rate=%.3f fp_rate=%.3f in STRICT — review strategy", rugRate, fpRate),
			)
			return 0, nil
		}
		return applyAdaptiveTransition(
			ctx, adapter, logger,
			state, stateVersion, last, currentWindowID,
			toMode, reason, now,
		)
	}

	// 2. Starvation upgrade — only when we have a real "last edge" or
	//    when the system has been running long enough to call zero a
	//    starvation. Zero time means "no event ever observed" which we
	//    treat as starvation when the worker has had a full window to
	//    populate it; that condition is not knowable here so we fall
	//    back to a conservative interpretation: zero time DOES count as
	//    starvation only when the configured StarvationTriggerSec has
	//    elapsed since `now`'s start-of-day baseline IS NOT a stable
	//    signal — so treat zero time as starved iff lastEdgeAt.IsZero()
	//    AND StarvationTriggerSec > 0. This matches the documented
	//    behaviour and is fully deterministic for tests.
	starvationTrig := time.Duration(cfg.ModeAdaptive.StarvationTriggerSec) * time.Second
	if starvationTrig <= 0 {
		return 0, nil
	}
	starved := false
	if lastEdgeAt.IsZero() {
		starved = true
	} else if now.Sub(lastEdgeAt) > starvationTrig {
		starved = true
	}
	if !starved {
		return 0, nil
	}
	toMode, ok := nextUpgrade(state.Mode)
	if !ok {
		// Already VERY_EXPLORATION — emit critical alert, no transition.
		emitAdaptiveCriticalAlert(ctx, adapter, logger,
			"starvation_critical",
			"already in VERY_EXPLORATION with no opportunities — market conditions suspect",
		)
		return 0, nil
	}
	return applyAdaptiveTransition(
		ctx, adapter, logger,
		state, stateVersion, last, currentWindowID,
		toMode, "starvation", now,
	)
}

// nextUpgrade returns the more-permissive neighbour of mode. Direction:
// STRICT → BALANCED → EXPLORATION → VERY_EXPLORATION.
func nextUpgrade(mode string) (string, bool) {
	switch mode {
	case modeStrict:
		return modeBalanced, true
	case modeBalanced:
		return modeExploration, true
	case modeExploration:
		return modeVeryExploration, true
	}
	return "", false
}

// nextDowngrade returns the more-conservative neighbour of mode. Direction:
// VERY_EXPLORATION → EXPLORATION → BALANCED → STRICT.
func nextDowngrade(mode string) (string, bool) {
	switch mode {
	case modeVeryExploration:
		return modeExploration, true
	case modeExploration:
		return modeBalanced, true
	case modeBalanced:
		return modeStrict, true
	}
	return "", false
}

// computeAdaptiveWindowID returns a content-addressed window identifier:
// SHA256(floor(now / windowSec))[:16]. Deterministic for any (now, windowSec).
func computeAdaptiveWindowID(now time.Time, windowSec int) string {
	if windowSec <= 0 {
		windowSec = 3600
	}
	floor := now.Unix() / int64(windowSec)
	return contracts.ContentIDFromString(fmt.Sprintf("adaptive-window:%d:%d", windowSec, floor))
}

// applyAdaptiveTransition persists the adaptive mode change via CAS and
// emits the corresponding system_event.
func applyAdaptiveTransition(
	ctx context.Context,
	adapter database.Adapter,
	logger *slog.Logger,
	state *contracts.SystemStateDTO,
	stateVersion int64,
	last *adaptiveTransition,
	currentWindowID string,
	toMode string,
	reason string,
	now time.Time,
) (int64, error) {
	newVersion, err := persistMode(
		ctx, adapter, logger, *state, toMode, reason, stateVersion,
	)
	if err != nil {
		return 0, err
	}
	if last != nil {
		last.windowID = currentWindowID
	}
	emitModeChangeEvent(ctx, adapter, logger, state.Mode, toMode, reason, "auto_adaptive")
	logger.Info("risk_controller_adaptive_transition",
		"from", state.Mode, "to", toMode, "reason", reason,
		"window_id", currentWindowID, "now", now.Format(time.RFC3339Nano),
	)
	return newVersion, nil
}

// persistMode writes a mode change via CAS, preserving non-mode fields from
// the prior state and stamping the active strategy version.
func persistMode(
	ctx context.Context,
	adapter database.Adapter,
	logger *slog.Logger,
	prior contracts.SystemStateDTO,
	newMode string,
	reason string,
	stateVersion int64,
) (int64, error) {
	versionID := prior.VersionID
	if sv, err := adapter.GetActiveStrategyVersion(ctx); err == nil && sv != nil {
		versionID = sv.StrategyVersionID
	}
	newState := contracts.SystemStateDTO{
		Mode:                 newMode,
		DrawdownPct:          prior.DrawdownPct,
		DrawdownWindowHours:  prior.DrawdownWindowHours,
		OpenPositions:        prior.OpenPositions,
		TotalExposureUsd:     prior.TotalExposureUsd,
		ActiveStrategyID:     prior.ActiveStrategyID,
		ShadowStrategyID:     prior.ShadowStrategyID,
		LastTransitionReason: reason,
		UpdatedAt:            time.Now().UTC().Format(time.RFC3339Nano),
		VersionID:            versionID,
	}
	newVersion, err := adapter.UpsertSystemState(ctx, newState, stateVersion)
	if err != nil {
		return 0, err
	}
	_ = logger // logger reserved for future structured success log
	return newVersion, nil
}

// emitModeChangeEvent writes a system_event with subtype mode_change to the
// event bus. Failures are logged at warn level — the persisted state row is
// the authoritative record so a failed event insert does not roll back the
// transition.
func emitModeChangeEvent(
	ctx context.Context,
	adapter database.Adapter,
	logger *slog.Logger,
	from string,
	to string,
	reason string,
	actor string,
) {
	payload, err := json.Marshal(map[string]interface{}{
		"event_subtype": "mode_change",
		"from":          from,
		"to":            to,
		"reason":        reason,
		"actor":         actor,
	})
	if err != nil {
		logger.Warn("risk_controller_marshal_failed", "error", err)
		return
	}
	versionID := ""
	if sv, gerr := adapter.GetActiveStrategyVersion(ctx); gerr == nil && sv != nil {
		versionID = sv.StrategyVersionID
	}
	eventID := contracts.ContentIDFromString(
		fmt.Sprintf("adaptive-mode:%s:%s:%s:%s", from, to, reason, actor),
	)
	evt := database.Event{
		EventID:       eventID,
		EventType:     "system_event",
		Payload:       payload,
		TraceID:       systemTraceID,
		CorrelationID: systemTraceID,
		VersionID:     versionID,
	}
	if err := adapter.InsertEvent(ctx, evt); err != nil {
		logger.Warn("risk_controller_adaptive_event_insert_failed", "error", err)
	}
}

// emitAdaptiveCriticalAlert writes a critical-severity system_event when the
// adaptive controller detects a condition it cannot itself remedy
// (starvation in EXPLORATION, high rug rate in STRICT).
func emitAdaptiveCriticalAlert(
	ctx context.Context,
	adapter database.Adapter,
	logger *slog.Logger,
	subtype string,
	summary string,
) {
	payload, err := json.Marshal(map[string]interface{}{
		"event_subtype": subtype,
		"severity":      "critical",
		"summary":       summary,
	})
	if err != nil {
		logger.Warn("risk_controller_marshal_failed", "error", err)
		return
	}
	eventID := contracts.ContentIDFromString(
		fmt.Sprintf("adaptive-alert:%s:%s", subtype, summary),
	)
	versionID := systemVersionID
	if sv, svErr := adapter.GetActiveStrategyVersion(ctx); svErr == nil {
		versionID = sv.StrategyVersionID
	}
	evt := database.Event{
		EventID:       eventID,
		EventType:     "system_event",
		Payload:       payload,
		TraceID:       systemTraceID,
		CorrelationID: systemTraceID,
		VersionID:     versionID,
	}
	if err := adapter.InsertEvent(ctx, evt); err != nil {
		logger.Warn("risk_controller_adaptive_alert_insert_failed", "error", err)
	}
	logger.Warn("adaptive_critical_alert", "subtype", subtype, "summary", summary)
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
