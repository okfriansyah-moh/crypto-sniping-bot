package orchestrator_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/sniper-bot/internal/orchestrator"
)

// ─── Mock Adapter ─────────────────────────────────────────────────────────────

type mockAdapter struct {
	database.ProbePendingQueueStub
	versions map[string]*database.StrategyVersion
	runs     map[string]*database.PipelineRun
	events   []database.Event
	active   *string
}

func newMock() *mockAdapter {
	return &mockAdapter{
		versions: make(map[string]*database.StrategyVersion),
		runs:     make(map[string]*database.PipelineRun),
	}
}

func (m *mockAdapter) Initialize(_ context.Context, _ database.Config) error { return nil }
func (m *mockAdapter) RunMigrations(_ context.Context) error                 { return nil }
func (m *mockAdapter) Close(_ context.Context) error                         { return nil }

func (m *mockAdapter) InsertEvent(_ context.Context, e database.Event) error {
	m.events = append(m.events, e)
	return nil
}
func (m *mockAdapter) ClaimNextEvent(_ context.Context, _ string, _ []string) (*database.Event, error) {
	return nil, nil
}
func (m *mockAdapter) MarkEventProcessed(_ context.Context, _ string) error { return nil }
func (m *mockAdapter) GetEventByID(_ context.Context, _ string) (*database.Event, error) {
	return nil, database.ErrNotFound
}
func (m *mockAdapter) GetEventsByTrace(_ context.Context, _ string) ([]database.Event, error) {
	return nil, nil
}
func (m *mockAdapter) GetLastEventTimestamp(_ context.Context, _ string) (time.Time, error) {
	return time.Time{}, database.ErrNotFound
}
func (m *mockAdapter) GetEventsByCorrelation(_ context.Context, _ string) ([]database.Event, error) {
	return nil, nil
}
func (m *mockAdapter) GetFailureChain(_ context.Context, _ string) ([]database.Event, error) {
	return nil, nil
}

func (m *mockAdapter) CreateRun(_ context.Context, run database.PipelineRun) error {
	if _, ok := m.runs[run.RunID]; !ok {
		cp := run
		m.runs[run.RunID] = &cp
	}
	return nil
}
func (m *mockAdapter) UpdateRunStage(_ context.Context, runID, stage string) error {
	if r, ok := m.runs[runID]; ok {
		r.LastCompletedStage = &stage
	}
	return nil
}
func (m *mockAdapter) UpdateRunStatus(_ context.Context, runID, status string) error {
	if r, ok := m.runs[runID]; ok {
		r.Status = status
	}
	return nil
}
func (m *mockAdapter) GetRun(_ context.Context, runID string) (*database.PipelineRun, error) {
	if r, ok := m.runs[runID]; ok {
		return r, nil
	}
	return nil, database.ErrNotFound
}

func (m *mockAdapter) CreateStrategyVersion(_ context.Context, sv database.StrategyVersion) error {
	if _, ok := m.versions[sv.StrategyVersionID]; !ok {
		cp := sv
		m.versions[sv.StrategyVersionID] = &cp
	}
	return nil
}
func (m *mockAdapter) ActivateStrategyVersion(_ context.Context, id string) error {
	m.active = &id
	return nil
}
func (m *mockAdapter) ReleaseEventClaim(_ context.Context, _ string) error { return nil }
func (m *mockAdapter) GetActiveStrategyVersion(_ context.Context) (*database.StrategyVersion, error) {
	if m.active == nil {
		return nil, database.ErrNotFound
	}
	if sv, ok := m.versions[*m.active]; ok {
		return sv, nil
	}
	return nil, database.ErrNotFound
}
func (m *mockAdapter) GetStrategyVersion(_ context.Context, id string) (*database.StrategyVersion, error) {
	if sv, ok := m.versions[id]; ok {
		return sv, nil
	}
	return nil, database.ErrUnknownVersion
}

// Stub all Phase 1–6 methods.
func (m *mockAdapter) UpsertIngestionWatermark(_ context.Context, _ string, _ uint64) error {
	return database.ErrNotImplemented
}
func (m *mockAdapter) GetIngestionWatermark(_ context.Context, _ string) (uint64, error) {
	return 0, database.ErrNotImplemented
}
func (m *mockAdapter) InsertMarketData(_ context.Context, _ contracts.MarketDataDTO) error {
	return database.ErrNotImplemented
}
func (m *mockAdapter) GetMarketData(_ context.Context, _ string) (*contracts.MarketDataDTO, error) {
	return nil, database.ErrNotImplemented
}
func (m *mockAdapter) StartLifecycle(_ context.Context, _ contracts.MarketDataDTO) (string, error) {
	return "", database.ErrNotImplemented
}
func (m *mockAdapter) TransitionState(_ context.Context, _ database.TransitionRequest) error {
	return database.ErrNotImplemented
}
func (m *mockAdapter) GetLifecycle(_ context.Context, _ string) (*database.Lifecycle, error) {
	return nil, database.ErrNotImplemented
}
func (m *mockAdapter) GetLifecycleByToken(_ context.Context, _ string) (*database.Lifecycle, error) {
	return nil, database.ErrNotImplemented
}
func (m *mockAdapter) QuarantineToken(_ context.Context, _, _ string) error {
	return database.ErrNotImplemented
}
func (m *mockAdapter) InsertStateViolation(_ context.Context, _, _, _, _ string) error {
	return database.ErrNotImplemented
}
func (m *mockAdapter) InsertDataQuality(_ context.Context, _ contracts.DataQualityDTO) error {
	return database.ErrNotImplemented
}
func (m *mockAdapter) InsertFeature(_ context.Context, _ contracts.FeatureDTO) error {
	return database.ErrNotImplemented
}
func (m *mockAdapter) InsertEdge(_ context.Context, _ contracts.EdgeDTO) error {
	return database.ErrNotImplemented
}
func (m *mockAdapter) InsertValidatedEdge(_ context.Context, _ contracts.ValidatedEdgeDTO) error {
	return database.ErrNotImplemented
}
func (m *mockAdapter) InsertSelection(_ context.Context, _ contracts.SelectionOutputDTO) error {
	return database.ErrNotImplemented
}
func (m *mockAdapter) InsertAllocation(_ context.Context, _ contracts.AllocationDTO) error {
	return database.ErrNotImplemented
}
func (m *mockAdapter) InsertExecutionResult(_ context.Context, _ contracts.ExecutionResultDTO) error {
	return database.ErrNotImplemented
}
func (m *mockAdapter) InsertPositionState(_ context.Context, _ contracts.PositionStateDTO) error {
	return database.ErrNotImplemented
}
func (m *mockAdapter) InsertEvaluation(_ context.Context, _ contracts.EvaluationDTO) error {
	return database.ErrNotImplemented
}
func (m *mockAdapter) GetExecutionByLifecycle(_ context.Context, _ string) (*contracts.ExecutionResultDTO, error) {
	return nil, database.ErrNotFound
}
func (m *mockAdapter) GetProbabilityForLifecycle(_ context.Context, _ string) (float64, bool, error) {
	return 0, false, nil
}
func (m *mockAdapter) InsertLearningRecord(_ context.Context, _ contracts.LearningRecordDTO) error {
	return database.ErrNotImplemented
}
func (m *mockAdapter) InsertProbabilityEstimate(_ context.Context, _ contracts.ProbabilityEstimateDTO) error {
	return database.ErrNotImplemented
}
func (m *mockAdapter) InsertSlippageEstimate(_ context.Context, _ contracts.SlippageEstimateDTO) error {
	return database.ErrNotImplemented
}
func (m *mockAdapter) InsertLatencyProfile(_ context.Context, _ contracts.LatencyProfileDTO) error {
	return database.ErrNotImplemented
}
func (m *mockAdapter) GetProbabilityEstimateByTrace(_ context.Context, _ string) (*contracts.ProbabilityEstimateDTO, error) {
	return nil, nil
}
func (m *mockAdapter) GetRealizedFillSamples(_ context.Context, _ int) (map[string][]database.FillSample, error) {
	return nil, nil
}
func (m *mockAdapter) UpsertSlippageAlpha(_ context.Context, _ string, _, _, _ float64, _ int) error {
	return nil
}
func (m *mockAdapter) GetSlippageAlpha(_ context.Context, _ string) (float64, error) {
	return 1.0, nil
}
func (m *mockAdapter) GetSlippageEstimateByTrace(_ context.Context, _ string) (*contracts.SlippageEstimateDTO, error) {
	return nil, nil
}
func (m *mockAdapter) GetEstimatesByTrace(ctx context.Context, traceID string) (*contracts.ProbabilityEstimateDTO, *contracts.SlippageEstimateDTO, error) {
	p, _ := m.GetProbabilityEstimateByTrace(ctx, traceID)
	s, _ := m.GetSlippageEstimateByTrace(ctx, traceID)
	return p, s, nil
}
func (m *mockAdapter) GetLatestLatencyProfile(_ context.Context, _ string) (*contracts.LatencyProfileDTO, error) {
	return nil, nil
}
func (m *mockAdapter) AllocateNonce(_ context.Context, _, _ string) (uint64, error) {
	return 0, database.ErrNotImplemented
}
func (m *mockAdapter) ReconcileNonce(_ context.Context, _, _ string, _ uint64) error {
	return database.ErrNotImplemented
}
func (m *mockAdapter) GetOpenPositions(_ context.Context) ([]contracts.PositionStateDTO, error) {
	return nil, database.ErrNotImplemented
}
func (m *mockAdapter) GetPosition(_ context.Context, _ string) (*contracts.PositionStateDTO, error) {
	return nil, database.ErrNotImplemented
}
func (m *mockAdapter) GetShadowGateStats(_ context.Context, _ int) (*database.ShadowGateStats, error) {
	return &database.ShadowGateStats{}, nil
}
func (m *mockAdapter) GetClosedPositions(_ context.Context, _ int) ([]contracts.PositionStateDTO, error) {
	return nil, database.ErrNotImplemented
}
func (m *mockAdapter) FindPositionByPrefix(_ context.Context, _ string) (*contracts.PositionStateDTO, error) {
	return nil, database.ErrNotFound
}

func (m *mockAdapter) MarkEventExpired(_ context.Context, _ string, _ string) error {
	return database.ErrNotImplemented
}

func (m *mockAdapter) GetSystemState(_ context.Context) (*contracts.SystemStateDTO, error) {
	return nil, database.ErrNotImplemented
}

func (m *mockAdapter) UpsertSystemState(_ context.Context, _ contracts.SystemStateDTO, _ int64) (int64, error) {
	return 0, database.ErrNotImplemented
}

func (m *mockAdapter) GetExposureSummary(_ context.Context) (*database.ExposureSummary, error) {
	return nil, database.ErrNotImplemented
}

func (m *mockAdapter) SetStrategyVersionStatus(_ context.Context, _, _, _ string) error {
	return database.ErrNotImplemented
}

func (m *mockAdapter) GetActiveStrategy(_ context.Context) (*database.StrategyVersion, error) {
	return nil, database.ErrNotImplemented
}

func (m *mockAdapter) GetShadowStrategy(_ context.Context) (*database.StrategyVersion, error) {
	return nil, database.ErrNotImplemented
}

func (m *mockAdapter) ArchiveEvents(_ context.Context, _ time.Time, _ int) (int, error) {
	return 0, database.ErrNotImplemented
}

func (m *mockAdapter) GetEventsByTraceIncludeArchive(_ context.Context, _ string) ([]contracts.EventEnvelope, error) {
	return nil, database.ErrNotImplemented
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

type noopHandler struct{}

func (noopHandler) Process(_ context.Context, _ *database.Event) (*database.Event, error) {
	return nil, nil
}

func minimalConfig() *config.Config {
	return &config.Config{
		Logging: config.LoggingConfig{Level: "info", Format: "text"},
		Worker:  config.WorkerConfig{IdleBackoffMs: 100},
	}
}

// ─── Tests ────────────────────────────────────────────────────────────────────

func TestBoot_PinsStrategyVersion(t *testing.T) {
	ctx := context.Background()
	mock := newMock()
	cfg := minimalConfig()

	orch, err := orchestrator.Boot(ctx, mock, cfg, nil)
	if err != nil {
		t.Fatalf("Boot failed: %v", err)
	}
	if orch.VersionID() == "" {
		t.Error("expected non-empty VersionID after Boot")
	}
	if mock.active == nil || *mock.active == "" {
		t.Error("expected a version to be activated")
	}
}

func TestBoot_Idempotent(t *testing.T) {
	ctx := context.Background()
	mock := newMock()
	cfg := minimalConfig()

	orch1, err := orchestrator.Boot(ctx, mock, cfg, nil)
	if err != nil {
		t.Fatalf("first Boot failed: %v", err)
	}
	orch2, err := orchestrator.Boot(ctx, mock, cfg, nil)
	if err != nil {
		t.Fatalf("second Boot failed: %v", err)
	}
	if orch1.VersionID() != orch2.VersionID() {
		t.Errorf("versionIDs differ: %s vs %s", orch1.VersionID(), orch2.VersionID())
	}
}

func TestRun_NoStages_ExitsOnCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	mock := newMock()
	cfg := minimalConfig()

	orch, err := orchestrator.Boot(ctx, mock, cfg, nil)
	if err != nil {
		t.Fatalf("Boot failed: %v", err)
	}

	err = orch.Run(ctx)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("expected context error, got: %v", err)
	}
}

func TestRegistry_RegisterAndEntries(t *testing.T) {
	r := orchestrator.NewRegistry()

	if !r.Empty() {
		t.Error("new registry should be empty")
	}

	r.Register("stage-a", noopHandler{}, "event.a")
	r.Register("stage-b", noopHandler{}, "event.b1", "event.b2")

	if r.Len() != 2 {
		t.Errorf("expected 2 entries, got %d", r.Len())
	}

	entries := r.Entries()
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries from Entries(), got %d", len(entries))
	}

	// Entries must be in sorted (deterministic) order.
	if entries[0].Group != "stage-a" {
		t.Errorf("expected stage-a first, got %s", entries[0].Group)
	}
	if entries[1].Group != "stage-b" {
		t.Errorf("expected stage-b second, got %s", entries[1].Group)
	}
}

func TestRegistry_DuplicatePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on duplicate registration")
		}
	}()

	r := orchestrator.NewRegistry()
	r.Register("stage-a", noopHandler{}, "event.a")
	r.Register("stage-a", noopHandler{}, "event.a") // must panic
}

func (m *mockAdapter) InsertShadowTrade(_ context.Context, _ database.ShadowTrade) error {
	return nil
}
func (m *mockAdapter) UpdateShadowTradeObservation(_ context.Context, _ string, _ float64, _ string) error {
	return nil
}
func (m *mockAdapter) GetShadowTradesByWindow(_ context.Context, _ int) ([]database.ShadowTrade, error) {
	return nil, nil
}
func (m *mockAdapter) GetLearningRecordsByWindow(_ context.Context, _ string, _, _ time.Time) ([]contracts.LearningRecordDTO, error) {
	return nil, nil
}
func (m *mockAdapter) GetEvaluationsByVersion(_ context.Context, _ string) ([]contracts.EvaluationDTO, error) {
	return nil, nil
}

func (m *mockAdapter) ComputeDrawdown(_ context.Context, _ int) (float64, error) {
	return 0.0, nil
}

// ── Phase 7: Solana stubs ─────────────────────────────────────────────────────

func (m *mockAdapter) GetSolanaEndpointState(_ context.Context, _ string) (*database.SolanaEndpointState, error) {
	return nil, nil
}
func (m *mockAdapter) UpsertSolanaEndpointState(_ context.Context, _ database.SolanaEndpointState) error {
	return nil
}
func (m *mockAdapter) InsertSolanaSignature(_ context.Context, _ database.SolanaSignature) error {
	return nil
}
func (m *mockAdapter) UpdateSolanaSignatureStatus(_ context.Context, _, _ string, _ int64, _ string) error {
	return nil
}
func (m *mockAdapter) UpsertSolanaEndpointHealth(_ context.Context, _ database.SolanaEndpointHealth) error {
	return nil
}
func (m *mockAdapter) ListSolanaEndpointsRanked(_ context.Context) ([]database.SolanaEndpointHealth, error) {
	return nil, nil
}
func (m *mockAdapter) GetSolanaIngestionWatermark(_ context.Context, _ string) (uint64, error) {
	return 0, nil
}
func (m *mockAdapter) UpsertSolanaIngestionWatermark(_ context.Context, _ string, _ uint64) error {
	return nil
}

// ── Phase 8: Production Hardening stubs ──────────────────────────────────────

func (m *mockAdapter) ClaimNextEvents(_ context.Context, _ database.EventClaimQuery) ([]contracts.EventEnvelope, error) {
	return nil, nil
}
func (m *mockAdapter) IncrementEventRetry(_ context.Context, _, _ string) (int, error) {
	return 0, nil
}
func (m *mockAdapter) MoveToDLQ(_ context.Context, _ database.DLQEntry) error { return nil }
func (m *mockAdapter) RequeueFromDLQ(_ context.Context, _ string) error       { return nil }
func (m *mockAdapter) ListDLQ(_ context.Context, _ database.DLQFilter) ([]database.DLQEntry, error) {
	return nil, nil
}
func (m *mockAdapter) ClaimExecution(_ context.Context, _ contracts.AllocationDTO) (bool, error) {
	return true, nil
}
func (m *mockAdapter) UpsertPositionFromExecution(_ context.Context, _ contracts.PositionStateDTO) (bool, error) {
	return true, nil
}
func (m *mockAdapter) ListOpenPositionsForReconciliation(_ context.Context) ([]database.ReconciliationPosition, error) {
	return nil, nil
}
func (m *mockAdapter) AdjustPositionAmount(_ context.Context, _, _, _ string) error { return nil }
func (m *mockAdapter) ClosePositionForced(_ context.Context, _, _ string) error     { return nil }
func (m *mockAdapter) InsertLatencyEvent(_ context.Context, _ database.LatencyEvent) error {
	return nil
}
func (m *mockAdapter) GetLatencyProfile(_ context.Context, _, _, _ string, _ int) (contracts.LatencyProfileDTO, error) {
	return contracts.LatencyProfileDTO{}, nil
}
func (m *mockAdapter) PromoteStrategyVersion(_ context.Context, _ string, _ int) error { return nil }
func (m *mockAdapter) DrainAndCheckPipelineIdle(_ context.Context, _ int) (bool, error) {
	return true, nil
}
func (m *mockAdapter) SetSystemHalt(_ context.Context, _ bool, _, _ string) error { return nil }
func (m *mockAdapter) IsSystemHalted(_ context.Context) (bool, string, error)     { return false, "", nil }
func (m *mockAdapter) ComputeStateHash(_ context.Context) (string, error)         { return "", nil }
func (m *mockAdapter) ClaimPartitions(_ context.Context, _, _, _ string, _, _ int) ([]int, error) {
	return nil, nil
}
func (m *mockAdapter) RenewPartitions(_ context.Context, _, _, _ string) error   { return nil }
func (m *mockAdapter) ReleasePartitions(_ context.Context, _, _, _ string) error { return nil }
func (m *mockAdapter) ListInFlightExecutions(_ context.Context) ([]database.InFlightExecution, error) {
	return nil, nil
}
func (m *mockAdapter) FinalizeExecution(_ context.Context, _ string, _ database.ExecutionReceipt) error {
	return nil
}
func (m *mockAdapter) AbortReservedExecution(_ context.Context, _, _ string) error { return nil }
func (m *mockAdapter) MarkExecutionLost(_ context.Context, _, _ string) error      { return nil }
func (m *mockAdapter) RecordReorg(_ context.Context, _ string, _, _ int64, _ int) error {
	return nil
}
func (m *mockAdapter) InvalidateBlockRange(_ context.Context, _ string, _, _ int64) (int, error) {
	return 0, nil
}
func (m *mockAdapter) MarkPositionsUncertain(_ context.Context, _ string, _ int64, _ string) error {
	return nil
}
func (m *mockAdapter) ReResolveExecutionAfterReorg(_ context.Context, _, _ string, _ database.ReorgOutcome) error {
	return nil
}
func (m *mockAdapter) RecordExecutionForEvaluation(_ context.Context, _ string, _ int) error {
	return nil
}
func (m *mockAdapter) MarkEvaluationDone(_ context.Context, _ string) error { return nil }
func (m *mockAdapter) ListMissingEvaluations(_ context.Context) ([]database.MissingEvaluation, error) {
	return nil, nil
}
func (m *mockAdapter) GetUnprocessedCount(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}
func (m *mockAdapter) RecordDrop(_ context.Context, _, _, _, _ string) error { return nil }
func (m *mockAdapter) GetPipelineStats(_ context.Context, _ int) (*database.PipelineStats, error) {
	return &database.PipelineStats{}, nil
}
func (m *mockAdapter) ListRecentEvents(_ context.Context, _ string, _ int) ([]database.RecentEventRow, error) {
	return nil, nil
}
func (m *mockAdapter) GetDQBreakdown(_ context.Context, _ int, _ string) (*database.DQBreakdown, error) {
	return nil, nil
}

// Phase 11 (Reference-Repo R2 — LEARN) creator-blacklist stubs.
func (m *mockAdapter) UpsertCreatorRugObservation(_ context.Context, _ database.CreatorRugObservation) error {
	return nil
}
func (m *mockAdapter) GetCreatorBlacklistEntry(_ context.Context, _ string, _ string) (*database.CreatorBlacklistEntry, error) {
	return nil, nil
}

// Task 8: Creator Profiles stubs.
func (m *mockAdapter) UpsertCreatorProfileOnLaunch(_ context.Context, _, _ string) error { return nil }
func (m *mockAdapter) IncrementCreatorOutcome(_ context.Context, _, _, _ string) error   { return nil }
func (m *mockAdapter) GetCreatorProfile(_ context.Context, _, _ string) (contracts.CreatorProfile, bool, error) {
	return contracts.CreatorProfile{}, false, nil
}
func (m *mockAdapter) GetAdaptiveDQStats(_ context.Context, _ int) (int, int, error) {
	return 0, 0, nil
}
func (m *mockAdapter) SaveBaseline(_ context.Context, _, _, _ string, _ []float64) error {
	return nil
}
func (m *mockAdapter) LoadBaselines(_ context.Context, _ string) (map[string]map[string][]float64, error) {
	return map[string]map[string][]float64{}, nil
}

// Phase 10 rescan layer stub.
func (m *mockAdapter) GetTokensForRescan(_ context.Context, _ database.RescanQuery) ([]contracts.MarketDataDTO, error) {
	return []contracts.MarketDataDTO{}, nil
}

// CheckTokenNameSeen stub — always returns (false, nil) so tests proceed through probes.
func (m *mockAdapter) CheckTokenNameSeen(_ context.Context, _, _, _ string) (bool, error) {
	return false, nil
}
func (m *mockAdapter) GetLatestPoolAddressForToken(_ context.Context, _, _ string) (string, bool, error) {
	return "", false, nil
}
func (m *mockAdapter) CountTokensByCreator(_ context.Context, _, _ string) (int32, error) {
	return 0, nil
}

// ── Historical Market Profiles stubs (Approach A) ─────────────────────────────

func (m *mockAdapter) UpsertHistoricalProfile(_ context.Context, _ contracts.HistoricalMarketProfileDTO) error {
	return nil
}
func (m *mockAdapter) GetHistoricalProfile(_ context.Context, _ string) (*contracts.HistoricalMarketProfileDTO, error) {
	return nil, nil
}
func (m *mockAdapter) ListHistoricalProfiles(_ context.Context) ([]contracts.HistoricalMarketProfileDTO, error) {
	return nil, nil
}

func (m *mockAdapter) GetExecutionLog(_ context.Context, _ int) ([]database.ExecutionLogRow, error) {
	return nil, nil
}
