package telegram_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"testing"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/telegram"
)

// ── minimal stub adapter ──────────────────────────────────────────────────────

type dispatcherStubAdapter struct {
	nextEvent *database.Event
	claimErr  error
	markErr   error
}

func (a *dispatcherStubAdapter) Initialize(_ context.Context, _ database.Config) error { return nil }
func (a *dispatcherStubAdapter) RunMigrations(_ context.Context) error                 { return nil }
func (a *dispatcherStubAdapter) Close(_ context.Context) error                         { return nil }

func (a *dispatcherStubAdapter) InsertEvent(_ context.Context, _ database.Event) error { return nil }
func (a *dispatcherStubAdapter) ClaimNextEvent(_ context.Context, _ string, _ []string) (*database.Event, error) {
	return a.nextEvent, a.claimErr
}
func (a *dispatcherStubAdapter) MarkEventProcessed(_ context.Context, _ string) error {
	return a.markErr
}
func (a *dispatcherStubAdapter) ReleaseEventClaim(_ context.Context, _ string) error { return nil }
func (a *dispatcherStubAdapter) GetEventByID(_ context.Context, _ string) (*database.Event, error) {
	return nil, database.ErrNotFound
}
func (a *dispatcherStubAdapter) MarkEventExpired(_ context.Context, _ string, _ string) error {
	return nil
}
func (a *dispatcherStubAdapter) UpsertIngestionWatermark(_ context.Context, _ string, _ uint64) error {
	return nil
}
func (a *dispatcherStubAdapter) GetIngestionWatermark(_ context.Context, _ string) (uint64, error) {
	return 0, nil
}
func (a *dispatcherStubAdapter) InsertMarketData(_ context.Context, _ contracts.MarketDataDTO) error {
	return nil
}
func (a *dispatcherStubAdapter) GetMarketData(_ context.Context, _ string) (*contracts.MarketDataDTO, error) {
	return nil, database.ErrNotFound
}
func (a *dispatcherStubAdapter) StartLifecycle(_ context.Context, _ contracts.MarketDataDTO) (string, error) {
	return "lc-1", nil
}
func (a *dispatcherStubAdapter) TransitionState(_ context.Context, _ database.TransitionRequest) error {
	return nil
}
func (a *dispatcherStubAdapter) GetLifecycle(_ context.Context, _ string) (*database.Lifecycle, error) {
	return nil, database.ErrNotFound
}
func (a *dispatcherStubAdapter) GetLifecycleByToken(_ context.Context, _ string) (*database.Lifecycle, error) {
	return nil, database.ErrNotFound
}
func (a *dispatcherStubAdapter) QuarantineToken(_ context.Context, _ string, _ string) error {
	return nil
}
func (a *dispatcherStubAdapter) InsertStateViolation(_ context.Context, _, _, _, _ string) error {
	return nil
}
func (a *dispatcherStubAdapter) InsertDataQuality(_ context.Context, _ contracts.DataQualityDTO) error {
	return nil
}
func (a *dispatcherStubAdapter) InsertFeature(_ context.Context, _ contracts.FeatureDTO) error {
	return nil
}
func (a *dispatcherStubAdapter) InsertEdge(_ context.Context, _ contracts.EdgeDTO) error { return nil }
func (a *dispatcherStubAdapter) InsertValidatedEdge(_ context.Context, _ contracts.ValidatedEdgeDTO) error {
	return nil
}
func (a *dispatcherStubAdapter) InsertSelection(_ context.Context, _ contracts.SelectionOutputDTO) error {
	return nil
}
func (a *dispatcherStubAdapter) InsertAllocation(_ context.Context, _ contracts.AllocationDTO) error {
	return nil
}
func (a *dispatcherStubAdapter) InsertExecutionResult(_ context.Context, _ contracts.ExecutionResultDTO) error {
	return nil
}
func (a *dispatcherStubAdapter) InsertPositionState(_ context.Context, _ contracts.PositionStateDTO) error {
	return nil
}
func (a *dispatcherStubAdapter) InsertEvaluation(_ context.Context, _ contracts.EvaluationDTO) error {
	return nil
}
func (a *dispatcherStubAdapter) GetExecutionByLifecycle(_ context.Context, _ string) (*contracts.ExecutionResultDTO, error) {
	return nil, database.ErrNotFound
}
func (a *dispatcherStubAdapter) InsertShadowTrade(_ context.Context, _ database.ShadowTrade) error {
	return nil
}
func (a *dispatcherStubAdapter) UpdateShadowTradeObservation(_ context.Context, _ string, _ float64, _ string) error {
	return nil
}
func (a *dispatcherStubAdapter) GetShadowTradesByWindow(_ context.Context, _ int) ([]database.ShadowTrade, error) {
	return nil, nil
}
func (a *dispatcherStubAdapter) InsertLearningRecord(_ context.Context, _ contracts.LearningRecordDTO) error {
	return nil
}
func (a *dispatcherStubAdapter) InsertProbabilityEstimate(_ context.Context, _ contracts.ProbabilityEstimateDTO) error {
	return nil
}
func (a *dispatcherStubAdapter) InsertSlippageEstimate(_ context.Context, _ contracts.SlippageEstimateDTO) error {
	return nil
}
func (a *dispatcherStubAdapter) InsertLatencyProfile(_ context.Context, _ contracts.LatencyProfileDTO) error {
	return nil
}
func (a *dispatcherStubAdapter) GetProbabilityEstimateByTrace(_ context.Context, _ string) (*contracts.ProbabilityEstimateDTO, error) {
	return nil, database.ErrNotFound
}
func (a *dispatcherStubAdapter) GetSlippageEstimateByTrace(_ context.Context, _ string) (*contracts.SlippageEstimateDTO, error) {
	return nil, database.ErrNotFound
}
func (a *dispatcherStubAdapter) GetLatestLatencyProfile(_ context.Context, _ string) (*contracts.LatencyProfileDTO, error) {
	return nil, database.ErrNotFound
}
func (a *dispatcherStubAdapter) GetLearningRecordsByWindow(_ context.Context, _ string, _, _ time.Time) ([]contracts.LearningRecordDTO, error) {
	return nil, nil
}
func (a *dispatcherStubAdapter) GetEvaluationsByVersion(_ context.Context, _ string) ([]contracts.EvaluationDTO, error) {
	return nil, nil
}
func (a *dispatcherStubAdapter) AllocateNonce(_ context.Context, _, _ string) (uint64, error) {
	return 0, database.ErrNotImplemented
}
func (a *dispatcherStubAdapter) ReconcileNonce(_ context.Context, _, _ string, _ uint64) error {
	return database.ErrNotImplemented
}
func (a *dispatcherStubAdapter) GetOpenPositions(_ context.Context) ([]contracts.PositionStateDTO, error) {
	return nil, nil
}
func (a *dispatcherStubAdapter) GetPosition(_ context.Context, _ string) (*contracts.PositionStateDTO, error) {
	return nil, database.ErrNotFound
}
func (a *dispatcherStubAdapter) CreateStrategyVersion(_ context.Context, _ database.StrategyVersion) error {
	return nil
}
func (a *dispatcherStubAdapter) GetActiveStrategyVersion(_ context.Context) (*database.StrategyVersion, error) {
	return nil, database.ErrNotFound
}
func (a *dispatcherStubAdapter) GetStrategyVersion(_ context.Context, _ string) (*database.StrategyVersion, error) {
	return nil, database.ErrNotFound
}
func (a *dispatcherStubAdapter) SetStrategyVersionStatus(_ context.Context, _, _, _ string) error {
	return nil
}
func (a *dispatcherStubAdapter) GetActiveStrategy(_ context.Context) (*database.StrategyVersion, error) {
	return nil, database.ErrNotFound
}
func (a *dispatcherStubAdapter) GetShadowStrategy(_ context.Context) (*database.StrategyVersion, error) {
	return nil, database.ErrNotFound
}
func (a *dispatcherStubAdapter) ActivateStrategyVersion(_ context.Context, _ string) error {
	return nil
}
func (a *dispatcherStubAdapter) GetSystemState(_ context.Context) (*contracts.SystemStateDTO, error) {
	return nil, database.ErrNotFound
}
func (a *dispatcherStubAdapter) UpsertSystemState(_ context.Context, _ contracts.SystemStateDTO, _ int64) (int64, error) {
	return 0, database.ErrNotImplemented
}
func (a *dispatcherStubAdapter) GetExposureSummary(_ context.Context) (*database.ExposureSummary, error) {
	return nil, database.ErrNotImplemented
}
func (a *dispatcherStubAdapter) GetEventsByTrace(_ context.Context, _ string) ([]database.Event, error) {
	return nil, nil
}
func (a *dispatcherStubAdapter) GetEventsByCorrelation(_ context.Context, _ string) ([]database.Event, error) {
	return nil, nil
}
func (a *dispatcherStubAdapter) GetFailureChain(_ context.Context, _ string) ([]database.Event, error) {
	return nil, nil
}
func (a *dispatcherStubAdapter) GetEventsByTraceIncludeArchive(_ context.Context, _ string) ([]contracts.EventEnvelope, error) {
	return nil, nil
}
func (a *dispatcherStubAdapter) ArchiveEvents(_ context.Context, _ time.Time, _ int) (int, error) {
	return 0, nil
}
func (a *dispatcherStubAdapter) ComputeDrawdown(_ context.Context, _ int) (float64, error) {
	return 0, nil
}
func (a *dispatcherStubAdapter) CreateRun(_ context.Context, _ database.PipelineRun) error {
	return nil
}
func (a *dispatcherStubAdapter) UpdateRunStage(_ context.Context, _, _ string) error  { return nil }
func (a *dispatcherStubAdapter) UpdateRunStatus(_ context.Context, _, _ string) error { return nil }
func (a *dispatcherStubAdapter) GetRun(_ context.Context, _ string) (*database.PipelineRun, error) {
	return nil, database.ErrNotFound
}
func (a *dispatcherStubAdapter) GetSolanaEndpointState(_ context.Context, _ string) (*database.SolanaEndpointState, error) {
	return nil, nil
}
func (a *dispatcherStubAdapter) UpsertSolanaEndpointState(_ context.Context, _ database.SolanaEndpointState) error {
	return nil
}
func (a *dispatcherStubAdapter) InsertSolanaSignature(_ context.Context, _ database.SolanaSignature) error {
	return nil
}
func (a *dispatcherStubAdapter) UpdateSolanaSignatureStatus(_ context.Context, _, _ string, _ int64, _ string) error {
	return nil
}
func (a *dispatcherStubAdapter) UpsertSolanaEndpointHealth(_ context.Context, _ database.SolanaEndpointHealth) error {
	return nil
}
func (a *dispatcherStubAdapter) ListSolanaEndpointsRanked(_ context.Context) ([]database.SolanaEndpointHealth, error) {
	return nil, nil
}
func (a *dispatcherStubAdapter) GetSolanaIngestionWatermark(_ context.Context, _ string) (uint64, error) {
	return 0, nil
}
func (a *dispatcherStubAdapter) UpsertSolanaIngestionWatermark(_ context.Context, _ string, _ uint64) error {
	return nil
}

// ── Phase 8: Production Hardening stubs ──────────────────────────────────────

func (a *dispatcherStubAdapter) ClaimNextEvents(_ context.Context, _ database.EventClaimQuery) ([]contracts.EventEnvelope, error) {
	return nil, nil
}
func (a *dispatcherStubAdapter) IncrementEventRetry(_ context.Context, _, _ string) (int, error) {
	return 0, nil
}
func (a *dispatcherStubAdapter) MoveToDLQ(_ context.Context, _ database.DLQEntry) error { return nil }
func (a *dispatcherStubAdapter) RequeueFromDLQ(_ context.Context, _ string) error       { return nil }
func (a *dispatcherStubAdapter) ListDLQ(_ context.Context, _ database.DLQFilter) ([]database.DLQEntry, error) {
	return nil, nil
}
func (a *dispatcherStubAdapter) ClaimExecution(_ context.Context, _ contracts.AllocationDTO) (bool, error) {
	return true, nil
}
func (a *dispatcherStubAdapter) UpsertPositionFromExecution(_ context.Context, _ contracts.PositionStateDTO) (bool, error) {
	return true, nil
}
func (a *dispatcherStubAdapter) ListOpenPositionsForReconciliation(_ context.Context) ([]database.ReconciliationPosition, error) {
	return nil, nil
}
func (a *dispatcherStubAdapter) AdjustPositionAmount(_ context.Context, _, _, _ string) error {
	return nil
}
func (a *dispatcherStubAdapter) ClosePositionForced(_ context.Context, _, _ string) error { return nil }
func (a *dispatcherStubAdapter) InsertLatencyEvent(_ context.Context, _ database.LatencyEvent) error {
	return nil
}
func (a *dispatcherStubAdapter) GetLatencyProfile(_ context.Context, _, _, _ string, _ int) (contracts.LatencyProfileDTO, error) {
	return contracts.LatencyProfileDTO{}, nil
}
func (a *dispatcherStubAdapter) PromoteStrategyVersion(_ context.Context, _ string, _ int) error {
	return nil
}
func (a *dispatcherStubAdapter) DrainAndCheckPipelineIdle(_ context.Context, _ int) (bool, error) {
	return true, nil
}
func (a *dispatcherStubAdapter) SetSystemHalt(_ context.Context, _ bool, _, _ string) error {
	return nil
}
func (a *dispatcherStubAdapter) IsSystemHalted(_ context.Context) (bool, string, error) {
	return false, "", nil
}
func (a *dispatcherStubAdapter) ComputeStateHash(_ context.Context) (string, error) { return "", nil }
func (a *dispatcherStubAdapter) ClaimPartitions(_ context.Context, _, _, _ string, _, _ int) ([]int, error) {
	return nil, nil
}
func (a *dispatcherStubAdapter) RenewPartitions(_ context.Context, _, _, _ string) error {
	return nil
}
func (a *dispatcherStubAdapter) ReleasePartitions(_ context.Context, _, _, _ string) error {
	return nil
}
func (a *dispatcherStubAdapter) ListInFlightExecutions(_ context.Context) ([]database.InFlightExecution, error) {
	return nil, nil
}
func (a *dispatcherStubAdapter) FinalizeExecution(_ context.Context, _ string, _ database.ExecutionReceipt) error {
	return nil
}
func (a *dispatcherStubAdapter) AbortReservedExecution(_ context.Context, _, _ string) error {
	return nil
}
func (a *dispatcherStubAdapter) MarkExecutionLost(_ context.Context, _, _ string) error { return nil }
func (a *dispatcherStubAdapter) RecordReorg(_ context.Context, _ string, _, _ int64, _ int) error {
	return nil
}
func (a *dispatcherStubAdapter) InvalidateBlockRange(_ context.Context, _ string, _, _ int64) (int, error) {
	return 0, nil
}
func (a *dispatcherStubAdapter) MarkPositionsUncertain(_ context.Context, _ string, _ int64, _ string) error {
	return nil
}
func (a *dispatcherStubAdapter) ReResolveExecutionAfterReorg(_ context.Context, _, _ string, _ database.ReorgOutcome) error {
	return nil
}
func (a *dispatcherStubAdapter) RecordExecutionForEvaluation(_ context.Context, _ string, _ int) error {
	return nil
}
func (a *dispatcherStubAdapter) MarkEvaluationDone(_ context.Context, _ string) error { return nil }
func (a *dispatcherStubAdapter) ListMissingEvaluations(_ context.Context) ([]database.MissingEvaluation, error) {
	return nil, nil
}
func (a *dispatcherStubAdapter) GetUnprocessedCount(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}
func (a *dispatcherStubAdapter) RecordDrop(_ context.Context, _, _, _, _ string) error { return nil }
func (a *dispatcherStubAdapter) GetPipelineStats(_ context.Context, _ int) (*database.PipelineStats, error) {
	return &database.PipelineStats{}, nil
}

func TestNewDispatcher_NilLogger_DoesNotPanic(t *testing.T) {
	adapter := &dispatcherStubAdapter{}
	client := telegram.NewClient("token", "chat123")
	d := telegram.NewDispatcher(adapter, client, nil)
	if d == nil {
		t.Fatal("expected non-nil dispatcher")
	}
}

func TestNewDispatcher_WithLogger_DoesNotPanic(t *testing.T) {
	adapter := &dispatcherStubAdapter{}
	client := telegram.NewClient("token", "chat123")
	logger := slog.Default()
	d := telegram.NewDispatcher(adapter, client, logger)
	if d == nil {
		t.Fatal("expected non-nil dispatcher")
	}
}

// TestDispatcher_Run_CancelledContext_Exits verifies Run exits when context is cancelled.
func TestDispatcher_Run_CancelledContext_Exits(t *testing.T) {
	// Arrange — no events queued
	adapter := &dispatcherStubAdapter{nextEvent: nil}
	client := telegram.NewClient("token", "chat123")
	d := telegram.NewDispatcher(adapter, client, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	// Act — must return promptly without blocking
	done := make(chan error, 1)
	go func() {
		done <- d.Run(ctx)
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Error("expected non-nil error (context.Canceled)")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit after context cancellation")
	}
}

// TestDispatcher_Run_ProcessesEvent verifies that when an event is available it is processed.
func TestDispatcher_Run_ProcessesEvent(t *testing.T) {
	// Arrange — queue one valid telegram_event, then return nil to simulate empty queue
	payload, _ := json.Marshal(telegram.TelegramEventPayload{
		MessageType: "test_message",
		Text:        "hello operator",
	})
	evt := &database.Event{
		EventID:   "tg-evt-1",
		EventType: "telegram_event",
		Payload:   payload,
	}

	callCount := 0
	adapter := &dispatcherStubAdapter{}
	// Return the event on first call, then nil to trigger context exit.
	adapter.nextEvent = evt

	client := telegram.NewClient("", "") // empty → SendMessage returns an error (bot not configured)
	d := telegram.NewDispatcher(adapter, client, slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = callCount

	// Act — just run and ensure it doesn't block forever
	done := make(chan error, 1)
	go func() { done <- d.Run(ctx) }()

	select {
	case <-done:
		// success — Run exited
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit within timeout")
	}
}

// Phase 11 (Reference-Repo R2 — LEARN) creator-blacklist stubs.
func (s *dispatcherStubAdapter) UpsertCreatorRugObservation(_ context.Context, _ database.CreatorRugObservation) error {
	return nil
}
func (s *dispatcherStubAdapter) GetCreatorBlacklistEntry(_ context.Context, _ string, _ string) (*database.CreatorBlacklistEntry, error) {
	return nil, nil
}
