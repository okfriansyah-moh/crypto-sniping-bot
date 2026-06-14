package main

import (
	"fmt"
	"strings"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/modules/health"
	"crypto-sniping-bot/internal/operator"
	"crypto-sniping-bot/sniper-bot/internal/telegram"
)

func formatTelegramStatus(
	overview *contracts.OverviewResponseDTO,
	startTime time.Time,
	halted bool,
	haltReason string,
) string {
	haltInfo := ""
	if halted {
		haltInfo = fmt.Sprintf("\n🔴 KILL SWITCH: <code>%s</code>", haltReason)
	}

	base := fmt.Sprintf(
		"<b>Status</b>\n"+
			"Mode: <code>%s</code>\n"+
			"Drawdown (24h): <code>%.2f%%</code>\n"+
			"Open positions: <code>%d</code>\n"+
			"Exposure: <code>$%.2f</code>\n"+
			"Strategy: <code>%s</code>\n"+
			"Running since: <code>%s (%s ago)</code>\n"+
			"Updated: <code>%s</code>%s",
		overview.Mode,
		overview.DrawdownPct*100,
		overview.OpenPositions,
		overview.TotalExposureUsd,
		overview.StrategyVersionID,
		startTime.Format("2006-01-02 15:04:05 UTC"),
		humanDuration(time.Since(startTime)),
		overview.UpdatedAt,
		haltInfo,
	)
	if overview.ShadowGate != nil {
		base += telegram.FormatShadowGateStatus(shadowGateResultFromDTO(overview.ShadowGate))
	}
	return base
}

func shadowGateResultFromDTO(dto *contracts.ShadowGateBlockDTO) health.ShadowGateResult {
	if dto == nil {
		return health.ShadowGateResult{}
	}
	return health.ShadowGateResult{
		Pass:               dto.Pass,
		TradeCount:         dto.TradeCount,
		AggregatePnlBps:    dto.AggregatePnlBps,
		AvgPnlBps:          dto.AvgPnlBps,
		MinTrades:          dto.MinTrades,
		MinWindowDays:      dto.MinWindowDays,
		MinAggregatePnlBps: dto.MinAggregatePnlBps,
		ExecutionMode:      dto.ExecutionMode,
		LiveFlipHint:       dto.LiveFlipHint,
	}
}

func formatTelegramPnL(summary *contracts.PnLSummaryDTO) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>PnL Summary (%dh)</b>\n", summary.LookbackHours))
	fmt.Fprintf(&sb, "Realized: <code>$%+.2f</code>  (W %d / L %d, %.1f%%)\n",
		summary.RealizedPnLUsd, summary.Wins, summary.Losses, summary.WinRatePct)
	fmt.Fprintf(&sb, "Unrealized: <code>$%+.2f</code>  (open exposure $%.2f)\n",
		summary.UnrealizedPnLUsd, summary.OpenExposureUsd)
	fmt.Fprintf(&sb, "Drawdown: <code>%.2f%%</code>\n", summary.DrawdownPct*100)
	fmt.Fprintf(&sb, "Open positions: <code>%d</code>\n", summary.OpenPositions)
	if summary.StuckPositions > 0 {
		fmt.Fprintf(&sb, "⚠️ Stuck (&gt;1h still open): <code>%d</code>  — try /positions or /force_close\n", summary.StuckPositions)
	}
	return sb.String()
}

func formatTelegramPositions(positions []contracts.PositionStateDTO) string {
	if len(positions) == 0 {
		return "No open positions."
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
	return sb.String()
}

func formatTelegramPipeline(stats *database.PipelineStats) string {
	var sb strings.Builder
	sb.WriteString("<b>Pipeline Funnel (last 24h — cumulative)</b>\n\n")

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

	return sb.String()
}

func formatTelegramDQ(report *operator.DQTelegramReport) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<b>Data Quality  (last %dh)</b>\n\n", report.WindowHours))

	if report.AdaptiveAvailable {
		sb.WriteString("<b>Adaptive DQ decisions:</b>\n")
		sb.WriteString(fmt.Sprintf("  Total decisions:  <code>%d</code>\n", report.AdaptiveTotalDecisions))
		sb.WriteString(fmt.Sprintf("  Rug rejects:      <code>%d</code>\n", report.AdaptiveRugRejects))
		rugRate := 0.0
		if report.AdaptiveTotalDecisions > 0 {
			rugRate = float64(report.AdaptiveRugRejects) / float64(report.AdaptiveTotalDecisions) * 100
		}
		sb.WriteString(fmt.Sprintf("  Rug rate:         <code>%.1f%%</code>\n", rugRate))
	}

	sb.WriteString("\n<b>Funnel gate (DQ):</b>\n")
	sb.WriteString(fmt.Sprintf("  Tokens detected:  <code>%d</code>\n", report.Detected))
	sb.WriteString(fmt.Sprintf("  DQ passed:        <code>%d</code>\n", report.DQPassed))
	sb.WriteString(fmt.Sprintf("  Rejected (any):   <code>%d</code>\n", report.Rejected))
	passRate := 0.0
	rejectRate := 0.0
	if report.Detected > 0 {
		passRate = float64(report.DQPassed) / float64(report.Detected) * 100
		rejectRate = float64(report.Rejected) / float64(report.Detected) * 100
	}
	sb.WriteString(fmt.Sprintf("  Pass rate:        <code>%.1f%%</code>\n", passRate))
	sb.WriteString(fmt.Sprintf("  Reject rate:      <code>%.1f%%</code>\n", rejectRate))

	verdict := "✅ Healthy"
	if rejectRate > 80 {
		verdict = "🔴 High reject rate — check thresholds or data feed"
	} else if passRate < 5 && report.Detected > 10 {
		verdict = "⚠️ Very low pass rate — review DQ thresholds"
	}
	sb.WriteString(fmt.Sprintf("\nVerdict: %s\n", verdict))

	return sb.String()
}
