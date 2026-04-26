package workers

import (
	"context"
	"log/slog"

	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/learning"
)

// RunShadowObserver is a periodic worker that closes observation windows
// for shadow trades and updates their FN/TN classification.
func RunShadowObserver(
	ctx context.Context,
	adapter database.Adapter,
	cfg *config.Config,
	priceClient learning.PriceClient,
	logger *slog.Logger,
) error {
	if logger == nil {
		logger = slog.Default()
	}

	chain := firstChain(cfg)
	observer := learning.NewShadowObserver(priceClient, chain)
	lcfg := &cfg.Learning

	windowSeconds := lcfg.ObservationWindowSeconds
	if windowSeconds <= 0 {
		windowSeconds = 3600
	}
	fnThreshold := lcfg.FnGainThresholdPct
	if fnThreshold <= 0 {
		fnThreshold = 0.10
	}

	pendingShadows, err := adapter.GetShadowTradesByWindow(ctx, windowSeconds)
	if err != nil {
		return err
	}

	for _, st := range pendingShadows {
		stLog := logger.With("shadow_id", st.ShadowID, "token", st.TokenAddress)

		// Price at rejection time is not stored — use empty string to signal unknown baseline.
		// Observer will return complete=true with 0 return (safe TN fallback).
		returnPct, complete, obsErr := observer.Observe(ctx, st.TokenAddress, "")
		if obsErr != nil {
			stLog.Warn("shadow_observer_price_failed", "error", obsErr)
			continue
		}
		if !complete {
			stLog.Debug("shadow_observer_window_open")
			continue
		}

		classification := learning.ClassifyShadow(returnPct, fnThreshold)
		if err := adapter.UpdateShadowTradeObservation(ctx, st.ShadowID, returnPct, classification); err != nil {
			stLog.Warn("shadow_observer_update_failed", "error", err)
			continue
		}

		stLog.Info("shadow_observation_complete",
			"return_pct", returnPct,
			"classification", classification,
		)
	}

	return nil
}