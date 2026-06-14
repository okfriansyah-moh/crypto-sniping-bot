package operator_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/operator"
)

type commandsStubDB struct {
	database.Adapter
	halt         bool
	haltReason   string
	haltOperator string
	systemState  *contracts.SystemStateDTO
	upsertMode   string
}

func (s *commandsStubDB) SetSystemHalt(_ context.Context, halt bool, reason, op string) error {
	s.halt = halt
	s.haltReason = reason
	s.haltOperator = op
	return nil
}

func (s *commandsStubDB) GetSystemState(_ context.Context) (*contracts.SystemStateDTO, error) {
	if s.systemState != nil {
		return s.systemState, nil
	}
	return &contracts.SystemStateDTO{Mode: "BALANCED", StateVersion: 1}, nil
}

func (s *commandsStubDB) UpsertSystemState(_ context.Context, state contracts.SystemStateDTO, _ int64) (int64, error) {
	s.upsertMode = state.Mode
	s.systemState = &state
	return state.StateVersion + 1, nil
}

func TestExecuteKill_SetsHalt(t *testing.T) {
	stub := &commandsStubDB{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := operator.ExecuteKill(context.Background(), stub, logger, "op-1", operator.CommandSourceDashboard); err != nil {
		t.Fatalf("ExecuteKill: %v", err)
	}
	if !stub.halt || stub.haltOperator != "dashboard_operator:op-1" {
		t.Fatalf("halt state = %+v", stub)
	}
}

func TestExecuteResume_ClearsHalt(t *testing.T) {
	stub := &commandsStubDB{halt: true}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := operator.ExecuteResume(context.Background(), stub, logger, "op-1", operator.CommandSourceDashboard); err != nil {
		t.Fatalf("ExecuteResume: %v", err)
	}
	if stub.halt {
		t.Fatal("expected halt cleared")
	}
}

func TestExecuteMode_UpdatesSystemState(t *testing.T) {
	stub := &commandsStubDB{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := operator.ExecuteMode(context.Background(), stub, logger, "EXPLORATION", "op-1", operator.CommandSourceDashboard); err != nil {
		t.Fatalf("ExecuteMode: %v", err)
	}
	if stub.upsertMode != "EXPLORATION" {
		t.Fatalf("mode = %q", stub.upsertMode)
	}
}

func TestExecuteCommand_ModeDispatch(t *testing.T) {
	stub := &commandsStubDB{}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	ts := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	cmd, err := contracts.NewOperatorCommandDTO(
		contracts.CommandTypeMode,
		"op-1",
		"",
		ts,
		map[string]string{"mode": "STRICT"},
	)
	if err != nil {
		t.Fatalf("NewOperatorCommandDTO: %v", err)
	}
	if err := operator.ExecuteCommand(context.Background(), stub, logger, cmd, operator.CommandSourceDashboard); err != nil {
		t.Fatalf("ExecuteCommand: %v", err)
	}
	if stub.upsertMode != "STRICT" {
		t.Fatalf("mode = %q", stub.upsertMode)
	}
}

func TestExecuteCommand_DestructiveRequiresConfirmToken(t *testing.T) {
	ts := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	cmd, err := contracts.NewOperatorCommandDTO(contracts.CommandTypeKill, "op-1", "", ts, nil)
	if err != nil {
		t.Fatalf("NewOperatorCommandDTO: %v", err)
	}
	err = operator.ExecuteCommand(context.Background(), &commandsStubDB{}, slog.Default(), cmd, operator.CommandSourceDashboard)
	if err == nil {
		t.Fatal("expected confirm_token error")
	}
}
