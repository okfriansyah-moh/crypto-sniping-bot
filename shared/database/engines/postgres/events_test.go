package postgres

import (
	"strings"
	"testing"

	"crypto-sniping-bot/shared/database"
)

func TestListRecentEventsSQL_IsReadOnlySelect(t *testing.T) {
	t.Parallel()
	upper := strings.ToUpper(listRecentEventsSQL)
	if strings.Contains(upper, "INSERT ") || strings.Contains(upper, "UPDATE ") || strings.Contains(upper, "DELETE ") {
		t.Fatal("listRecentEventsSQL must be SELECT-only")
	}
	if !strings.Contains(listRecentEventsSQL, "ORDER BY e.created_at DESC") {
		t.Fatal("listRecentEventsSQL must order newest first")
	}
	if !strings.Contains(listRecentEventsSQL, "LIMIT $2") {
		t.Fatal("listRecentEventsSQL must bound result size with LIMIT")
	}
}

func TestListRecentEventsSQL_ChainFilterUsesPayloadFallback(t *testing.T) {
	t.Parallel()
	if !strings.Contains(listRecentEventsSQL, "payload->>'chain'") {
		t.Fatal("listRecentEventsSQL must fall back to payload chain for legacy rows")
	}
	if !strings.Contains(listRecentEventsSQL, "payload->>'token_address'") {
		t.Fatal("listRecentEventsSQL must extract token_address for activity rows")
	}
}

func TestCapRecentEventsLimit_Bounds(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in, want int
	}{
		{0, 50},
		{-1, 50},
		{1, 1},
		{50, 50},
		{200, 200},
		{201, 200},
		{1000, 200},
	}
	for _, tc := range tests {
		if got := database.CapRecentEventsLimit(tc.in); got != tc.want {
			t.Errorf("CapRecentEventsLimit(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
}
