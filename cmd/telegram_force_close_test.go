package main

import (
	"context"
	"strings"
	"testing"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
)

// forceCloseStubAdapter stubs the three methods buildForceCloseFn uses.
type forceCloseStubAdapter struct {
	database.Adapter
	open      []contracts.PositionStateDTO
	openErr   error
	inserted  []contracts.PositionStateDTO
	insertErr error
}

func (s *forceCloseStubAdapter) GetOpenPositions(_ context.Context) ([]contracts.PositionStateDTO, error) {
	return s.open, s.openErr
}

func (s *forceCloseStubAdapter) InsertPositionState(_ context.Context, p contracts.PositionStateDTO) error {
	if s.insertErr != nil {
		return s.insertErr
	}
	s.inserted = append(s.inserted, p)
	return nil
}

func makePosition(tokenAddr, positionID, chain string) contracts.PositionStateDTO {
	return contracts.PositionStateDTO{
		TokenAddress: tokenAddr,
		PositionID:   positionID,
		Chain:        chain,
		Status:       "open",
		EventID:      "evt" + positionID,
		CurrentPrice: "1.00",
	}
}

func TestForceClose_SinglePositionByTokenPrefix(t *testing.T) {
	stub := &forceCloseStubAdapter{
		open: []contracts.PositionStateDTO{
			makePosition("0xABCDEF1234", "pos1", "eth"),
		},
	}
	fn := buildForceCloseFn(stub, quietLogger())
	out, err := fn(context.Background(), "0xABCD", "op")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(out, "Force-close emitted") {
		t.Fatalf("expected success, got: %q", out)
	}
	if len(stub.inserted) != 1 {
		t.Fatalf("expected 1 insert, got %d", len(stub.inserted))
	}
	if stub.inserted[0].Status != "exited" {
		t.Fatalf("status must be exited, got %s", stub.inserted[0].Status)
	}
	if stub.inserted[0].ExitReason != "MANUAL" {
		t.Fatalf("exit reason must be MANUAL, got %s", stub.inserted[0].ExitReason)
	}
}

func TestForceClose_MultiplePositionsSameToken(t *testing.T) {
	stub := &forceCloseStubAdapter{
		open: []contracts.PositionStateDTO{
			makePosition("0xTOKEN", "pos1", "bsc"),
			makePosition("0xTOKEN", "pos2", "bsc"),
			makePosition("0xTOKEN", "pos3", "bsc"),
		},
	}
	fn := buildForceCloseFn(stub, quietLogger())
	out, err := fn(context.Background(), "0xTOKEN", "op")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(out, "3 positions") {
		t.Fatalf("expected 3 positions in response, got: %q", out)
	}
	if len(stub.inserted) != 3 {
		t.Fatalf("expected 3 inserts, got %d", len(stub.inserted))
	}
	for _, p := range stub.inserted {
		if p.Status != "exited" || p.ExitReason != "MANUAL" {
			t.Fatalf("bad exit state: %+v", p)
		}
	}
}

func TestForceClose_AmbiguousMultipleTokens(t *testing.T) {
	stub := &forceCloseStubAdapter{
		open: []contracts.PositionStateDTO{
			makePosition("0xAAA111", "pos1", "eth"),
			makePosition("0xAAA222", "pos2", "eth"),
		},
	}
	fn := buildForceCloseFn(stub, quietLogger())
	out, err := fn(context.Background(), "0xAAA", "op")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(out, "different tokens") {
		t.Fatalf("expected ambiguity warning, got: %q", out)
	}
	if len(stub.inserted) != 0 {
		t.Fatal("must not insert on ambiguous prefix")
	}
}

func TestForceClose_FallbackToPositionIDPrefix(t *testing.T) {
	stub := &forceCloseStubAdapter{
		open: []contracts.PositionStateDTO{
			makePosition("0xDEADBEEF", "position-xyz-abc", "eth"),
		},
	}
	fn := buildForceCloseFn(stub, quietLogger())
	// Use position_id prefix (does not match token_address)
	out, err := fn(context.Background(), "position-xyz", "op")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(out, "Force-close emitted") {
		t.Fatalf("expected success via pos-id fallback, got: %q", out)
	}
	if len(stub.inserted) != 1 {
		t.Fatalf("expected 1 insert, got %d", len(stub.inserted))
	}
}

func TestForceClose_NotFound(t *testing.T) {
	stub := &forceCloseStubAdapter{
		open: []contracts.PositionStateDTO{
			makePosition("0xFFF", "pos1", "eth"),
		},
	}
	fn := buildForceCloseFn(stub, quietLogger())
	out, err := fn(context.Background(), "0xAAA", "op")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(out, "No open position") {
		t.Fatalf("expected not-found, got: %q", out)
	}
	if len(stub.inserted) != 0 {
		t.Fatal("must not insert when nothing matched")
	}
}

func TestForceClose_ExitPriceFallsBackToCurrentPrice(t *testing.T) {
	stub := &forceCloseStubAdapter{
		open: []contracts.PositionStateDTO{
			{
				TokenAddress: "0xTKN", PositionID: "pos1", Chain: "eth",
				Status:       "open",
				EventID:      "evt1",
				CurrentPrice: "42.5",
				ExitPrice:    "", // not yet set
			},
		},
	}
	fn := buildForceCloseFn(stub, quietLogger())
	_, err := fn(context.Background(), "0xTKN", "op")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if stub.inserted[0].ExitPrice != "42.5" {
		t.Fatalf("exit price should fall back to current_price, got %q", stub.inserted[0].ExitPrice)
	}
}
