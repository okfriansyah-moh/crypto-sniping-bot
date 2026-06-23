package operator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
)

const defaultExecutionLogLimit = 20

// BuildExecutionLog wraps GetExecutionLog into dashboard execution rows.
func BuildExecutionLog(
	ctx context.Context,
	db database.Adapter,
	cfg *config.Config,
	limit int,
) (*contracts.ExecutionsResponseDTO, error) {
	if limit <= 0 {
		limit = defaultExecutionLogLimit
	}
	rows, err := db.GetExecutionLog(ctx, limit)
	if err != nil {
		return nil, fmt.Errorf("get execution log: %w", err)
	}

	shadowMode := cfg != nil && cfg.Execution.Mode == "shadow"
	out := &contracts.ExecutionsResponseDTO{
		Rows: make([]contracts.ExecutionRowDTO, 0, len(rows)),
	}
	for _, r := range rows {
		token := r.Symbol
		if token == "" {
			token = r.TokenAddress
		}
		status := r.Status
		if status == "" {
			status = r.LifecycleState
		}
		out.Rows = append(out.Rows, contracts.ExecutionRowDTO{
			ExecutionID: executionIDFromRow(r),
			Token:       token,
			Status:      status,
			Shadow:      shadowMode || r.Status == "simulated",
			TxHash:      r.TxHash,
			Timestamp:   r.UpdatedAt,
		})
	}
	return out, nil
}

func executionIDFromRow(r database.ExecutionLogRow) string {
	if r.TxHash != "" {
		return r.TxHash
	}
	raw := r.TokenAddress + "|" + r.UpdatedAt
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:8])
}
