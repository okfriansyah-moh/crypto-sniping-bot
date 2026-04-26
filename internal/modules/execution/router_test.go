package execution

import (
	"context"
	"errors"
	"testing"

	"crypto-sniping-bot/contracts"
)

// ── mock implementations ──────────────────────────────────────────────────────

type stubEVMExecutor struct {
	result contracts.ExecutionResultDTO
	err    error
}

func (s *stubEVMExecutor) Process(_ context.Context, _ contracts.AllocationDTO, _ uint64, _ string) (contracts.ExecutionResultDTO, error) {
	return s.result, s.err
}

type stubSolanaExecutor struct {
	result contracts.ExecutionResultDTO
	err    error
}

func (s *stubSolanaExecutor) Execute(_ context.Context, _ contracts.AllocationDTO, _, _ string) (contracts.ExecutionResultDTO, error) {
	return s.result, s.err
}

// ── Router tests ──────────────────────────────────────────────────────────────

func TestRouter_EVMChain_CallsEVMExecutor(t *testing.T) {
	// Arrange
	want := contracts.ExecutionResultDTO{Status: "confirmed"}
	evm := &stubEVMExecutor{result: want}
	r := NewRouter(evm, nil)
	alloc := contracts.AllocationDTO{Chain: "eth"}

	// Act
	got, err := r.Route(context.Background(), alloc, 42, "0xROUTER")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != want.Status {
		t.Errorf("expected status %q, got %q", want.Status, got.Status)
	}
}

func TestRouter_SolanaChain_CallsSolanaExecutor(t *testing.T) {
	// Arrange
	want := contracts.ExecutionResultDTO{Status: "confirmed"}
	sol := &stubSolanaExecutor{result: want}
	r := NewRouter(nil, sol)
	alloc := contracts.AllocationDTO{Chain: "solana"}

	// Act
	got, err := r.Route(context.Background(), alloc, 0, "")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != want.Status {
		t.Errorf("expected status %q, got %q", want.Status, got.Status)
	}
}

func TestRouter_SolanaChain_NilSolanaExecutor_ReturnsError(t *testing.T) {
	// Arrange — no Solana executor wired
	evm := &stubEVMExecutor{}
	r := NewRouter(evm, nil)
	alloc := contracts.AllocationDTO{Chain: "solana"}

	// Act
	_, err := r.Route(context.Background(), alloc, 0, "")

	// Assert
	if err == nil {
		t.Fatal("expected error when solana executor is nil")
	}
}

func TestRouter_EVMChain_NilEVMExecutor_ReturnsError(t *testing.T) {
	// Arrange — no EVM executor wired
	r := NewRouter(nil, nil)
	alloc := contracts.AllocationDTO{Chain: "bsc"}

	// Act
	_, err := r.Route(context.Background(), alloc, 0, "")

	// Assert
	if err == nil {
		t.Fatal("expected error when evm executor is nil")
	}
}

func TestRouter_EVMExecutor_ErrorPropagated(t *testing.T) {
	// Arrange
	execErr := errors.New("rpc: connection refused")
	evm := &stubEVMExecutor{err: execErr}
	r := NewRouter(evm, nil)
	alloc := contracts.AllocationDTO{Chain: "polygon"}

	// Act
	_, err := r.Route(context.Background(), alloc, 0, "")

	// Assert
	if !errors.Is(err, execErr) {
		t.Errorf("expected wrapped execErr, got: %v", err)
	}
}

func TestRouter_BSCChain_RoutedToEVM(t *testing.T) {
	// Arrange — "bsc" is an EVM chain (default case)
	want := contracts.ExecutionResultDTO{Status: "simulated"}
	evm := &stubEVMExecutor{result: want}
	r := NewRouter(evm, nil)
	alloc := contracts.AllocationDTO{Chain: "bsc"}

	got, err := r.Route(context.Background(), alloc, 0, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != want.Status {
		t.Errorf("expected %q, got %q", want.Status, got.Status)
	}
}
