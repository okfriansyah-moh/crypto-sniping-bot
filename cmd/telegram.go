package main

// telegram.go — Telegram dispatcher + command handler builder for cmd/server.go.
//
// Reads from environment variables:
//   SNIPER_TELEGRAM_BOT_TOKEN     — bot token from @BotFather
//   SNIPER_TELEGRAM_CHAT_ID       — operator chat or group ID
//   SNIPER_TELEGRAM_ALLOWED_USERS — comma-separated Telegram user IDs (optional;
//                                   if unset, /kill and /resume are blocked)
//
// Returns two objects that callers run as goroutines:
//   - Dispatcher  (event bus → Telegram outbound)
//   - Poller      (Telegram inbound → command handler)

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/telegram"
)

// buildTelegramComponents constructs the Dispatcher and Poller for the Telegram
// integration. Returns (nil, nil) when no bot token is configured so the
// caller can skip starting them without error.
func buildTelegramComponents(
	db database.Adapter,
	logger *slog.Logger,
) (*telegram.Dispatcher, *telegram.Poller) {
	token := os.Getenv("SNIPER_TELEGRAM_BOT_TOKEN")
	chatID := os.Getenv("SNIPER_TELEGRAM_CHAT_ID")

	if token == "" {
		logger.Info("telegram_not_configured",
			"note", "set SNIPER_TELEGRAM_BOT_TOKEN + SNIPER_TELEGRAM_CHAT_ID to enable Telegram")
		return nil, nil
	}
	if chatID == "" {
		logger.Error("telegram_misconfigured",
			"note", "SNIPER_TELEGRAM_BOT_TOKEN is set but SNIPER_TELEGRAM_CHAT_ID is empty — Telegram disabled")
		return nil, nil
	}

	allowedIDs := parseTelegramAllowedUsers()
	client := telegram.NewClient(token, chatID)

	handler := telegram.NewHandler(telegram.HandlerOptions{
		StatusFn:       buildStatusFn(db),
		PnlFn:          buildPnlFn(db),
		PositionsFn:    buildPositionsFn(db),
		KillFn:         buildKillFn(db, logger),
		ResumeFn:       buildResumeFn(db, logger),
		VersionFn:      buildVersionFn(db),
		ModeFn:         buildModeFn(db, logger),
		AllowedUserIDs: allowedIDs,
		Logger:         logger,
	})

	dispatcher := telegram.NewDispatcher(db, client, logger)
	poller := telegram.NewPoller(client, handler, logger)

	return dispatcher, poller
}

// parseTelegramAllowedUsers reads SNIPER_TELEGRAM_ALLOWED_USERS (comma-separated).
func parseTelegramAllowedUsers() []string {
	raw := os.Getenv("SNIPER_TELEGRAM_ALLOWED_USERS")
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// ── Command function builders ─────────────────────────────────────────────────

func buildStatusFn(db database.Adapter) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		state, err := db.GetSystemState(ctx)
		if err != nil {
			return "", fmt.Errorf("get system state: %w", err)
		}
		if state == nil {
			return "⚠️ System state not yet initialized.", nil
		}

		halted, haltReason, hErr := db.IsSystemHalted(ctx)
		haltInfo := ""
		if hErr == nil && halted {
			haltInfo = fmt.Sprintf("\n🔴 KILL SWITCH: <code>%s</code>", haltReason)
		}

		sv, svErr := db.GetActiveStrategyVersion(ctx)
		versionLabel := "unknown"
		if svErr == nil && sv != nil {
			versionLabel = sv.StrategyVersionID
		}

		return fmt.Sprintf(
			"<b>Status</b>\n"+
				"Mode: <code>%s</code>\n"+
				"Drawdown (24h): <code>%.2f%%</code>\n"+
				"Open positions: <code>%d</code>\n"+
				"Exposure: <code>$%.2f</code>\n"+
				"Strategy: <code>%s</code>\n"+
				"Updated: <code>%s</code>%s",
			state.Mode,
			state.DrawdownPct*100,
			state.OpenPositions,
			state.TotalExposureUsd,
			versionLabel,
			state.UpdatedAt,
			haltInfo,
		), nil
	}
}

func buildPnlFn(db database.Adapter) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		drawdown, err := db.ComputeDrawdown(ctx, 24)
		if err != nil {
			return "", fmt.Errorf("compute drawdown: %w", err)
		}

		positions, err := db.GetOpenPositions(ctx)
		if err != nil {
			return "", fmt.Errorf("get open positions: %w", err)
		}

		var totalEntryUsd float64
		for _, p := range positions {
			totalEntryUsd += p.EntrySizeUsd
		}

		return fmt.Sprintf(
			"<b>PnL Summary (24h)</b>\n"+
				"Realized drawdown: <code>%.2f%%</code>\n"+
				"Open positions: <code>%d</code>\n"+
				"Total entry exposure: <code>$%.2f</code>",
			drawdown*100,
			len(positions),
			totalEntryUsd,
		), nil
	}
}

func buildPositionsFn(db database.Adapter) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		positions, err := db.GetOpenPositions(ctx)
		if err != nil {
			return "", fmt.Errorf("get open positions: %w", err)
		}
		if len(positions) == 0 {
			return "No open positions.", nil
		}

		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("<b>Open Positions (%d)</b>\n", len(positions)))
		for i, p := range positions {
			token := p.TokenAddress
			if len(token) > 10 {
				token = token[:6] + "..." + token[len(token)-4:]
			}
			sb.WriteString(fmt.Sprintf(
				"%d. <code>%s</code> [%s] — $%.2f @ %s\n",
				i+1,
				token,
				p.Chain,
				p.EntrySizeUsd,
				p.EntryPrice,
			))
		}
		return sb.String(), nil
	}
}

func buildKillFn(db database.Adapter, logger *slog.Logger) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		if err := db.SetSystemHalt(ctx, true, "/kill command", "telegram_operator"); err != nil {
			return fmt.Errorf("set kill switch: %w", err)
		}
		logger.Info("telegram_kill_executed", "kill_switch", true)
		return nil
	}
}

func buildResumeFn(db database.Adapter, logger *slog.Logger) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		if err := db.SetSystemHalt(ctx, false, "/resume command", "telegram_operator"); err != nil {
			return fmt.Errorf("clear kill switch: %w", err)
		}
		logger.Info("telegram_resume_executed", "kill_switch", false)
		return nil
	}
}

func buildVersionFn(db database.Adapter) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		sv, err := db.GetActiveStrategyVersion(ctx)
		if err != nil {
			return "", fmt.Errorf("get strategy version: %w", err)
		}
		if sv == nil {
			return "No active strategy version.", nil
		}
		return fmt.Sprintf(
			"<b>Active Strategy</b>\n"+
				"ID: <code>%s</code>\n"+
				"Status: <code>%s</code>\n"+
				"Created: <code>%s</code>",
			sv.StrategyVersionID,
			sv.Status,
			sv.CreatedAt,
		), nil
	}
}

func buildModeFn(db database.Adapter, logger *slog.Logger) func(ctx context.Context, mode string) (string, error) {
	return func(ctx context.Context, mode string) (string, error) {
		// Get current state to read live fields and the CAS version counter.
		current, err := db.GetSystemState(ctx)
		if err != nil {
			return "", fmt.Errorf("get system state: %w", err)
		}

		var newState contracts.SystemStateDTO
		var expectedVersion int64
		if current != nil {
			newState = *current
			expectedVersion = current.StateVersion
		}
		newState.Mode = mode
		newState.LastTransitionReason = "/mode command (telegram_operator)"

		if _, err := db.UpsertSystemState(ctx, newState, expectedVersion); err != nil {
			return "", fmt.Errorf("update system mode: %w", err)
		}

		logger.Info("telegram_mode_changed", "mode", mode)
		return fmt.Sprintf("✅ Mode switched to <code>%s</code>", mode), nil
	}
}
