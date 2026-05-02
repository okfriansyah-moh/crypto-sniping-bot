package position

import (
	"context"
	"testing"
	"time"
)

// TestPollExit_TimeExitFiresWhenPriceMissing protects the price-feed-resilient
// TIME exit invariant:
//
// Without a working price feed (priceClient nil or returning "" / error)
// the position monitoring loop previously skipped PollExit entirely, so the
// TIME exit never fired and positions could stay open indefinitely. This is
// what produced the 1h+ stuck positions reported in production logs.
//
// With the fix, when the loop calls PollExit with an empty/unparseable
// current price BUT the position has reached MaxHoldSeconds, the TIME exit
// MUST still fire so the slot is reclaimed and the lifecycle progresses.
// PnL stays at 0 and ExitPrice stays "" — best truth available without a
// market quote — but the position transitions to status="exited".
func TestPollExit_TimeExitFiresWhenPriceMissing(t *testing.T) {
	mod := New(defaultPosCfg())
	pos := openPosition()
	pos.MaxHoldSeconds = 60

	// Open 5 minutes ago — well past MaxHoldSeconds.
	openedAt := time.Now().UTC().Add(-5 * time.Minute)
	pos.OpenedAt = openedAt.Format(time.RFC3339Nano)

	out, err := mod.PollExit(context.Background(), pos, "", time.Now().UTC())
	if err != nil {
		t.Fatalf("PollExit returned error: %v", err)
	}
	if out.Status != "exited" {
		t.Fatalf("expected status=exited (TIME) without price, got %q", out.Status)
	}
	if out.ExitReason != "TIME" {
		t.Fatalf("expected ExitReason=TIME, got %q", out.ExitReason)
	}
}

// TestPollExit_NoExitBeforeMaxHoldWithoutPrice verifies that without a price
// feed we don't aggressively close positions before their hold window.
// Only TIME may fire on missing price; TP/SL must NOT fabricate prices.
func TestPollExit_NoExitBeforeMaxHoldWithoutPrice(t *testing.T) {
	mod := New(defaultPosCfg())
	pos := openPosition()
	pos.MaxHoldSeconds = 600

	// Opened 30s ago — far inside the hold window.
	openedAt := time.Now().UTC().Add(-30 * time.Second)
	pos.OpenedAt = openedAt.Format(time.RFC3339Nano)

	out, err := mod.PollExit(context.Background(), pos, "", time.Now().UTC())
	if err != nil {
		t.Fatalf("PollExit returned error: %v", err)
	}
	if out.Status == "exited" {
		t.Fatalf("expected position to remain open before MaxHoldSeconds, got status=%q reason=%q",
			out.Status, out.ExitReason)
	}
}
