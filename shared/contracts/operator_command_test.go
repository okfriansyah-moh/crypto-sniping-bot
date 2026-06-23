package contracts_test

import (
	"testing"

	"crypto-sniping-bot/shared/contracts"
)

func TestNewOperatorCommandDTO_ContentAddressableID(t *testing.T) {
	cmd, err := contracts.NewOperatorCommandDTO(
		contracts.CommandTypeMode,
		"op-dashboard-1",
		"",
		"2026-06-14T00:00:00Z",
		map[string]string{"mode": "EXPLORATION"},
	)
	if err != nil {
		t.Fatalf("NewOperatorCommandDTO: %v", err)
	}
	if len(cmd.CommandID) != 16 {
		t.Fatalf("CommandID length = %d, want 16", len(cmd.CommandID))
	}
	if err := cmd.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	cmd2, err := contracts.NewOperatorCommandDTO(
		contracts.CommandTypeMode,
		"op-dashboard-1",
		"",
		"2026-06-14T00:00:00Z",
		map[string]string{"mode": "EXPLORATION"},
	)
	if err != nil {
		t.Fatalf("NewOperatorCommandDTO repeat: %v", err)
	}
	if cmd.CommandID != cmd2.CommandID {
		t.Errorf("CommandID not deterministic: %q vs %q", cmd.CommandID, cmd2.CommandID)
	}
}

func TestOperatorCommandDTO_Validate_RejectsBadCommandID(t *testing.T) {
	cmd := contracts.OperatorCommandDTO{
		CommandID:   "deadbeefdeadbeef",
		CommandType: contracts.CommandTypeKill,
		IssuerID:    "op-1",
		Timestamp:   "2026-06-14T00:00:00Z",
	}
	if err := cmd.Validate(); err == nil {
		t.Fatal("expected command_id mismatch error")
	}
}

func TestOperatorCommandDTO_IsDestructive(t *testing.T) {
	mode, _ := contracts.NewOperatorCommandDTO(contracts.CommandTypeMode, "op", "", "2026-06-14T00:00:00Z", nil)
	if mode.IsDestructive() {
		t.Error("mode should not be destructive")
	}
	kill, _ := contracts.NewOperatorCommandDTO(contracts.CommandTypeKill, "op", "tok", "2026-06-14T00:00:00Z", nil)
	if !kill.IsDestructive() {
		t.Error("kill should be destructive")
	}
}

func TestNewEventEnvelope_OperatorCommandEvent(t *testing.T) {
	cmd, err := contracts.NewOperatorCommandDTO(
		contracts.CommandTypeResume,
		"op-2",
		"confirm-abc",
		"2026-06-14T12:00:00Z",
		nil,
	)
	if err != nil {
		t.Fatalf("NewOperatorCommandDTO: %v", err)
	}
	trace := contracts.NewTraceFields(cmd.CommandID, cmd.CommandID, "", "ver-0000000000000001")
	env, err := contracts.NewEventEnvelope(contracts.OperatorCommandEventType, cmd, trace, cmd.Timestamp)
	if err != nil {
		t.Fatalf("NewEventEnvelope: %v", err)
	}
	if env.EventType != contracts.OperatorCommandEventType {
		t.Errorf("EventType = %q", env.EventType)
	}
	decoded, err := contracts.DecodePayload[contracts.OperatorCommandDTO](env)
	if err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if decoded.CommandID != cmd.CommandID {
		t.Errorf("decoded CommandID = %q, want %q", decoded.CommandID, cmd.CommandID)
	}
}
