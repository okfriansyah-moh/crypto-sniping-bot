package workers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
)

type operatorCommandStubDB struct {
	stubAdapter
	claimSeq   []*database.Event
	claimIdx   int
	marked     []string
	released   []string
	halt       bool
	mode       string
	upsertMode string
}

func (s *operatorCommandStubDB) ClaimNextEvent(_ context.Context, _ string, _ []string) (*database.Event, error) {
	if s.claimIdx >= len(s.claimSeq) {
		return nil, nil
	}
	evt := s.claimSeq[s.claimIdx]
	s.claimIdx++
	return evt, nil
}

func (s *operatorCommandStubDB) MarkEventProcessed(_ context.Context, eventID string) error {
	s.marked = append(s.marked, eventID)
	return nil
}

func (s *operatorCommandStubDB) ReleaseEventClaim(_ context.Context, eventID string) error {
	s.released = append(s.released, eventID)
	return nil
}

func (s *operatorCommandStubDB) SetSystemHalt(_ context.Context, halt bool, _, _ string) error {
	s.halt = halt
	return nil
}

func (s *operatorCommandStubDB) GetSystemState(_ context.Context) (*contracts.SystemStateDTO, error) {
	if s.mode == "" {
		s.mode = "BALANCED"
	}
	return &contracts.SystemStateDTO{Mode: s.mode, StateVersion: 1}, nil
}

func (s *operatorCommandStubDB) UpsertSystemState(_ context.Context, state contracts.SystemStateDTO, _ int64) (int64, error) {
	s.upsertMode = state.Mode
	s.mode = state.Mode
	return 2, nil
}

func TestOperatorCommand_ProcessKillEvent(t *testing.T) {
	ts := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	cmd, err := contracts.NewOperatorCommandDTO(contracts.CommandTypeKill, "op-1", "tok-abc", ts, nil)
	if err != nil {
		t.Fatalf("NewOperatorCommandDTO: %v", err)
	}
	payload, err := json.Marshal(cmd)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	stub := &operatorCommandStubDB{
		claimSeq: []*database.Event{{
			EventID:   cmd.CommandID,
			EventType: contracts.OperatorCommandEventType,
			Payload:   payload,
		}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	err = RunOperatorCommands(ctx, stub, logger)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RunOperatorCommands err = %v", err)
	}
	if !stub.halt {
		t.Fatal("expected kill switch engaged")
	}
	if len(stub.marked) != 1 || stub.marked[0] != cmd.CommandID {
		t.Fatalf("marked = %v", stub.marked)
	}
}

func TestOperatorCommand_ProcessModeEvent(t *testing.T) {
	ts := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	cmd, err := contracts.NewOperatorCommandDTO(
		contracts.CommandTypeMode,
		"op-1",
		"",
		ts,
		map[string]string{"mode": "VERY_EXPLORATION"},
	)
	if err != nil {
		t.Fatalf("NewOperatorCommandDTO: %v", err)
	}
	payload, _ := json.Marshal(cmd)
	stub := &operatorCommandStubDB{
		claimSeq: []*database.Event{{
			EventID:   cmd.CommandID,
			EventType: contracts.OperatorCommandEventType,
			Payload:   payload,
		}},
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	err = RunOperatorCommands(ctx, stub, slog.Default())
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if stub.upsertMode != "VERY_EXPLORATION" {
		t.Fatalf("mode = %q", stub.upsertMode)
	}
}

func TestOperatorCommand_InvalidPayloadReleasesClaim(t *testing.T) {
	stub := &operatorCommandStubDB{
		claimSeq: []*database.Event{{
			EventID:   "bad-evt",
			EventType: contracts.OperatorCommandEventType,
			Payload:   []byte(`not-json`),
		}},
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	_ = RunOperatorCommands(ctx, stub, slog.Default())
	if len(stub.released) != 1 || stub.released[0] != "bad-evt" {
		t.Fatalf("released = %v", stub.released)
	}
	if len(stub.marked) != 0 {
		t.Fatalf("expected no mark, got %v", stub.marked)
	}
}

func TestProcessOperatorCommandEvent_Mode(t *testing.T) {
	ts := time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC).Format(time.RFC3339Nano)
	cmd, _ := contracts.NewOperatorCommandDTO(contracts.CommandTypeMode, "op-2", "", ts, map[string]string{"mode": "STRICT"})
	payload, _ := json.Marshal(cmd)
	stub := &operatorCommandStubDB{}
	err := processOperatorCommandEvent(context.Background(), stub, slog.Default(), &database.Event{
		EventID: cmd.CommandID, Payload: payload,
	})
	if err != nil {
		t.Fatalf("process: %v", err)
	}
	if stub.upsertMode != "STRICT" {
		t.Fatalf("mode = %q", stub.upsertMode)
	}
}
