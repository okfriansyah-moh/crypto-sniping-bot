package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/operator"
)

const operatorCommandWorkerID = "operator_command_worker"

// RunOperatorCommands consumes operator_command_event rows and applies mode/kill/resume/force_close.
func RunOperatorCommands(ctx context.Context, db database.Adapter, logger *slog.Logger) error {
	if logger == nil {
		logger = slog.Default()
	}
	logger.Info("operator_command_worker_started")
	for {
		select {
		case <-ctx.Done():
			logger.Info("operator_command_worker_stopped")
			return ctx.Err()
		default:
		}

		evt, err := db.ClaimNextEvent(ctx, operatorCommandWorkerID, []string{contracts.OperatorCommandEventType})
		if err != nil {
			logger.Warn("operator_command_claim_failed", "error", err)
			if sleepOrDone(ctx, 2*time.Second) {
				return ctx.Err()
			}
			continue
		}
		if evt == nil {
			if sleepOrDone(ctx, 500*time.Millisecond) {
				return ctx.Err()
			}
			continue
		}

		if err := processOperatorCommandEvent(ctx, db, logger, evt); err != nil {
			logger.Warn("operator_command_process_failed",
				"event_id", evt.EventID,
				"error", err,
			)
			if releaseErr := db.ReleaseEventClaim(ctx, evt.EventID); releaseErr != nil {
				logger.Warn("operator_command_release_failed",
					"event_id", evt.EventID,
					"error", releaseErr,
				)
			}
			continue
		}

		if markErr := db.MarkEventProcessed(ctx, evt.EventID); markErr != nil {
			logger.Warn("operator_command_mark_failed",
				"event_id", evt.EventID,
				"error", markErr,
			)
		}
	}
}

func processOperatorCommandEvent(ctx context.Context, db database.Adapter, logger *slog.Logger, evt *database.Event) error {
	if evt == nil {
		return fmt.Errorf("nil event")
	}
	var cmd contracts.OperatorCommandDTO
	if err := json.Unmarshal(evt.Payload, &cmd); err != nil {
		return fmt.Errorf("unmarshal operator command: %w", err)
	}
	if err := operator.ExecuteCommand(ctx, db, logger, cmd, operator.CommandSourceDashboard); err != nil {
		return err
	}
	logger.Info("operator_command_applied",
		"event_id", evt.EventID,
		"command_id", cmd.CommandID,
		"command_type", cmd.CommandType,
		"issuer_id", cmd.IssuerID,
	)
	return nil
}

func sleepOrDone(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return true
	case <-timer.C:
		return false
	}
}
