package operator

import (
	"context"
	"fmt"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
)

// BuildDQBreakdown wraps adapter GetDQBreakdown for the dashboard DQ view.
func BuildDQBreakdown(
	ctx context.Context,
	db database.Adapter,
	windowHours int,
	chain string,
) (*contracts.DQBreakdownResponseDTO, error) {
	chain = normalizeChainFilter(chain)
	windowHours = database.CapDQWindowHours(windowHours)

	raw, err := db.GetDQBreakdown(ctx, windowHours, chain)
	if err != nil {
		return nil, fmt.Errorf("get dq breakdown: %w", err)
	}
	if raw == nil {
		return &contracts.DQBreakdownResponseDTO{
			WindowHours:      windowHours,
			Chain:            chain,
			TopRejectReasons: []contracts.DQRejectReasonDTO{},
		}, nil
	}

	reasons := make([]contracts.DQRejectReasonDTO, 0, len(raw.TopRejectReasons))
	for _, r := range raw.TopRejectReasons {
		reasons = append(reasons, contracts.DQRejectReasonDTO{
			Reason: r.Reason,
			Count:  r.Count,
		})
	}

	return &contracts.DQBreakdownResponseDTO{
		WindowHours:          raw.WindowHours,
		Chain:                raw.Chain,
		TotalDecisions:       raw.TotalDecisions,
		PassCount:            raw.PassCount,
		RiskyPassCount:       raw.RiskyPassCount,
		RejectCount:          raw.RejectCount,
		SkipCount:            raw.SkipCount,
		PassRatePct:          raw.PassRatePct,
		TopRejectReasons:     reasons,
		SocialLinksKnownPct:  raw.SocialLinksKnownPct,
		TotalSupplyKnownPct:  raw.TotalSupplyKnownPct,
		CreatorCountKnownPct: raw.CreatorCountKnownPct,
		HolderDistKnownPct:   raw.HolderDistKnownPct,
		FairChanceSkipCount:  raw.FairChanceSkipCount,
	}, nil
}

// DQTelegramReport holds /dq command data gathered from the adapter.
type DQTelegramReport struct {
	WindowHours            int
	AdaptiveAvailable      bool
	AdaptiveTotalDecisions int
	AdaptiveRugRejects     int
	Detected               int64
	DQPassed               int64
	Rejected               int64
}

// BuildDQTelegramReport gathers adaptive DQ and funnel stats for Telegram /dq.
// Mirrors the data layer previously inlined in cmd/telegram.go buildDqFn.
func BuildDQTelegramReport(ctx context.Context, db database.Adapter, hours int) (*DQTelegramReport, error) {
	if hours <= 0 {
		hours = 24
	}
	hours = database.CapDQWindowHours(hours)

	out := &DQTelegramReport{WindowHours: hours}

	totalDecisions, rugRejects, dqErr := db.GetAdaptiveDQStats(ctx, hours*3600)
	if dqErr == nil {
		out.AdaptiveAvailable = true
		out.AdaptiveTotalDecisions = totalDecisions
		out.AdaptiveRugRejects = rugRejects
	}

	ps, err := db.GetPipelineStats(ctx, hours)
	if err != nil {
		return nil, fmt.Errorf("get pipeline stats: %w", err)
	}
	if ps != nil {
		out.Detected = ps.Detected
		out.DQPassed = ps.DQPassed
		out.Rejected = ps.Rejected
	}
	return out, nil
}
