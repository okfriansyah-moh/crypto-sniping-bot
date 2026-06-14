package postgres

import (
	"strings"
	"testing"
)

// TestRescan_IncludesSkipWhenFlagSetAndProbeFailed verifies that the rescan SQL
// contains the $9 condition that includes SKIP'd tokens when the
// serial_launcher_skipped flag is set and holder_dist_known is FALSE.
func TestRescan_IncludesSkipWhenFlagSetAndProbeFailed(t *testing.T) {
	t.Parallel()
	if !strings.Contains(rescanQuerySQL, "$9 AND dq.decision = 'SKIP'") {
		t.Error("query must include $9 skip retry condition for SKIP decisions")
	}
	if !strings.Contains(rescanQuerySQL, `dq.flags @> '["serial_launcher_skipped"]'::jsonb`) {
		t.Error("query must filter by serial_launcher_skipped flag in flags JSONB column")
	}
}

// TestRescan_ExcludesSkipWhenFlagOff verifies that LIMIT is bound to $10 (not $9),
// confirming that $9 is the IncludeSkippedForRetry parameter.
// When $9=false the SKIP branch is short-circuited; LIMIT remains at $10.
func TestRescan_ExcludesSkipWhenFlagOff(t *testing.T) {
	t.Parallel()
	if !strings.Contains(rescanQuerySQL, "LIMIT $10") {
		t.Error("LIMIT must be $10 after adding $9 skip retry param")
	}
	// Also verify $9 is referenced before $10 — maintains correct parameter order.
	idx9 := strings.Index(rescanQuerySQL, "$9")
	idx10 := strings.Index(rescanQuerySQL, "$10")
	if idx9 < 0 || idx10 < 0 || idx9 >= idx10 {
		t.Errorf("$9 (skip retry) must appear before $10 (LIMIT) in query; idx9=%d idx10=%d", idx9, idx10)
	}
}

// TestRescan_ExcludesSkipWithKnownHolderDist verifies that the SKIP retry
// condition is gated on holder_dist_known=FALSE, so tokens with a known (valid)
// holder distribution are never re-ingested via the skip-retry path.
func TestRescan_ExcludesSkipWithKnownHolderDist(t *testing.T) {
	t.Parallel()
	if !strings.Contains(rescanQuerySQL, "COALESCE(md.holder_dist_known, FALSE) = FALSE") {
		t.Error("query must filter to holder_dist_known=FALSE for skip retry eligibility")
	}
}
