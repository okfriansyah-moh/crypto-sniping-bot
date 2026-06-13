package workers

// run_reconciliation.go — Phase 8 on-chain position reconciliation worker.
// Periodically checks open positions against on-chain token balances and
// adjusts the database state when discrepancies exceed the tolerance.
// This worker is NON-DESTRUCTIVE: it adjusts amounts but never books losses
// directly — that is handled by the learning engine on position close.
// See docs/reference/implementation_roadmap.md § 8.4.

import (
	"context"
	"log/slog"
	"math/big"
	"time"

	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
)

// ReconciliationRPCClient is the minimal on-chain interface required by the
// reconciliation worker. The full rpc.Client interface is a superset of this.
type ReconciliationRPCClient interface {
	// GetTokenBalance returns the ERC-20 balance for wallet on chain as a
	// decimal integer string (no trailing zeros, no decimal point).
	GetTokenBalance(ctx context.Context, walletAddress, tokenAddress, chain string) (*big.Int, error)
}

// ReconCfg carries runtime parameters for the reconciliation worker.
// All values are sourced from config.HardeningConfig — no magic numbers.
type ReconCfg struct {
	IntervalMs   int // polling cadence in milliseconds
	ToleranceBps int // discrepancy threshold in basis points (1 bps = 0.01%)
}

// reconCfgFromConfig converts config.HardeningConfig to ReconCfg with safe defaults.
func reconCfgFromConfig(h config.HardeningConfig) ReconCfg {
	cfg := ReconCfg{
		IntervalMs:   h.ReconciliationIntervalMs,
		ToleranceBps: h.ReconciliationToleranceBps,
	}
	if cfg.IntervalMs <= 0 {
		cfg.IntervalMs = 30_000
	}
	if cfg.ToleranceBps <= 0 {
		cfg.ToleranceBps = 50
	}
	return cfg
}

// RunReconciliation runs the on-chain position reconciliation loop until ctx
// is cancelled. It is designed to be run in a separate goroutine.
//
// Worker discipline:
//  1. On each tick, list all open positions.
//  2. For each position, check kill switch first — stop if halted.
//  3. Fetch on-chain balance via rpcClient.GetTokenBalance.
//  4. Skip on RPC error (non-destructive by design).
//  5. If discrepancy ≤ toleranceBps: noop.
//  6. If on-chain balance is zero: ClosePositionForced.
//  7. Otherwise: AdjustPositionAmount.
func RunReconciliation(
	ctx context.Context,
	adp database.Adapter,
	rpcClient ReconciliationRPCClient,
	cfg *config.Config,
	logger *slog.Logger,
) {
	recon := reconCfgFromConfig(cfg.Hardening)
	ticker := time.NewTicker(time.Duration(recon.IntervalMs) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			logger.Info("reconciliation_worker_stopped", "reason", ctx.Err())
			return
		case <-ticker.C:
			runReconciliationCycle(ctx, adp, rpcClient, recon, logger)
		}
	}
}

// runReconciliationCycle executes one reconciliation pass over all open positions.
func runReconciliationCycle(
	ctx context.Context,
	adp database.Adapter,
	rpcClient ReconciliationRPCClient,
	cfg ReconCfg,
	logger *slog.Logger,
) {
	// Check kill switch before doing any work.
	halted, haltReason, err := adp.IsSystemHalted(ctx)
	if err != nil {
		logger.Error("reconciliation_halt_check_failed", "error", err)
		return
	}
	if halted {
		logger.Warn("reconciliation_skipped_halted", "reason", haltReason)
		return
	}

	positions, err := adp.ListOpenPositionsForReconciliation(ctx)
	if err != nil {
		logger.Error("reconciliation_list_positions_failed", "error", err)
		return
	}

	for _, p := range positions {
		if p.WalletAddress == "" || p.TokenAddress == "" {
			logger.Warn("reconciliation_skipping_missing_fields",
				"position_id", p.PositionID,
				"wallet_address_empty", p.WalletAddress == "",
				"token_address_empty", p.TokenAddress == "",
			)
			continue
		}

		onchain, err := rpcClient.GetTokenBalance(ctx, p.WalletAddress, p.TokenAddress, p.Chain)
		if err != nil {
			logger.Warn("reconciliation_rpc_error",
				"position_id", p.PositionID,
				"token_address", p.TokenAddress,
				"error", err,
			)
			continue // non-destructive: skip on RPC error
		}

		reconcilePosition(ctx, adp, p, onchain, cfg, logger)
	}
}

// reconcilePosition applies the reconciliation decision for a single position.
func reconcilePosition(
	ctx context.Context,
	adp database.Adapter,
	p database.ReconciliationPosition,
	onchain *big.Int,
	cfg ReconCfg,
	logger *slog.Logger,
) {
	if onchain == nil {
		onchain = big.NewInt(0)
	}

	if onchain.Sign() == 0 {
		// On-chain balance is zero — position is gone, force close.
		logger.Info("reconciliation_close_forced",
			"position_id", p.PositionID,
			"reason", "onchain_zero",
		)
		if err := adp.ClosePositionForced(ctx, p.PositionID, "onchain_zero"); err != nil {
			logger.Error("reconciliation_close_forced_failed",
				"position_id", p.PositionID,
				"error", err,
			)
		}
		return
	}

	// Parse the recorded amount; if empty, treat as zero.
	var dbAmount *big.Int
	if p.AmountRaw != "" {
		dbAmount = new(big.Int)
		if _, ok := dbAmount.SetString(p.AmountRaw, 10); !ok {
			dbAmount = big.NewInt(0)
		}
	} else {
		dbAmount = big.NewInt(0)
	}

	if dbAmount.Sign() == 0 {
		// No recorded amount — just update with on-chain value.
		if err := adp.AdjustPositionAmount(ctx, p.PositionID, onchain.String(), "initial_reconciliation"); err != nil {
			logger.Error("reconciliation_adjust_failed",
				"position_id", p.PositionID, "error", err)
		}
		return
	}

	if !exceedsToleranceBps(dbAmount, onchain, cfg.ToleranceBps) {
		// Within tolerance — no action needed.
		return
	}

	reason := "reconciliation"
	logger.Info("reconciliation_adjust",
		"position_id", p.PositionID,
		"db_amount", dbAmount.String(),
		"onchain_amount", onchain.String(),
		"tolerance_bps", cfg.ToleranceBps,
	)
	if err := adp.AdjustPositionAmount(ctx, p.PositionID, onchain.String(), reason); err != nil {
		logger.Error("reconciliation_adjust_failed",
			"position_id", p.PositionID,
			"error", err,
		)
	}
}

// ExceedsToleranceBps reports whether |db - onchain| / db > toleranceBps / 10000.
// Uses integer arithmetic to avoid float precision issues.
// Exported for testability.
func ExceedsToleranceBps(db, onchain *big.Int, toleranceBps int) bool {
	return exceedsToleranceBps(db, onchain, toleranceBps)
}

// exceedsToleranceBps is the internal implementation.
func exceedsToleranceBps(db, onchain *big.Int, toleranceBps int) bool {
	if db.Sign() == 0 {
		return onchain.Sign() != 0
	}
	diff := new(big.Int).Sub(db, onchain)
	diff.Abs(diff)

	// diff * 10000 > db * toleranceBps
	lhs := new(big.Int).Mul(diff, big.NewInt(10000))
	rhs := new(big.Int).Mul(db, big.NewInt(int64(toleranceBps)))
	return lhs.Cmp(rhs) > 0
}
