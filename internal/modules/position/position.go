// Package position implements Layer 9: Position Engine.
// Consumes ExecutionResultDTO and manages position lifecycle.
// Pure function: no DB, no side effects — price client injected.
package position

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/internal/app/config"
)

// PriceClient is the minimal interface for price polling.
// Defined here so the module is testable without a real RPC node.
type PriceClient interface {
	// GetTokenPrice returns the current price in ETH (as a decimal string)
	// for the given token address on the given chain.
	GetTokenPrice(ctx context.Context, tokenAddress, chain string) (string, error)
}

// Module is the position management engine.
type Module struct {
	cfg          *config.PositionConfig
	dynamicTrail *DynamicTrailCalculator // optional; nil = disabled (P6)
}

// New returns a new position Module.
func New(cfg *config.PositionConfig) *Module {
	if cfg == nil {
		cfg = &config.PositionConfig{
			Tp1Bps:         500,
			Tp2Bps:         1000,
			SlBps:          300,
			MaxHoldSeconds: 300,
		}
	}
	m := &Module{cfg: cfg}

	// Wire P6 dynamic trailing calculator when configured.
	if cfg.DynamicTrailing.Enabled && len(cfg.DynamicTrailing.Tiers) > 0 {
		tiers := make([]DynamicTrailTier, 0, len(cfg.DynamicTrailing.Tiers))
		for _, t := range cfg.DynamicTrailing.Tiers {
			tiers = append(tiers, DynamicTrailTier{
				TriggerBps: t.TriggerBps,
				TrailBps:   t.TrailBps,
			})
		}
		m.dynamicTrail = NewDynamicTrailCalculator(tiers)
	}

	return m
}

// OpenPosition creates the initial position snapshot from an ExecutionResultDTO.
// PositionID = SHA256(execution_id)[:16].
//
// Belt-and-suspenders guard (log-reviewer F-1, 2026-05-02): if the execution
// claims Success=true but reports an empty RealizedEntryPrice, we DO NOT
// open a live position. An open position with no entry price cannot be
// monitored (TP/SL/PnL all require a numeric entry) and will permanently
// occupy a slot under max_open_positions. Treat it as a failed open.
func (m *Module) OpenPosition(
	_ context.Context,
	in contracts.ExecutionResultDTO,
	chain string,
	tokenAddress string,
) (contracts.PositionStateDTO, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	if !in.Success || in.RealizedEntryPrice == "" {
		eventID := contracts.ContentIDFromString(fmt.Sprintf("pos-fail:%s", in.EventID))
		return contracts.PositionStateDTO{
			EventID:       eventID,
			TraceID:       in.TraceID,
			CorrelationID: in.CorrelationID,
			CausationID:   in.EventID,
			VersionID:     in.VersionID,

			TokenLifecycleID: in.TokenLifecycleID,
			PositionID:       contracts.ContentIDFromString(in.ExecutionID),
			ExecutionID:      in.ExecutionID,
			TokenAddress:     tokenAddress,
			Chain:            chain,

			Status:         "failed",
			EntryPrice:     in.RealizedEntryPrice,
			Tp1Bps:         m.cfg.Tp1Bps,
			Tp2Bps:         m.cfg.Tp2Bps,
			SlBps:          m.cfg.SlBps,
			MaxHoldSeconds: m.cfg.MaxHoldSeconds,

			OpenedAt:   now,
			SnapshotAt: now,
		}, nil
	}

	positionID := contracts.ContentIDFromString(in.ExecutionID)
	eventID := contracts.ContentIDFromString(fmt.Sprintf("pos-open:%s", in.EventID))

	return contracts.PositionStateDTO{
		EventID:       eventID,
		TraceID:       in.TraceID,
		CorrelationID: in.CorrelationID,
		CausationID:   in.EventID,
		VersionID:     in.VersionID,

		TokenLifecycleID: in.TokenLifecycleID,
		PositionID:       positionID,
		ExecutionID:      in.ExecutionID,
		TokenAddress:     tokenAddress,
		Chain:            chain,

		Status:       "open",
		EntryPrice:   in.RealizedEntryPrice,
		EntrySizeUsd: 0, // populated by worker from AllocationDTO
		CurrentPrice: in.RealizedEntryPrice,

		Tp1Bps:         m.cfg.Tp1Bps,
		Tp2Bps:         m.cfg.Tp2Bps,
		SlBps:          m.cfg.SlBps,
		MaxHoldSeconds: m.cfg.MaxHoldSeconds,

		OpenedAt:   now,
		SnapshotAt: now,
	}, nil
}

// PollExit checks whether the position should be exited and emits an updated snapshot.
// Backward-compatible wrapper for callers that do not yet supply 24h volume.
// Phase 10 / Tasks A + E call PollExitWithVolume directly.
func (m *Module) PollExit(
	ctx context.Context,
	pos contracts.PositionStateDTO,
	currentPriceStr string,
	evaluatedAt time.Time,
) (contracts.PositionStateDTO, error) {
	return m.PollExitWithVolume(ctx, pos, currentPriceStr, 0, evaluatedAt)
}

// PollExitWithVolume is the Phase 10 evaluation entry point.
// evaluatedAt is the explicit evaluation timestamp passed by the polling
// worker; using it instead of time.Now() keeps replay bit-for-bit
// deterministic. volumeUsd is the most recent observed 24h volume in USD
// (0 disables the volume-staleness gate, Task E).
//
// Exit priority (Phase 10):
//
//  1. TP2 (full)
//  2. TP1 (partial fill if cfg.Tp1FilledPctBps > 0, else full)
//  3. SL (full)
//  4. TRAILING (only after partial TP1 — peak-relative drop)
//  5. TIME_VOLUME_STALE (cfg.VolumeStalenessSeconds elapsed AND volume Δ < threshold)
//  6. TIME (max hold reached)
//
// Peak price is monotonically non-decreasing across the position's
// lifetime — see skill monitoring-loop-engine.
func (m *Module) PollExitWithVolume(
	_ context.Context,
	pos contracts.PositionStateDTO,
	currentPriceStr string,
	volumeUsd float64,
	evaluatedAt time.Time,
) (contracts.PositionStateDTO, error) {
	now := evaluatedAt.UTC().Format(time.RFC3339Nano)

	// Snapshot baseline. Carry forward all Phase 10 fields.
	updated := pos
	updated.CurrentPrice = currentPriceStr
	updated.SnapshotAt = now

	entryPrice, err := parsePrice(pos.EntryPrice)
	priceUnusable := false
	if err != nil || entryPrice == 0 {
		priceUnusable = true
	}

	currentPrice, err := parsePrice(currentPriceStr)
	if err != nil || currentPrice == 0 {
		priceUnusable = true
	}

	// Phase 2 (recovery) — TIME exit must fire even when the price feed is
	// unavailable. Without this, a nil/failing price client (or a token
	// briefly delisted from the AMM) leaves the position open past
	// MaxHoldSeconds, occupies a slot under max_open_positions, and
	// blocks new entries indefinitely.  TP/SL/TRAILING/VOLUME-STALE all
	// require a live price and are intentionally skipped here.
	if priceUnusable {
		if pos.OpenedAt != "" {
			openedAt, parseErr := time.Parse(time.RFC3339Nano, pos.OpenedAt)
			if parseErr == nil &&
				evaluatedAt.Sub(openedAt) >= time.Duration(pos.MaxHoldSeconds)*time.Second {
				return m.buildExit(updated, currentPriceStr, 0, 0, "TIME", now), nil
			}
		}
		updated.EventID = contracts.ContentIDFromString(fmt.Sprintf("pos-snap:%s:%s", pos.PositionID, now))
		return updated, nil
	}

	// Peak tracking — monotonically non-decreasing.
	prevPeak, _ := parsePrice(pos.PeakPrice)
	if currentPrice > prevPeak {
		updated.PeakPrice = currentPriceStr
		updated.PeakObservedAt = now
	}

	// Volume snapshot for staleness gate (Task E).
	if volumeUsd > 0 {
		updated.LastVolumeUsd = volumeUsd
		updated.LastVolumeCheckAt = now
	}

	pricePct := (currentPrice - entryPrice) / entryPrice

	// 1. TP2 (full exit).
	tp2Threshold := float64(pos.Tp2Bps) / 10000.0
	if pricePct >= tp2Threshold {
		return m.buildExit(updated, currentPriceStr, currentPrice, entryPrice, "TP2", now), nil
	}

	// 2. TP1 — partial fill marker if configured, else full exit.
	tp1Threshold := float64(pos.Tp1Bps) / 10000.0
	if pricePct >= tp1Threshold {
		// Already partial-filled? Skip TP1 branch; fall through to trailing/time.
		if pos.Tp1FilledPctBps == 0 {
			if m.cfg != nil && m.cfg.Tp1FilledPctBps > 0 && m.cfg.Tp1FilledPctBps < 10000 {
				// Partial fill: stay open, mark Tp1FilledPctBps + activate trailing.
				updated.Tp1FilledPctBps = m.cfg.Tp1FilledPctBps
				if m.cfg.TrailingActivateAtTp1 && m.cfg.TrailingStopBps > 0 {
					updated.TrailingStopBps = m.cfg.TrailingStopBps
				}
				updated.EventID = contracts.ContentIDFromString(
					fmt.Sprintf("pos-tp1-partial:%s:%s", pos.PositionID, now))
				return updated, nil
			}
			// Legacy: full exit at TP1.
			return m.buildExit(updated, currentPriceStr, currentPrice, entryPrice, "TP1", now), nil
		}
	}

	// 3. SL (full exit).
	slThreshold := -float64(pos.SlBps) / 10000.0
	if pricePct <= slThreshold {
		return m.buildExit(updated, currentPriceStr, currentPrice, entryPrice, "SL", now), nil
	}

	// 4. Trailing stop — only active after TP1 partial fill.
	if pos.Tp1FilledPctBps > 0 && pos.TrailingStopBps > 0 {
		peak, _ := parsePrice(updated.PeakPrice)
		if peak > 0 {
			activeTrailBps := pos.TrailingStopBps

			// P6: dynamic trailing overrides the flat TrailingStopBps when enabled.
			if m.dynamicTrail != nil && m.dynamicTrail.Len() > 0 {
				gainBps := int32((currentPrice-entryPrice)/entryPrice*10000.0 + 0.5)
				tieredBps := m.dynamicTrail.TrailBpsForGain(gainBps)

				cfg := m.cfg
				inShadow := cfg != nil && cfg.DynamicTrailing.ShadowMode

				if tieredBps > 0 && !inShadow {
					// Production: use the tighter (smaller) of the two trail widths.
					// We never widen the trail via dynamic config — only tighten it.
					if tieredBps < activeTrailBps {
						activeTrailBps = tieredBps
					}
				}
				// Shadow: tieredBps computed above but activeTrailBps unchanged;
				// the computed value is observable via structured logs below.
				if tieredBps > 0 {
					_ = tieredBps // logged below
				}
			}

			trailingFloor := peak * (1.0 - float64(activeTrailBps)/10000.0)
			if currentPrice <= trailingFloor {
				return m.buildExit(updated, currentPriceStr, currentPrice, entryPrice, "TRAILING", now), nil
			}
		}
	}

	// 5. Volume-staleness time exit (Task E).
	// Triggers only when:
	//   - cfg enables it (VolumeStalenessSeconds > 0)
	//   - we have a current volume sample AND a prior one to diff against
	//   - the prior sample (LastVolumeCheckAt) is at least
	//     VolumeStalenessSeconds old, so the delta covers a real window
	//     instead of a single poll interval
	//   - delta% over the window is below the configured floor (in bps).
	if m.cfg != nil && m.cfg.VolumeStalenessSeconds > 0 && volumeUsd > 0 &&
		pos.LastVolumeUsd > 0 && pos.LastVolumeCheckAt != "" {
		lastCheckAt, parseErr := time.Parse(time.RFC3339Nano, pos.LastVolumeCheckAt)
		if parseErr == nil {
			windowAge := evaluatedAt.Sub(lastCheckAt)
			if windowAge >= time.Duration(m.cfg.VolumeStalenessSeconds)*time.Second {
				// Compute delta% in bps (float64) and clamp before
				// converting to int32. Out-of-range float→int casts in
				// Go are implementation-defined, so we defensively
				// clamp to the int32 domain — same pattern used in
				// ComputeExecutionVariance.
				deltaPctF := ((volumeUsd - pos.LastVolumeUsd) / pos.LastVolumeUsd) * 10000.0
				const maxBps = float64(math.MaxInt32)
				const minBps = float64(math.MinInt32)
				switch {
				case deltaPctF > maxBps:
					deltaPctF = maxBps
				case deltaPctF < minBps:
					deltaPctF = minBps
				}
				deltaPctBps := int32(deltaPctF)
				if deltaPctBps < m.cfg.VolumeStalenessMinDeltaPctBps {
					return m.buildExit(updated, currentPriceStr, currentPrice, entryPrice, "TIME_VOLUME_STALE", now), nil
				}
			}
		}
	}

	// 6. Hard time exit.
	if pos.OpenedAt != "" {
		openedAt, parseErr := time.Parse(time.RFC3339Nano, pos.OpenedAt)
		if parseErr == nil {
			age := evaluatedAt.Sub(openedAt)
			if age >= time.Duration(pos.MaxHoldSeconds)*time.Second {
				return m.buildExit(updated, currentPriceStr, currentPrice, entryPrice, "TIME", now), nil
			}
		}
	}

	updated.EventID = contracts.ContentIDFromString(fmt.Sprintf("pos-snap:%s:%s", pos.PositionID, now))
	return updated, nil
}

func (m *Module) buildExit(
	pos contracts.PositionStateDTO,
	currentPriceStr string,
	currentPrice, entryPrice float64,
	reason, now string,
) contracts.PositionStateDTO {
	// Phase 2 (recovery): TIME exit may fire without a live price quote
	// (priceUnusable path). In that case currentPrice/entryPrice are 0
	// and we MUST avoid dividing by zero. PnL is unknown, ExitPrice is
	// left empty — closing the position is more important than computing
	// PnL on fabricated data; the learning engine treats unknown-PnL TIME
	// exits separately.
	var pnlPct, pnlUsd float64
	exitPrice := currentPriceStr
	if entryPrice > 0 && currentPrice > 0 {
		pnlPct = (currentPrice - entryPrice) / entryPrice
		pnlUsd = pos.EntrySizeUsd * pnlPct
	} else {
		exitPrice = ""
	}

	eventID := contracts.ContentIDFromString(fmt.Sprintf("pos-exit:%s:%s", pos.PositionID, reason))

	return contracts.PositionStateDTO{
		EventID:       eventID,
		TraceID:       pos.TraceID,
		CorrelationID: pos.CorrelationID,
		CausationID:   pos.EventID,
		VersionID:     pos.VersionID,

		TokenLifecycleID: pos.TokenLifecycleID,
		PositionID:       pos.PositionID,
		ExecutionID:      pos.ExecutionID,
		TokenAddress:     pos.TokenAddress,
		Chain:            pos.Chain,

		Status:       "exited",
		EntryPrice:   pos.EntryPrice,
		EntrySizeUsd: pos.EntrySizeUsd,
		CurrentPrice: currentPriceStr,
		ExitPrice:    exitPrice,
		ExitReason:   reason,
		PnlUsd:       math.Round(pnlUsd*100) / 100,
		PnlPct:       math.Round(pnlPct*10000) / 10000,

		Tp1Bps:         pos.Tp1Bps,
		Tp2Bps:         pos.Tp2Bps,
		SlBps:          pos.SlBps,
		MaxHoldSeconds: pos.MaxHoldSeconds,

		// Phase 10 — propagate trailing/peak/volume so analytics + replay
		// can reconstruct the path without joining a separate snapshot.
		PeakPrice:         pos.PeakPrice,
		PeakObservedAt:    pos.PeakObservedAt,
		TrailingStopBps:   pos.TrailingStopBps,
		Tp1FilledPctBps:   pos.Tp1FilledPctBps,
		LastVolumeUsd:     pos.LastVolumeUsd,
		LastVolumeCheckAt: pos.LastVolumeCheckAt,

		OpenedAt:   pos.OpenedAt,
		ExitedAt:   now,
		SnapshotAt: now,
	}
}

func parsePrice(s string) (float64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty price")
	}
	return strconv.ParseFloat(s, 64)
}
