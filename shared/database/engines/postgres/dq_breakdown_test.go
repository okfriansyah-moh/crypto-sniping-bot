package postgres

import (
	"strings"
	"testing"

	"crypto-sniping-bot/shared/database"
)

func TestDQBreakdownCountsSQL_IsReadOnlySelect(t *testing.T) {
	t.Parallel()
	upper := strings.ToUpper(dqBreakdownCountsSQL)
	if strings.Contains(upper, "INSERT ") || strings.Contains(upper, "UPDATE ") || strings.Contains(upper, "DELETE ") {
		t.Fatal("dqBreakdownCountsSQL must be SELECT-only")
	}
}

func TestDQBreakdownCountsSQL_IncludesAllDecisionBuckets(t *testing.T) {
	t.Parallel()
	for _, fragment := range []string{
		"decision = 'PASS'",
		"decision = 'RISKY_PASS'",
		"decision = 'REJECT'",
		"current_state = 'DQ_SKIPPED'",
	} {
		if !strings.Contains(dqBreakdownCountsSQL, fragment) {
			t.Fatalf("dqBreakdownCountsSQL must reference %q", fragment)
		}
	}
}

func TestDQBreakdownCountsSQL_ChainFilterOptional(t *testing.T) {
	t.Parallel()
	if !strings.Contains(dqBreakdownCountsSQL, "$2 = '' OR chain = $2") {
		t.Fatal("dqBreakdownCountsSQL must support optional chain filter")
	}
	if !strings.Contains(dqBreakdownCountsSQL, "$2 = '' OR md.chain = $2") {
		t.Fatal("dqBreakdownCountsSQL skip bucket must support optional chain filter")
	}
}

func TestDQTopRejectReasonsSQL_UnnestsRejectReasons(t *testing.T) {
	t.Parallel()
	if !strings.Contains(dqTopRejectReasonsSQL, "jsonb_array_elements_text") {
		t.Fatal("dqTopRejectReasonsSQL must unnest reject_reasons JSON array")
	}
	if !strings.Contains(dqTopRejectReasonsSQL, "decision = 'REJECT'") {
		t.Fatal("dqTopRejectReasonsSQL must filter REJECT decisions only")
	}
}

func TestCapDQWindowHours_Bounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want int
	}{
		{0, 24},
		{-5, 24},
		{1, 1},
		{24, 24},
		{168, 168},
		{200, 168},
	}
	for _, tc := range tests {
		if got := database.CapDQWindowHours(tc.in); got != tc.want {
			t.Errorf("CapDQWindowHours(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestDQBreakdown_PassRateMatchesMockupSemantics(t *testing.T) {
	t.Parallel()
	// Mockup pass rate = (PASS + RISKY) / total including SKIP.
	b := &database.DQBreakdown{
		PassCount:      4,
		RiskyPassCount: 2,
		RejectCount:    180,
		SkipCount:      126,
	}
	b.TotalDecisions = b.PassCount + b.RiskyPassCount + b.RejectCount + b.SkipCount
	if b.TotalDecisions != 312 {
		t.Fatalf("total = %d, want 312", b.TotalDecisions)
	}
	b.PassRatePct = float64(b.PassCount+b.RiskyPassCount) / float64(b.TotalDecisions) * 100
	if b.PassRatePct < 1.92 || b.PassRatePct > 1.93 {
		t.Fatalf("pass rate = %.3f%%, want ~1.9%%", b.PassRatePct)
	}
}
