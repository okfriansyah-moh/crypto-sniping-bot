package operator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
)

// CommandSource identifies whether a command originated from Telegram or the dashboard.
type CommandSource string

const (
	CommandSourceTelegram  CommandSource = "telegram"
	CommandSourceDashboard CommandSource = "dashboard"
)

// ForceCloseResult summarizes positions closed by ForceClosePositions.
type ForceCloseResult struct {
	ClosedPositionIDs []string
	TokenAddress      string
	Chain             string
}

// ExecuteCommand dispatches a validated operator command to the same code paths as Telegram.
func ExecuteCommand(ctx context.Context, db database.Adapter, logger *slog.Logger, cmd contracts.OperatorCommandDTO, source CommandSource) error {
	if err := cmd.Validate(); err != nil {
		return err
	}
	if cmd.IsDestructive() && strings.TrimSpace(cmd.ConfirmToken) == "" {
		return fmt.Errorf("operator command: confirm_token required for %s", cmd.CommandType)
	}

	issuer := strings.TrimSpace(cmd.IssuerID)
	switch cmd.CommandType {
	case contracts.CommandTypeMode:
		mode := strings.ToUpper(strings.TrimSpace(cmd.Args["mode"]))
		return ExecuteMode(ctx, db, logger, mode, issuer, source)
	case contracts.CommandTypeKill:
		return ExecuteKill(ctx, db, logger, issuer, source)
	case contracts.CommandTypeResume:
		return ExecuteResume(ctx, db, logger, issuer, source)
	case contracts.CommandTypeForceClose:
		target := strings.TrimSpace(cmd.Args["position_id"])
		if target == "" {
			target = strings.TrimSpace(cmd.Args["token_address"])
		}
		if target == "" {
			return fmt.Errorf("operator command: position_id or token_address required")
		}
		_, err := ForceClosePositions(ctx, db, logger, issuer, target, cmd.Timestamp, source, cmd.CommandID)
		return err
	default:
		return fmt.Errorf("operator command: unknown command_type %q", cmd.CommandType)
	}
}

// ExecuteMode switches operational mode (same CAS path as Telegram /mode).
func ExecuteMode(ctx context.Context, db database.Adapter, logger *slog.Logger, mode, issuer string, source CommandSource) error {
	mode = strings.ToUpper(strings.TrimSpace(mode))
	if mode == "" {
		return fmt.Errorf("mode required")
	}

	current, err := db.GetSystemState(ctx)
	if err != nil {
		return fmt.Errorf("get system state: %w", err)
	}

	var newState contracts.SystemStateDTO
	var expectedVersion int64
	if current != nil {
		newState = *current
		expectedVersion = current.StateVersion
	}
	newState.Mode = mode
	newState.LastTransitionReason = transitionReason(source)

	if _, err := db.UpsertSystemState(ctx, newState, expectedVersion); err != nil {
		return fmt.Errorf("update system mode: %w", err)
	}

	logger.Info("operator_mode_changed",
		"mode", mode,
		"issuer_id", issuer,
		"source", string(source),
	)
	return nil
}

// ExecuteKill activates the global kill switch.
func ExecuteKill(ctx context.Context, db database.Adapter, logger *slog.Logger, issuer string, source CommandSource) error {
	reason := killReason(source)
	operatorTag := haltOperatorTag(source, issuer)
	if err := db.SetSystemHalt(ctx, true, reason, operatorTag); err != nil {
		return fmt.Errorf("set kill switch: %w", err)
	}
	logger.Info("operator_kill_executed",
		"kill_switch", true,
		"issuer_id", issuer,
		"source", string(source),
	)
	return nil
}

// ExecuteResume clears the global kill switch.
func ExecuteResume(ctx context.Context, db database.Adapter, logger *slog.Logger, issuer string, source CommandSource) error {
	reason := resumeReason(source)
	operatorTag := haltOperatorTag(source, issuer)
	if err := db.SetSystemHalt(ctx, false, reason, operatorTag); err != nil {
		return fmt.Errorf("clear kill switch: %w", err)
	}
	logger.Info("operator_resume_executed",
		"kill_switch", false,
		"issuer_id", issuer,
		"source", string(source),
	)
	return nil
}

// ForceClosePositions marks matching open positions as MANUAL exited.
// When commandID is set (dashboard replay path), event IDs are derived from commandID for idempotency.
func ForceClosePositions(
	ctx context.Context,
	db database.Adapter,
	logger *slog.Logger,
	issuer, idOrAddr, snapshotAt string,
	source CommandSource,
	commandID string,
) (ForceCloseResult, error) {
	idOrAddr = strings.TrimSpace(idOrAddr)
	if idOrAddr == "" {
		return ForceCloseResult{}, fmt.Errorf("force close target required")
	}
	prefix := strings.ToLower(idOrAddr)

	allOpen, err := db.GetOpenPositions(ctx)
	if err != nil {
		return ForceCloseResult{}, fmt.Errorf("get open positions: %w", err)
	}

	var matching []contracts.PositionStateDTO
	for _, p := range allOpen {
		if strings.HasPrefix(strings.ToLower(p.TokenAddress), prefix) {
			matching = append(matching, p)
		}
	}
	if len(matching) == 0 {
		for _, p := range allOpen {
			if strings.HasPrefix(strings.ToLower(p.PositionID), prefix) {
				matching = append(matching, p)
			}
		}
	}
	if len(matching) == 0 {
		return ForceCloseResult{}, fmt.Errorf("no open position matches %q", idOrAddr)
	}

	distinctTokens := make(map[string]struct{}, len(matching))
	for _, p := range matching {
		distinctTokens[strings.ToLower(p.TokenAddress)] = struct{}{}
	}
	if len(distinctTokens) > 1 {
		return ForceCloseResult{}, fmt.Errorf("prefix %q matches multiple tokens", idOrAddr)
	}

	if snapshotAt == "" {
		snapshotAt = time.Now().UTC().Format(time.RFC3339Nano)
	}

	var closedIDs []string
	for _, p := range matching {
		exit := p
		exit.Status = "exited"
		exit.ExitReason = "MANUAL"
		exit.ExitedAt = snapshotAt
		exit.SnapshotAt = snapshotAt
		exit.EventID = forceCloseEventID(p.PositionID, issuer, snapshotAt, commandID)
		exit.CausationID = p.EventID
		if exit.ExitPrice == "" {
			exit.ExitPrice = exit.CurrentPrice
		}
		if err := db.InsertPositionState(ctx, exit); err != nil {
			return ForceCloseResult{}, fmt.Errorf("insert force-close state for %s: %w", p.PositionID, err)
		}
		logger.Warn("operator_force_close",
			"position_id", p.PositionID,
			"token_address", p.TokenAddress,
			"chain", p.Chain,
			"issuer_id", issuer,
			"source", string(source),
			"reason", "MANUAL",
		)
		closedIDs = append(closedIDs, p.PositionID)
	}

	return ForceCloseResult{
		ClosedPositionIDs: closedIDs,
		TokenAddress:      matching[0].TokenAddress,
		Chain:             matching[0].Chain,
	}, nil
}

func forceCloseEventID(positionID, issuer, snapshotAt, commandID string) string {
	seed := fmt.Sprintf("force_close|%s|%s|%s", positionID, issuer, snapshotAt)
	if commandID != "" {
		seed = fmt.Sprintf("force_close|%s|%s|%s", positionID, issuer, commandID)
	}
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])[:16]
}

func transitionReason(source CommandSource) string {
	switch source {
	case CommandSourceDashboard:
		return "manual_dashboard"
	default:
		return "manual_telegram"
	}
}

func killReason(source CommandSource) string {
	switch source {
	case CommandSourceDashboard:
		return "dashboard kill command"
	default:
		return "/kill command"
	}
}

func resumeReason(source CommandSource) string {
	switch source {
	case CommandSourceDashboard:
		return "dashboard resume command"
	default:
		return "/resume command"
	}
}

func haltOperatorTag(source CommandSource, issuer string) string {
	switch source {
	case CommandSourceDashboard:
		if issuer != "" {
			return "dashboard_operator:" + issuer
		}
		return "dashboard_operator"
	default:
		return "telegram_operator"
	}
}
