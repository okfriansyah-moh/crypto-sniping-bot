package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/position"
)

// RunPositionPoll runs a timer-based position polling loop.
// Unlike other workers, this is periodic, not event-driven.
//
// Each poll cycle:
//  1. Fetch all open positions from the database.
//  2. For each position: get current price via priceClient.
//  3. Call position.Module.PollExit to check TP/SL/TIME rules.
//  4. Persist updated snapshot.
//  5. On exit: transition lifecycle to POSITION_CLOSED and emit position_state_event.
//
// priceClient may be nil — positions will not be polled (safe fallback for testing).
func RunPositionPoll(
	ctx context.Context,
	adapter database.Adapter,
	cfg *config.Config,
	priceClient position.PriceClient,
	logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}
	mod := position.New(&cfg.Position)

	pollInterval := time.Duration(cfg.Position.PollIntervalSeconds) * time.Second
	if pollInterval <= 0 {
		pollInterval = 5 * time.Second
	}

	logger.Info("position_poll_started", "interval_seconds", cfg.Position.PollIntervalSeconds)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}

		if err := pollOnce(ctx, adapter, mod, priceClient, logger); err != nil {
			logger.Error("position_poll_cycle_failed", "error", err)
		}
	}
}

func pollOnce(
	ctx context.Context,
	adapter database.Adapter,
	mod *position.Module,
	priceClient position.PriceClient,
	logger *slog.Logger,
) error {
	openPositions, err := adapter.GetOpenPositions(ctx)
	if err != nil {
		return fmt.Errorf("poll_once: get_open_positions: %w", err)
	}
	if len(openPositions) == 0 {
		return nil
	}

	// evaluatedAt is captured once for the entire poll cycle so all positions
	// evaluated in the same cycle share a consistent timestamp.  This keeps
	// PollExit deterministic: same price + same evaluatedAt → identical output.
	evaluatedAt := time.Now()

	for _, pos := range openPositions {
		posLog := logger.With("position_id", pos.PositionID, "token", pos.TokenAddress)

		currentPrice := ""
		if priceClient != nil {
			// Use the chain stored on the position for per-market isolation (arch §2.4).
			price, priceErr := priceClient.GetTokenPrice(ctx, pos.TokenAddress, pos.Chain)
			if priceErr != nil {
				posLog.Warn("position_poll_price_failed", "error", priceErr)
				continue
			}
			currentPrice = price
		}
		if currentPrice == "" {
			continue
		}

		updated, exitErr := mod.PollExit(ctx, pos, currentPrice, evaluatedAt)
		if exitErr != nil {
			posLog.Warn("position_poll_exit_eval_failed", "error", exitErr)
			continue
		}

		if err := adapter.InsertPositionState(ctx, updated); err != nil {
			posLog.Warn("position_poll_persist_failed", "error", err)
			continue
		}

		if updated.Status == "exited" {
			posLog.Info("position_exited",
				"reason", updated.ExitReason,
				"pnl_pct", updated.PnlPct,
				"pnl_usd", updated.PnlUsd,
				"chain", updated.Chain,
				"trace_id", updated.TraceID,
			)

			if err := doMandatoryTransition(ctx, adapter, updated.TokenLifecycleID, "POSITION_OPEN", "POSITION_CLOSED", updated.ExitReason, "position_poll"); err != nil {
				posLog.Warn("position_poll_transition_failed", "error", err)
				continue
			}

			payload, marshalErr := json.Marshal(updated)
			if marshalErr != nil {
				posLog.Warn("position_poll_marshal_failed", "error", marshalErr)
				continue
			}
			cid := pos.EventID
			exitEvt := database.Event{
				EventID:       updated.EventID,
				EventType:     "position_state_event",
				Payload:       payload,
				TraceID:       updated.TraceID,
				CorrelationID: updated.CorrelationID,
				CausationID:   &cid,
				VersionID:     updated.VersionID,
			}
			if err := adapter.InsertEvent(ctx, exitEvt); err != nil {
				posLog.Warn("position_poll_emit_failed", "error", err)
			}
		}
	}
	return nil
}
