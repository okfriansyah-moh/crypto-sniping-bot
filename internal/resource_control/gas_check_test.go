package resource_control

// gas_check_test.go — additional coverage for GasBudget, ComputeBudget,
// RPCBudget, and backpressure helpers not exercised by resource_control_test.go.

import (
	"context"
	"errors"
	"testing"

	"crypto-sniping-bot/shared/database"
)

// ── GasBudget.CheckWallet ─────────────────────────────────────────────────────

func TestCheckWallet_NoHistory_ReturnsNil(t *testing.T) {
	t.Parallel()
	gb := NewGasBudget(100, 1000)
	// Wallet with no recorded usage should pass.
	if err := gb.CheckWallet("wallet-new"); err != nil {
		t.Errorf("CheckWallet for unknown wallet: unexpected error: %v", err)
	}
}

func TestCheckWallet_AtCap_ReturnsError(t *testing.T) {
	t.Parallel()
	gb := NewGasBudget(100, 1000)
	// Spend exactly up to the cap.
	if err := gb.RecordGasUsed("wallet-x", 100); err != nil {
		t.Fatalf("RecordGasUsed: unexpected error: %v", err)
	}
	if err := gb.CheckWallet("wallet-x"); err == nil {
		t.Fatal("CheckWallet at cap: expected ErrWalletGasCap, got nil")
	} else if !errors.Is(err, ErrWalletGasCap) {
		t.Errorf("expected ErrWalletGasCap, got %v", err)
	}
}

func TestCheckWallet_BelowCap_ReturnsNil(t *testing.T) {
	t.Parallel()
	gb := NewGasBudget(100, 1000)
	if err := gb.RecordGasUsed("wallet-y", 50); err != nil {
		t.Fatalf("RecordGasUsed: unexpected error: %v", err)
	}
	if err := gb.CheckWallet("wallet-y"); err != nil {
		t.Errorf("CheckWallet below cap: unexpected error: %v", err)
	}
}

// ── GasBudget.CheckSystem ─────────────────────────────────────────────────────

func TestCheckSystem_BelowCap_ReturnsNil(t *testing.T) {
	t.Parallel()
	gb := NewGasBudget(1000, 1000)
	if err := gb.CheckSystem(); err != nil {
		t.Errorf("CheckSystem on fresh budget: unexpected error: %v", err)
	}
}

func TestCheckSystem_AtCap_ReturnsError(t *testing.T) {
	t.Parallel()
	gb := NewGasBudget(500, 100)
	if err := gb.RecordGasUsed("wallet-a", 100); err != nil {
		t.Fatalf("RecordGasUsed: %v", err)
	}
	if err := gb.CheckSystem(); err == nil {
		t.Fatal("CheckSystem at system cap: expected ErrSystemGasCap, got nil")
	} else if !errors.Is(err, ErrSystemGasCap) {
		t.Errorf("expected ErrSystemGasCap, got %v", err)
	}
}

// ── GasBudget.Reset ───────────────────────────────────────────────────────────

func TestReset_ClearsCounters(t *testing.T) {
	t.Parallel()
	gb := NewGasBudget(100, 1000)
	// Exhaust wallet cap.
	if err := gb.RecordGasUsed("wallet-z", 100); err != nil {
		t.Fatalf("RecordGasUsed: %v", err)
	}
	// Verify capped.
	if err := gb.CheckWallet("wallet-z"); err == nil {
		t.Fatal("expected wallet to be capped before reset")
	}

	gb.Reset()

	// After reset wallet should be fresh.
	if err := gb.CheckWallet("wallet-z"); err != nil {
		t.Errorf("after Reset, CheckWallet should return nil, got: %v", err)
	}
	if err := gb.CheckSystem(); err != nil {
		t.Errorf("after Reset, CheckSystem should return nil, got: %v", err)
	}
}

// ── IsExitEvent ───────────────────────────────────────────────────────────────

func TestIsExitEvent_ProtectedTypes_ReturnsTrue(t *testing.T) {
	t.Parallel()
	protected := []string{
		"position_event_exit",
		"execution_replacement",
		"execution_result_event",
		"position_state_event",
	}
	for _, et := range protected {
		if !IsExitEvent(et) {
			t.Errorf("IsExitEvent(%q) = false, want true", et)
		}
	}
}

func TestIsExitEvent_UnprotectedType_ReturnsFalse(t *testing.T) {
	t.Parallel()
	if IsExitEvent("market_data_event") {
		t.Error("IsExitEvent(market_data_event) = true, want false")
	}
	if IsExitEvent("") {
		t.Error("IsExitEvent(\"\") = true, want false")
	}
	if IsExitEvent("unknown") {
		t.Error("IsExitEvent(unknown) = true, want false")
	}
}

// ── ComputeBudget.Dequeue and Depth ──────────────────────────────────────────

func TestComputeBudget_DequeueAndDepth(t *testing.T) {
	t.Parallel()
	cb := NewComputeBudget(5)

	// Depth starts at zero.
	if d := cb.Depth(); d != 0 {
		t.Errorf("initial Depth() = %d, want 0", d)
	}

	// Enqueue increases depth.
	if err := cb.Enqueue(false); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}
	if d := cb.Depth(); d != 1 {
		t.Errorf("after 1 Enqueue, Depth() = %d, want 1", d)
	}

	// Dequeue decreases depth.
	cb.Dequeue()
	if d := cb.Depth(); d != 0 {
		t.Errorf("after Dequeue, Depth() = %d, want 0", d)
	}
}

func TestComputeBudget_ExitBypassesCap(t *testing.T) {
	t.Parallel()
	cb := NewComputeBudget(1)

	// Fill to cap with entry event.
	if err := cb.Enqueue(false); err != nil {
		t.Fatalf("entry Enqueue: %v", err)
	}
	// Entry event should now be rejected.
	if err := cb.Enqueue(false); err == nil {
		t.Fatal("expected ErrQueueFull for entry event over cap")
	}
	// But exit event should succeed.
	if err := cb.Enqueue(true); err != nil {
		t.Fatalf("exit Enqueue should bypass cap: %v", err)
	}
}

// ── RPCBudget.Release ─────────────────────────────────────────────────────────

func TestRPCBudget_Release_IsNoop(t *testing.T) {
	t.Parallel()
	b := NewRPCBudget(10, 10, 0)
	// Release on non-acquired endpoint should not panic.
	b.Release("http://localhost:9999")
	// Acquire after Release should still work (tokens are time-based, not count-based).
	b.Release("http://localhost:9999")
}

// ── BackpressurePolicy — additional paths ─────────────────────────────────────

func TestBackpressure_NilEvent_NeverDropped(t *testing.T) {
	t.Parallel()
	policy := NewBackpressurePolicy()
	ctx := context.Background()
	// nil event must never be dropped regardless of queue depth.
	drop, reason := policy.ShouldDrop(ctx, nil, 99999, 1)
	if drop {
		t.Error("nil event was dropped — invariant violated")
	}
	if reason != "" {
		t.Errorf("expected empty reason for nil event, got %q", reason)
	}
}

func TestBackpressure_EntryEvent_WithinCapacity_NotDropped(t *testing.T) {
	t.Parallel()
	policy := NewBackpressurePolicy()
	ctx := context.Background()
	entryEvt := &database.Event{EventType: "market_data_event"}
	// queueDepth (5) <= maxDepth (100) → should NOT drop.
	drop, _ := policy.ShouldDrop(ctx, entryEvt, 5, 100)
	if drop {
		t.Error("entry event within capacity should not be dropped")
	}
}
