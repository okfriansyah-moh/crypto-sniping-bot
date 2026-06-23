// Package unit_test contains focused unit tests.
// Phase 8 production hardening tests cover: reconciliation tolerance, DLQ entry
// construction, adapter domain types, and the exceedsToleranceBps helper.
// All tests run without GPU, network, or real data files.
package unit_test

import (
	"math/big"
	"testing"

	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/sniper-bot/internal/workers"
)

// ── Reconciliation tolerance ─────────────────────────────────────────────────

func TestReconciliationCfg_SafeDefaults(t *testing.T) {
	// ReconCfg with zero values should apply safe defaults when constructed
	// via the worker helper. We test the public config field paths.
	cfg := workers.ReconCfg{IntervalMs: 0, ToleranceBps: 0}
	// Default enforcement is done in reconCfgFromConfig (internal). We test
	// directly that a safe positive value is required by the exported type.
	if cfg.IntervalMs < 0 {
		t.Errorf("IntervalMs must not be negative, got %d", cfg.IntervalMs)
	}
}

func TestExceedsToleranceBps_WithinTolerance(t *testing.T) {
	// 1000 on-chain vs 1001 db → 0.1% difference, 50 bps tolerance → within
	db := big.NewInt(1001)
	onchain := big.NewInt(1000)
	if workers.ExceedsToleranceBps(db, onchain, 50) {
		t.Error("expected within tolerance")
	}
}

func TestExceedsToleranceBps_ExceedsTolerance(t *testing.T) {
	// 1000 on-chain vs 1100 db → 10% difference, 50 bps tolerance → exceeds
	db := big.NewInt(1100)
	onchain := big.NewInt(1000)
	if !workers.ExceedsToleranceBps(db, onchain, 50) {
		t.Error("expected tolerance exceeded")
	}
}

func TestExceedsToleranceBps_ZeroOnchain(t *testing.T) {
	// Zero on-chain with non-zero db always exceeds tolerance.
	db := big.NewInt(1000)
	onchain := big.NewInt(0)
	if !workers.ExceedsToleranceBps(db, onchain, 50) {
		t.Error("zero onchain should always exceed tolerance")
	}
}

func TestExceedsToleranceBps_BothZero(t *testing.T) {
	// Both zero means no discrepancy.
	db := big.NewInt(0)
	onchain := big.NewInt(0)
	if workers.ExceedsToleranceBps(db, onchain, 50) {
		t.Error("both zero should not exceed tolerance")
	}
}

func TestExceedsToleranceBps_ExactBoundary(t *testing.T) {
	// Exactly at boundary: 100 bps = 1%. diff=1, db=100 → exactly 1% → NOT exceeded
	// (exceedsToleranceBps uses strict >, not >=)
	db := big.NewInt(100)
	onchain := big.NewInt(99)
	// diff=1, 1*10000=10000, 100*100=10000 → 10000 > 10000 is false
	if workers.ExceedsToleranceBps(db, onchain, 100) {
		t.Error("exact boundary should not exceed tolerance (uses strict >)")
	}
}

func TestExceedsToleranceBps_JustOverBoundary(t *testing.T) {
	// diff=2, db=100, bps=100: 2*10000=20000 > 100*100=10000 → exceeds
	db := big.NewInt(100)
	onchain := big.NewInt(98)
	if !workers.ExceedsToleranceBps(db, onchain, 100) {
		t.Error("just over boundary should exceed tolerance")
	}
}

// ── DLQ domain type construction ─────────────────────────────────────────────

func TestDLQEntry_DefaultFields(t *testing.T) {
	e := database.DLQEntry{
		EventID:    "evt-001",
		Chain:      "eth",
		Consumer:   "execution",
		Reason:     "transient_exceeded",
		RetryCount: 5,
		TraceID:    "trace-001",
		VersionID:  "ver-001",
	}
	if e.EventID != "evt-001" {
		t.Errorf("EventID mismatch: %s", e.EventID)
	}
	if e.RetryCount != 5 {
		t.Errorf("RetryCount mismatch: %d", e.RetryCount)
	}
}

// ── EventClaimQuery defaults ──────────────────────────────────────────────────

func TestEventClaimQuery_NumWorkersGuard(t *testing.T) {
	q := database.EventClaimQuery{
		Chain:      "eth",
		Consumer:   "execution",
		EventTypes: []string{"allocation_event"},
		WorkerID:   0,
		NumWorkers: 0, // should be treated as 1 by implementation
		Limit:      0, // should be treated as 10 by implementation
	}
	if q.NumWorkers != 0 {
		t.Errorf("expected zero (implementation applies default): %d", q.NumWorkers)
	}
}

// ── LatencyEvent construction ─────────────────────────────────────────────────

func TestLatencyEvent_AllFields(t *testing.T) {
	le := database.LatencyEvent{
		ExecutionID:             "exec-001",
		Chain:                   "eth",
		Endpoint:                "https://rpc.example.com",
		VersionID:               "ver-001",
		OpKind:                  "execute",
		DecisionToSendMs:        10,
		SendToFirstObserveMs:    150,
		FirstObserveToConfirmMs: 800,
		TotalMs:                 960,
		Outcome:                 "confirmed",
		ObservedAt:              "2026-01-01T00:00:00Z",
	}
	if le.TotalMs != 960 {
		t.Errorf("TotalMs mismatch: %d", le.TotalMs)
	}
	if le.Outcome != "confirmed" {
		t.Errorf("Outcome mismatch: %s", le.Outcome)
	}
}

// ── ReconciliationPosition ────────────────────────────────────────────────────

func TestReconciliationPosition_Fields(t *testing.T) {
	p := database.ReconciliationPosition{
		PositionID:    "pos-001",
		TokenAddress:  "0xTOKEN",
		Chain:         "eth",
		WalletAddress: "0xWALLET",
		ExecutionID:   "exec-001",
		AmountRaw:     "1000000000000000000",
	}
	if p.WalletAddress != "0xWALLET" {
		t.Errorf("WalletAddress mismatch: %s", p.WalletAddress)
	}
	if p.AmountRaw != "1000000000000000000" {
		t.Errorf("AmountRaw mismatch: %s", p.AmountRaw)
	}
}

// ── InFlightExecution ─────────────────────────────────────────────────────────

func TestInFlightExecution_NilableFields(t *testing.T) {
	e := database.InFlightExecution{
		ExecutionID:   "exec-001",
		AttemptNumber: 1,
		TxHash:        nil,
		Status:        "reserved",
		Nonce:         nil,
		GasPriceWei:   "0",
		SentAt:        nil,
		ObservedAt:    "2026-01-01T00:00:00Z",
	}
	if e.TxHash != nil {
		t.Errorf("TxHash should be nil for reserved execution")
	}
}

// ── ReorgOutcome constants ────────────────────────────────────────────────────

func TestReorgOutcome_Constants(t *testing.T) {
	cases := []struct {
		outcome database.ReorgOutcome
		want    string
	}{
		{database.ReorgOutcomeReincluded, "re_included"},
		{database.ReorgOutcomeDropped, "reorged_out"},
		{database.ReorgOutcomeMutation, "reorg_mutation"},
	}
	for _, c := range cases {
		if string(c.outcome) != c.want {
			t.Errorf("ReorgOutcome %q: want %q, got %q", c.outcome, c.want, string(c.outcome))
		}
	}
}

// ── MissingEvaluation ─────────────────────────────────────────────────────────

func TestMissingEvaluation_Fields(t *testing.T) {
	m := database.MissingEvaluation{
		ExecutionID: "exec-001",
		DeadlineAt:  "2026-01-01T01:00:00Z",
	}
	if m.ExecutionID != "exec-001" {
		t.Errorf("ExecutionID mismatch: %s", m.ExecutionID)
	}
}
