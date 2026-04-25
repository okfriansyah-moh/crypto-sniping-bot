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
	cfg *config.PositionConfig
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
	return &Module{cfg: cfg}
}

// OpenPosition creates the initial position snapshot from an ExecutionResultDTO.
// PositionID = SHA256(execution_id)[:16].
func (m *Module) OpenPosition(
	_ context.Context,
	in contracts.ExecutionResultDTO,
	chain string,
	tokenAddress string,
) (contracts.PositionStateDTO, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	if !in.Success {
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

		Status:         "open",
		EntryPrice:     in.RealizedEntryPrice,
		EntrySizeUsd:   0, // populated by worker from AllocationDTO
		CurrentPrice:   in.RealizedEntryPrice,

		Tp1Bps:         m.cfg.Tp1Bps,
		Tp2Bps:         m.cfg.Tp2Bps,
		SlBps:          m.cfg.SlBps,
		MaxHoldSeconds: m.cfg.MaxHoldSeconds,

		OpenedAt:   now,
		SnapshotAt: now,
	}, nil
}

// PollExit checks whether the position should be exited and emits an updated snapshot.
// Returns the updated PositionStateDTO with ExitReason set if exit is triggered.
func (m *Module) PollExit(
	_ context.Context,
	pos contracts.PositionStateDTO,
	currentPriceStr string,
) (contracts.PositionStateDTO, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	// Update snapshot price.
	updated := pos
	updated.CurrentPrice = currentPriceStr
	updated.SnapshotAt = now

	entryPrice, err := parsePrice(pos.EntryPrice)
	if err != nil || entryPrice == 0 {
		// Can't evaluate without entry price; emit price-only update.
		updated.EventID = contracts.ContentIDFromString(fmt.Sprintf("pos-snap:%s:%s", pos.PositionID, now))
		return updated, nil
	}

	currentPrice, err := parsePrice(currentPriceStr)
	if err != nil || currentPrice == 0 {
		updated.EventID = contracts.ContentIDFromString(fmt.Sprintf("pos-snap:%s:%s", pos.PositionID, now))
		return updated, nil
	}

	pricePct := (currentPrice - entryPrice) / entryPrice

	// TP2 check (higher threshold — must be evaluated before TP1).
	tp2Threshold := float64(pos.Tp2Bps) / 10000.0
	if pricePct >= tp2Threshold {
		return m.buildExit(pos, currentPriceStr, currentPrice, entryPrice, "TP2", now), nil
	}

	// TP1 check.
	tp1Threshold := float64(pos.Tp1Bps) / 10000.0
	if pricePct >= tp1Threshold {
		return m.buildExit(pos, currentPriceStr, currentPrice, entryPrice, "TP1", now), nil
	}

	// Stop-loss check.
	slThreshold := -float64(pos.SlBps) / 10000.0
	if pricePct <= slThreshold {
		return m.buildExit(pos, currentPriceStr, currentPrice, entryPrice, "SL", now), nil
	}

	// Time-based exit.
	if pos.OpenedAt != "" {
		openedAt, parseErr := time.Parse(time.RFC3339Nano, pos.OpenedAt)
		if parseErr == nil {
			age := time.Since(openedAt)
			if age >= time.Duration(pos.MaxHoldSeconds)*time.Second {
				return m.buildExit(pos, currentPriceStr, currentPrice, entryPrice, "TIME", now), nil
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
	pnlPct := (currentPrice - entryPrice) / entryPrice
	pnlUsd := pos.EntrySizeUsd * pnlPct

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

		Status:         "exited",
		EntryPrice:     pos.EntryPrice,
		EntrySizeUsd:   pos.EntrySizeUsd,
		CurrentPrice:   currentPriceStr,
		ExitPrice:      currentPriceStr,
		ExitReason:     reason,
		PnlUsd:         math.Round(pnlUsd*100) / 100,
		PnlPct:         math.Round(pnlPct*10000) / 10000,

		Tp1Bps:         pos.Tp1Bps,
		Tp2Bps:         pos.Tp2Bps,
		SlBps:          pos.SlBps,
		MaxHoldSeconds: pos.MaxHoldSeconds,

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
