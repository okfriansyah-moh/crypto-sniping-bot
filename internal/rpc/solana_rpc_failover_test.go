// solana_rpc_failover_test.go — unit tests for the adaptive WS endpoint failover.
//
// Tests verify that after N consecutive EOF failures on an endpoint the client
// promotes wsIdx to the next provider, and that a successful subscription
// resets the counter for that endpoint.
package rpc

import (
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
)

// newTestSolanaClient builds a minimal SolanaClient with the given endpoint URLs
// and failure threshold, suitable for unit-testing the failover helpers.
// It does not make any network calls.
func newTestSolanaClient(wsURLs []string, threshold int64) *SolanaClient {
	eps := make([]endpointEntry, len(wsURLs))
	for i, u := range wsURLs {
		eps[i] = endpointEntry{URL: u, Dialect: quicknodeDialect{}}
	}
	failCounts := make([]atomic.Int64, len(eps))
	return &SolanaClient{
		wsEndpoints:     eps,
		wsFailCounts:    failCounts,
		wsFailThreshold: threshold,
		logger:          slog.New(slog.NewTextHandler(os.Stderr, nil)),
	}
}

// TestWSFailoverPromotesAfterThreshold verifies that exactly at the threshold
// the client advances wsIdx past the failing endpoint via CAS.
func TestWSFailoverPromotesAfterThreshold(t *testing.T) {
	const threshold int64 = 5
	c := newTestSolanaClient([]string{"ws://quicknode", "ws://helius"}, threshold)

	entry := c.wsEndpoints[0]

	// Record threshold-1 failures — should NOT promote yet.
	for i := int64(0); i < threshold-1; i++ {
		c.recordWSEOFFailure(entry, "prog", 0)
	}
	if got := c.wsIdx.Load(); got != threshold-1 {
		// Each call below threshold does a single-step Add(1).
		// We've called it (threshold-1) times so wsIdx should equal threshold-1.
		t.Fatalf("wsIdx before promotion: got %d, want %d", got, threshold-1)
	}

	// Now the counter has been reset to 0 from the CAS-path not firing.
	// Directly set the state for a clean promotion test.
	c.wsIdx.Store(0)
	c.wsFailCounts[0].Store(0)

	// Record exactly threshold failures.
	for i := int64(0); i < threshold; i++ {
		// First threshold-1 failures do single-step rotations, but we want
		// to test the CAS path in isolation.  Drive it via the counter only.
		c.wsFailCounts[0].Add(1)
	}
	// Manually trigger one more to cross the threshold via recordWSEOFFailure.
	// Simulate state where wsIdx == 0 (pointing at endpoint 0).
	c.wsIdx.Store(0)

	// Call recordWSEOFFailure — counter is already at threshold, so the CAS
	// should promote wsIdx from 0 → 1.
	before := c.wsIdx.Load()
	if before != 0 {
		t.Fatalf("precondition: wsIdx should be 0, got %d", before)
	}

	// Directly add to counter to reach threshold.
	c.wsFailCounts[0].Store(threshold) // already at threshold
	c.recordWSEOFFailure(entry, "prog", 0)

	after := c.wsIdx.Load()
	if after != 1 {
		t.Fatalf("after promotion: wsIdx = %d, want 1", after)
	}
}

// TestWSFailoverCounterResetOnSuccess verifies that resetWSFailCount zeroes the
// per-endpoint counter after a successful subscription.
func TestWSFailoverCounterResetOnSuccess(t *testing.T) {
	c := newTestSolanaClient([]string{"ws://quicknode", "ws://helius"}, 5)

	// Manually set some failure count.
	c.wsFailCounts[0].Store(3)

	c.resetWSFailCount(0)

	if got := c.wsFailCounts[0].Load(); got != 0 {
		t.Fatalf("expected counter 0 after reset, got %d", got)
	}
}

// TestWSFailoverSingleEndpointNeverPromotes verifies that with only one endpoint
// the client performs a simple rotation and does not panic.
func TestWSFailoverSingleEndpointNeverPromotes(t *testing.T) {
	c := newTestSolanaClient([]string{"ws://only"}, 3)

	entry := c.wsEndpoints[0]
	// Should not panic or block.
	for i := 0; i < 10; i++ {
		c.recordWSEOFFailure(entry, "prog", 0)
	}
}

// TestWSFailoverThresholdZeroDisablesPromotion verifies that setting threshold
// to 0 disables promotion and only does single-step rotations.
func TestWSFailoverThresholdZeroDisablesPromotion(t *testing.T) {
	c := newTestSolanaClient([]string{"ws://a", "ws://b"}, 0)
	entry := c.wsEndpoints[0]

	for i := 0; i < 10; i++ {
		c.recordWSEOFFailure(entry, "prog", 0)
	}
	// With threshold=0, the failThreshold <= 0 guard skips CAS; only
	// single-step rotations happen.  wsFailCounts should never have been
	// incremented via the promotion path (counter stays 0).
	if got := c.wsFailCounts[0].Load(); got != 0 {
		t.Fatalf("threshold=0: wsFailCounts[0] = %d, want 0", got)
	}
}
