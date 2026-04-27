package ingestion_solana

// gap_recovery.go — slot gap recovery for Solana ingestion.
// On reconnect, fetches signatures for a program within the missed slot range
// and re-processes them in chronological order.

import (
	"context"
	"fmt"
	"log/slog"

	"crypto-sniping-bot/internal/app/config"
)

const gapRecoveryBatchSize = 100

// RecoverGap fetches and re-emits events for slots [fromSlot+1, toSlot].
// Returns the highest slot successfully processed, or fromSlot on error.
//
// The function is intentionally conservative: it only re-emits PoolCreated /
// PumpFunCreate events (the ones that matter for Layer 1+). Swap events in
// the gap are skipped — they are not actionable for tokens not yet in the
// pipeline.
func RecoverGap(
	ctx context.Context,
	client SolanaRPCClient,
	prog config.SolanaProgramConfig,
	fromSlot, toSlot uint64,
	versionID string,
	emit EventEmitter,
	maxSlots uint64,
	logger *slog.Logger,
) (uint64, error) {
	if toSlot <= fromSlot {
		return fromSlot, nil
	}
	gapSize := toSlot - fromSlot
	if gapSize > maxSlots {
		logger.Warn("solana_gap_recovery_truncated",
			"program", prog.ProgramID,
			"from_slot", fromSlot,
			"to_slot", toSlot,
			"max_slots", maxSlots,
		)
		fromSlot = toSlot - maxSlots
	}

	logger.Info("solana_gap_recovery_start",
		"program", prog.ProgramID,
		"family", prog.Family,
		"from_slot", fromSlot,
		"to_slot", toSlot,
	)

	sigs, err := client.GetSignaturesForAddress(ctx, prog.ProgramID, fromSlot, toSlot, gapRecoveryBatchSize)
	if err != nil {
		return fromSlot, fmt.Errorf("gap_recovery get_signatures: %w", err)
	}

	// Signatures arrive newest-first; reverse to process oldest first.
	for i, j := 0, len(sigs)-1; i < j; i, j = i+1, j-1 {
		sigs[i], sigs[j] = sigs[j], sigs[i]
	}

	highWater := fromSlot
	for _, sig := range sigs {
		select {
		case <-ctx.Done():
			return highWater, ctx.Err()
		default:
		}

		tx, err := client.GetTransaction(ctx, sig)
		if err != nil || tx == nil {
			continue
		}

		for _, instr := range tx.Instructions {
			if instr.ProgramID != prog.ProgramID {
				continue
			}
			var emitErr error
			switch prog.Family {
			case "pumpfun":
				d, _ := NormalizePumpFunCreate(tx, instr, versionID)
				if d != nil {
					emitErr = emit(ctx, *d)
				}
			case "raydium-v4":
				d, _ := NormalizeRaydiumPoolInit(tx, instr, versionID)
				if d != nil {
					emitErr = emit(ctx, *d)
				}
			}
			if emitErr != nil {
				logger.Warn("solana_gap_recovery_emit_error",
					"signature", sig,
					"error", emitErr,
				)
			}
		}

		if tx.Slot > highWater {
			highWater = tx.Slot
		}
	}

	logger.Info("solana_gap_recovery_done",
		"program", prog.ProgramID,
		"high_water_slot", highWater,
		"recovered_txs", len(sigs),
	)
	return highWater, nil
}
