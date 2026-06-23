package state_machine

import (
	"context"
	"errors"
	"testing"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
)

// ── ValidateTransition tests ──────────────────────────────────────────────────

func TestValidateTransition_AllowedPaths(t *testing.T) {
	cases := [][2]string{
		{"DETECTED", "DQ_PASSED"},
		{"DETECTED", "REJECTED"},
		{"REJECTED", "DQ_PASSED"}, // rescan recovery
		{"DQ_PASSED", "FEATURE_READY"},
		{"DQ_PASSED", "REJECTED"},
		{"FEATURE_READY", "EDGE_DETECTED"},
		{"FEATURE_READY", "REJECTED"},
		{"EDGE_DETECTED", "VALIDATED"},
		{"EDGE_DETECTED", "REJECTED"},
		{"VALIDATED", "SELECTED"},
		{"VALIDATED", "REJECTED"},
		{"SELECTED", "EXECUTED"},
		{"SELECTED", "FAILED"},
		{"EXECUTED", "POSITION_OPEN"},
		{"EXECUTED", "FAILED"},
		{"POSITION_OPEN", "POSITION_CLOSED"},
		{"POSITION_OPEN", "FAILED"},
		{"POSITION_CLOSED", "EVALUATED"},
	}
	for _, tc := range cases {
		if err := ValidateTransition(tc[0], tc[1]); err != nil {
			t.Errorf("expected %s→%s to be allowed, got: %v", tc[0], tc[1], err)
		}
	}
}

func TestValidateTransition_ForbiddenPaths(t *testing.T) {
	cases := [][2]string{
		{"DETECTED", "SELECTED"},
		{"EVALUATED", "DETECTED"},
		{"REJECTED", "FEATURE_READY"}, // REJECTED can only recover to DQ_PASSED
		{"FAILED", "EXECUTED"},
		{"POSITION_CLOSED", "REJECTED"},
	}
	for _, tc := range cases {
		if err := ValidateTransition(tc[0], tc[1]); err == nil {
			t.Errorf("expected %s→%s to be forbidden", tc[0], tc[1])
		}
	}
}

func TestValidateTransition_UnknownState(t *testing.T) {
	if err := ValidateTransition("UNKNOWN", "DETECTED"); err == nil {
		t.Error("expected error for unknown source state")
	}
}

func TestIsTerminal(t *testing.T) {
	if !IsTerminal("EVALUATED") {
		t.Error("EVALUATED should be terminal")
	}
	if IsTerminal("REJECTED") {
		t.Error("REJECTED should not be terminal — rescan recovery (REJECTED→DQ_PASSED) is permitted")
	}
	if !IsTerminal("FAILED") {
		t.Error("FAILED should be terminal")
	}
	if IsTerminal("DETECTED") {
		t.Error("DETECTED should not be terminal")
	}
}

func TestValidStates_Sorted(t *testing.T) {
	states := ValidStates()
	for i := 1; i < len(states); i++ {
		if states[i] < states[i-1] {
			t.Errorf("ValidStates not sorted at index %d: %s < %s", i, states[i], states[i-1])
		}
	}
}

// ── QuarantineChecker tests ───────────────────────────────────────────────────

type stubAdapter struct {
	quarantined []string
	violations  []string
}

func (s *stubAdapter) QuarantineToken(_ context.Context, tokenAddress, _ string) error {
	s.quarantined = append(s.quarantined, tokenAddress)
	return nil
}

func (s *stubAdapter) InsertStateViolation(_ context.Context, lifecycleID, _, _, _ string) error {
	s.violations = append(s.violations, lifecycleID)
	return nil
}

// Implement all remaining Adapter methods as stubs.
func (s *stubAdapter) Initialize(_ context.Context, _ database.Config) error { return nil }
func (s *stubAdapter) RunMigrations(_ context.Context) error                 { return nil }
func (s *stubAdapter) Close(_ context.Context) error                         { return nil }
func (s *stubAdapter) InsertEvent(_ context.Context, _ database.Event) error { return nil }
func (s *stubAdapter) ClaimNextEvent(_ context.Context, _ string, _ []string) (*database.Event, error) {
	return nil, nil
}
func (s *stubAdapter) MarkEventProcessed(_ context.Context, _ string) error { return nil }
func (s *stubAdapter) GetEventByID(_ context.Context, _ string) (*database.Event, error) {
	return nil, nil
}
func (s *stubAdapter) MarkEventExpired(_ context.Context, _, _ string) error { return nil }
func (s *stubAdapter) ReleaseEventClaim(_ context.Context, _ string) error   { return nil }
func (s *stubAdapter) UpsertIngestionWatermark(_ context.Context, _ string, _ uint64) error {
	return nil
}
func (s *stubAdapter) GetIngestionWatermark(_ context.Context, _ string) (uint64, error) {
	return 0, nil
}
func (s *stubAdapter) InsertMarketData(_ context.Context, _ contracts.MarketDataDTO) error {
	return nil
}
func (s *stubAdapter) GetMarketData(_ context.Context, _ string) (*contracts.MarketDataDTO, error) {
	return nil, nil
}
func (s *stubAdapter) StartLifecycle(_ context.Context, _ contracts.MarketDataDTO) (string, error) {
	return "", nil
}
func (s *stubAdapter) TransitionState(_ context.Context, _ database.TransitionRequest) error {
	return nil
}
func (s *stubAdapter) GetLifecycle(_ context.Context, _ string) (*database.Lifecycle, error) {
	return nil, nil
}
func (s *stubAdapter) GetLifecycleByToken(_ context.Context, _ string) (*database.Lifecycle, error) {
	return nil, nil
}
func (s *stubAdapter) InsertDataQuality(_ context.Context, _ contracts.DataQualityDTO) error {
	return nil
}
func (s *stubAdapter) InsertFeature(_ context.Context, _ contracts.FeatureDTO) error { return nil }
func (s *stubAdapter) InsertEdge(_ context.Context, _ contracts.EdgeDTO) error       { return nil }
func (s *stubAdapter) InsertValidatedEdge(_ context.Context, _ contracts.ValidatedEdgeDTO) error {
	return nil
}
func (s *stubAdapter) InsertSelection(_ context.Context, _ contracts.SelectionOutputDTO) error {
	return nil
}
func (s *stubAdapter) InsertAllocation(_ context.Context, _ contracts.AllocationDTO) error {
	return nil
}
func (s *stubAdapter) InsertExecutionResult(_ context.Context, _ contracts.ExecutionResultDTO) error {
	return nil
}
func (s *stubAdapter) InsertPositionState(_ context.Context, _ contracts.PositionStateDTO) error {
	return nil
}
func (s *stubAdapter) InsertEvaluation(_ context.Context, _ contracts.EvaluationDTO) error {
	return nil
}
func (s *stubAdapter) InsertLearningRecord(_ context.Context, _ contracts.LearningRecordDTO) error {
	return nil
}
func (s *stubAdapter) AllocateNonce(_ context.Context, _, _ string) (uint64, error)  { return 0, nil }
func (s *stubAdapter) ReconcileNonce(_ context.Context, _, _ string, _ uint64) error { return nil }
func (s *stubAdapter) GetOpenPositions(_ context.Context) ([]contracts.PositionStateDTO, error) {
	return nil, nil
}
func (s *stubAdapter) GetPosition(_ context.Context, _ string) (*contracts.PositionStateDTO, error) {
	return nil, nil
}
func (s *stubAdapter) GetClosedPositions(_ context.Context, _ int) ([]contracts.PositionStateDTO, error) {
	return nil, nil
}
func (s *stubAdapter) GetShadowGateStats(_ context.Context, _ int) (*database.ShadowGateStats, error) {
	return &database.ShadowGateStats{}, nil
}
func (s *stubAdapter) FindPositionByPrefix(_ context.Context, _ string) (*contracts.PositionStateDTO, error) {
	return nil, database.ErrNotFound
}
func (s *stubAdapter) CreateStrategyVersion(_ context.Context, _ database.StrategyVersion) error {
	return nil
}
func (s *stubAdapter) GetActiveStrategyVersion(_ context.Context) (*database.StrategyVersion, error) {
	return &database.StrategyVersion{StrategyVersionID: "v1"}, nil
}
func (s *stubAdapter) GetStrategyVersion(_ context.Context, _ string) (*database.StrategyVersion, error) {
	return nil, nil
}
func (s *stubAdapter) SetStrategyVersionStatus(_ context.Context, _, _, _ string) error { return nil }
func (s *stubAdapter) GetActiveStrategy(_ context.Context) (*database.StrategyVersion, error) {
	return nil, nil
}
func (s *stubAdapter) GetShadowStrategy(_ context.Context) (*database.StrategyVersion, error) {
	return nil, nil
}
func (s *stubAdapter) ActivateStrategyVersion(_ context.Context, _ string) error { return nil }
func (s *stubAdapter) GetSystemState(_ context.Context) (*contracts.SystemStateDTO, error) {
	return nil, nil
}
func (s *stubAdapter) UpsertSystemState(_ context.Context, _ contracts.SystemStateDTO, _ int64) (int64, error) {
	return 0, nil
}
func (s *stubAdapter) GetExposureSummary(_ context.Context) (*database.ExposureSummary, error) {
	return nil, nil
}
func (s *stubAdapter) GetEventsByTrace(_ context.Context, _ string) ([]database.Event, error) {
	return nil, nil
}
func (s *stubAdapter) GetLastEventTimestamp(_ context.Context, _ string) (time.Time, error) {
	return time.Time{}, database.ErrNotFound
}
func (s *stubAdapter) GetEventsByCorrelation(_ context.Context, _ string) ([]database.Event, error) {
	return nil, nil
}
func (s *stubAdapter) GetFailureChain(_ context.Context, _ string) ([]database.Event, error) {
	return nil, nil
}
func (s *stubAdapter) GetEventsByTraceIncludeArchive(_ context.Context, _ string) ([]contracts.EventEnvelope, error) {
	return nil, nil
}
func (s *stubAdapter) ArchiveEvents(_ context.Context, _ time.Time, _ int) (int, error) {
	return 0, nil
}
func (s *stubAdapter) CreateRun(_ context.Context, _ database.PipelineRun) error { return nil }
func (s *stubAdapter) UpdateRunStage(_ context.Context, _, _ string) error       { return nil }
func (s *stubAdapter) UpdateRunStatus(_ context.Context, _, _ string) error      { return nil }
func (s *stubAdapter) GetRun(_ context.Context, _ string) (*database.PipelineRun, error) {
	return nil, nil
}
func (s *stubAdapter) GetExecutionByLifecycle(_ context.Context, _ string) (*contracts.ExecutionResultDTO, error) {
	return nil, database.ErrNotFound
}
func (s *stubAdapter) GetProbabilityForLifecycle(_ context.Context, _ string) (float64, bool, error) {
	return 0, false, nil
}
func (s *stubAdapter) GetShadowTradesByWindow(_ context.Context, _, _ string) ([]database.ShadowTrade, error) {
	return nil, nil
}

func TestQuarantineChecker_BelowThreshold(t *testing.T) {
	stub := &stubAdapter{}
	qc := NewQuarantineChecker(3, stub, nil)
	for i := 0; i < 2; i++ {
		if err := qc.RecordViolation(context.Background(), "lc1", "0xTOKEN", "cas_failed"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if len(stub.quarantined) != 0 {
		t.Errorf("expected no quarantine, got %v", stub.quarantined)
	}
	if qc.ViolationCount("lc1") != 2 {
		t.Errorf("expected 2 violations, got %d", qc.ViolationCount("lc1"))
	}
}

func TestQuarantineChecker_AtThreshold(t *testing.T) {
	stub := &stubAdapter{}
	qc := NewQuarantineChecker(3, stub, nil)
	for i := 0; i < 3; i++ {
		_ = qc.RecordViolation(context.Background(), "lc2", "0xTOKEN2", "cas_failed")
	}
	if len(stub.quarantined) != 1 || stub.quarantined[0] != "0xTOKEN2" {
		t.Errorf("expected quarantine of 0xTOKEN2, got %v", stub.quarantined)
	}
	// After quarantine, violation count should reset.
	if qc.ViolationCount("lc2") != 0 {
		t.Errorf("expected violation count reset after quarantine, got %d", qc.ViolationCount("lc2"))
	}
}

func TestQuarantineChecker_AdapterError(t *testing.T) {
	errStub := &errorQuarantineAdapter{}
	qc := NewQuarantineChecker(1, errStub, nil)
	err := qc.RecordViolation(context.Background(), "lc3", "0xBAD", "cas_failed")
	if err == nil {
		t.Error("expected error when quarantine adapter fails")
	}
}

type errorQuarantineAdapter struct{ stubAdapter }

func (e *errorQuarantineAdapter) QuarantineToken(_ context.Context, _, _ string) error {
	return errors.New("quarantine failed")
}
