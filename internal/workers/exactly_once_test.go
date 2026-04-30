package workers

import (
	"context"
	"encoding/json"
	"testing"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
)

// exactlyOnceAdapter wraps stubAdapter to track ClaimExecution and
// UpsertPositionFromExecution call counts and simulate the "already claimed"
// outcome on the second call. This is the contract the postgres adapter
// honours via INSERT … ON CONFLICT DO NOTHING — see hardening.go § 4.10.D/E.
//
// On a duplicate redelivery the worker is expected to LOOK UP the prior
// persisted ExecutionResultDTO via GetExecutionByLifecycle and re-emit the
// same downstream event, so we record the prior result on the first delivery.
type exactlyOnceAdapter struct {
	*stubAdapter

	claimCalls        int
	upsertPosCalls    int
	insertResultCalls int
	insertPosCalls    int
	transitionCalls   int

	priorExecResult *contracts.ExecutionResultDTO
}

func newExactlyOnceAdapter() *exactlyOnceAdapter {
	return &exactlyOnceAdapter{
		stubAdapter: &stubAdapter{lifecycleResult: defaultLC("lc-exec-1")},
	}
}

func (a *exactlyOnceAdapter) ClaimExecution(_ context.Context, _ contracts.AllocationDTO) (bool, error) {
	a.claimCalls++
	return a.claimCalls == 1, nil
}

func (a *exactlyOnceAdapter) GetExecutionByLifecycle(_ context.Context, _ string) (*contracts.ExecutionResultDTO, error) {
	if a.priorExecResult == nil {
		return nil, database.ErrNotFound
	}
	return a.priorExecResult, nil
}

func (a *exactlyOnceAdapter) UpsertPositionFromExecution(_ context.Context, _ contracts.PositionStateDTO) (bool, error) {
	a.upsertPosCalls++
	return a.upsertPosCalls == 1, nil
}

func (a *exactlyOnceAdapter) InsertExecutionResult(_ context.Context, dto contracts.ExecutionResultDTO) error {
	a.insertResultCalls++
	// Capture the prior result so a duplicate redelivery can re-emit the same
	// downstream event via GetExecutionByLifecycle.
	copy := dto
	a.priorExecResult = &copy
	return nil
}

func (a *exactlyOnceAdapter) InsertPositionState(_ context.Context, _ contracts.PositionStateDTO) error {
	a.insertPosCalls++
	return nil
}

func (a *exactlyOnceAdapter) TransitionState(_ context.Context, _ database.TransitionRequest) error {
	a.transitionCalls++
	return nil
}

// TestExecutionWorker_DuplicateAllocation_Suppressed verifies the
// exactly-once execution gate (architecture.md § 4.10.D, certification § D+E).
//
// First delivery of an allocation event must execute (simulated path here)
// and emit one execution_result_event. A second redelivery of the same
// allocation event must:
//   - call ClaimExecution again (returns false)
//   - SKIP transaction submission, InsertExecutionResult, and lifecycle transition
//   - RE-EMIT the same downstream execution_result_event (looked up via
//     GetExecutionByLifecycle), so a crash between InsertExecutionResult and
//     the outer InsertEvent cannot leave the pipeline permanently stuck.
//     The downstream InsertEvent is idempotent via ON CONFLICT (event_id),
//     making re-emission safe.
func TestExecutionWorker_DuplicateAllocation_Suppressed(t *testing.T) {
	adapter := newExactlyOnceAdapter()
	w := NewExecutionWorker(adapter, minConfig(), nil, "", 1, "", nil, nil)

	alloc := contracts.AllocationDTO{
		EventID:          "alloc-evt-1",
		TraceID:          "trace-1",
		CorrelationID:    "corr-1",
		VersionID:        "v1",
		TokenLifecycleID: "lc-exec-1",
		ExecutionID:      "exec-id-deterministic-1",
		WalletAddress:    "0xWALLET",
		Chain:            "ethereum",
		SizeUsd:          100,
	}
	payload, err := json.Marshal(alloc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	evt := &database.Event{
		EventID:       "exec-evt-1",
		EventType:     "allocation_event",
		Payload:       payload,
		TraceID:       alloc.TraceID,
		CorrelationID: alloc.CorrelationID,
		VersionID:     alloc.VersionID,
	}

	// Act 1: first delivery.
	out1, err := w.Process(context.Background(), evt)
	if err != nil {
		t.Fatalf("first delivery: unexpected error: %v", err)
	}
	if out1 == nil {
		t.Fatal("first delivery: expected execution_result_event, got nil")
	}
	if out1.EventType != "execution_result_event" {
		t.Errorf("first delivery: expected execution_result_event, got %q", out1.EventType)
	}

	// Act 2: redelivery of same allocation.
	out2, err := w.Process(context.Background(), evt)
	if err != nil {
		t.Fatalf("redelivery: unexpected error: %v", err)
	}
	if out2 == nil {
		t.Fatal("redelivery: expected re-emitted execution_result_event, got nil")
	}
	if out2.EventType != "execution_result_event" {
		t.Errorf("redelivery: expected execution_result_event, got %q", out2.EventType)
	}
	if out2.EventID != out1.EventID {
		t.Errorf("redelivery: expected same EventID %q (idempotent re-emit), got %q",
			out1.EventID, out2.EventID)
	}

	// Assert exactly-once invariants.
	if adapter.claimCalls != 2 {
		t.Errorf("expected 2 ClaimExecution calls, got %d", adapter.claimCalls)
	}
	if adapter.insertResultCalls != 1 {
		t.Errorf("expected exactly 1 InsertExecutionResult call (first delivery only), got %d",
			adapter.insertResultCalls)
	}
	if adapter.transitionCalls != 1 {
		t.Errorf("expected exactly 1 lifecycle transition (first delivery only), got %d",
			adapter.transitionCalls)
	}
}

// TestPositionOpenWorker_DuplicateExecution_Suppressed verifies the
// exactly-once position creation gate (certification § E).
//
// Re-delivery of the same execution_result_event must call
// UpsertPositionFromExecution (returns false on the second call by the
// source_execution_id partial unique index), suppress the lifecycle
// transition, and RE-EMIT the same content-addressed position_state_event
// so the downstream InsertEvent (idempotent via ON CONFLICT) cannot lose
// the event during a crash window between Upsert and InsertEvent.
func TestPositionOpenWorker_DuplicateExecution_Suppressed(t *testing.T) {
	adapter := newExactlyOnceAdapter()
	w := NewPositionOpenWorker(adapter, minConfig(), nil)

	exec := contracts.ExecutionResultDTO{
		EventID:          "exec-result-evt-1",
		TraceID:          "trace-1",
		CorrelationID:    "corr-1",
		VersionID:        "v1",
		TokenLifecycleID: "lc-exec-1",
		ExecutionID:      "exec-id-deterministic-1",
		AllocationID:     "alloc-evt-1",
		Status:           "confirmed",
		Success:          true,
		WalletAddress:    "0xWALLET",
		CompletedAt:      "2026-04-27T00:00:00Z",
	}
	payload, err := json.Marshal(exec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	evt := &database.Event{
		EventID:       "pos-open-evt-1",
		EventType:     "execution_result_event",
		Payload:       payload,
		TraceID:       exec.TraceID,
		CorrelationID: exec.CorrelationID,
		VersionID:     exec.VersionID,
	}

	// Act 1: first delivery.
	out1, err := w.Process(context.Background(), evt)
	if err != nil {
		t.Fatalf("first delivery: unexpected error: %v", err)
	}
	if out1 == nil {
		t.Fatal("first delivery: expected position_state_event, got nil")
	}
	if out1.EventType != "position_state_event" {
		t.Errorf("first delivery: expected position_state_event, got %q", out1.EventType)
	}

	// Act 2: redelivery of same execution result.
	out2, err := w.Process(context.Background(), evt)
	if err != nil {
		t.Fatalf("redelivery: unexpected error: %v", err)
	}
	if out2 == nil {
		t.Fatal("redelivery: expected re-emitted position_state_event, got nil")
	}
	if out2.EventType != "position_state_event" {
		t.Errorf("redelivery: expected position_state_event, got %q", out2.EventType)
	}
	if out2.EventID != out1.EventID {
		t.Errorf("redelivery: expected same EventID %q (idempotent re-emit), got %q",
			out1.EventID, out2.EventID)
	}

	// Assert exactly-once invariants.
	if adapter.upsertPosCalls != 2 {
		t.Errorf("expected 2 UpsertPositionFromExecution calls, got %d", adapter.upsertPosCalls)
	}
	if adapter.transitionCalls != 1 {
		t.Errorf("expected exactly 1 lifecycle transition (first delivery only), got %d",
			adapter.transitionCalls)
	}
	// InsertPositionState should never be called now — UpsertPositionFromExecution
	// is the single position-persistence path.
	if adapter.insertPosCalls != 0 {
		t.Errorf("expected 0 InsertPositionState calls (replaced by upsert), got %d",
			adapter.insertPosCalls)
	}
}

// Phase 11 (Reference-Repo R2 — LEARN) creator-blacklist stubs.
func (a *exactlyOnceAdapter) UpsertCreatorRugObservation(_ context.Context, _ database.CreatorRugObservation) error {
	return nil
}
func (a *exactlyOnceAdapter) GetCreatorBlacklistEntry(_ context.Context, _ string, _ string) (*database.CreatorBlacklistEntry, error) {
	return nil, nil
}
