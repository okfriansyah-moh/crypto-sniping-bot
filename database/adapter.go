package database

import (
	"context"
	"time"

	"crypto-sniping-bot/contracts"
)

// Adapter is the single entry point for all database operations.
// Every method accepts and returns immutable types.
// Every write method is idempotent (ON CONFLICT DO NOTHING semantics).
// Only the orchestrator calls the adapter — modules NEVER import database/.
//
// See docs/db_adapter_spec.md § 2 for the full specification.
type Adapter interface {
	// ── Lifecycle ────────────────────────────────────────────────────────────

	// Initialize establishes the database connection pool.
	Initialize(ctx context.Context, cfg Config) error

	// RunMigrations applies all pending SQL migration files in order.
	// Tracks applied migrations in the _migrations table.
	// Idempotent: safe to call on every startup.
	RunMigrations(ctx context.Context) error

	// Close releases the connection pool.
	Close(ctx context.Context) error

	// ── Event Bus (append-only) ──────────────────────────────────────────────

	// InsertEvent appends a DTO transition to the event bus.
	// Idempotent: ON CONFLICT (event_id) DO NOTHING.
	// Validates trace_id, correlation_id, causation_id (except Layer 0), version_id.
	// Returns ErrMissingTraceField if required trace fields are absent.
	// Returns ErrOrphanEvent if causation_id references a non-existent event.
	InsertEvent(ctx context.Context, evt Event) error

	// ClaimNextEvent atomically claims the next unprocessed event for a worker
	// group using SELECT ... FOR UPDATE SKIP LOCKED. Returns nil if queue is empty.
	// Ordering: priority DESC, created_at ASC. Excludes rows where expires_at < NOW().
	ClaimNextEvent(ctx context.Context, group string, eventTypes []string) (*Event, error)

	// MarkEventProcessed marks an event as handled. Call only after successful
	// stage execution and any resulting event writes.
	MarkEventProcessed(ctx context.Context, eventID string) error

	// GetEventByID fetches a specific event by its ID. Used for trace traversal and failure chain reconstruction.
	GetEventByID(ctx context.Context, eventID string) (*Event, error)

	// MarkEventExpired marks an event as expired without processing.
	// Emits an expired_event in the same transaction.
	MarkEventExpired(ctx context.Context, eventID string, reason string) error

	// ReleaseEventClaim clears claimed_at on an event so it can be re-claimed
	// immediately by the next worker. Call on handler failure to bypass the
	// stale-claim timeout and allow prompt retry.
	ReleaseEventClaim(ctx context.Context, eventID string) error

	// UpsertIngestionWatermark records the last processed block per chain.
	UpsertIngestionWatermark(ctx context.Context, chain string, blockNumber uint64) error

	// GetIngestionWatermark returns the last processed block for a chain.
	GetIngestionWatermark(ctx context.Context, chain string) (uint64, error)

	// InsertMarketData persists a MarketDataDTO.
	InsertMarketData(ctx context.Context, dto contracts.MarketDataDTO) error

	// GetMarketData retrieves a MarketDataDTO by event ID.
	GetMarketData(ctx context.Context, eventID string) (*contracts.MarketDataDTO, error)

	// GetTokensForRescan returns up to q.Limit MarketDataDTOs whose
	// (current_time - market_data.ingested_at) falls in [minAge, maxAge],
	// filtered by the latest data_quality row's sub-scores (honeypot_score,
	// rug_score, buy_tax_bps), excluding tokens in open lifecycle states when
	// q.SkipOpenPositions is true.
	//
	// Results are deterministic: ORDER BY token_address ASC, ingested_at DESC;
	// one row per token (latest). Read-only. Idempotent. No side effects.
	GetTokensForRescan(ctx context.Context, q RescanQuery) ([]contracts.MarketDataDTO, error)

	// ── Token Lifecycle State Machine ────────────────────────────────────────

	// StartLifecycle creates a new lifecycle entry at state DETECTED.
	StartLifecycle(ctx context.Context, dto contracts.MarketDataDTO) (lifecycleID string, err error)

	// TransitionState applies a forward-only CAS transition.
	// Returns ErrInvalidTransition if the CAS guard (state_version or current_state) fails.
	TransitionState(ctx context.Context, req TransitionRequest) error

	// GetLifecycle fetches a lifecycle by ID.
	GetLifecycle(ctx context.Context, lifecycleID string) (*Lifecycle, error)

	// GetLifecycleByToken fetches the active lifecycle for a token address.
	GetLifecycleByToken(ctx context.Context, tokenAddress string) (*Lifecycle, error)

	// QuarantineToken marks a token as quarantined and transitions its lifecycle to REJECTED.
	QuarantineToken(ctx context.Context, tokenAddress string, reason string) error

	// InsertStateViolation records a CAS conflict violation for audit purposes.
	InsertStateViolation(ctx context.Context, lifecycleID, fromState, toState, reason string) error

	// ── DTO Persistence (one method per DTO type) ────────────────────────────

	// InsertDataQuality persists a DataQualityDTO.
	InsertDataQuality(ctx context.Context, dto contracts.DataQualityDTO) error

	// InsertFeature persists a FeatureDTO.
	InsertFeature(ctx context.Context, dto contracts.FeatureDTO) error

	// InsertEdge persists an EdgeDTO.
	InsertEdge(ctx context.Context, dto contracts.EdgeDTO) error

	// InsertValidatedEdge persists a ValidatedEdgeDTO.
	InsertValidatedEdge(ctx context.Context, dto contracts.ValidatedEdgeDTO) error

	// InsertSelection persists a SelectionOutputDTO.
	InsertSelection(ctx context.Context, dto contracts.SelectionOutputDTO) error

	// InsertAllocation persists an AllocationDTO.
	InsertAllocation(ctx context.Context, dto contracts.AllocationDTO) error

	// InsertExecutionResult persists an ExecutionResultDTO.
	InsertExecutionResult(ctx context.Context, dto contracts.ExecutionResultDTO) error

	// InsertPositionState persists a PositionStateDTO.
	InsertPositionState(ctx context.Context, dto contracts.PositionStateDTO) error

	// InsertEvaluation persists an EvaluationDTO.
	InsertEvaluation(ctx context.Context, dto contracts.EvaluationDTO) error

	// GetExecutionByLifecycle returns the ExecutionResultDTO for a lifecycle ID.
	// Returns ErrNotFound if no execution record exists for the lifecycle.
	GetExecutionByLifecycle(ctx context.Context, lifecycleID string) (*contracts.ExecutionResultDTO, error)

	// GetShadowTradesByWindow returns pending shadow trades whose rejected_at is
	// older than (now - windowSeconds) and whose observation_complete is false.
	GetShadowTradesByWindow(ctx context.Context, windowSeconds int) ([]ShadowTrade, error)

	// InsertLearningRecord persists a LearningRecordDTO.
	InsertLearningRecord(ctx context.Context, dto contracts.LearningRecordDTO) error

	// ── Phase 4: Signal Quality Models ───────────────────────────────────────

	// InsertProbabilityEstimate persists a ProbabilityEstimateDTO. Idempotent.
	InsertProbabilityEstimate(ctx context.Context, dto contracts.ProbabilityEstimateDTO) error

	// InsertSlippageEstimate persists a SlippageEstimateDTO. Idempotent.
	InsertSlippageEstimate(ctx context.Context, dto contracts.SlippageEstimateDTO) error

	// InsertLatencyProfile persists a LatencyProfileDTO. Idempotent.
	InsertLatencyProfile(ctx context.Context, dto contracts.LatencyProfileDTO) error

	// GetProbabilityEstimateByTrace returns the most recent probability estimate
	// for the given trace ID, or nil if not present.
	GetProbabilityEstimateByTrace(ctx context.Context, traceID string) (*contracts.ProbabilityEstimateDTO, error)

	// GetSlippageEstimateByTrace returns the most recent slippage estimate
	// for the given trace ID, or nil if not present.
	GetSlippageEstimateByTrace(ctx context.Context, traceID string) (*contracts.SlippageEstimateDTO, error)

	// GetEstimatesByTrace returns the most recent probability and slippage
	// estimates for the given trace ID in a single round-trip. Either or
	// both may be nil if the corresponding row has not yet been committed.
	// F-SEC-05: halves DB round-trips on the validation hot path.
	GetEstimatesByTrace(ctx context.Context, traceID string) (
		*contracts.ProbabilityEstimateDTO, *contracts.SlippageEstimateDTO, error)

	// GetSlippageAlpha returns the per-market α calibration coefficient
	// for the Layer 4 slippage model. α is the multiplier applied to the
	// CPMM closed-form base impact and absorbs realized fee/tax/MEV drift.
	//
	// market is a stable per-market key (e.g. "eth-uniswap-v2"); empty
	// string requests the global default.
	//
	// Cold-start behavior: when no row exists for the requested market,
	// implementations MUST return (1.0, nil). The execution_quality.AlphaAggregator
	// worker populates rows from realized fills (residual risk #3 closure).
	GetSlippageAlpha(ctx context.Context, market string) (float64, error)

	// GetRealizedFillSamples returns realized-vs-predicted slippage samples
	// keyed by market, captured within the last sinceSeconds. Joins
	// execution_results × slippage_estimates × allocations to produce one
	// FillSample per successful execution with a non-zero predicted &
	// realized slippage. Used by the AlphaAggregator worker.
	GetRealizedFillSamples(ctx context.Context, sinceSeconds int) (map[string][]FillSample, error)

	// UpsertSlippageAlpha persists the computed α calibration for a market.
	// Idempotent: ON CONFLICT (market) DO UPDATE.
	UpsertSlippageAlpha(ctx context.Context, market string, alpha, ewmaPred, ewmaReal float64, sampleCount int) error

	// GetLatestLatencyProfile returns the most recent latency profile for the
	// given chain, or nil if no profile has been recorded.
	GetLatestLatencyProfile(ctx context.Context, chain string) (*contracts.LatencyProfileDTO, error)

	// ── Phase 5: Learning Engine ──────────────────────────────────────────────

	// InsertShadowTrade persists a new shadow trade observation row.
	// shadowID is SHA256(token_lifecycle_id||stage||rejected_at)[:16].
	InsertShadowTrade(ctx context.Context, st ShadowTrade) error

	// UpdateShadowTradeObservation marks a shadow trade's observation window
	// as complete and records the final observed return and classification.
	UpdateShadowTradeObservation(ctx context.Context, shadowID string, observedReturnPct float64, classification string) error

	// GetLearningRecordsByWindow returns all LearningRecordDTOs for a given
	// version within [start, end].
	GetLearningRecordsByWindow(ctx context.Context, versionID string, start, end time.Time) ([]contracts.LearningRecordDTO, error)

	// GetEvaluationsByVersion returns all EvaluationDTOs for a version, ordered by evaluated_at DESC.
	GetEvaluationsByVersion(ctx context.Context, versionID string) ([]contracts.EvaluationDTO, error)

	// ── Phase 11: Creator Blacklist (Reference-Repo Improvements R2) ─────────

	// UpsertCreatorRugObservation increments the rug counter for a creator
	// wallet on a given chain.
	//
	// Semantics: each successful call records one additional confirmed rug
	// for the (creator_address, chain) row. CreatorRugObservation does NOT
	// carry an observation_id / learning_record_id, so this method is NOT
	// idempotent for repeated calls representing the same logical
	// observation — the SQL increments rug_count on every call.
	//
	// Callers MUST therefore invoke this method at most once per confirmed
	// rug LearningRecord. The Learning Engine relies on its own
	// at-least-once de-duplication (LearningRecord EventID + the events
	// log) before reaching the adapter. A future change may extend
	// CreatorRugObservation with an idempotency key so the adapter can
	// de-duplicate on its own.
	//
	// Postgres implementation:
	//   INSERT … (creator_address, chain, rug_count=1, …)
	//   ON CONFLICT (creator_address, chain) DO UPDATE SET
	//     rug_count = creator_blacklist.rug_count + 1,
	//     last_seen_at = NOW(),
	//     last_token_address = EXCLUDED.last_token_address,
	//     strategy_version_id = EXCLUDED.strategy_version_id;
	UpsertCreatorRugObservation(ctx context.Context, obs CreatorRugObservation) error

	// GetCreatorBlacklistEntry returns the blacklist row for a creator on
	// a chain. Returns (nil, nil) when absent.
	GetCreatorBlacklistEntry(ctx context.Context, creatorAddress string, chain string) (*CreatorBlacklistEntry, error)

	// CountTokensByCreator returns the number of distinct tokens in market_data
	// whose creator_address matches creatorAddress (excluding excludeToken).
	// Returns 0 (not an error) when the column is absent or the creator is new.
	// Used by the Layer 1 dev-reputation detector to detect serial launchers.
	CountTokensByCreator(ctx context.Context, creatorAddress string, excludeToken string) (int32, error)

	// ── Execution: Nonce Manager ─────────────────────────────────────────────

	// AllocateNonce atomically reserves the next nonce for a wallet.
	AllocateNonce(ctx context.Context, walletAddress string, chain string) (nonce uint64, err error)

	// ReconcileNonce updates local state from the on-chain nonce value.
	ReconcileNonce(ctx context.Context, walletAddress string, chain string, onchainNonce uint64) error

	// ── Positions ────────────────────────────────────────────────────────────

	// GetOpenPositions returns all currently open positions.
	GetOpenPositions(ctx context.Context) ([]contracts.PositionStateDTO, error)

	// GetPosition fetches a single position by ID.
	GetPosition(ctx context.Context, positionID string) (*contracts.PositionStateDTO, error)

	// GetClosedPositions returns positions that exited within the last
	// sinceSeconds. The latest snapshot per position_id is returned, ordered
	// newest-first by exited_at. Used by /pnl for win-rate and realized
	// PnL summaries. sinceSeconds <= 0 returns the last 7 days.
	GetClosedPositions(ctx context.Context, sinceSeconds int) ([]contracts.PositionStateDTO, error)

	// FindPositionByPrefix returns the latest snapshot of a position whose
	// position_id OR token_address starts with the given prefix (case-insensitive).
	// Returns ErrNotFound if no match. Returns ErrAmbiguous if more than one
	// open position matches the prefix. Used by /position and /force_close
	// to accept short user input without forcing operators to copy/paste full IDs.
	FindPositionByPrefix(ctx context.Context, prefix string) (*contracts.PositionStateDTO, error)

	// ── Strategy Versions ────────────────────────────────────────────────────

	// CreateStrategyVersion persists an immutable strategy version snapshot.
	// Idempotent: ON CONFLICT DO NOTHING.
	CreateStrategyVersion(ctx context.Context, sv StrategyVersion) error

	// GetActiveStrategyVersion returns the currently active strategy version.
	GetActiveStrategyVersion(ctx context.Context) (*StrategyVersion, error)

	// GetStrategyVersion fetches a strategy version by ID.
	// Returns ErrUnknownVersion if not found.
	GetStrategyVersion(ctx context.Context, versionID string) (*StrategyVersion, error)

	// SetStrategyVersionStatus transitions a strategy version through its lifecycle.
	// Legal transitions: draft→shadow|deactivated, shadow→active|deactivated,
	// active→rolled_back|deactivated. Promotion to active atomically demotes the
	// existing active version to deactivated.
	// Returns ErrIllegalTransition if the transition is not permitted.
	SetStrategyVersionStatus(ctx context.Context, versionID string, newStatus string, reason string) error

	// GetActiveStrategy returns the version with status="active".
	GetActiveStrategy(ctx context.Context) (*StrategyVersion, error)

	// GetShadowStrategy returns the strategy version with status="shadow", if any.
	// Returns nil (no error) when no shadow version exists.
	GetShadowStrategy(ctx context.Context) (*StrategyVersion, error)

	// ActivateStrategyVersion deactivates the current active version and
	// activates the given version in a single transaction.
	// Safe to call on restart: re-activating an already-active version is a no-op.
	ActivateStrategyVersion(ctx context.Context, versionID string) error

	// ── System State ─────────────────────────────────────────────────────────

	// GetSystemState returns the singleton system state row.
	GetSystemState(ctx context.Context) (*contracts.SystemStateDTO, error)

	// UpsertSystemState updates the system state using CAS on state_version.
	// Returns ErrStaleState if expectedVersion does not match the current state_version.
	UpsertSystemState(ctx context.Context, state contracts.SystemStateDTO, expectedVersion int64) (newVersion int64, err error)

	// ── Exposure Summary ─────────────────────────────────────────────────────

	// GetExposureSummary returns aggregated capital exposure in O(1) via maintained aggregates.
	GetExposureSummary(ctx context.Context) (*ExposureSummary, error)

	// ── Trace Queries ────────────────────────────────────────────────────────

	// GetEventsByTrace returns all events sharing a trace ID, ordered by created_at.
	GetEventsByTrace(ctx context.Context, traceID string) ([]Event, error)

	// GetEventsByCorrelation returns all events sharing a correlation ID.
	GetEventsByCorrelation(ctx context.Context, correlationID string) ([]Event, error)

	// GetLastEventTimestamp returns the created_at timestamp of the most recent
	// event of the given event_type. Used by the adaptive risk-appetite
	// controller (operational-modes skill) to compute starvation duration.
	// Returns ErrNotFound when no event of that type exists.
	GetLastEventTimestamp(ctx context.Context, eventType string) (time.Time, error)

	// GetFailureChain reconstructs the causal chain leading to a failed event.
	GetFailureChain(ctx context.Context, failedEventID string) ([]Event, error)

	// GetEventsByTraceIncludeArchive returns events from both events and events_archive.
	// Used by replay and audit tools.
	GetEventsByTraceIncludeArchive(ctx context.Context, traceID string) ([]contracts.EventEnvelope, error)

	// ── Archival ─────────────────────────────────────────────────────────────

	// ArchiveEvents moves processed events older than olderThan to events_archive.
	// Runs in single transaction per batch; idempotent.
	// Never archives events linked to open positions.
	ArchiveEvents(ctx context.Context, olderThan time.Time, batchSize int) (archivedCount int, err error)

	// ComputeDrawdown returns the realized drawdown fraction for the given window.
	// drawdown = |sum(pnl_usd where pnl_usd < 0, exited_at >= now()-windowHours)|
	//            / max(sum(entry_size_usd of all exited positions in window), 1)
	// Returns 0 when no positions exist or total PnL is non-negative.
	ComputeDrawdown(ctx context.Context, windowHours int) (drawdownFraction float64, err error)

	// ── Pipeline Runs ────────────────────────────────────────────────────────

	// CreateRun creates a new pipeline run record.
	// Idempotent: ON CONFLICT DO NOTHING.
	CreateRun(ctx context.Context, run PipelineRun) error

	// UpdateRunStage checkpoints the last completed stage for a run.
	UpdateRunStage(ctx context.Context, runID string, stage string) error

	// UpdateRunStatus sets the terminal status (completed, failed, partial).
	UpdateRunStatus(ctx context.Context, runID string, status string) error

	// GetRun fetches a pipeline run by ID.
	GetRun(ctx context.Context, runID string) (*PipelineRun, error)

	// ── Phase 7: Solana ──────────────────────────────────────────────────────

	// UpsertSolanaEndpointState updates the circuit breaker state for a Solana
	// RPC endpoint. Idempotent: ON CONFLICT DO UPDATE.
	UpsertSolanaEndpointState(ctx context.Context, s SolanaEndpointState) error

	// GetSolanaEndpointState retrieves the current circuit breaker state for an endpoint.
	GetSolanaEndpointState(ctx context.Context, endpointURL string) (*SolanaEndpointState, error)

	// InsertSolanaSignature records a submitted Solana transaction for idempotency
	// and confirmation tracking. Idempotent: ON CONFLICT (execution_id) DO NOTHING.
	InsertSolanaSignature(ctx context.Context, sig SolanaSignature) error

	// UpdateSolanaSignatureStatus transitions a signature's status field.
	UpdateSolanaSignatureStatus(ctx context.Context, executionID string, status string, slot int64, errMsg string) error

	// UpsertSolanaEndpointHealth updates rolling health metrics for an endpoint.
	UpsertSolanaEndpointHealth(ctx context.Context, h SolanaEndpointHealth) error

	// ListSolanaEndpointsRanked returns all endpoint health rows ordered by
	// error_rate ASC, p95_latency_ms ASC (best-first).
	ListSolanaEndpointsRanked(ctx context.Context) ([]SolanaEndpointHealth, error)

	// UpsertSolanaIngestionWatermark records the last processed slot for a market.
	UpsertSolanaIngestionWatermark(ctx context.Context, market string, lastSlot uint64) error

	// GetSolanaIngestionWatermark returns the last processed slot for a market.
	// Returns 0 if no watermark exists yet.
	GetSolanaIngestionWatermark(ctx context.Context, market string) (uint64, error)

	// ── Phase 8: Production Hardening (architecture § 4.10) ─────────────────

	// ClaimNextEvents claims a batch of unprocessed events for a given consumer
	// and chain in strict ascending logical_order_key order, restricted to the
	// worker's partition shard. NEVER orders by created_at.
	ClaimNextEvents(ctx context.Context, q EventClaimQuery) ([]contracts.EventEnvelope, error)

	// IncrementEventRetry increments the retry_count for an event and returns
	// the new count. Used to decide whether to route to DLQ.
	IncrementEventRetry(ctx context.Context, eventID, consumer string) (retryCount int, err error)

	// MoveToDLQ moves a failed event to the dead_letter_events table and marks
	// the source row as processed so the partition advances.
	// Idempotent: ON CONFLICT (event_id) DO NOTHING.
	MoveToDLQ(ctx context.Context, e DLQEntry) error

	// RequeueFromDLQ re-inserts a DLQ event back into the events table for
	// reprocessing. Clears its retry_count and processed flag.
	RequeueFromDLQ(ctx context.Context, eventID string) error

	// ListDLQ returns dead-letter events matching the given filter.
	ListDLQ(ctx context.Context, filter DLQFilter) ([]DLQEntry, error)

	// ClaimExecution atomically reserves an execution_id for exactly-once
	// submission via INSERT ... ON CONFLICT DO NOTHING.
	// Returns claimed=true if this caller is the first to claim the ID;
	// false means another worker already claimed it — caller MUST NOT submit.
	ClaimExecution(ctx context.Context, dto contracts.AllocationDTO) (claimed bool, err error)

	// UpsertPositionFromExecution creates a position row keyed on
	// source_execution_id with a UNIQUE constraint to enforce single-position
	// per execution. Returns created=true on first insert, false on conflict.
	UpsertPositionFromExecution(ctx context.Context, p contracts.PositionStateDTO) (created bool, err error)

	// ListOpenPositionsForReconciliation returns all open positions with their
	// wallet_address and token_address for on-chain reconciliation.
	ListOpenPositionsForReconciliation(ctx context.Context) ([]ReconciliationPosition, error)

	// AdjustPositionAmount updates the current_price / entry_size_usd of an
	// open position to reflect an on-chain balance discrepancy and emits a
	// reconciliation_event row.
	AdjustPositionAmount(ctx context.Context, positionID string, onchainAmount string, reason string) error

	// ClosePositionForced marks an open position as closed (status='closed')
	// when the on-chain balance is zero. Emits a reconciliation_event row.
	ClosePositionForced(ctx context.Context, positionID string, reason string) error

	// InsertLatencyEvent records one execution latency sample.
	// Idempotent: duplicate rows are harmless (no PK check required).
	InsertLatencyEvent(ctx context.Context, le LatencyEvent) error

	// GetLatencyProfile aggregates latency samples over windowSec and returns
	// a LatencyProfileDTO with P50/P95 estimates.
	GetLatencyProfile(ctx context.Context, chain, endpoint, opKind string, windowSec int) (contracts.LatencyProfileDTO, error)

	// PromoteStrategyVersion atomically deactivates the current active version
	// and activates newVersionID. Blocks until DrainAndCheckPipelineIdle returns
	// true or drainTimeoutSec elapses (returns ErrDrainTimeout on timeout).
	PromoteStrategyVersion(ctx context.Context, newVersionID string, drainTimeoutSec int) error

	// DrainAndCheckPipelineIdle returns true when all in-flight events (claimed
	// but not processed) have completed within timeoutSec.
	DrainAndCheckPipelineIdle(ctx context.Context, timeoutSec int) (idle bool, err error)

	// SetSystemHalt sets or clears the global kill switch. All execution workers
	// MUST check IsSystemHalted before submitting any transaction.
	SetSystemHalt(ctx context.Context, halt bool, reason, operator string) error

	// IsSystemHalted returns the current kill switch state.
	IsSystemHalted(ctx context.Context) (halted bool, reason string, err error)

	// ComputeStateHash returns a deterministic hex digest over canonical pipeline
	// state (positions, executions, strategy_versions). Used for replay validation.
	ComputeStateHash(ctx context.Context) (hexDigest string, err error)

	// ── Phase 8: Stage 4 Hardening (architecture § 4.11) ────────────────────

	// ClaimPartitions acquires partition leases for the given worker.
	// Returns the list of partition keys granted. Idempotent on re-claim.
	ClaimPartitions(ctx context.Context, chain, consumer, workerID string, n, ttlSec int) ([]int, error)

	// RenewPartitions extends the expiry of all leases held by workerID.
	RenewPartitions(ctx context.Context, chain, consumer, workerID string) error

	// ReleasePartitions removes all leases held by workerID.
	ReleasePartitions(ctx context.Context, chain, consumer, workerID string) error

	// ListInFlightExecutions returns execution_attempts rows in reserved or
	// sent state for crash-safe recovery.
	ListInFlightExecutions(ctx context.Context) ([]InFlightExecution, error)

	// FinalizeExecution records the on-chain outcome (confirmed/reverted) for
	// an in-flight execution attempt.
	FinalizeExecution(ctx context.Context, executionID string, receipt ExecutionReceipt) error

	// AbortReservedExecution clears a reserved execution_attempt so the
	// execution_id can be retried by the next worker.
	AbortReservedExecution(ctx context.Context, executionID, reason string) error

	// MarkExecutionLost marks an execution as lost (tx never landed after
	// recovery_grace_sec). Does NOT book a loss — capital is returned.
	MarkExecutionLost(ctx context.Context, executionID, reason string) error

	// RecordReorg records a chain reorganization event.
	RecordReorg(ctx context.Context, chain string, oldBlock, newBlock int64, depth int) error

	// InvalidateBlockRange marks events in [fromBlock, toBlock] as invalidated
	// due to a reorg. Returns the number of affected rows.
	InvalidateBlockRange(ctx context.Context, chain string, fromBlock, toBlock int64) (affected int, err error)

	// MarkPositionsUncertain marks open positions whose execution confirmed in
	// a reorged block range as status='uncertain'.
	MarkPositionsUncertain(ctx context.Context, chain string, fromBlock int64, reason string) error

	// ReResolveExecutionAfterReorg updates confirmation_status after a reorg
	// resolution (re-included or dropped).
	ReResolveExecutionAfterReorg(ctx context.Context, executionID, txHash string, outcome ReorgOutcome) error

	// RecordExecutionForEvaluation registers an execution in the evaluation
	// invariant table. Called immediately after ClaimExecution succeeds.
	RecordExecutionForEvaluation(ctx context.Context, executionID string, deadlineSec int) error

	// MarkEvaluationDone marks the evaluation invariant as complete for an
	// execution. Called after the evaluation event is persisted.
	MarkEvaluationDone(ctx context.Context, executionID string) error

	// ListMissingEvaluations returns executions whose deadline has passed but
	// no evaluation event has been recorded.
	ListMissingEvaluations(ctx context.Context) ([]MissingEvaluation, error)

	// GetUnprocessedCount returns the number of unprocessed events for a
	// consumer + chain combination. Used for backpressure decisions.
	GetUnprocessedCount(ctx context.Context, chain, consumer string) (int64, error)

	// RecordDrop records a dropped ingestion event for observability.
	RecordDrop(ctx context.Context, chain, reason, tokenAddress, score string) error

	// GetPipelineStats returns token counts per lifecycle state for the last
	// windowHours and the most recent tokens that passed DQ validation.
	// Used by the /pipeline Telegram command.
	GetPipelineStats(ctx context.Context, windowHours int) (*PipelineStats, error)

	// GetAdaptiveDQStats returns the data-quality decision counters for the
	// past sinceSeconds: total decisions and rug/honeypot rejects. Used by
	// the adaptive risk-appetite controller (operational-modes skill) to
	// compute a real rug_rate, replacing the prior hardcoded 0.0 path.
	// Returns (0, 0, nil) when no decisions exist within the window.
	GetAdaptiveDQStats(ctx context.Context, sinceSeconds int) (totalDecisions int, rugRejects int, err error)

	// ── Baseline Persistence (residual risk #1) ──────────────────────────────

	// SaveBaseline writes/upserts the rolling-window ring buffer for a single
	// (module, market, signal) tuple. `values` is stored oldest-first.
	// Idempotent: ON CONFLICT (module, market, signal) DO UPDATE SET values, updated_at.
	// Best-effort from the caller's perspective — workers MUST log + continue
	// on error rather than abort processing.
	SaveBaseline(ctx context.Context, module, market, signal string, values []float64) error

	// LoadBaselines returns every persisted (market, signal) → values entry
	// for `module`. Used by the features and edge workers at startup to
	// rehydrate their in-memory ring buffers.
	// Returns an empty (non-nil) outer map when no rows exist.
	LoadBaselines(ctx context.Context, module string) (map[string]map[string][]float64, error)
}

// ── Domain Types ─────────────────────────────────────────────────────────────

// PipelineStats is a snapshot of token funnel counts over a time window.
// Returned by GetPipelineStats for the /pipeline Telegram command.
//
// Counts are CUMULATIVE — each value represents "tokens that reached AT LEAST
// this stage", not "tokens currently sitting in this state". This matches the
// operator's mental model of a funnel: SELECTED ≤ VALIDATED ≤ DQ_PASSED ≤ DETECTED.
// DETECTED equals the total token count in the window (all tokens were at
// DETECTED at some point). REJECTED and FAILED are raw (non-cumulative) terminal counts.
type PipelineStats struct {
	// Funnel counts (cumulative — see comment above).
	Detected       int64 // total tokens in window (all started at DETECTED)
	DQPassed       int64 // reached at least DQ_PASSED
	FeatureReady   int64 // reached at least FEATURE_READY
	EdgeDetected   int64 // reached at least EDGE_DETECTED
	Validated      int64 // reached at least VALIDATED
	Selected       int64 // reached at least SELECTED (incl. FAILED from SELECTED+)
	Executed       int64 // reached at least EXECUTED (incl. FAILED from EXECUTED+)
	PositionOpen   int64 // reached at least POSITION_OPEN (incl. FAILED from POSITION_OPEN+)
	PositionClosed int64 // reached POSITION_CLOSED or EVALUATED
	Evaluated      int64 // reached terminal EVALUATED state

	// Terminal counts (non-cumulative).
	Rejected int64 // terminal REJECTED (failed DQ or a validation gate)
	Failed   int64 // terminal FAILED (execution or position failure)

	// Failure breakdown — how many FAILED tokens failed at each stage.
	// Sums to Failed. Useful for diagnosing whether failures are execution
	// failures (FailedAtSelected, SELECTED→FAILED) vs position-open failures
	// (FailedAtExecuted, EXECUTED→FAILED) vs position-close failures
	// (FailedAtPositionOpen, POSITION_OPEN→FAILED).
	FailedAtSelected     int64 // failed during execution attempt (SELECTED→FAILED)
	FailedAtExecuted     int64 // failed during position-open (EXECUTED→FAILED)
	FailedAtPositionOpen int64 // failed during position-close (POSITION_OPEN→FAILED)

	// Recent is the last 10 tokens detected in the window, newest first.
	// Includes ticker/name when available (Solana tokens).
	Recent []RecentToken

	// WindowHours is the lookback window used for the query.
	WindowHours int
}

// RescanStats holds emission counts for the rescan worker over a time window.
// Returned by GetRescanStats (concrete method on the postgres engine, not part
// of the Adapter interface — callers type-assert to rescanQueryer when needed).
type RescanStats struct {
	TotalEmitted int64            // market_data rows with transport LIKE 'rescan_%' in window
	ByBand       map[string]int64 // band name (e.g. "15m") → count; nil when none
	WindowHours  int
}

// RecentToken is one row in PipelineStats.Recent.
type RecentToken struct {
	TokenAddress string
	Symbol       string // empty for EVM tokens
	Name         string // empty for EVM tokens
	State        string // current lifecycle state
	Chain        string
	DetectedAt   string // ISO 8601
}

// Config holds database connection parameters.
// Values are loaded from config/pipeline.yaml via the config package.
type Config struct {
	Engine               string // postgres
	Host                 string
	Port                 int
	Database             string
	User                 string
	Password             string
	SSLMode              string // disable | require | verify-ca | verify-full
	MaxOpenConns         int
	MaxIdleConns         int
	ConnMaxLifetimeSecs  int
	MigrationsDir        string // absolute path to database/migrations/
	ClaimTimeoutSecs     int    // seconds before a stale claim is eligible for re-claim (default 300)
	PartitionLeaseTTLSec int    // TTL for partition leases in seconds (default 60); used by RenewPartitions
}

// Event is the event bus row representation.
// See docs/db_adapter_spec.md § 2.
type Event struct {
	EventID       string // SHA256(payload_signature)[:16]
	EventType     string // e.g., "market_data_event"
	Payload       []byte // canonical JSON of the DTO
	TraceID       string
	CorrelationID string
	CausationID   *string // nil only for Layer 0 root events
	VersionID     string
	CreatedAt     string // ISO 8601
	Processed     bool
	Priority      int    // higher = processed first; default 0
	ExpiresAt     string // ISO 8601 UTC; "" = no expiry
	// Phase 8 routing fields — required for ClaimNextEvents partitioned workers.
	// Set to the source chain (e.g. "eth-uniswap-v2") and consuming worker group.
	Chain    string // source chain identifier; "" = unrouted (legacy)
	Consumer string // target consumer group; "" = unrouted (legacy)
	// LogicalOrderKey is a big-endian byte representation of created_at nanoseconds.
	// Auto-generated by InsertEvent if not set. Used for deterministic ordering.
	LogicalOrderKey []byte // big-endian int64 nanoseconds since epoch
	// PartitionKey pre-computed shard index for the dispatch index.
	// Auto-generated by InsertEvent as HASHTEXT(correlation_id) % total_partitions.
	// Zero is the default partition and is always valid.
	PartitionKey int
	// BlockNumber is the on-chain block in which this event was observed.
	// Zero means unknown. Used by InvalidateBlockRange for reorg filtering.
	BlockNumber int64
}

// TransitionRequest carries the CAS parameters for a state machine transition.
// See docs/implementation_roadmap.md § 0.7.
type TransitionRequest struct {
	LifecycleID       string
	ExpectedFromState string // current_state value at time of read
	ExpectedVersion   int64  // state_version value at time of read (CAS guard)
	NewState          string // target state (must be a valid forward transition)
	TraceID           string
	CorrelationID     string
	Reason            string
	ActorWorker       string // worker group name stored in audit row
}

// Lifecycle is the current state of a token's journey through the pipeline.
type Lifecycle struct {
	TokenLifecycleID string
	TokenAddress     string
	CurrentState     string
	StateVersion     int64
	TerminalReason   *string
	CreatedAt        string
	UpdatedAt        string
}

// StrategyVersion is an immutable snapshot of all tunable configuration.
// StrategyVersionID = SHA256(config_snapshot)[:16].
type StrategyVersion struct {
	StrategyVersionID string
	ConfigSnapshot    []byte  // canonical JSON of all config parameters
	CreatedAt         string  // ISO 8601
	ActivatedAt       *string // ISO 8601; nil if not yet activated
	DeactivatedAt     *string // ISO 8601; nil if still active

	// §8.7 additive: shadow/rollback lifecycle fields.
	// Status ∈ {"draft","shadow","active","deactivated","rolled_back"}.
	// Exactly one version has Status="active" at any time (partial unique index).
	Status          string  // "draft" | "shadow" | "active" | "deactivated" | "rolled_back"
	ShadowStartedAt *string // ISO 8601; nil if never in shadow
	PromotedAt      *string // ISO 8601; nil if never promoted to active
	RolledBackAt    *string // ISO 8601; nil if never rolled back
	ParentVersionID string  // previous active version ID for rollback target; "" if none
}

// PipelineRun tracks a per-market pipeline execution.
type PipelineRun struct {
	RunID              string
	TraceID            string
	Status             string  // started | processing | completed | partial | failed
	LastCompletedStage *string // nil if no stage has completed yet
	StrategyVersionID  string
	CreatedAt          string // ISO 8601
	UpdatedAt          string // ISO 8601
}

// ExposureSummary is the aggregated capital exposure snapshot.
// Returned by GetExposureSummary; computed via maintained aggregates (O(1)).
type ExposureSummary struct {
	TotalUsd      float64            // total USD value of open positions
	PerToken      map[string]float64 // tokenAddress → usd
	PerCohort     map[string]float64 // cohortID     → usd
	OpenPositions int32
}

// FillSample is one realized-vs-predicted slippage observation captured
// from execution_results × slippage_estimates × allocations. Consumed by
// the AlphaAggregator worker (residual risk #3 closure).
type FillSample struct {
	PredictedBps float64
	RealizedBps  float64
	At           time.Time
}

// ShadowTrade is an observation row tracking the price trajectory of a rejected
// token over a configurable window. Used to classify false negatives.
type ShadowTrade struct {
	ShadowID            string // SHA256(token_lifecycle_id||stage||rejected_at)[:16]
	TokenAddress        string
	Stage               string // data_quality|edge|validated_edge|selection
	RejectedAt          string // ISO 8601
	ObservationComplete bool
	ObservedReturnPct   float64
	Classification      string // TN | FN
	LearningRecordID    string // FK to learning_records.record_id
	VersionID           string
}

// CreatorRugObservation is a single confirmed-rug observation. Phase 11.
// Produced by the Learning Engine when a LearningRecordDTO is classified
// as a rug; consumed by Adapter.UpsertCreatorRugObservation.
type CreatorRugObservation struct {
	CreatorAddress    string // dev wallet (mint authority on Solana, deployer on EVM)
	Chain             string // e.g. "ethereum", "bsc", "solana-mainnet"
	TokenAddress      string // token from the rug observation; informational
	StrategyVersionID string // version active when the rug was confirmed
}

// CreatorBlacklistEntry is a row from the creator_blacklist table. Phase 11.
type CreatorBlacklistEntry struct {
	CreatorAddress    string
	Chain             string
	RugCount          int32
	FirstSeenAt       string // ISO 8601
	LastSeenAt        string // ISO 8601
	LastTokenAddress  string
	StrategyVersionID string
}

// ── Phase 7: Solana Domain Types ─────────────────────────────────────────────

// SolanaEndpointState is the circuit breaker state for a single RPC endpoint.
// States: closed (normal) → open (failing) → half_open (probing).
type SolanaEndpointState struct {
	EndpointURL         string
	State               string // closed | open | half_open
	ConsecutiveFailures int
	LastFailureAt       *string // ISO 8601; nil if no failures
	CircuitOpenedAt     *string // ISO 8601; nil if never opened
	UpdatedAt           string  // ISO 8601
}

// SolanaSignature tracks a submitted Solana transaction for idempotency and
// confirmation polling. One row per AllocationDTO.ExecutionID.
type SolanaSignature struct {
	ExecutionID string
	Signature   string
	Status      string // pending | confirmed | failed | expired
	Slot        int64  // confirmed slot; 0 if pending
	ErrMsg      string // non-empty if failed
	CreatedAt   string // ISO 8601
}

// SolanaEndpointHealth holds rolling health metrics for a Solana RPC endpoint.
type SolanaEndpointHealth struct {
	EndpointURL  string
	P95LatencyMs int32
	ErrorRate    float64
	SuccessCount int64
	FailureCount int64
	UpdatedAt    string // ISO 8601
}

// ── Phase 8 Domain Types ─────────────────────────────────────────────────────

// EventClaimQuery carries the parameters for ClaimNextEvents.
// Partition assignment: HASHTEXT(correlation_id) % NumWorkers == WorkerID.
// Events are returned sorted by logical_order_key ASC, event_id ASC — callers MUST NOT
// reorder them.
type EventClaimQuery struct {
	Chain          string   // required
	Consumer       string   // required
	EventTypes     []string // required; exact match on event_type
	WorkerID       int      // this worker's shard index (0-based)
	NumWorkers     int      // total shard count (must be > 0)
	Limit          int      // max rows to return (default 10 if 0)
	MinOrderingKey []byte   // resume cursor; nil = start from beginning
}

// DLQEntry is a row in the dead_letter_events table.
type DLQEntry struct {
	EventID       string // PK — matches events.event_id
	Chain         string
	Consumer      string
	Reason        string // "transient_exceeded"|"application_error"|"version_mismatch"|"determinism_violation"
	ErrorMessage  string
	RetryCount    int
	FirstFailedAt string // ISO 8601
	LastFailedAt  string // ISO 8601
	MovedToDLQAt  string // ISO 8601
	TraceID       string
	CorrelationID string
	CausationID   *string
	VersionID     string
	PayloadJSON   []byte // snapshot of the original event payload
}

// DLQFilter constrains a ListDLQ query.
type DLQFilter struct {
	Consumer string // optional; "" = all consumers
	Reason   string // optional; "" = all reasons
	Limit    int    // max rows; 0 = 100
}

// RescanQuery parameterises the GetTokensForRescan adapter method.
// All filters are applied server-side in a single parameterised SQL statement.
// See database/engines/postgres/rescan.go for the implementation.
type RescanQuery struct {
	Chain             string  // optional chain filter; "" = all chains
	MinAgeSeconds     int     // lower bound of age window (inclusive)
	MaxAgeSeconds     int     // upper bound of age window (exclusive)
	MaxHoneypotScore  float64 // latest DQ honeypot_score must be ≤ this
	MaxRugScore       float64 // latest DQ rug_score must be ≤ this
	MaxBuyTaxBps      int32   // latest DQ buy_tax_bps must be ≤ this
	IncludePassed     bool    // include decision IN ('PASS','RISKY_PASS') alongside REJECT
	SkipOpenPositions bool    // exclude tokens in POSITION_OPEN / EXECUTION_PENDING / etc.
	Limit             int     // max rows returned; 0 = 100
}

// LatencyEvent is one raw execution latency sample written to latency_events.
type LatencyEvent struct {
	ExecutionID             string
	Chain                   string
	Endpoint                string
	VersionID               string
	OpKind                  string // execute|confirm|rpc_call
	DecisionToSendMs        int
	SendToFirstObserveMs    int
	FirstObserveToConfirmMs int
	TotalMs                 int
	Outcome                 string // confirmed|reverted|dropped|timeout
	ObservedAt              string // ISO 8601
}

// InFlightExecution is an execution_attempt in reserved or sent state.
// Used by crash-safe recovery to detect and finalize stuck transactions.
type InFlightExecution struct {
	ExecutionID   string
	AttemptNumber int
	TxHash        *string
	Status        string // reserved|sent
	Nonce         *int64
	GasPriceWei   string
	SentAt        *string // ISO 8601; nil if not yet sent
	ObservedAt    string  // ISO 8601
}

// ExecutionReceipt carries the on-chain outcome of an execution attempt.
type ExecutionReceipt struct {
	TxHash      string
	BlockNumber int64
	Status      string // confirmed|reverted|lost
	ErrMsg      string // non-empty on revert/lost
}

// MissingEvaluation is returned by ListMissingEvaluations for executions
// whose evaluation deadline has passed without a corresponding evaluation event.
type MissingEvaluation struct {
	ExecutionID string
	DeadlineAt  string // ISO 8601
}

// ReorgOutcome is the resolution of a reorged execution.
type ReorgOutcome string

const (
	ReorgOutcomeReincluded ReorgOutcome = "re_included"    // tx landed in new chain
	ReorgOutcomeDropped    ReorgOutcome = "reorged_out"    // tx not in new chain
	ReorgOutcomeMutation   ReorgOutcome = "reorg_mutation" // tx modified by reorg
)

// ReconciliationPosition is an open position enriched with wallet_address.
// Returned by ListOpenPositionsForReconciliation for on-chain balance checks.
type ReconciliationPosition struct {
	PositionID    string
	TokenAddress  string
	Chain         string
	WalletAddress string // from execution_results.wallet_address
	ExecutionID   string
	AmountRaw     string // last known on-chain amount; empty if unknown
}
