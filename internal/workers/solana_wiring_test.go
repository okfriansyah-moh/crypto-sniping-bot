package workers

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/execution"
)

// ── RunIngestionSolana tests ──────────────────────────────────────────────────

// solanaIngestionAdapter embeds stubAdapter and overrides GetActiveStrategyVersion
// so RunIngestionSolana can proceed past the version-pin step.
type solanaIngestionAdapter struct {
	*stubAdapter
}

func (a *solanaIngestionAdapter) GetActiveStrategyVersion(_ context.Context) (*database.StrategyVersion, error) {
	return &database.StrategyVersion{StrategyVersionID: "sv-test-1"}, nil
}

// TestRunIngestionSolana_AdapterError verifies that an error from
// GetActiveStrategyVersion propagates immediately.
func TestRunIngestionSolana_AdapterError(t *testing.T) {
	// Default stubAdapter returns ErrNotFound from GetActiveStrategyVersion.
	adapter := &stubAdapter{}
	cfg := &config.Config{}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	err := RunIngestionSolana(ctx, adapter, cfg, nil, nil)
	if err == nil {
		t.Fatal("expected error from failed GetActiveStrategyVersion, got nil")
	}
}

// TestRunIngestionSolana_NoProgramsConfigured verifies that when no Solana
// programs are configured the function blocks until ctx is cancelled and
// returns nil (graceful noop).
func TestRunIngestionSolana_NoProgramsConfigured(t *testing.T) {
	adapter := &solanaIngestionAdapter{stubAdapter: &stubAdapter{}}
	cfg := &config.Config{
		Solana: config.SolanaConfig{Programs: nil}, // empty → noop
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := RunIngestionSolana(ctx, adapter, cfg, nil, nil)
	// ctx.Err() is returned; treat as nil-equivalent (graceful shutdown).
	if err != nil && err != ctx.Err() {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRunIngestionSolana_NilClient_Noop verifies that when programs are
// configured but the RPC client is nil the module waits for ctx cancellation
// (no-op mode) rather than crashing.
func TestRunIngestionSolana_NilClient_Noop(t *testing.T) {
	adapter := &solanaIngestionAdapter{stubAdapter: &stubAdapter{}}
	cfg := &config.Config{
		Solana: config.SolanaConfig{
			Programs: []config.SolanaProgramConfig{
				{ProgramID: "675kPX9MHTjS2zt1qfr1NYHuzeLXfQM9H24wFSUt1Mp8", Family: "raydium-v4"},
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	err := RunIngestionSolana(ctx, adapter, cfg, nil, nil)
	// nil client → module logs "solana_ingestion_no_client_noop" and waits on ctx.Done().
	if err != nil && err != ctx.Err() {
		t.Fatalf("unexpected error with nil client: %v", err)
	}
}

// ── ExecutionWorker Solana path tests ─────────────────────────────────────────

// stubSolanaExecutor implements execution.SolanaExecutor for tests.
type stubSolanaExecutor struct {
	called bool
	result contracts.ExecutionResultDTO
	err    error
}

func (s *stubSolanaExecutor) Execute(
	_ context.Context,
	alloc contracts.AllocationDTO,
	_, _ string,
) (contracts.ExecutionResultDTO, error) {
	s.called = true
	if s.result.EventID == "" {
		s.result.EventID = contracts.ContentIDFromString("exec-sol-test:" + alloc.EventID)
		s.result.TokenLifecycleID = alloc.TokenLifecycleID
		s.result.ExecutionID = alloc.ExecutionID
		s.result.AllocationID = alloc.EventID
		s.result.TraceID = alloc.TraceID
		s.result.CorrelationID = alloc.CorrelationID
		s.result.VersionID = alloc.VersionID
		s.result.Status = "confirmed"
		s.result.Success = true
		s.result.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return s.result, s.err
}

// Compile-time assertion: stubSolanaExecutor satisfies execution.SolanaExecutor.
var _ execution.SolanaExecutor = (*stubSolanaExecutor)(nil)

// makeSolanaAllocationEvent builds a minimal allocation event with Chain="solana".
func makeSolanaAllocationEvent(lifecycleID string) *database.Event {
	alloc := contracts.AllocationDTO{
		EventID:          "alloc-sol-1",
		TraceID:          "trace-sol-1",
		CorrelationID:    "corr-sol-1",
		VersionID:        "v1",
		TokenLifecycleID: lifecycleID,
		ExecutionID:      contracts.ContentIDFromString("exec-sol-1"),
		Chain:            "solana",
		TokenAddress:     "TokenkegQfeZyiNwAJbNbGKPFXCWuBvf9Ss623VQ5DA",
		WalletAddress:    "SoLWa11et1111111111111111111111111111111111",
		SizeUsd:          100.0,
	}
	payload, _ := json.Marshal(alloc)
	return &database.Event{
		EventID:       alloc.EventID,
		EventType:     "allocation_event",
		Payload:       payload,
		TraceID:       alloc.TraceID,
		CorrelationID: alloc.CorrelationID,
		VersionID:     alloc.VersionID,
	}
}

// TestExecutionWorker_SolanaChain_NilExecutor_Simulated verifies that a
// Solana allocation with no executor wired produces a simulated result.
func TestExecutionWorker_SolanaChain_NilExecutor_Simulated(t *testing.T) {
	lcID := "lc-sol-sim-1"
	adapter := &stubAdapter{lifecycleResult: defaultLC(lcID)}

	worker := NewExecutionWorker(
		adapter,
		minConfig(),
		nil, // evmClient nil → simulated for EVM
		"",  // privKey
		1,   // chainID
		"",  // router
		nil, // walletShards
		nil, // logger
	)
	// Do NOT call WithSolanaExecutor — executor stays nil.

	evt := makeSolanaAllocationEvent(lcID)
	out, err := worker.Process(context.Background(), evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == nil {
		t.Fatal("expected output event, got nil")
	}
	if out.EventType != "execution_result_event" {
		t.Errorf("expected execution_result_event, got %s", out.EventType)
	}

	var result contracts.ExecutionResultDTO
	if err := json.Unmarshal(out.Payload, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if !result.Simulated {
		t.Error("expected Simulated=true when no executor is configured")
	}
	if !result.Success {
		t.Error("expected Success=true for simulated result")
	}
}

// TestExecutionWorker_SolanaChain_ExecutorCalled verifies that a Solana
// allocation with a wired executor delegates to the executor, not simulation.
func TestExecutionWorker_SolanaChain_ExecutorCalled(t *testing.T) {
	lcID := "lc-sol-exec-1"
	adapter := &stubAdapter{lifecycleResult: defaultLC(lcID)}

	stub := &stubSolanaExecutor{}
	worker := NewExecutionWorker(
		adapter,
		minConfig(),
		nil, "", 1, "", nil, nil,
	).WithSolanaExecutor(stub)

	evt := makeSolanaAllocationEvent(lcID)
	out, err := worker.Process(context.Background(), evt)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out == nil {
		t.Fatal("expected output event, got nil")
	}
	if !stub.called {
		t.Error("expected SolanaExecutor.Execute to be called, but it was not")
	}

	var result contracts.ExecutionResultDTO
	if err := json.Unmarshal(out.Payload, &result); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if result.Simulated {
		t.Error("expected Simulated=false when executor is configured")
	}
	if !result.Success {
		t.Errorf("expected Success=true from stub executor, got %v", result.Success)
	}
}
