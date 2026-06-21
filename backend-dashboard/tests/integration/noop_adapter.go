package integration

import (
	"context"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
)

// noopAdapter satisfies database.Adapter with inert defaults for integration tests.

type noopAdapter struct{}

func (s *noopAdapter) Initialize(_ context.Context, _ database.Config) error { return nil }
func (s *noopAdapter) RunMigrations(_ context.Context) error                 { return nil }
func (s *noopAdapter) Close(_ context.Context) error                         { return nil }

func (s *noopAdapter) InsertEvent(_ context.Context, _ database.Event) error { return nil }
func (s *noopAdapter) ClaimNextEvent(_ context.Context, _ string, _ []string) (*database.Event, error) {
	return nil, nil
}
func (s *noopAdapter) MarkEventProcessed(_ context.Context, _ string) error { return nil }
func (s *noopAdapter) ReleaseEventClaim(_ context.Context, _ string) error  { return nil }
func (s *noopAdapter) GetEventByID(_ context.Context, _ string) (*database.Event, error) {
	return nil, database.ErrNotFound
}
func (s *noopAdapter) MarkEventExpired(_ context.Context, _ string, _ string) error {
	return nil
}

func (s *noopAdapter) UpsertIngestionWatermark(_ context.Context, _ string, _ uint64) error {
	return nil
}
func (s *noopAdapter) GetIngestionWatermark(_ context.Context, _ string) (uint64, error) {
	return 0, nil
}
func (s *noopAdapter) InsertMarketData(_ context.Context, _ contracts.MarketDataDTO) error {
	return nil
}
func (s *noopAdapter) GetMarketData(_ context.Context, _ string) (*contracts.MarketDataDTO, error) {
	return nil, database.ErrNotFound
}
func (s *noopAdapter) GetLatestMarketDataForToken(_ context.Context, _, _ string) (*contracts.MarketDataDTO, error) {
	return nil, nil
}
func (s *noopAdapter) EnqueueProbePending(_ context.Context, _ database.ProbePendingEnqueue) error {
	return nil
}
func (s *noopAdapter) ClaimDueProbePending(_ context.Context, _ int) ([]database.ProbePendingRow, error) {
	return nil, nil
}
func (s *noopAdapter) CompleteProbePending(_ context.Context, _ string) error { return nil }
func (s *noopAdapter) FailProbePending(_ context.Context, _, _ string, _ int) error {
	return nil
}
func (s *noopAdapter) ExpireStaleProbePending(_ context.Context, _ int) (int64, error) {
	return 0, nil
}
func (s *noopAdapter) ExpireStaleProbePendingRows(_ context.Context, _ int) ([]database.ProbePendingRow, error) {
	return nil, nil
}
func (s *noopAdapter) GetProbePendingStats(_ context.Context) (*database.ProbePendingStats, error) {
	return &database.ProbePendingStats{}, nil
}

func (s *noopAdapter) StartLifecycle(_ context.Context, _ contracts.MarketDataDTO) (string, error) {
	return "lc-1", nil
}
func (s *noopAdapter) TransitionState(_ context.Context, _ database.TransitionRequest) error {
	return nil
}
func (s *noopAdapter) GetLifecycle(_ context.Context, _ string) (*database.Lifecycle, error) {
	return nil, database.ErrNotFound
}
func (s *noopAdapter) GetLifecycleByToken(_ context.Context, _ string) (*database.Lifecycle, error) {
	return nil, database.ErrNotFound
}
func (s *noopAdapter) QuarantineToken(_ context.Context, _ string, _ string) error { return nil }
func (s *noopAdapter) InsertStateViolation(_ context.Context, _, _, _, _ string) error {
	return nil
}

func (s *noopAdapter) InsertDataQuality(_ context.Context, _ contracts.DataQualityDTO) error {
	return nil
}
func (s *noopAdapter) InsertFeature(_ context.Context, _ contracts.FeatureDTO) error { return nil }
func (s *noopAdapter) InsertEdge(_ context.Context, _ contracts.EdgeDTO) error       { return nil }
func (s *noopAdapter) InsertValidatedEdge(_ context.Context, _ contracts.ValidatedEdgeDTO) error {
	return nil
}
func (s *noopAdapter) InsertSelection(_ context.Context, _ contracts.SelectionOutputDTO) error {
	return nil
}
func (s *noopAdapter) InsertAllocation(_ context.Context, _ contracts.AllocationDTO) error {
	return nil
}
func (s *noopAdapter) InsertExecutionResult(_ context.Context, _ contracts.ExecutionResultDTO) error {
	return nil
}
func (s *noopAdapter) InsertPositionState(_ context.Context, _ contracts.PositionStateDTO) error {
	return nil
}
func (s *noopAdapter) InsertEvaluation(_ context.Context, _ contracts.EvaluationDTO) error {
	return nil
}
func (s *noopAdapter) GetExecutionByLifecycle(_ context.Context, _ string) (*contracts.ExecutionResultDTO, error) {
	return nil, database.ErrNotFound
}
func (s *noopAdapter) GetProbabilityForLifecycle(_ context.Context, _ string) (float64, bool, error) {
	return 0, false, nil
}
func (s *noopAdapter) InsertShadowTrade(_ context.Context, _ database.ShadowTrade) error { return nil }
func (s *noopAdapter) UpdateShadowTradeObservation(_ context.Context, _ string, _ float64, _ string) error {
	return nil
}
func (s *noopAdapter) GetShadowTradesByWindow(_ context.Context, _ int) ([]database.ShadowTrade, error) {
	return nil, nil
}
func (s *noopAdapter) InsertLearningRecord(_ context.Context, _ contracts.LearningRecordDTO) error {
	return nil
}
func (s *noopAdapter) InsertProbabilityEstimate(_ context.Context, _ contracts.ProbabilityEstimateDTO) error {
	return nil
}
func (s *noopAdapter) InsertSlippageEstimate(_ context.Context, _ contracts.SlippageEstimateDTO) error {
	return nil
}
func (s *noopAdapter) InsertLatencyProfile(_ context.Context, _ contracts.LatencyProfileDTO) error {
	return nil
}
func (s *noopAdapter) GetProbabilityEstimateByTrace(_ context.Context, _ string) (*contracts.ProbabilityEstimateDTO, error) {
	return nil, database.ErrNotFound
}
func (s *noopAdapter) GetSlippageAlpha(_ context.Context, _ string) (float64, error) {
	return 1.0, nil
}
func (s *noopAdapter) GetRealizedFillSamples(_ context.Context, _ int) (map[string][]database.FillSample, error) {
	return nil, nil
}
func (s *noopAdapter) UpsertSlippageAlpha(_ context.Context, _ string, _, _, _ float64, _ int) error {
	return nil
}
func (s *noopAdapter) GetSlippageEstimateByTrace(_ context.Context, _ string) (*contracts.SlippageEstimateDTO, error) {
	return nil, database.ErrNotFound
}
func (s *noopAdapter) GetEstimatesByTrace(ctx context.Context, traceID string) (*contracts.ProbabilityEstimateDTO, *contracts.SlippageEstimateDTO, error) {
	p, _ := s.GetProbabilityEstimateByTrace(ctx, traceID)
	sl, _ := s.GetSlippageEstimateByTrace(ctx, traceID)
	return p, sl, nil
}
func (s *noopAdapter) GetLatestLatencyProfile(_ context.Context, _ string) (*contracts.LatencyProfileDTO, error) {
	return nil, database.ErrNotFound
}
func (s *noopAdapter) GetLearningRecordsByWindow(_ context.Context, _ string, _, _ time.Time) ([]contracts.LearningRecordDTO, error) {
	return nil, nil
}
func (s *noopAdapter) GetEvaluationsByVersion(_ context.Context, _ string) ([]contracts.EvaluationDTO, error) {
	return nil, nil
}
func (s *noopAdapter) AllocateNonce(_ context.Context, _, _ string) (uint64, error) {
	return 0, database.ErrNotImplemented
}
func (s *noopAdapter) ReconcileNonce(_ context.Context, _, _ string, _ uint64) error {
	return database.ErrNotImplemented
}
func (s *noopAdapter) GetOpenPositions(_ context.Context) ([]contracts.PositionStateDTO, error) {
	return nil, nil
}
func (s *noopAdapter) GetPosition(_ context.Context, _ string) (*contracts.PositionStateDTO, error) {
	return nil, database.ErrNotFound
}
func (s *noopAdapter) GetClosedPositions(_ context.Context, _ int) ([]contracts.PositionStateDTO, error) {
	return nil, nil
}
func (s *noopAdapter) GetShadowGateStats(_ context.Context, _ int) (*database.ShadowGateStats, error) {
	return &database.ShadowGateStats{}, nil
}
func (s *noopAdapter) FindPositionByPrefix(_ context.Context, _ string) (*contracts.PositionStateDTO, error) {
	return nil, database.ErrNotFound
}
func (s *noopAdapter) CreateStrategyVersion(_ context.Context, _ database.StrategyVersion) error {
	return nil
}
func (s *noopAdapter) GetActiveStrategyVersion(_ context.Context) (*database.StrategyVersion, error) {
	return nil, database.ErrNotFound
}
func (s *noopAdapter) GetStrategyVersion(_ context.Context, _ string) (*database.StrategyVersion, error) {
	return nil, database.ErrNotFound
}
func (s *noopAdapter) SetStrategyVersionStatus(_ context.Context, _, _, _ string) error { return nil }
func (s *noopAdapter) GetActiveStrategy(_ context.Context) (*database.StrategyVersion, error) {
	return nil, database.ErrNotFound
}
func (s *noopAdapter) GetShadowStrategy(_ context.Context) (*database.StrategyVersion, error) {
	return nil, database.ErrNotFound
}
func (s *noopAdapter) ActivateStrategyVersion(_ context.Context, _ string) error { return nil }
func (s *noopAdapter) GetSystemState(_ context.Context) (*contracts.SystemStateDTO, error) {
	return nil, database.ErrNotFound
}
func (s *noopAdapter) UpsertSystemState(_ context.Context, _ contracts.SystemStateDTO, _ int64) (int64, error) {
	return 0, database.ErrNotImplemented
}
func (s *noopAdapter) GetExposureSummary(_ context.Context) (*database.ExposureSummary, error) {
	return nil, database.ErrNotImplemented
}
func (s *noopAdapter) GetEventsByTrace(_ context.Context, _ string) ([]database.Event, error) {
	return nil, nil
}
func (s *noopAdapter) GetEventsByCorrelation(_ context.Context, _ string) ([]database.Event, error) {
	return nil, nil
}
func (s *noopAdapter) GetLastEventTimestamp(_ context.Context, _ string) (time.Time, error) {
	return time.Time{}, database.ErrNotFound
}
func (s *noopAdapter) GetFailureChain(_ context.Context, _ string) ([]database.Event, error) {
	return nil, nil
}
func (s *noopAdapter) GetEventsByTraceIncludeArchive(_ context.Context, _ string) ([]contracts.EventEnvelope, error) {
	return nil, nil
}
func (s *noopAdapter) ArchiveEvents(_ context.Context, _ time.Time, _ int) (int, error) {
	return 0, nil
}
func (s *noopAdapter) ComputeDrawdown(_ context.Context, _ int) (float64, error) { return 0, nil }
func (s *noopAdapter) CreateRun(_ context.Context, _ database.PipelineRun) error { return nil }
func (s *noopAdapter) UpdateRunStage(_ context.Context, _, _ string) error       { return nil }
func (s *noopAdapter) UpdateRunStatus(_ context.Context, _, _ string) error      { return nil }
func (s *noopAdapter) GetRun(_ context.Context, _ string) (*database.PipelineRun, error) {
	return nil, database.ErrNotFound
}

// ── Solana stubs (Phase 7) ────────────────────────────────────────────────────

func (s *noopAdapter) GetSolanaEndpointState(_ context.Context, _ string) (*database.SolanaEndpointState, error) {
	return nil, nil
}
func (s *noopAdapter) UpsertSolanaEndpointState(_ context.Context, _ database.SolanaEndpointState) error {
	return nil
}
func (s *noopAdapter) InsertSolanaSignature(_ context.Context, _ database.SolanaSignature) error {
	return nil
}
func (s *noopAdapter) UpdateSolanaSignatureStatus(_ context.Context, _, _ string, _ int64, _ string) error {
	return nil
}
func (s *noopAdapter) UpsertSolanaEndpointHealth(_ context.Context, _ database.SolanaEndpointHealth) error {
	return nil
}
func (s *noopAdapter) ListSolanaEndpointsRanked(_ context.Context) ([]database.SolanaEndpointHealth, error) {
	return nil, nil
}
func (s *noopAdapter) GetSolanaIngestionWatermark(_ context.Context, _ string) (uint64, error) {
	return 0, nil
}
func (s *noopAdapter) UpsertSolanaIngestionWatermark(_ context.Context, _ string, _ uint64) error {
	return nil
}

// ── Phase 8: Production Hardening stubs ──────────────────────────────────────

func (s *noopAdapter) ClaimNextEvents(_ context.Context, _ database.EventClaimQuery) ([]contracts.EventEnvelope, error) {
	return nil, nil
}
func (s *noopAdapter) IncrementEventRetry(_ context.Context, _, _ string) (int, error) {
	return 0, nil
}
func (s *noopAdapter) MoveToDLQ(_ context.Context, _ database.DLQEntry) error { return nil }
func (s *noopAdapter) RequeueFromDLQ(_ context.Context, _ string) error       { return nil }
func (s *noopAdapter) ListDLQ(_ context.Context, _ database.DLQFilter) ([]database.DLQEntry, error) {
	return nil, nil
}
func (s *noopAdapter) ClaimExecution(_ context.Context, _ contracts.AllocationDTO) (bool, error) {
	return true, nil
}
func (s *noopAdapter) UpsertPositionFromExecution(_ context.Context, _ contracts.PositionStateDTO) (bool, error) {
	return true, nil
}
func (s *noopAdapter) ListOpenPositionsForReconciliation(_ context.Context) ([]database.ReconciliationPosition, error) {
	return nil, nil
}
func (s *noopAdapter) AdjustPositionAmount(_ context.Context, _, _, _ string) error { return nil }
func (s *noopAdapter) ClosePositionForced(_ context.Context, _, _ string) error     { return nil }
func (s *noopAdapter) InsertLatencyEvent(_ context.Context, _ database.LatencyEvent) error {
	return nil
}
func (s *noopAdapter) GetLatencyProfile(_ context.Context, _, _, _ string, _ int) (contracts.LatencyProfileDTO, error) {
	return contracts.LatencyProfileDTO{}, nil
}
func (s *noopAdapter) PromoteStrategyVersion(_ context.Context, _ string, _ int) error { return nil }
func (s *noopAdapter) DrainAndCheckPipelineIdle(_ context.Context, _ int) (bool, error) {
	return true, nil
}
func (s *noopAdapter) SetSystemHalt(_ context.Context, _ bool, _, _ string) error { return nil }
func (s *noopAdapter) IsSystemHalted(_ context.Context) (bool, string, error)     { return false, "", nil }
func (s *noopAdapter) ComputeStateHash(_ context.Context) (string, error)         { return "", nil }
func (s *noopAdapter) ClaimPartitions(_ context.Context, _, _, _ string, _, _ int) ([]int, error) {
	return nil, nil
}
func (s *noopAdapter) RenewPartitions(_ context.Context, _, _, _ string) error   { return nil }
func (s *noopAdapter) ReleasePartitions(_ context.Context, _, _, _ string) error { return nil }
func (s *noopAdapter) ListInFlightExecutions(_ context.Context) ([]database.InFlightExecution, error) {
	return nil, nil
}
func (s *noopAdapter) FinalizeExecution(_ context.Context, _ string, _ database.ExecutionReceipt) error {
	return nil
}
func (s *noopAdapter) AbortReservedExecution(_ context.Context, _, _ string) error { return nil }
func (s *noopAdapter) MarkExecutionLost(_ context.Context, _, _ string) error      { return nil }
func (s *noopAdapter) RecordReorg(_ context.Context, _ string, _, _ int64, _ int) error {
	return nil
}
func (s *noopAdapter) InvalidateBlockRange(_ context.Context, _ string, _, _ int64) (int, error) {
	return 0, nil
}
func (s *noopAdapter) MarkPositionsUncertain(_ context.Context, _ string, _ int64, _ string) error {
	return nil
}
func (s *noopAdapter) ReResolveExecutionAfterReorg(_ context.Context, _, _ string, _ database.ReorgOutcome) error {
	return nil
}
func (s *noopAdapter) RecordExecutionForEvaluation(_ context.Context, _ string, _ int) error {
	return nil
}
func (s *noopAdapter) MarkEvaluationDone(_ context.Context, _ string) error { return nil }
func (s *noopAdapter) ListMissingEvaluations(_ context.Context) ([]database.MissingEvaluation, error) {
	return nil, nil
}
func (s *noopAdapter) GetUnprocessedCount(_ context.Context, _, _ string) (int64, error) {
	return 0, nil
}
func (s *noopAdapter) RecordDrop(_ context.Context, _, _, _, _ string) error { return nil }
func (s *noopAdapter) GetPipelineStats(_ context.Context, _ int) (*database.PipelineStats, error) {
	return &database.PipelineStats{}, nil
}
func (s *noopAdapter) ListRecentEvents(_ context.Context, _ string, _ int) ([]database.RecentEventRow, error) {
	return nil, nil
}
func (s *noopAdapter) GetDQBreakdown(_ context.Context, _ int, _ string) (*database.DQBreakdown, error) {
	return nil, nil
}
// Phase 11 (Reference-Repo R2 — LEARN) creator-blacklist stubs.
func (s *noopAdapter) UpsertCreatorRugObservation(_ context.Context, _ database.CreatorRugObservation) error {
	return nil
}
func (s *noopAdapter) GetCreatorBlacklistEntry(_ context.Context, _ string, _ string) (*database.CreatorBlacklistEntry, error) {
	return nil, nil
}
func (s *noopAdapter) CountTokensByCreator(_ context.Context, _ string, _ string) (int32, error) {
	return 0, nil
}
func (s *noopAdapter) GetAdaptiveDQStats(_ context.Context, _ int) (int, int, error) {
	return 0, 0, nil
}
func (s *noopAdapter) SaveBaseline(_ context.Context, _, _, _ string, _ []float64) error {
	return nil
}
func (s *noopAdapter) LoadBaselines(_ context.Context, _ string) (map[string]map[string][]float64, error) {
	return map[string]map[string][]float64{}, nil
}

// Phase 10 rescan layer stub.
func (s *noopAdapter) GetTokensForRescan(_ context.Context, _ database.RescanQuery) ([]contracts.MarketDataDTO, error) {
	return []contracts.MarketDataDTO{}, nil
}

// CheckTokenNameSeen stub — always returns (false, nil) so tests using
// noopAdapter proceed through probes without hitting a DB.
func (s *noopAdapter) CheckTokenNameSeen(_ context.Context, _, _, _ string) (bool, error) {
	return false, nil
}
func (s *noopAdapter) GetLatestPoolAddressForToken(_ context.Context, _, _ string) (string, bool, error) {
	return "", false, nil
}

// ── Historical Market Profiles stubs (Approach A) ─────────────────────────────

func (s *noopAdapter) UpsertHistoricalProfile(_ context.Context, _ contracts.HistoricalMarketProfileDTO) error {
	return nil
}
func (s *noopAdapter) GetHistoricalProfile(_ context.Context, _ string) (*contracts.HistoricalMarketProfileDTO, error) {
	return nil, nil
}
func (s *noopAdapter) ListHistoricalProfiles(_ context.Context) ([]contracts.HistoricalMarketProfileDTO, error) {
	return nil, nil
}

func (s *noopAdapter) GetExecutionLog(_ context.Context, _ int) ([]database.ExecutionLogRow, error) {
	return nil, nil
}

// Task 8: Creator Profiles stubs.
func (s *noopAdapter) UpsertCreatorProfileOnLaunch(_ context.Context, _, _ string) error { return nil }
func (s *noopAdapter) IncrementCreatorOutcome(_ context.Context, _, _, _ string) error   { return nil }
func (s *noopAdapter) GetCreatorProfile(_ context.Context, _, _ string) (contracts.CreatorProfile, bool, error) {
	return contracts.CreatorProfile{}, false, nil
}
