package database

import (
	"testing"
	"time"
)

func TestProbePendingID_DeterministicPerHour(t *testing.T) {
	due := time.Date(2026, 6, 15, 14, 30, 0, 0, time.UTC)
	a := ProbePendingID("evt-abc", due)
	b := ProbePendingID("evt-abc", due.Add(10*time.Minute))
	if a != b {
		t.Fatalf("same hour boundary: want identical IDs, got %q vs %q", a, b)
	}
	nextHour := ProbePendingID("evt-abc", due.Add(time.Hour))
	if nextHour == a {
		t.Fatal("expected different pending ID for next hour window")
	}
}
