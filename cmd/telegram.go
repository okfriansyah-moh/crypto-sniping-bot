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
	"html"
	"log/slog"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/telegram"
)

// buildTelegramComponents constructs the Dispatcher and Poller for the Telegram
// integration. Returns (nil, nil) when no bot token is configured so the
// caller can skip starting them without error.
func buildTelegramComponents(
	db database.Adapter,
	logger *slog.Logger,
	cfg *config.Config,
	startTime time.Time,
	rescanTrigger chan struct{},
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
		StatusFn:         buildStatusFn(db, startTime),
		PnlFn:            buildPnlFn(db),
		PositionsFn:      buildPositionsFn(db),
		PositionFn:       buildPositionFn(db),
		HealthFn:         buildHealthFn(db),
		ForceCloseFn:     buildForceCloseFn(db, logger),
		EnableTradingFn:  buildEnableTradingFn(db, logger),
		KillFn:           buildKillFn(db, logger),
		ResumeFn:         buildResumeFn(db, logger),
		VersionFn:        buildVersionFn(db),
		ModeFn:           buildModeFn(db, logger),
		PipelineFn:       buildPipelineFn(db),
		RescanPipelineFn: buildRescanPipelineFn(db),
		RescanFn:         buildRescanFn(rescanTrigger),
		RescanStatusFn:   buildRescanStatusFn(db, cfg),
		DqFn:             buildDqFn(db),
		DlqFn:            buildDlqFn(db),
		AllowedUserIDs:   allowedIDs,
		Logger:           logger,
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

func buildStatusFn(db database.Adapter, startTime time.Time) func(ctx context.Context) (string, error) {
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
				"Running since: <code>%s (%s ago)</code>\n"+
				"Updated: <code>%s</code>%s",
			state.Mode,
			state.DrawdownPct*100,
			state.OpenPositions,
			state.TotalExposureUsd,
			versionLabel,
			startTime.Format("2006-01-02 15:04:05 UTC"),
			humanDuration(time.Since(startTime)),
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
			return fmt.Sprintf("⚠️ Prefix <code>%s</code> matches multiple open positions — give more characters.", html.EscapeString(idOrAddr)), nil
		}
		if errors.Is(err, database.ErrNotFound) {
			return fmt.Sprintf("No open position matches prefix <code>%s</code>.", html.EscapeString(idOrAddr)), nil
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
		prefix := strings.ToLower(idOrAddr)

		allOpen, err := db.GetOpenPositions(ctx)
		if err != nil {
			return "", fmt.Errorf("get open positions: %w", err)
		}

		// Phase 1: match by token_address prefix.
		var matching []contracts.PositionStateDTO
		for _, p := range allOpen {
			if strings.HasPrefix(strings.ToLower(p.TokenAddress), prefix) {
				matching = append(matching, p)
			}
		}

		// Phase 2: if no token match, fall back to position_id prefix.
		if len(matching) == 0 {
			for _, p := range allOpen {
				if strings.HasPrefix(strings.ToLower(p.PositionID), prefix) {
					matching = append(matching, p)
				}
			}
		}

		if len(matching) == 0 {
			return fmt.Sprintf("No open position matches <code>%s</code>.", html.EscapeString(idOrAddr)), nil
		}

		// Reject if the prefix is so short it spans multiple different tokens.
		distinctTokens := make(map[string]struct{}, len(matching))
		for _, p := range matching {
			distinctTokens[strings.ToLower(p.TokenAddress)] = struct{}{}
		}
		if len(distinctTokens) > 1 {
			return fmt.Sprintf(
				"⚠️ Prefix <code>%s</code> matches %d different tokens — give more characters.",
				html.EscapeString(idOrAddr), len(distinctTokens),
			), nil
		}

		// Close all matching positions (same token may have multiple open slots).
		now := time.Now().UTC().Format(time.RFC3339)
		var closedIDs []string
		for _, p := range matching {
			// Emit a final PositionStateDTO snapshot with status=exited and
			// reason=MANUAL. The position monitoring loop and learning engine
			// treat this as an operator-driven close. Exit price stays at
			// last polled value — best truth available without a live quote.
			exit := p
			exit.Status = "exited"
			exit.ExitReason = "MANUAL"
			exit.ExitedAt = now
			exit.SnapshotAt = now
			// Re-derive event_id: idempotent + chains causation off prior snapshot.
			seed := fmt.Sprintf("force_close|%s|%s|%s", p.PositionID, issuer, now)
			sum := sha256.Sum256([]byte(seed))
			exit.EventID = hex.EncodeToString(sum[:])[:16]
			exit.CausationID = p.EventID
			if exit.ExitPrice == "" {
				exit.ExitPrice = exit.CurrentPrice
			}
			if err := db.InsertPositionState(ctx, exit); err != nil {
				return "", fmt.Errorf("insert force-close state for %s: %w", p.PositionID, err)
			}
			logger.Warn("telegram_force_close",
				"position_id", p.PositionID,
				"token_address", p.TokenAddress,
				"chain", p.Chain,
				"issuer", issuer,
				"reason", "MANUAL",
			)
			closedIDs = append(closedIDs, p.PositionID)
		}

		tokenAddr := matching[0].TokenAddress
		chain := matching[0].Chain
		if len(closedIDs) == 1 {
			return fmt.Sprintf(
				"✅ Force-close emitted for <code>%s</code> [%s]\n"+
					"Position <code>%s</code> marked MANUAL exited.\n"+
					"<i>Note: this records the exit; on-chain unwind (if needed) is the operator's responsibility.</i>",
				tokenAddr, chain, closedIDs[0],
			), nil
		}
		return fmt.Sprintf(
			"✅ Force-close emitted for <code>%s</code> [%s] — <b>%d positions</b> closed.\n"+
				"Position IDs: <code>%s</code>\n"+
				"<i>Note: this records the exits; on-chain unwind (if needed) is the operator's responsibility.</i>",
			tokenAddr, chain, len(closedIDs), strings.Join(closedIDs, ", "),
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
		sb.WriteString("<b>Pipeline Funnel (last 24h — cumulative)</b>\n\n")

		// stats.Detected is the true total (COUNT(*) of all tokens in window).
		// All percentages are relative to this base, so DETECTED always reads 100%.
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
		sb.WriteString(fmt.Sprintf("EVALUATED    <code>%6d</code>  (%s)\n", stats.Evaluated, pct(stats.Evaluated)))
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("REJECTED     <code>%6d</code>\n", stats.Rejected))
		sb.WriteString(fmt.Sprintf("FAILED       <code>%6d</code>\n", stats.Failed))
		if stats.Failed > 0 {
			sb.WriteString(fmt.Sprintf("  ↳ exec fail  <code>%6d</code>  (SELECTED→FAILED)\n", stats.FailedAtSelected))
			sb.WriteString(fmt.Sprintf("  ↳ pos-open   <code>%6d</code>  (EXECUTED→FAILED)\n", stats.FailedAtExecuted))
			sb.WriteString(fmt.Sprintf("  ↳ pos-close  <code>%6d</code>  (POSITION_OPEN→FAILED)\n", stats.FailedAtPositionOpen))
		}

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

// rescanQueryer is a local interface for the optional GetRescanStats method.
// The postgres *DB implements this concretely; stubs and other adapters do not.
// Using a local interface avoids adding GetRescanStats to the global Adapter interface.
type rescanQueryer interface {
	GetRescanStats(ctx context.Context, windowHours int) (*database.RescanStats, error)
}

// rescanPipelineQueryer is a local interface for the optional GetRescanPipelineStats
// method.  The postgres *DB implements this concretely; stubs do not.
type rescanPipelineQueryer interface {
	GetRescanPipelineStats(ctx context.Context, windowHours int) (*database.RescanPipelineStats, error)
}

// buildRescanFn returns a function that sends a force-trigger to the rescan worker.
// triggerCh is a buffered channel (cap=1) created in server.go and shared with
// RunRescan.  The send is non-blocking: if a trigger is already queued the
// operator is told the worker will fire shortly.
func buildRescanFn(triggerCh chan struct{}) func(ctx context.Context) (string, error) {
	return func(_ context.Context) (string, error) {
		if triggerCh == nil {
			return "⚠️ Rescan worker not configured.", nil
		}
		select {
		case triggerCh <- struct{}{}:
			return "✅ Rescan force-triggered. Check /rescan_status in a few seconds for results.", nil
		default:
			return "⏳ Rescan trigger already queued — worker will fire shortly.", nil
		}
	}
}

// buildRescanStatusFn returns a function that shows the rescan worker
// configuration and last-24h per-band emission counts.

// buildRescanPipelineFn returns a function that shows the pipeline funnel for
// tokens re-emitted via the rescan worker (transport LIKE 'rescan_%').
// Uses a type assertion to the concrete postgres method — adapters that don't
// implement it return a graceful "not supported" message.
func buildRescanPipelineFn(db database.Adapter) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		rq, ok := db.(rescanPipelineQueryer)
		if !ok {
			return "⚠️ /rescan_pipeline is not supported by the current database adapter.", nil
		}
		rs, err := rq.GetRescanPipelineStats(ctx, 24)
		if err != nil {
			return "", fmt.Errorf("get rescan pipeline stats: %w", err)
		}

		var sb strings.Builder
		sb.WriteString("<b>Rescan Pipeline Funnel (last 24h — rescan tokens only)</b>\n\n")

		if rs.Detected == 0 {
			sb.WriteString("No tokens were rescanned in the last 24h.")
			return sb.String(), nil
		}

		total := rs.Detected
		pct := func(n int64) string {
			if total == 0 {
				return "0.0%"
			}
			return fmt.Sprintf("%.1f%%", float64(n)/float64(total)*100)
		}

		sb.WriteString(fmt.Sprintf("DETECTED     <code>%6d</code>  (100%%)\n", rs.Detected))
		sb.WriteString(fmt.Sprintf("DQ_PASSED    <code>%6d</code>  (%s)\n", rs.DQPassed, pct(rs.DQPassed)))
		sb.WriteString(fmt.Sprintf("FEATURE      <code>%6d</code>  (%s)\n", rs.FeatureReady, pct(rs.FeatureReady)))
		sb.WriteString(fmt.Sprintf("EDGE         <code>%6d</code>  (%s)\n", rs.EdgeDetected, pct(rs.EdgeDetected)))
		sb.WriteString(fmt.Sprintf("VALIDATED    <code>%6d</code>  (%s)\n", rs.Validated, pct(rs.Validated)))
		sb.WriteString(fmt.Sprintf("SELECTED     <code>%6d</code>  (%s)\n", rs.Selected, pct(rs.Selected)))
		sb.WriteString(fmt.Sprintf("EXECUTED     <code>%6d</code>  (%s)\n", rs.Executed, pct(rs.Executed)))
		sb.WriteString(fmt.Sprintf("POS OPEN     <code>%6d</code>  (%s)\n", rs.PositionOpen, pct(rs.PositionOpen)))
		sb.WriteString(fmt.Sprintf("POS CLOSED   <code>%6d</code>  (%s)\n", rs.PositionClosed, pct(rs.PositionClosed)))
		sb.WriteString(fmt.Sprintf("EVALUATED    <code>%6d</code>  (%s)\n", rs.Evaluated, pct(rs.Evaluated)))
		sb.WriteString("\n")
		sb.WriteString(fmt.Sprintf("REJECTED     <code>%6d</code>\n", rs.Rejected))
		sb.WriteString(fmt.Sprintf("FAILED       <code>%6d</code>\n", rs.Failed))
		if rs.Failed > 0 {
			sb.WriteString(fmt.Sprintf("  ↳ exec fail  <code>%6d</code>  (SELECTED→FAILED)\n", rs.FailedAtSelected))
			sb.WriteString(fmt.Sprintf("  ↳ pos-open   <code>%6d</code>  (EXECUTED→FAILED)\n", rs.FailedAtExecuted))
			sb.WriteString(fmt.Sprintf("  ↳ pos-close  <code>%6d</code>  (POSITION_OPEN→FAILED)\n", rs.FailedAtPositionOpen))
		}

		// Per-band breakdown
		if len(rs.ByBand) > 0 {
			sb.WriteString(fmt.Sprintf("\n<b>Emissions by band (24h):  %d total</b>\n", rs.TotalEmitted))
			bandNames := make([]string, 0, len(rs.ByBand))
			for b := range rs.ByBand {
				bandNames = append(bandNames, b)
			}
			sort.Strings(bandNames)
			for _, b := range bandNames {
				sb.WriteString(fmt.Sprintf("  <code>%-6s</code>  %d\n", b, rs.ByBand[b]))
			}
		}

		// Recent rescanned tokens
		if len(rs.Recent) > 0 {
			sb.WriteString("\n<b>Recent rescanned tokens:</b>\n")
			for _, rt := range rs.Recent {
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

func buildRescanStatusFn(db database.Adapter, cfg *config.Config) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		rc := cfg.Rescan
		var sb strings.Builder

		// Header
		status := "✅ enabled"
		if !rc.Enabled {
			status = "🔴 disabled"
		}
		sb.WriteString(fmt.Sprintf("<b>Rescan Status</b>  %s\n\n", status))

		// Config
		sb.WriteString(fmt.Sprintf("Interval:           <code>%ds</code>\n", rc.IntervalSeconds))
		sb.WriteString(fmt.Sprintf("Max per band/tick:  <code>%d</code>\n", rc.MaxPerBandPerTick))
		skipOpen := "no"
		if rc.SkipOpenPositions {
			skipOpen = "yes"
		}
		sb.WriteString(fmt.Sprintf("Skip open positions:<code>%s</code>\n", skipOpen))

		// Bands table
		if len(rc.Bands) > 0 {
			sb.WriteString("\n<b>Bands:</b>\n")
			bands := make([]config.RescanBand, len(rc.Bands))
			copy(bands, rc.Bands)
			sort.Slice(bands, func(i, j int) bool { return bands[i].Priority < bands[j].Priority })
			for _, b := range bands {
				minM := b.MinAgeSeconds / 60
				maxM := b.MaxAgeSeconds / 60
				sb.WriteString(fmt.Sprintf("  <code>%-6s</code>  %d–%dm  priority %d\n",
					b.Name, minM, maxM, b.Priority))
			}
		}

		// Mode overrides eligibility
		if len(rc.ModeOverrides) > 0 {
			sb.WriteString("\n<b>Eligibility thresholds by mode:</b>\n")
			modes := make([]string, 0, len(rc.ModeOverrides))
			for m := range rc.ModeOverrides {
				modes = append(modes, m)
			}
			sort.Strings(modes)
			for _, mode := range modes {
				e := rc.ModeOverrides[mode]
				parts := []string{}
				if e.MaxHoneypotScore != nil {
					parts = append(parts, fmt.Sprintf("hp≤%.2f", *e.MaxHoneypotScore))
				}
				if e.MaxRugScore != nil {
					parts = append(parts, fmt.Sprintf("rug≤%.2f", *e.MaxRugScore))
				}
				if e.MaxBuyTaxBps != nil {
					parts = append(parts, fmt.Sprintf("tax≤%dbps", *e.MaxBuyTaxBps))
				}
				desc := "default"
				if len(parts) > 0 {
					desc = strings.Join(parts, ", ")
				}
				sb.WriteString(fmt.Sprintf("  <code>%-12s</code>  %s\n", mode, desc))
			}
		}

		// Last 24h emission stats (optional — postgres only, via type assertion)
		if rq, ok := db.(rescanQueryer); ok {
			rs, err := rq.GetRescanStats(ctx, 24)
			if err == nil && rs.TotalEmitted > 0 {
				sb.WriteString(fmt.Sprintf("\n<b>Last 24h emissions:</b>  <code>%d total</code>\n", rs.TotalEmitted))
				if len(rs.ByBand) > 0 {
					bandNames := make([]string, 0, len(rs.ByBand))
					for b := range rs.ByBand {
						bandNames = append(bandNames, b)
					}
					sort.Strings(bandNames)
					for _, b := range bandNames {
						sb.WriteString(fmt.Sprintf("  <code>%-6s</code>  %d\n", b, rs.ByBand[b]))
					}
				}
			} else if err == nil {
				sb.WriteString("\n<i>No rescan events in the last 24h.</i>\n")
			}
		}

		return sb.String(), nil
	}
}

func buildDqFn(db database.Adapter) func(ctx context.Context, hours int) (string, error) {
	return func(ctx context.Context, hours int) (string, error) {
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("<b>Data Quality  (last %dh)</b>\n\n", hours))

		// Adaptive DQ stats (rolling window — uses seconds)
		windowSec := hours * 3600
		totalDecisions, rugRejects, dqErr := db.GetAdaptiveDQStats(ctx, windowSec)
		if dqErr == nil {
			sb.WriteString("<b>Adaptive DQ decisions:</b>\n")
			sb.WriteString(fmt.Sprintf("  Total decisions:  <code>%d</code>\n", totalDecisions))
			sb.WriteString(fmt.Sprintf("  Rug rejects:      <code>%d</code>\n", rugRejects))
			rugRate := 0.0
			if totalDecisions > 0 {
				rugRate = float64(rugRejects) / float64(totalDecisions) * 100
			}
			sb.WriteString(fmt.Sprintf("  Rug rate:         <code>%.1f%%</code>\n", rugRate))
		}

		// Pipeline DQ stage counts (from cumulative funnel)
		ps, err := db.GetPipelineStats(ctx, hours)
		if err != nil {
			return "", fmt.Errorf("get pipeline stats: %w", err)
		}
		sb.WriteString("\n<b>Funnel gate (DQ):</b>\n")
		sb.WriteString(fmt.Sprintf("  Tokens detected:  <code>%d</code>\n", ps.Detected))
		sb.WriteString(fmt.Sprintf("  DQ passed:        <code>%d</code>\n", ps.DQPassed))
		sb.WriteString(fmt.Sprintf("  Rejected (any):   <code>%d</code>\n", ps.Rejected))
		passRate := 0.0
		rejectRate := 0.0
		if ps.Detected > 0 {
			passRate = float64(ps.DQPassed) / float64(ps.Detected) * 100
			rejectRate = float64(ps.Rejected) / float64(ps.Detected) * 100
		}
		sb.WriteString(fmt.Sprintf("  Pass rate:        <code>%.1f%%</code>\n", passRate))
		sb.WriteString(fmt.Sprintf("  Reject rate:      <code>%.1f%%</code>\n", rejectRate))

		// Health verdict
		verdict := "✅ Healthy"
		if rejectRate > 80 {
			verdict = "🔴 High reject rate — check thresholds or data feed"
		} else if passRate < 5 && ps.Detected > 10 {
			verdict = "⚠️ Very low pass rate — review DQ thresholds"
		}
		sb.WriteString(fmt.Sprintf("\nVerdict: %s\n", verdict))

		return sb.String(), nil
	}
}

func buildDlqFn(db database.Adapter) func(ctx context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		entries, err := db.ListDLQ(ctx, database.DLQFilter{Limit: 10})
		if err != nil {
			return "", fmt.Errorf("list dlq: %w", err)
		}

		var sb strings.Builder
		sb.WriteString("<b>Dead-Letter Queue</b>\n\n")

		if len(entries) == 0 {
			sb.WriteString("✅ DLQ is empty.\n")
			return sb.String(), nil
		}

		sb.WriteString(fmt.Sprintf("Showing last %d entries:\n\n", len(entries)))

		// Summarize by reason
		byCause := make(map[string]int)
		for _, e := range entries {
			cause := e.Reason
			if cause == "" {
				cause = "unknown"
			}
			byCause[cause]++
		}

		if len(byCause) > 0 {
			sb.WriteString("<b>Reason breakdown:</b>\n")
			type kv struct {
				k string
				v int
			}
			sorted := make([]kv, 0, len(byCause))
			for k, v := range byCause {
				sorted = append(sorted, kv{k, v})
			}
			sort.Slice(sorted, func(i, j int) bool { return sorted[i].v > sorted[j].v })
			for _, kv := range sorted {
				sb.WriteString(fmt.Sprintf("  <code>%s</code>  ×%d\n", html.EscapeString(kv.k), kv.v))
			}
			sb.WriteString("\n")
		}

		sb.WriteString("<b>Recent entries:</b>\n")
		for _, e := range entries {
			reason := e.Reason
			if reason == "" {
				reason = "unknown"
			}
			errMsg := e.ErrorMessage
			if len(errMsg) > 80 {
				errMsg = errMsg[:80] + "…"
			}
			ts := e.LastFailedAt
			if len(ts) > 16 {
				ts = ts[:16] // ISO truncated to minute
			}
			sb.WriteString(fmt.Sprintf(
				"<code>%s</code>  retries:%d  consumer:%s  reason:%s\n<i>%s</i>\n",
				ts, e.RetryCount, html.EscapeString(e.Consumer),
				html.EscapeString(reason), html.EscapeString(errMsg),
			))
		}

		sb.WriteString("\n<i>Use the rescan worker or requeue mechanism to retry events.</i>\n")
		return sb.String(), nil
	}
}
