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
	"time"
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
		wsEndpoints:         eps,
		wsFailCounts:        failCounts,
		wsFailThreshold:     threshold,
		wsCooldownUntil:     make([]atomic.Int64, len(eps)),
		wsRateLimitCooldown: defaultWSRateLimitCooldown,
		logger:              slog.New(slog.NewTextHandler(os.Stderr, nil)),
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

// TestWSConnectRateLimitCooldown verifies that markWSRateLimited sets a cooldown
// on the endpoint and activeWSEntry skips it in favour of the other provider.
// It also checks that wsIdx is NOT mutated (no oscillation).
func TestWSConnectRateLimitCooldown(t *testing.T) {
	c := newTestSolanaClient([]string{"ws://quicknode", "ws://helius"}, 5)

	// Sanity: before any rate-limit, activeWSEntry returns index 0 (wsIdx=0).
	_, idx := c.activeWSEntry()
	if idx != 0 {
		t.Fatalf("initial activeWSEntry: idx=%d, want 0", idx)
	}

	// Mark endpoint 0 (quicknode) as rate-limited.
	c.markWSRateLimited(0, c.wsEndpoints[0], "test-program")

	// wsIdx must NOT have been mutated (no oscillation).
	if got := c.wsIdx.Load(); got != 0 {
		t.Fatalf("wsIdx after markWSRateLimited: got %d, want 0 (no mutation)", got)
	}

	// activeWSEntry should now return index 1 (helius) because 0 is in cooldown.
	entry, idx2 := c.activeWSEntry()
	if idx2 != 1 {
		t.Fatalf("activeWSEntry after cooldown on 0: idx=%d, want 1", idx2)
	}
	if entry.URL != "ws://helius" {
		t.Fatalf("activeWSEntry URL: got %q, want ws://helius", entry.URL)
	}

	// When ALL endpoints are in cooldown, activeWSEntry returns the soonest-
	// expiring one without panicking.
	c.markWSRateLimited(1, c.wsEndpoints[1], "test-program")
	_, idx3 := c.activeWSEntry()
	if idx3 != 0 && idx3 != 1 {
		t.Fatalf("all-cooled activeWSEntry: idx=%d, want 0 or 1", idx3)
	}

	// After the cooldown expires, activeWSEntry should return to index 0.
	c.wsCooldownUntil[0].Store(0)
	c.wsCooldownUntil[1].Store(0)
	_, idxAfter := c.activeWSEntry()
	if idxAfter != 0 {
		t.Fatalf("after cooldown expiry: idx=%d, want 0", idxAfter)
	}
}

// TestWSConnectRateLimitNoOscillation verifies that when both providers are
// rate-limited and multiple goroutines call markWSRateLimited concurrently,
// wsIdx remains stable (no spinning).
func TestWSConnectRateLimitNoOscillation(t *testing.T) {
	c := newTestSolanaClient([]string{"ws://quicknode", "ws://helius"}, 5)
	c.wsRateLimitCooldown = 50 * time.Millisecond // short cooldown for test speed

	// Six goroutines all 429 at once (simulates 6 program subscribers).
	done := make(chan struct{})
	for i := 0; i < 6; i++ {
		go func(slot int) {
			c.markWSRateLimited(slot%2, c.wsEndpoints[slot%2], "prog")
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 6; i++ {
		<-done
	}

	// wsIdx must be unchanged — all rotation happened via cooldowns only.
	if got := c.wsIdx.Load(); got != 0 {
		t.Fatalf("wsIdx after 6 concurrent rate-limits: got %d, want 0", got)
	}
}
