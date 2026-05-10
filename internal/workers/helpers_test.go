package workers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
)

// ── minimal mock adapter ──────────────────────────────────────────────────────

// stubAdapter is a zero-value-safe Adapter stub for worker helper tests.
// Methods relevant to helpers are overridable via fields.
type stubAdapter struct {
	transitionErr   error
	lifecycleResult *database.Lifecycle
	lifecycleErr    error
	correlationEvts []database.Event
	correlationErr  error
}

func (s *stubAdapter) Initialize(_ context.Context, _ database.Config) error { return nil }
func (s *stubAdapter) RunMigrations(_ context.Context) error                 { return nil }
func (s *stubAdapter) Close(_ context.Context) error                         { return nil }

func (s *stubAdapter) InsertEvent(_ context.Context, _ database.Event) error { return nil }
func (s *stubAdapter) ClaimNextEvent(_ context.Context, _ string, _ []string) (*database.Event, error) {
	return nil, nil
}
func (s *stubAdapter) MarkEventProcessed(_ context.Context, _ string) error { return nil }
func (s *stubAdapter) ReleaseEventClaim(_ context.Context, _ string) error  { return nil }
func (s *stubAdapter) GetEventByID(_ context.Context, _ string) (*database.Event, error) {
	return nil, database.ErrNotFound
}
func (s *stubAdapter) MarkEventExpired(_ context.Context, _ string, _ string) error {
	return nil
}

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
	return nil, database.ErrNotFound
}

func (s *stubAdapter) StartLifecycle(_ context.Context, _ contracts.MarketDataDTO) (string, error) {
	return "lc-1", nil
}
func (s *stubAdapter) TransitionState(_ context.Context, _ database.TransitionRequest) error {
	return s.transitionErr
}
func (s *stubAdapter) GetLifecycle(_ context.Context, _ string) (*database.Lifecycle, error) {
	return s.lifecycleResult, s.lifecycleErr
}
func (s *stubAdapter) GetLifecycleByToken(_ context.Context, _ string) (*database.Lifecycle, error) {
	return nil, database.ErrNotFound
}
func (s *stubAdapter) QuarantineToken(_ context.Context, _ string, _ string) error { return nil }
func (s *stubAdapter) InsertStateViolation(_ context.Context, _, _, _, _ string) error {
	return nil
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
func (s *stubAdapter) GetExecutionByLifecycle(_ context.Context, _ string) (*contracts.ExecutionResultDTO, error) {
	return nil, database.ErrNotFound
}
func (s *stubAdapter) InsertShadowTrade(_ context.Context, _ database.ShadowTrade) error { return nil }
func (s *stubAdapter) UpdateShadowTradeObservation(_ context.Context, _ string, _ float64, _ string) error {
	return nil
}
func (s *stubAdapter) GetShadowTradesByWindow(_ context.Context, _ int) ([]database.ShadowTrade, error) {
	return nil, nil
}
func (s *stubAdapter) InsertLearningRecord(_ context.Context, _ contracts.LearningRecordDTO) error {
	return nil
}
func (s *stubAdapter) InsertProbabilityEstimate(_ context.Context, _ contracts.ProbabilityEstimateDTO) error {
	return nil
}
func (s *stubAdapter) InsertSlippageEstimate(_ context.Context, _ contracts.SlippageEstimateDTO) error {
	return nil
}
func (s *stubAdapter) InsertLatencyProfile(_ context.Context, _ contracts.LatencyProfileDTO) error {
	return nil
}
func (s *stubAdapter) GetProbabilityEstimateByTrace(_ context.Context, _ string) (*contracts.ProbabilityEstimateDTO, error) {
	return nil, database.ErrNotFound
}
func (s *stubAdapter) GetSlippageAlpha(_ context.Context, _ string) (float64, error) {
	return 1.0, nil
}
func (s *stubAdapter) GetRealizedFillSamples(_ context.Context, _ int) (map[string][]database.FillSample, error) {
	return nil, nil
}
func (s *stubAdapter) UpsertSlippageAlpha(_ context.Context, _ string, _, _, _ float64, _ int) error {
	return nil
}
func (s *stubAdapter) GetSlippageEstimateByTrace(_ context.Context, _ string) (*contracts.SlippageEstimateDTO, error) {
	return nil, database.ErrNotFound
}
func (s *stubAdapter) GetEstimatesByTrace(ctx context.Context, traceID string) (*contracts.ProbabilityEstimateDTO, *contracts.SlippageEstimateDTO, error) {
	p, _ := s.GetProbabilityEstimateByTrace(ctx, traceID)
	sl, _ := s.GetSlippageEstimateByTrace(ctx, traceID)
	return p, sl, nil
}
func (s *stubAdapter) GetLatestLatencyProfile(_ context.Context, _ string) (*contracts.LatencyProfileDTO, error) {
	return nil, database.ErrNotFound
}
func (s *stubAdapter) GetLearningRecordsByWindow(_ context.Context, _ string, _, _ time.Time) ([]contracts.LearningRecordDTO, error) {
	return nil, nil
}
func (s *stubAdapter) GetEvaluationsByVersion(_ context.Context, _ string) ([]contracts.EvaluationDTO, error) {
	return nil, nil
}
func (s *stubAdapter) AllocateNonce(_ context.Context, _, _ string) (uint64, error) {
	return 0, database.ErrNotImplemented
}
func (s *stubAdapter) ReconcileNonce(_ context.Context, _, _ string, _ uint64) error {
	return database.ErrNotImplemented
}
func (s *stubAdapter) GetOpenPositions(_ context.Context) ([]contracts.PositionStateDTO, error) {
	return nil, nil
}
func (s *stubAdapter) GetPosition(_ context.Context, _ string) (*contracts.PositionStateDTO, error) {
	return nil, database.ErrNotFound
}
func (s *stubAdapter) GetClosedPositions(_ context.Context, _ int) ([]contracts.PositionStateDTO, error) {
	return nil, nil
}
func (s *stubAdapter) FindPositionByPrefix(_ context.Context, _ string) (*contracts.PositionStateDTO, error) {
	return nil, database.ErrNotFound
}
func (s *stubAdapter) CreateStrategyVersion(_ context.Context, _ database.StrategyVersion) error {
	return nil
}
func (s *stubAdapter) GetActiveStrategyVersion(_ context.Context) (*database.StrategyVersion, error) {
	return nil, database.ErrNotFound
}
func (s *stubAdapter) GetStrategyVersion(_ context.Context, _ string) (*database.StrategyVersion, error) {
	return nil, database.ErrNotFound
}
func (s *stubAdapter) SetStrategyVersionStatus(_ context.Context, _, _, _ string) error { return nil }
func (s *stubAdapter) GetActiveStrategy(_ context.Context) (*database.StrategyVersion, error) {
	return nil, database.ErrNotFound
}
func (s *stubAdapter) GetShadowStrategy(_ context.Context) (*database.StrategyVersion, error) {
	return nil, database.ErrNotFound
}
func (s *stubAdapter) ActivateStrategyVersion(_ context.Context, _ string) error { return nil }
func (s *stubAdapter) GetSystemState(_ context.Context) (*contracts.SystemStateDTO, error) {
	return nil, database.ErrNotFound
}
func (s *stubAdapter) UpsertSystemState(_ context.Context, _ contracts.SystemStateDTO, _ int64) (int64, error) {
	return 0, database.ErrNotImplemented
}
func (s *stubAdapter) GetExposureSummary(_ context.Context) (*database.ExposureSummary, error) {
	return nil, database.ErrNotImplemented
}
func (s *stubAdapter) GetEventsByTrace(_ context.Context, _ string) ([]database.Event, error) {
	return nil, nil
}
func (s *stubAdapter) GetEventsByCorrelation(_ context.Context, _ string) ([]database.Event, error) {
	return s.correlationEvts, s.correlationErr
}
func (s *stubAdapter) GetLastEventTimestamp(_ context.Context, _ string) (time.Time, error) {
	return time.Time{}, database.ErrNotFound
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
func (s *stubAdapter) ComputeDrawdown(_ context.Context, _ int) (float64, error) { return 0, nil }
func (s *stubAdapter) CreateRun(_ context.Context, _ database.PipelineRun) error { return nil }
func (s *stubAdapter) UpdateRunStage(_ context.Context, _, _ string) error       { return nil }
func (s *stubAdapter) UpdateRunStatus(_ context.Context, _, _ string) error      { return nil }
func (s *stubAdapter) GetRun(_ context.Context, _ string) (*database.PipelineRun, error) {
	return nil, database.ErrNotFound
}

// ── Solana stubs (Phase 7) ────────────────────────────────────────────────────

func (s *stubAdapter) GetSolanaEndpointState(_ context.Context, _ string) (*database.SolanaEndpointState, error) {
	return nil, nil
}
func (s *stubAdapter) UpsertSolanaEndpointState(_ context.Context, _ database.SolanaEndpointState) error {
	return nil
}
func (s *stubAdapter) InsertSolanaSignature(_ context.Context, _ database.SolanaSignature) error {
	return nil
}
func (s *stubAdapter) UpdateSolanaSignatureStatus(_ context.Context, _, _ string, _ int64, _ string) error {
	return nil
}
func (s *stubAdapter) UpsertSolanaEndpointHealth(_ context.Context, _ database.SolanaEndpointHealth) error {
	return nil
}
func (s *stubAdapter) ListSolanaEndpointsRanked(_ context.Context) ([]database.SolanaEndpointHealth, error) {
	return nil, nil
}
func (s *stubAdapter) GetSolanaIngestionWatermark(_ context.Context, _ string) (uint64, error) {
	return 0, nil
}
func (s *stubAdapter) UpsertSolanaIngestionWatermark(_ context.Context, _ string, _ uint64) error {
	return nil
}

// ── Phase 8: Production Hardening stubs ──────────────────────────────────────

func (s *stubAdapter) ClaimNextEvents(_ context.Context, _ database.EventClaimQuery) ([]contracts.EventEnvelope, error) {
	return nil, nil
}
func (s *stubAdapter) IncrementEventRetry(_ context.Context, _, _ string) (int, error) {
	return 0, nil
}
func (s *stubAdapter) MoveToDLQ(_ context.Context, _ database.DLQEntry) error { return nil }
func (s *stubAdapter) RequeueFromDLQ(_ context.Context, _ string) error       { return nil }
func (s *stubAdapter) ListDLQ(_ context.Context, _ database.DLQFilter) ([]database.DLQEntry, error) {
	return nil, nil
}
func (s *stubAdapter) ClaimExecution(_ context.Context, _ contracts.AllocationDTO) (bool, error) {
	return true, nil
}
func (s *stubAdapter) UpsertPositionFromExecution(_ context.Context, _ contracts.PositionStateDTO) (bool, error) {
	return true, nil
}
func (s *stubAdapter) ListOpenPositionsForReconciliation(_ context.Context) ([]database.ReconciliationPosition, error) {
	return nil, nil
}
func (s *stubAdapter) AdjustPositionAmount(_ context.Context, _, _, _ string) error { return nil }
func (s *stubAdapter) ClosePositionForced(_ context.Context, _, _ string) error     { return nil }
func (s *stubAdapter) InsertLatencyEvent(_ context.Context, _ database.LatencyEvent) error {
	return nil
}
func (s *stubAdapter) GetLatencyProfile(_ context.Context, _, _, _ string, _ int) (contracts.LatencyProfileDTO, error) {
	return contracts.LatencyProfileDTO{}, nil
}
func (s *stubAdapter) PromoteStrategyVersion(_ context.Context, _ string, _ int) error { return nil }
func (s *stubAdapter) DrainAndCheckPipelineIdle(_ context.Context, _ int) (bool, error) {
	return true, nil
}
func (s *stubAdapter) SetSystemHalt(_ context.Context, _ bool, _, _ string) error { return nil }
func (s *stubAdapter) IsSystemHalted(_ context.Context) (bool, string, error)     { return false, "", nil }
func (s *stubAdapter) ComputeStateHash(_ context.Context) (string, error)         { return "", nil }
func (s *stubAdapter) ClaimPartitions(_ context.Context, _, _, _ string, _, _ int) ([]int, error) {
	return nil, nil
}
func (s *stubAdapter) RenewPartitions(_ context.Context, _, _, _ string) error   { return nil }
func (s *stubAdapter) ReleasePartitions(_ context.Context, _, _, _ string) error { return nil }
func (s *stubAdapter) ListInFlightExecutions(_ context.Context) ([]database.InFlightExecution, error) {
	return nil, nil
}
func (s *stubAdapter) FinalizeExecution(_ context.Context, _ string, _ database.ExecutionReceipt) error {
	return nil
}
func (s *stubAdapter) AbortReservedExecution(_ context.Context, _, _ string) error { return nil }
func (s *stubAdapter) MarkExecutionLost(_ context.Context, _, _ string) error      { return nil }
func (s *stubAdapter) RecordReorg(_ context.Context, _ string, _, _ int64, _ int) error {
	return nil
}
func (s *stubAdapter) InvalidateBlockRange(_ context.Context, _ string, _, _ int64) (int, error) {
	return 0, nil
}
func (s *stubAdapter) MarkPositionsUncertain(_ context.Context, _ string, _ int64, _ string) error {
	return nil
}
func (s *stubAdapter) ReResolveExecutionAfterReorg(_ context.Context, _, _ string, _ database.ReorgOutcome) error {
	return nil
}
func (s *stubAdapter) RecordExecutionForEvaluation(_ context.Context, _ string, _ int) error {
	return nil
}
func (s *stubAdapter) MarkEvaluationDone(_ context.Context, _ string) error { return nil }
func (s *stubAdapter) ListMissingEvaluations(_ context.Context) ([]database.MissingEvaluation, error) {
	return nil, nil
}
func (s *stubAdapter) GetUnprocessedCount(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}
func (s *stubAdapter) RecordDrop(_ context.Context, _, _, _, _ string) error { return nil }
func (s *stubAdapter) GetPipelineStats(_ context.Context, _ int) (*database.PipelineStats, error) {
	return &database.PipelineStats{}, nil
}

func TestMakeOutputEvent_HappyPath(t *testing.T) {
	// Arrange
	type simple struct{ X int }
	dto := simple{X: 42}

	// Act
	evt, err := makeOutputEvent("evt-id", dto, "my_event", "trace-1", "corr-1", "cause-1", "v1")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt == nil {
		t.Fatal("expected non-nil event")
	}
	if evt.EventID != "evt-id" {
		t.Errorf("EventID: got %q want %q", evt.EventID, "evt-id")
	}
	if evt.EventType != "my_event" {
		t.Errorf("EventType: got %q want %q", evt.EventType, "my_event")
	}
	if evt.TraceID != "trace-1" {
		t.Errorf("TraceID: got %q want %q", evt.TraceID, "trace-1")
	}
	if evt.CorrelationID != "corr-1" {
		t.Errorf("CorrelationID: got %q want %q", evt.CorrelationID, "corr-1")
	}
	if evt.CausationID == nil || *evt.CausationID != "cause-1" {
		t.Errorf("CausationID: got %v want %q", evt.CausationID, "cause-1")
	}
	if evt.VersionID != "v1" {
		t.Errorf("VersionID: got %q want %q", evt.VersionID, "v1")
	}
	var decoded simple
	if err := json.Unmarshal(evt.Payload, &decoded); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if decoded.X != 42 {
		t.Errorf("payload.X: got %d want 42", decoded.X)
	}
}

func TestMakeOutputEvent_EmptyCausationID_SetsNilPointer(t *testing.T) {
	// Arrange / Act
	evt, err := makeOutputEvent("id", struct{}{}, "t", "tr", "co", "", "v1")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if evt.CausationID != nil {
		t.Errorf("CausationID should be nil for empty string, got %v", evt.CausationID)
	}
}

func TestMakeOutputEvent_UnmarshalableDTO_ReturnsError(t *testing.T) {
	// Arrange — channels cannot be marshalled to JSON
	dto := make(chan int)

	// Act
	_, err := makeOutputEvent("id", dto, "t", "tr", "co", "ca", "v1")

	// Assert
	if err == nil {
		t.Fatal("expected marshal error, got nil")
	}
}

// ── transitionBestEffort ──────────────────────────────────────────────────────

func TestTransitionBestEffort_Success_NoError(t *testing.T) {
	// Arrange
	adapter := &stubAdapter{transitionErr: nil}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	req := database.TransitionRequest{
		LifecycleID:       "lc-1",
		ExpectedFromState: "DETECTED",
		NewState:          "DQ_PASSED",
	}

	// Act — must not panic or return error (function returns void)
	transitionBestEffort(context.Background(), adapter, req, logger)
}

func TestTransitionBestEffort_AdapterError_IsAbsorbed(t *testing.T) {
	// Arrange — adapter returns error; function must not propagate it
	adapter := &stubAdapter{transitionErr: errors.New("db down")}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	req := database.TransitionRequest{
		LifecycleID:       "lc-1",
		ExpectedFromState: "DETECTED",
		NewState:          "DQ_PASSED",
	}

	// Act / Assert — no panic, no return value to check
	transitionBestEffort(context.Background(), adapter, req, logger)
}

// ── transitionMandatory ───────────────────────────────────────────────────────

func TestTransitionMandatory_Success(t *testing.T) {
	// Arrange
	adapter := &stubAdapter{transitionErr: nil}
	req := database.TransitionRequest{
		LifecycleID:       "lc-2",
		ExpectedFromState: "DQ_PASSED",
		NewState:          "FEATURES_EXTRACTED",
	}

	// Act
	err := transitionMandatory(context.Background(), adapter, req)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTransitionMandatory_AdapterError_WrappedAndReturned(t *testing.T) {
	// Arrange
	inner := errors.New("invalid transition")
	adapter := &stubAdapter{transitionErr: inner}
	req := database.TransitionRequest{
		LifecycleID:       "lc-2",
		ExpectedFromState: "DQ_PASSED",
		NewState:          "FEATURES_EXTRACTED",
	}

	// Act
	err := transitionMandatory(context.Background(), adapter, req)

	// Assert
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, inner) {
		t.Errorf("expected wrapped inner error, got: %v", err)
	}
}

// ── doMandatoryTransition ─────────────────────────────────────────────────────

func TestDoMandatoryTransition_Success(t *testing.T) {
	// Arrange
	lc := &database.Lifecycle{
		TokenLifecycleID: "lc-3",
		CurrentState:     "DQ_PASSED",
		StateVersion:     1,
	}
	adapter := &stubAdapter{lifecycleResult: lc, lifecycleErr: nil, transitionErr: nil}

	// Act
	err := doMandatoryTransition(context.Background(), adapter, "lc-3", "DQ_PASSED", "FEATURES_EXTRACTED", "test", "worker")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDoMandatoryTransition_GetLifecycleError_Propagates(t *testing.T) {
	// Arrange
	fetchErr := errors.New("not found")
	adapter := &stubAdapter{lifecycleErr: fetchErr}

	// Act
	err := doMandatoryTransition(context.Background(), adapter, "lc-x", "from", "to", "r", "a")

	// Assert
	if err == nil {
		t.Fatal("expected error from GetLifecycle, got nil")
	}
	if !errors.Is(err, fetchErr) {
		t.Errorf("expected wrapped fetchErr, got: %v", err)
	}
}

func TestDoMandatoryTransition_TransitionError_Propagates(t *testing.T) {
	// Arrange: lifecycle is at the expected from-state; CAS itself fails.
	lc := &database.Lifecycle{TokenLifecycleID: "lc-4", CurrentState: "from", StateVersion: 2}
	transErr := database.ErrInvalidTransition
	adapter := &stubAdapter{lifecycleResult: lc, transitionErr: transErr}

	// Act
	err := doMandatoryTransition(context.Background(), adapter, "lc-4", "from", "to", "r", "a")

	// Assert
	if !errors.Is(err, transErr) {
		t.Errorf("expected ErrInvalidTransition, got: %v", err)
	}
}

func TestDoMandatoryTransition_LifecycleAlreadyAdvanced_ReturnsSentinel(t *testing.T) {
	// Arrange: lifecycle is at FEATURE_READY — already past DQ_PASSED.
	// This simulates a stale market_data_enriched event from a prior session
	// being re-consumed by the DQ worker.
	lc := &database.Lifecycle{
		TokenLifecycleID: "lc-stale",
		CurrentState:     "FEATURE_READY",
		StateVersion:     3,
	}
	adapter := &stubAdapter{lifecycleResult: lc}

	// Act: DQ worker tries DETECTED → DQ_PASSED but lifecycle is already further.
	err := doMandatoryTransition(context.Background(), adapter, "lc-stale", "DETECTED", "DQ_PASSED", "PASS", "dq_worker")

	// Assert: must return ErrLifecycleAlreadyAdvanced (not ErrInvalidTransition).
	if err == nil {
		t.Fatal("expected error for already-advanced lifecycle, got nil")
	}
	if !errors.Is(err, database.ErrLifecycleAlreadyAdvanced) {
		t.Errorf("expected ErrLifecycleAlreadyAdvanced, got: %v", err)
	}
	// TransitionState must NOT be called when the state check short-circuits.
	if adapter.transitionErr != nil {
		t.Error("expected TransitionState not called for already-advanced lifecycle")
	}
}

// ── fetchLifecycle ────────────────────────────────────────────────────────────

func TestFetchLifecycle_Success(t *testing.T) {
	// Arrange
	lc := &database.Lifecycle{TokenLifecycleID: "lc-5", CurrentState: "DETECTED"}
	adapter := &stubAdapter{lifecycleResult: lc}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Act
	result, ok := fetchLifecycle(context.Background(), adapter, "lc-5", logger)

	// Assert
	if !ok {
		t.Fatal("expected ok=true")
	}
	if result == nil || result.TokenLifecycleID != "lc-5" {
		t.Errorf("unexpected lifecycle: %+v", result)
	}
}

func TestFetchLifecycle_Error_ReturnsFalse(t *testing.T) {
	// Arrange
	adapter := &stubAdapter{lifecycleErr: database.ErrNotFound}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Act
	result, ok := fetchLifecycle(context.Background(), adapter, "lc-missing", logger)

	// Assert
	if ok {
		t.Fatal("expected ok=false on error")
	}
	if result != nil {
		t.Errorf("expected nil lifecycle, got %+v", result)
	}
}

// ── firstChain ────────────────────────────────────────────────────────────────

func TestFirstChain_NilConfig_ReturnsEmpty(t *testing.T) {
	got := firstChain(nil)
	if got != "" {
		t.Errorf("expected empty string for nil config, got %q", got)
	}
}

func TestFirstChain_EmptyChains_ReturnsEmpty(t *testing.T) {
	cfg := &config.Config{Chains: map[string]config.ChainConfig{}}
	got := firstChain(cfg)
	if got != "" {
		t.Errorf("expected empty string for empty chains, got %q", got)
	}
}

func TestFirstChain_SingleChain_ReturnsKey(t *testing.T) {
	cfg := &config.Config{
		Chains: map[string]config.ChainConfig{
			"eth-mainnet": {},
		},
	}
	got := firstChain(cfg)
	if got != "eth-mainnet" {
		t.Errorf("expected %q, got %q", "eth-mainnet", got)
	}
}

func TestFirstChain_MultipleChains_ReturnsEmpty(t *testing.T) {
	cfg := &config.Config{
		Chains: map[string]config.ChainConfig{
			"eth-mainnet": {},
			"bsc-mainnet": {},
		},
	}
	got := firstChain(cfg)
	if got != "" {
		t.Errorf("expected empty string for multiple chains, got %q", got)
	}
}

// ── chainBaseToken ────────────────────────────────────────────────────────────

func TestChainBaseToken_NilConfig_ReturnsEmpty(t *testing.T) {
	got := chainBaseToken(nil, "eth-mainnet")
	if got != "" {
		t.Errorf("expected empty string for nil config, got %q", got)
	}
}

func TestChainBaseToken_ChainNotFound_ReturnsEmpty(t *testing.T) {
	cfg := &config.Config{Chains: map[string]config.ChainConfig{}}
	got := chainBaseToken(cfg, "eth-mainnet")
	if got != "" {
		t.Errorf("expected empty string for missing chain, got %q", got)
	}
}

func TestChainBaseToken_NoBaseTokens_ReturnsEmpty(t *testing.T) {
	cfg := &config.Config{
		Chains: map[string]config.ChainConfig{
			"eth-mainnet": {BaseTokens: nil},
		},
	}
	got := chainBaseToken(cfg, "eth-mainnet")
	if got != "" {
		t.Errorf("expected empty string for no base tokens, got %q", got)
	}
}

func TestChainBaseToken_Found_ReturnsFirstAddress(t *testing.T) {
	const addr = "0xC02aaA39b223FE8D0A0e5C4F27eAD9083C756Cc2"
	cfg := &config.Config{
		Chains: map[string]config.ChainConfig{
			"eth-mainnet": {
				BaseTokens: []config.BaseToken{
					{Address: addr, Symbol: "WETH"},
					{Address: "0xother", Symbol: "USDC"},
				},
			},
		},
	}
	got := chainBaseToken(cfg, "eth-mainnet")
	if got != addr {
		t.Errorf("expected %q, got %q", addr, got)
	}
}

// ── allocationSizeFromCorrelation ─────────────────────────────────────────────

func TestAllocationSizeFromCorrelation_Found(t *testing.T) {
	// Arrange
	alloc := contracts.AllocationDTO{SizeUsd: 150.0}
	payload, _ := json.Marshal(alloc)
	adapter := &stubAdapter{
		correlationEvts: []database.Event{
			{EventType: "other_event", Payload: []byte(`{}`)},
			{EventType: "allocation_event", Payload: payload},
		},
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Act
	got := allocationSizeFromCorrelation(context.Background(), adapter, "corr-1", logger)

	// Assert
	if got != 150.0 {
		t.Errorf("expected 150.0, got %f", got)
	}
}

func TestAllocationSizeFromCorrelation_AdapterError_ReturnsZero(t *testing.T) {
	// Arrange
	adapter := &stubAdapter{correlationErr: errors.New("db failure")}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Act
	got := allocationSizeFromCorrelation(context.Background(), adapter, "corr-x", logger)

	// Assert
	if got != 0 {
		t.Errorf("expected 0 on error, got %f", got)
	}
}

func TestAllocationSizeFromCorrelation_NoAllocationEvent_ReturnsZero(t *testing.T) {
	// Arrange — no allocation_event in list
	adapter := &stubAdapter{
		correlationEvts: []database.Event{
			{EventType: "market_data_event", Payload: []byte(`{}`)},
		},
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Act
	got := allocationSizeFromCorrelation(context.Background(), adapter, "corr-2", logger)

	// Assert
	if got != 0 {
		t.Errorf("expected 0 when no allocation event, got %f", got)
	}
}

// ── chainFromCorrelation ──────────────────────────────────────────────────────

func TestChainFromCorrelation_Found(t *testing.T) {
	// Arrange
	md := contracts.MarketDataDTO{Chain: "bsc-mainnet"}
	payload, _ := json.Marshal(md)
	adapter := &stubAdapter{
		correlationEvts: []database.Event{
			{EventType: "market_data_event", Payload: payload},
		},
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Act
	got := chainFromCorrelation(context.Background(), adapter, "corr-1", logger)

	// Assert
	if got != "bsc-mainnet" {
		t.Errorf("expected %q, got %q", "bsc-mainnet", got)
	}
}

func TestChainFromCorrelation_AdapterError_ReturnsEmpty(t *testing.T) {
	// Arrange
	adapter := &stubAdapter{correlationErr: errors.New("db failure")}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Act
	got := chainFromCorrelation(context.Background(), adapter, "corr-x", logger)

	// Assert
	if got != "" {
		t.Errorf("expected empty on error, got %q", got)
	}
}

func TestChainFromCorrelation_NoMarketDataEvent_ReturnsEmpty(t *testing.T) {
	// Arrange — no market_data_event
	adapter := &stubAdapter{
		correlationEvts: []database.Event{
			{EventType: "data_quality_event", Payload: []byte(`{}`)},
		},
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	// Act
	got := chainFromCorrelation(context.Background(), adapter, "corr-3", logger)

	// Assert
	if got != "" {
		t.Errorf("expected empty when no market_data_event, got %q", got)
	}
}

// Phase 11 (Reference-Repo R2 — LEARN) creator-blacklist stubs.
func (s *stubAdapter) UpsertCreatorRugObservation(_ context.Context, _ database.CreatorRugObservation) error {
	return nil
}
func (s *stubAdapter) GetCreatorBlacklistEntry(_ context.Context, _ string, _ string) (*database.CreatorBlacklistEntry, error) {
	return nil, nil
}
func (s *stubAdapter) CountTokensByCreator(_ context.Context, _ string, _ string) (int32, error) {
	return 0, nil
}
func (s *stubAdapter) GetAdaptiveDQStats(_ context.Context, _ int) (int, int, error) {
	return 0, 0, nil
}
func (s *stubAdapter) SaveBaseline(_ context.Context, _, _, _ string, _ []float64) error {
	return nil
}
func (s *stubAdapter) LoadBaselines(_ context.Context, _ string) (map[string]map[string][]float64, error) {
	return map[string]map[string][]float64{}, nil
}

// Phase 10 rescan layer stub.
func (s *stubAdapter) GetTokensForRescan(_ context.Context, _ database.RescanQuery) ([]contracts.MarketDataDTO, error) {
	return []contracts.MarketDataDTO{}, nil
}

// ── Historical Market Profiles stubs (Approach A) ─────────────────────────────

func (s *stubAdapter) UpsertHistoricalProfile(_ context.Context, _ contracts.HistoricalMarketProfileDTO) error {
	return nil
}
func (s *stubAdapter) GetHistoricalProfile(_ context.Context, _ string) (*contracts.HistoricalMarketProfileDTO, error) {
	return nil, nil
}
func (s *stubAdapter) ListHistoricalProfiles(_ context.Context) ([]contracts.HistoricalMarketProfileDTO, error) {
	return nil, nil
}
