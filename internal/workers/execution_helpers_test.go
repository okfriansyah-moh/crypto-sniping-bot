package workers

import (
	"testing"

	"crypto-sniping-bot/contracts"
)

// testAlloc returns a minimal AllocationDTO for helper function tests.
func testAlloc(eventID, traceID, corrID, versionID, walletAddr, rejectReason string) contracts.AllocationDTO {
	return contracts.AllocationDTO{
		EventID:          eventID,
		TraceID:          traceID,
		CorrelationID:    corrID,
		VersionID:        versionID,
		TokenLifecycleID: "tl-abc",
		ExecutionID:      "exec-001",
		WalletAddress:    walletAddr,
		RejectReason:     rejectReason,
	}
}

// ── simulatedExecResult ───────────────────────────────────────────────────────

func TestSimulatedExecResult_IsConfirmedAndSimulated(t *testing.T) {
	// Arrange
	alloc := testAlloc("evt-1", "trace-1", "corr-1", "v1", "0xWALLET", "")

	// Act
	result := simulatedExecResult(alloc, "2026-01-01T00:00:00Z")

	// Assert
	if result.Status != "confirmed" {
		t.Errorf("Status: want confirmed, got %q", result.Status)
	}
	if !result.Success {
		t.Error("Success: want true, got false")
	}
	if !result.Simulated {
		t.Error("Simulated: want true, got false")
	}
}

func TestSimulatedExecResult_TraceFieldsPropagated(t *testing.T) {
	// Arrange
	alloc := testAlloc("evt-sim", "trace-sim", "corr-sim", "ver-sim", "0xW", "")

	// Act
	result := simulatedExecResult(alloc, "2026-01-01T00:00:00Z")

	// Assert
	if result.TraceID != "trace-sim" {
		t.Errorf("TraceID: want %q, got %q", "trace-sim", result.TraceID)
	}
	if result.CorrelationID != "corr-sim" {
		t.Errorf("CorrelationID: want %q, got %q", "corr-sim", result.CorrelationID)
	}
	if result.CausationID != "evt-sim" {
		t.Errorf("CausationID: want %q, got %q", "evt-sim", result.CausationID)
	}
	if result.VersionID != "ver-sim" {
		t.Errorf("VersionID: want %q, got %q", "ver-sim", result.VersionID)
	}
}

func TestSimulatedExecResult_EventIDIsDeterministic(t *testing.T) {
	// Arrange
	alloc := testAlloc("evt-x", "t", "c", "v", "0xW", "")

	// Act: call twice, EventID must be identical.
	r1 := simulatedExecResult(alloc, "2026-01-01T00:00:00Z")
	r2 := simulatedExecResult(alloc, "2026-01-02T00:00:00Z") // different timestamp — EventID must not change

	// Assert
	if r1.EventID != r2.EventID {
		t.Errorf("EventID non-deterministic: %q vs %q", r1.EventID, r2.EventID)
	}
}

func TestSimulatedExecResult_MempoolRoute_IsPublic(t *testing.T) {
	// Arrange
	alloc := testAlloc("evt-2", "t2", "c2", "v2", "0xW2", "")

	// Act
	result := simulatedExecResult(alloc, "now")

	// Assert
	if result.MempoolRoute != "public" {
		t.Errorf("MempoolRoute: want public, got %q", result.MempoolRoute)
	}
}

// ── rejectedExecResult ────────────────────────────────────────────────────────

func TestRejectedExecResult_StatusIsRejected(t *testing.T) {
	// Arrange
	alloc := testAlloc("evt-rej", "t-rej", "c-rej", "v-rej", "0xW-rej", "below_min_edge")

	// Act
	result := rejectedExecResult(alloc, "2026-01-01T00:00:00Z")

	// Assert
	if result.Status != "rejected" {
		t.Errorf("Status: want rejected, got %q", result.Status)
	}
	if result.Success {
		t.Error("Success: want false, got true")
	}
}

func TestRejectedExecResult_PropagatesRejectReason(t *testing.T) {
	// Arrange
	alloc := testAlloc("evt-rej2", "t", "c", "v", "0xW", "price_too_high")

	// Act
	result := rejectedExecResult(alloc, "now")

	// Assert
	if result.RejectionReason != "price_too_high" {
		t.Errorf("RejectionReason: want price_too_high, got %q", result.RejectionReason)
	}
}

func TestRejectedExecResult_EventIDIsDeterministic(t *testing.T) {
	// Arrange
	alloc := testAlloc("evt-rej3", "t", "c", "v", "0xW", "")

	// Act
	r1 := rejectedExecResult(alloc, "ts-1")
	r2 := rejectedExecResult(alloc, "ts-2")

	// Assert
	if r1.EventID != r2.EventID {
		t.Errorf("EventID non-deterministic: %q vs %q", r1.EventID, r2.EventID)
	}
}

// ── haltedExecResult ──────────────────────────────────────────────────────────

func TestHaltedExecResult_StatusIsRejected(t *testing.T) {
	// Arrange
	alloc := testAlloc("evt-halt", "t-halt", "c-halt", "v-halt", "0xW-halt", "")

	// Act
	result := haltedExecResult(alloc, "2026-01-01T00:00:00Z")

	// Assert
	if result.Status != "rejected" {
		t.Errorf("Status: want rejected, got %q", result.Status)
	}
	if result.Success {
		t.Error("Success: want false, got true")
	}
}

func TestHaltedExecResult_RejectionReasonIsSystemHalted(t *testing.T) {
	// Arrange
	alloc := testAlloc("evt-halt2", "t", "c", "v", "0xW", "")

	// Act
	result := haltedExecResult(alloc, "now")

	// Assert
	if result.RejectionReason != "system_halted" {
		t.Errorf("RejectionReason: want system_halted, got %q", result.RejectionReason)
	}
}

func TestHaltedExecResult_TraceFieldsPropagated(t *testing.T) {
	// Arrange
	alloc := testAlloc("evt-halt3", "trace-halt", "corr-halt", "ver-halt", "0xW", "")

	// Act
	result := haltedExecResult(alloc, "now")

	// Assert
	if result.TraceID != "trace-halt" {
		t.Errorf("TraceID: want trace-halt, got %q", result.TraceID)
	}
	if result.CausationID != "evt-halt3" {
		t.Errorf("CausationID: want evt-halt3, got %q", result.CausationID)
	}
}

// ── mevRouteToNamespace ───────────────────────────────────────────────────────

func TestMevRouteToNamespace_Flashbots_ReturnsPrivateFlashbots(t *testing.T) {
	// Arrange / Act / Assert
	if got := mevRouteToNamespace("flashbots"); got != "private_flashbots" {
		t.Errorf("mevRouteToNamespace(flashbots): want private_flashbots, got %q", got)
	}
}

func TestMevRouteToNamespace_Eden_ReturnsPrivateFlashbots(t *testing.T) {
	// Arrange / Act / Assert: eden uses Flashbots-compatible relay semantics.
	if got := mevRouteToNamespace("eden"); got != "private_flashbots" {
		t.Errorf("mevRouteToNamespace(eden): want private_flashbots, got %q", got)
	}
}

func TestMevRouteToNamespace_Beaverbuild_ReturnsPrivateBeaverbuild(t *testing.T) {
	// Arrange / Act / Assert
	if got := mevRouteToNamespace("beaverbuild"); got != "private_beaverbuild" {
		t.Errorf("mevRouteToNamespace(beaverbuild): want private_beaverbuild, got %q", got)
	}
}

func TestMevRouteToNamespace_Public_ReturnsPublic(t *testing.T) {
	// Arrange / Act / Assert
	if got := mevRouteToNamespace("public"); got != "public" {
		t.Errorf("mevRouteToNamespace(public): want public, got %q", got)
	}
}

func TestMevRouteToNamespace_Unknown_ReturnsPublic(t *testing.T) {
	// Arrange / Act / Assert: unknown relay names default to "public".
	for _, name := range []string{"", "titan", "unknown-relay", "FLASHBOTS"} {
		if got := mevRouteToNamespace(name); got != "public" {
			t.Errorf("mevRouteToNamespace(%q): want public, got %q", name, got)
		}
	}
}
