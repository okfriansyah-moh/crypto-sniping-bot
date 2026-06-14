package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
)

// emitOperatorCommand inserts operator_command_event on the append-only bus (idempotent).
func emitOperatorCommand(ctx context.Context, db database.Adapter, cmd contracts.OperatorCommandDTO) error {
	if err := cmd.Validate(); err != nil {
		return err
	}
	payload, err := json.Marshal(cmd)
	if err != nil {
		return fmt.Errorf("marshal operator command: %w", err)
	}

	versionID, err := activeVersionID(ctx, db)
	if err != nil {
		return err
	}

	evt := database.Event{
		EventID:       cmd.CommandID,
		EventType:     contracts.OperatorCommandEventType,
		Payload:       payload,
		TraceID:       cmd.CommandID,
		CorrelationID: cmd.CommandID,
		CausationID:   nil,
		VersionID:     versionID,
		CreatedAt:     cmd.Timestamp,
		Consumer:      "operator_command_worker",
	}
	return db.InsertEvent(ctx, evt)
}

func activeVersionID(ctx context.Context, db database.Adapter) (string, error) {
	sv, err := db.GetActiveStrategyVersion(ctx)
	if err != nil {
		return "", fmt.Errorf("active strategy version: %w", err)
	}
	if sv == nil || sv.StrategyVersionID == "" {
		return "", fmt.Errorf("active strategy version not found")
	}
	return sv.StrategyVersionID, nil
}

func buildCommand(commandType, issuerID, confirmToken string, args map[string]string, now time.Time) (contracts.OperatorCommandDTO, error) {
	ts := now.UTC().Format(time.RFC3339Nano)
	return contracts.NewOperatorCommandDTO(commandType, issuerID, confirmToken, ts, args)
}
