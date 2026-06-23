package execution_solana

// confirm.go — signature confirmation polling.

import (
	"context"
	"fmt"
	"time"

	"crypto-sniping-bot/internal/app/config"
)

// confirmWithTimeout polls for signature confirmation until the timeout.
// Returns the confirmed slot or an error if timeout is reached.
func confirmWithTimeout(
	ctx context.Context,
	client SolanaClient,
	signature string,
	cfg *config.SolanaExecutionConfig,
) (uint64, error) {
	timeout := time.Duration(cfg.ConfirmTimeoutMs) * time.Millisecond
	pollInterval := time.Duration(cfg.ReceiptPollIntervalMs) * time.Millisecond
	if pollInterval <= 0 {
		pollInterval = 400 * time.Millisecond
	}

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(pollInterval):
		}

		status, err := client.GetSignatureStatus(ctx, signature)
		if err != nil {
			continue // transient error — keep polling
		}
		if status == nil {
			continue // not yet seen
		}
		if status.Err != nil {
			return status.Slot, fmt.Errorf("transaction failed on-chain: %v", status.Err)
		}
		if isConfirmed(status.ConfirmationStatus) {
			return status.Slot, nil
		}
	}
	return 0, fmt.Errorf("confirm timeout after %s for signature %s", timeout, signature)
}

// isConfirmed returns true when the transaction has reached "confirmed" or "finalized".
func isConfirmed(status string) bool {
	return status == "confirmed" || status == "finalized"
}
