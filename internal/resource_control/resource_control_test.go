package resource_control

import (
	"context"
	"testing"
	"time"

	"crypto-sniping-bot/database"
)

// TestComputePriority verifies that exit events are always ≥ PRIORITY_EXIT.
func TestComputePriority(t *testing.T) {
	t.Parallel()
	w := DefaultWeights()

	cases := []struct {
		eventType string
		isExit    bool
		wantMin   int32
	}{
		{"position_event", true, PRIORITY_EXIT},
		{"execution_replacement", false, PRIORITY_EXIT},
		{"allocation_event", false, 400},
		{"market_data_event", false, 100},
		{"unknown_event", false, 0},
	}

	for _, tc := range cases {
		got := ComputePriority(tc.eventType, tc.isExit, time.Time{}, time.Now(), w)
		if got < tc.wantMin {
			t.Errorf("ComputePriority(%s, exit=%v) = %d, want >= %d", tc.eventType, tc.isExit, got, tc.wantMin)
		}
	}
}

// TestRPCBudgetExhausted verifies that the token bucket returns ErrBudgetExhausted
// once all tokens are consumed.
func TestRPCBudgetExhausted(t *testing.T) {
	t.Parallel()
	// 1 token per second, burst of 1, no wait time.
	b := NewRPCBudget(1, 1, 0)
	ctx := context.Background()

	// First call should succeed.
	if err := b.Acquire(ctx, "http://localhost:8545"); err != nil {
		t.Fatalf("first Acquire: unexpected error: %v", err)
	}

	// Second call should fail immediately (waitMs=0).
	if err := b.Acquire(ctx, "http://localhost:8545"); err == nil {
		t.Fatal("second Acquire: expected ErrBudgetExhausted, got nil")
	}
}

// TestRPCBudgetContextCancel verifies that a cancelled context unblocks Acquire.
func TestRPCBudgetContextCancel(t *testing.T) {
	t.Parallel()
	b := NewRPCBudget(1, 1, 5000) // 5 second wait
	ctx, cancel := context.WithCancel(context.Background())

	// Drain the bucket.
	if err := b.Acquire(ctx, "http://localhost:8545"); err != nil {
		t.Fatalf("first Acquire: %v", err)
	}

	cancel()
	err := b.Acquire(ctx, "http://localhost:8545")
	if err == nil {
		t.Fatal("expected context error, got nil")
	}
}

// TestGasBudgetCap verifies per-wallet and system cap enforcement.
func TestGasBudgetCap(t *testing.T) {
	t.Parallel()
	gb := NewGasBudget(100, 200)

	// Under cap.
	if err := gb.RecordGasUsed("wallet1", 50); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Wallet cap.
	if err := gb.RecordGasUsed("wallet1", 60); err == nil {
		t.Fatal("expected ErrWalletGasCap")
	}
	// System cap across wallets.
	if err := gb.RecordGasUsed("wallet2", 100); err != nil {
		t.Fatalf("wallet2 unexpected error: %v", err)
	}
	if err := gb.RecordGasUsed("wallet2", 60); err == nil {
		t.Fatal("expected ErrSystemGasCap")
	}
}

// TestComputeBudgetQueueFull verifies that entry events are rejected when queue is full.
func TestComputeBudgetQueueFull(t *testing.T) {
	t.Parallel()
	cb := NewComputeBudget(2)

	// Fill the queue with entry events.
	if err := cb.Enqueue(false); err != nil {
		t.Fatal(err)
	}
	if err := cb.Enqueue(false); err != nil {
		t.Fatal(err)
	}
	// Third entry event should be rejected.
	if err := cb.Enqueue(false); err == nil {
		t.Fatal("expected ErrQueueFull")
	}
	// Exit events always succeed.
	if err := cb.Enqueue(true); err != nil {
		t.Fatalf("exit event should not be rejected: %v", err)
	}
}

// TestBackpressureExitProtected verifies exit events are never dropped.
func TestBackpressureExitProtected(t *testing.T) {
	t.Parallel()
	policy := NewBackpressurePolicy()
	ctx := context.Background()

	exitEvt := &database.Event{EventType: "position_event_exit"}
	drop, _ := policy.ShouldDrop(ctx, exitEvt, 9999, 1)
	if drop {
		t.Fatal("exit event was dropped — invariant violated")
	}

	entryEvt := &database.Event{EventType: "market_data_event"}
	drop, reason := policy.ShouldDrop(ctx, entryEvt, 9999, 1)
	if !drop {
		t.Fatal("entry event should have been dropped")
	}
	if reason == "" {
		t.Fatal("drop reason must not be empty")
	}
}

// TestHaltEvaluateMode verifies system mode transitions.
func TestHaltEvaluateMode(t *testing.T) {
	t.Parallel()
	cases := []struct {
		current  string
		dd       float64
		wantMode string
	}{
		{"BALANCED", 0.02, "BALANCED"},
		{"BALANCED", 0.06, "DEGRADED"},
		{"BALANCED", 0.11, "HALTED"},
		{"HALTED", 0.02, "BALANCED"},   // auto-resume
		{"DEGRADED", 0.02, "BALANCED"}, // DEGRADED also resumes when below resume threshold
		{"DEGRADED", 0.07, "DEGRADED"}, // stays degraded
	}
	for _, tc := range cases {
		result := EvaluateMode(tc.current, tc.dd, 0.05, 0.10, 0.03)
		if result.Mode != tc.wantMode {
			t.Errorf("EvaluateMode(%s, dd=%.2f) = %s, want %s", tc.current, tc.dd, result.Mode, tc.wantMode)
		}
	}
}
