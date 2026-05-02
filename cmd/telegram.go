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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

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
		StatusFn:        buildStatusFn(db),
		PnlFn:           buildPnlFn(db),
		PositionsFn:     buildPositionsFn(db),
		PositionFn:      buildPositionFn(db),
		HealthFn:        buildHealthFn(db),
		ForceCloseFn:    buildForceCloseFn(db, logger),
		EnableTradingFn: buildEnableTradingFn(db, logger),
		KillFn:          buildKillFn(db, logger),
		ResumeFn:        buildResumeFn(db, logger),
		VersionFn:       buildVersionFn(db),
		ModeFn:          buildModeFn(db, logger),
		PipelineFn:      buildPipelineFn(db),
		AllowedUserIDs:  allowedIDs,
		Logger:          logger,
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

// stuckThreshold defines how long an open position can run before /pnl and
// /positions flag it. The reference repo's monitoring loop polls every ~5s,
// so anything past one hour is suspicious and worth surfacing.
const stuckThreshold = 1 * time.Hour

// truncatedPositionWindowSec is the lookback used by /pnl when summarising
// realized PnL and win-rate. 24h matches /status' drawdown window.
const closedPositionsLookbackSec = 24 * 3600

func buildPnlFn(db database.Adapter) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		drawdown, err := db.ComputeDrawdown(ctx, 24)
		if err != nil {
			return "", fmt.Errorf("compute drawdown: %w", err)
		}

		open, err := db.GetOpenPositions(ctx)
		if err != nil {
			return "", fmt.Errorf("get open positions: %w", err)
		}
		closed, err := db.GetClosedPositions(ctx, closedPositionsLookbackSec)
		if err != nil {
			return "", fmt.Errorf("get closed positions: %w", err)
		}

		now := time.Now().UTC()

		var openEntryUsd, openUnrealizedUsd float64
		var stuck int
		for _, p := range open {
			openEntryUsd += p.EntrySizeUsd
			openUnrealizedUsd += unrealizedUsd(p)
			if positionAge(p.OpenedAt, now) > stuckThreshold {
				stuck++
			}
		}

		var realized float64
		var wins, losses int
		for _, p := range closed {
			realized += p.PnlUsd
			switch {
			case p.PnlUsd > 0:
				wins++
			case p.PnlUsd < 0:
				losses++
			}
		}
		total := wins + losses
		winRate := 0.0
		if total > 0 {
			winRate = float64(wins) / float64(total) * 100
		}

		var sb strings.Builder
		sb.WriteString("<b>PnL Summary (24h)</b>\n")
		fmt.Fprintf(&sb, "Realized: <code>$%+.2f</code>  (W %d / L %d, %.1f%%)\n", realized, wins, losses, winRate)
		fmt.Fprintf(&sb, "Unrealized: <code>$%+.2f</code>  (open exposure $%.2f)\n", openUnrealizedUsd, openEntryUsd)
		fmt.Fprintf(&sb, "Drawdown: <code>%.2f%%</code>\n", drawdown*100)
		fmt.Fprintf(&sb, "Open positions: <code>%d</code>\n", len(open))
		if stuck > 0 {
			fmt.Fprintf(&sb, "⚠️ Stuck (&gt;1h still open): <code>%d</code>  — try /positions or /force_close\n", stuck)
		}
		return sb.String(), nil
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

		now := time.Now().UTC()
		var sb strings.Builder
		fmt.Fprintf(&sb, "<b>Open Positions (%d)</b>\n", len(positions))
		for i, p := range positions {
			age := positionAge(p.OpenedAt, now)
			marker := ""
			if age > stuckThreshold {
				marker = " ⚠️"
			}
			pnlPct := unrealizedPctBps(p)
			fmt.Fprintf(&sb,
				"\n<b>%d.</b> <code>%s</code> [%s]%s\n"+
					"   id: <code>%s</code>\n"+
					"   age: <code>%s</code>  size: <code>$%.2f</code>\n"+
					"   entry: <code>%s</code>  current: <code>%s</code>\n"+
					"   unrealized: <code>%+.2f%%</code> (<code>$%+.2f</code>)\n",
				i+1, p.TokenAddress, p.Chain, marker,
				p.PositionID,
				humanDuration(age), p.EntrySizeUsd,
				priceOrDash(p.EntryPrice), priceOrDash(p.CurrentPrice),
				pnlPct, unrealizedUsd(p),
			)
		}
		return sb.String(), nil
	}
}

func buildPositionFn(db database.Adapter) func(ctx context.Context, idOrAddr string) (string, error) {
	return func(ctx context.Context, idOrAddr string) (string, error) {
		idOrAddr = strings.TrimSpace(idOrAddr)
		if idOrAddr == "" {
			return "Usage: /position <position_id|token_address prefix>", nil
		}
		p, err := db.FindPositionByPrefix(ctx, idOrAddr)
		if errors.Is(err, database.ErrAmbiguous) {
			return fmt.Sprintf("⚠️ Prefix <code>%s</code> matches multiple open positions — give more characters.", idOrAddr), nil
		}
		if errors.Is(err, database.ErrNotFound) {
			return fmt.Sprintf("No open position matches prefix <code>%s</code>.", idOrAddr), nil
		}
		if err != nil {
			return "", fmt.Errorf("find position: %w", err)
		}
		now := time.Now().UTC()
		age := positionAge(p.OpenedAt, now)
		marker := ""
		if age > stuckThreshold {
			marker = " ⚠️ stuck"
		}

		var sb strings.Builder
		fmt.Fprintf(&sb, "<b>Position</b> [%s]%s\n", p.Chain, marker)
		fmt.Fprintf(&sb, "Token: <code>%s</code>\n", p.TokenAddress)
		fmt.Fprintf(&sb, "ID: <code>%s</code>\n", p.PositionID)
		fmt.Fprintf(&sb, "Status: <code>%s</code>\n", p.Status)
		fmt.Fprintf(&sb, "Opened: <code>%s</code>  (age <code>%s</code>)\n", p.OpenedAt, humanDuration(age))
		fmt.Fprintf(&sb, "Entry: <code>%s</code>  size <code>$%.2f</code>\n", priceOrDash(p.EntryPrice), p.EntrySizeUsd)
		fmt.Fprintf(&sb, "Current: <code>%s</code>\n", priceOrDash(p.CurrentPrice))
		fmt.Fprintf(&sb, "Peak: <code>%s</code>\n", priceOrDash(p.PeakPrice))
		fmt.Fprintf(&sb, "Unrealized: <code>%+.2f%%</code> (<code>$%+.2f</code>)\n", unrealizedPctBps(*p), unrealizedUsd(*p))
		fmt.Fprintf(&sb, "Exits: TP1 <code>%d bps</code> · TP2 <code>%d bps</code> · SL <code>%d bps</code> · TTL <code>%ds</code>\n",
			p.Tp1Bps, p.Tp2Bps, p.SlBps, p.MaxHoldSeconds)
		if p.TrailingStopBps > 0 {
			fmt.Fprintf(&sb, "Trailing: <code>%d bps</code> from peak\n", p.TrailingStopBps)
		}
		return sb.String(), nil
	}
}

func buildHealthFn(db database.Adapter) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		halted, haltReason, hErr := db.IsSystemHalted(ctx)

		state, sErr := db.GetSystemState(ctx)
		stats, pErr := db.GetPipelineStats(ctx, 1)

		var sb strings.Builder
		sb.WriteString("<b>Health</b>\n")
		switch {
		case hErr != nil:
			fmt.Fprintf(&sb, "Kill switch: <code>unknown</code> (err: %v)\n", hErr)
		case halted:
			fmt.Fprintf(&sb, "🔴 Kill switch: <code>HALTED</code> — %s\n", haltReason)
		default:
			sb.WriteString("🟢 Kill switch: <code>active</code>\n")
		}
		if sErr == nil && state != nil {
			fmt.Fprintf(&sb, "Mode: <code>%s</code>  drawdown <code>%.2f%%</code>  open <code>%d</code>\n",
				state.Mode, state.DrawdownPct*100, state.OpenPositions)
			fmt.Fprintf(&sb, "Updated: <code>%s</code>\n", state.UpdatedAt)
		}
		if pErr == nil && stats != nil {
			fmt.Fprintf(&sb, "Last 1h funnel: detected <code>%d</code> · validated <code>%d</code> · executed <code>%d</code>\n",
				stats.Detected, stats.Validated, stats.Executed)
			if stats.Detected == 0 {
				sb.WriteString("⚠️ No detections in last hour — check ingestion workers.\n")
			}
		}
		return sb.String(), nil
	}
}

func buildForceCloseFn(db database.Adapter, logger *slog.Logger) func(ctx context.Context, idOrAddr, issuer string) (string, error) {
	return func(ctx context.Context, idOrAddr, issuer string) (string, error) {
		idOrAddr = strings.TrimSpace(idOrAddr)
		p, err := db.FindPositionByPrefix(ctx, idOrAddr)
		if errors.Is(err, database.ErrAmbiguous) {
			return fmt.Sprintf("⚠️ Prefix <code>%s</code> matches multiple positions — give more characters.", idOrAddr), nil
		}
		if errors.Is(err, database.ErrNotFound) {
			return fmt.Sprintf("No open position matches prefix <code>%s</code>.", idOrAddr), nil
		}
		if err != nil {
			return "", fmt.Errorf("find position: %w", err)
		}

		// Emit a final PositionStateDTO snapshot with status=exited and
		// reason=MANUAL. The position monitoring loop and learning engine
		// will treat this as an operator-driven close. Exit price and PnL
		// stay at their last polled values — best truth available without
		// blocking on a fresh on-chain quote.
		now := time.Now().UTC().Format(time.RFC3339)
		exit := *p
		exit.Status = "exited"
		exit.ExitReason = "MANUAL"
		exit.ExitedAt = now
		exit.SnapshotAt = now
		// Re-derive event_id so the insert is idempotent and trace fields
		// chain off the previous snapshot via causation_id.
		seed := fmt.Sprintf("force_close|%s|%s|%s", p.PositionID, issuer, now)
		sum := sha256.Sum256([]byte(seed))
		exit.EventID = hex.EncodeToString(sum[:])[:16]
		exit.CausationID = p.EventID
		if exit.ExitPrice == "" {
			exit.ExitPrice = exit.CurrentPrice
		}

		if err := db.InsertPositionState(ctx, exit); err != nil {
			return "", fmt.Errorf("insert force-close state: %w", err)
		}
		logger.Warn("telegram_force_close",
			"position_id", p.PositionID,
			"token_address", p.TokenAddress,
			"chain", p.Chain,
			"issuer", issuer,
			"reason", "MANUAL",
		)
		return fmt.Sprintf(
			"✅ Force-close emitted for <code>%s</code> [%s]\n"+
				"Position <code>%s</code> marked MANUAL exited.\n"+
				"<i>Note: this records the exit; on-chain unwind (if needed) is the operator's responsibility.</i>",
			p.TokenAddress, p.Chain, p.PositionID,
		), nil
	}
}

func buildEnableTradingFn(db database.Adapter, logger *slog.Logger) func(ctx context.Context, issuer string) (string, error) {
	return func(ctx context.Context, issuer string) (string, error) {
		// Phase 6: this is the gated command that flips the safety-net halt
		// after the 48h shadow run. It is intentionally identical in effect
		// to /resume, but logs distinct context so audit trails make the
		// "shadow → live" transition easy to grep.
		halted, _, err := db.IsSystemHalted(ctx)
		if err != nil {
			return "", fmt.Errorf("read halt state: %w", err)
		}
		if !halted {
			return "ℹ️ Trading already enabled.", nil
		}

		// Live-data validation gate: refuse to clear the halt until the
		// shadow run has produced enough DQ-passed events for downstream
		// dispersion checks to be meaningful. The thresholds are
		// intentionally conservative — operators who want to bypass the
		// gate must use /resume directly (which is logged identically).
		const (
			phase6MinDQPassed = 500
			phase6WindowHours = 48
		)
		stats, statsErr := db.GetPipelineStats(ctx, phase6WindowHours)
		switch {
		case statsErr != nil:
			// Infra failures must NOT block enablement indefinitely — log and
			// continue. Operator-visible warning makes the path obvious.
			logger.Warn("telegram_enable_trading_stats_unavailable",
				"error", statsErr,
			)
		case stats == nil || stats.DQPassed < phase6MinDQPassed:
			var got int64
			if stats != nil {
				got = stats.DQPassed
			}
			logger.Warn("telegram_enable_trading_blocked_insufficient_shadow_data",
				"issuer", issuer,
				"dq_passed", got,
				"required", phase6MinDQPassed,
				"window_hours", phase6WindowHours,
			)
			return fmt.Sprintf(
				"⛔ <b>Enable refused — shadow run incomplete.</b>\n"+
					"DQ-passed events (last %dh): <b>%d</b> / %d required.\n"+
					"<i>Wait for the shadow window to fill, or use /resume to bypass (also logged).</i>",
				phase6WindowHours, got, phase6MinDQPassed,
			), nil
		}

		if err := db.SetSystemHalt(ctx, false, "/enable_trading after Phase 6 shadow run", "telegram_operator:"+issuer); err != nil {
			return "", fmt.Errorf("clear halt: %w", err)
		}

		var dqPassed int64
		if stats != nil {
			dqPassed = stats.DQPassed
		}
		logger.Warn("telegram_enable_trading",
			"issuer", issuer,
			"phase", "6_live_data_validation",
			"dq_passed_48h", dqPassed,
		)
		return fmt.Sprintf(
			"🟢 Trading enabled by <code>%s</code> at <code>%s</code>.\n"+
				"Shadow stats (last %dh): <b>%d</b> DQ-passed events.\n"+
				"<i>Phase 6 transition logged. Use /kill to halt instantly.</i>",
			issuer, time.Now().UTC().Format(time.RFC3339),
			phase6WindowHours, dqPassed,
		), nil
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func positionAge(openedAt string, now time.Time) time.Duration {
	if openedAt == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, openedAt)
	if err != nil {
		// SnapshotAt may use RFC3339Nano; try that too.
		t, err = time.Parse(time.RFC3339Nano, openedAt)
		if err != nil {
			return 0
		}
	}
	return now.Sub(t)
}

func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%dd%02dh", int(d.Hours())/24, int(d.Hours())%24)
}

func priceOrDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// unrealizedPctBps returns (current - entry) / entry * 100 in percent.
// Returns 0 when either price is missing or unparseable, so the operator
// sees a neutral row rather than a misleading huge swing.
func unrealizedPctBps(p contracts.PositionStateDTO) float64 {
	entry, ok1 := parseFloat(p.EntryPrice)
	current, ok2 := parseFloat(p.CurrentPrice)
	if !ok1 || !ok2 || entry == 0 {
		return 0
	}
	return (current - entry) / entry * 100
}

func unrealizedUsd(p contracts.PositionStateDTO) float64 {
	pct := unrealizedPctBps(p) / 100.0
	return p.EntrySizeUsd * pct
}

func parseFloat(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
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
		newState.LastTransitionReason = "manual_telegram"

		if _, err := db.UpsertSystemState(ctx, newState, expectedVersion); err != nil {
			return "", fmt.Errorf("update system mode: %w", err)
		}

		logger.Info("telegram_mode_changed", "mode", mode)
		return fmt.Sprintf("✅ Mode switched to <code>%s</code>", mode), nil
	}
}

func buildPipelineFn(db database.Adapter) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		stats, err := db.GetPipelineStats(ctx, 24)
		if err != nil {
			return "", fmt.Errorf("get pipeline stats: %w", err)
		}

		var sb strings.Builder
		sb.WriteString("<b>Pipeline Funnel (last 24h)</b>\n\n")

		total := stats.Detected
		pct := func(n int64) string {
			if total == 0 {
				return "0.0%"
			}
			return fmt.Sprintf("%.1f%%", float64(n)/float64(total)*100)
		}

		sb.WriteString(fmt.Sprintf("DETECTED     <code>%6d</code>  (100%%)\n", stats.Detected))
		sb.WriteString(fmt.Sprintf("DQ_PASSED    <code>%6d</code>  (%s)\n", stats.DQPassed, pct(stats.DQPassed)))
		sb.WriteString(fmt.Sprintf("FEATURE      <code>%6d</code>  (%s)\n", stats.FeatureReady, pct(stats.FeatureReady)))
		sb.WriteString(fmt.Sprintf("EDGE         <code>%6d</code>  (%s)\n", stats.EdgeDetected, pct(stats.EdgeDetected)))
		sb.WriteString(fmt.Sprintf("VALIDATED    <code>%6d</code>  (%s)\n", stats.Validated, pct(stats.Validated)))
		sb.WriteString(fmt.Sprintf("SELECTED     <code>%6d</code>  (%s)\n", stats.Selected, pct(stats.Selected)))
		sb.WriteString(fmt.Sprintf("EXECUTED     <code>%6d</code>  (%s)\n", stats.Executed, pct(stats.Executed)))
		sb.WriteString(fmt.Sprintf("POS OPEN     <code>%6d</code>  (%s)\n", stats.PositionOpen, pct(stats.PositionOpen)))
		sb.WriteString(fmt.Sprintf("POS CLOSED   <code>%6d</code>  (%s)\n", stats.PositionClosed, pct(stats.PositionClosed)))
		sb.WriteString(fmt.Sprintf("REJECTED     <code>%6d</code>\n", stats.Rejected))
		sb.WriteString(fmt.Sprintf("FAILED       <code>%6d</code>\n", stats.Failed))

		if len(stats.Recent) > 0 {
			sb.WriteString("\n<b>Recent tokens:</b>\n")
			for _, rt := range stats.Recent {
				addr := rt.TokenAddress
				if len(addr) > 12 {
					addr = addr[:6] + "…" + addr[len(addr)-4:]
				}
				ticker := rt.Symbol
				if ticker == "" {
					ticker = "—"
				}
				chain := rt.Chain
				if chain == "" {
					chain = "?"
				}
				sb.WriteString(fmt.Sprintf(
					"<code>%s</code> [%s] %s · %s\n",
					addr, ticker, rt.State, chain,
				))
			}
		}

		return sb.String(), nil
	}
}
