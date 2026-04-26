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

	// GetShadowTradesByWindow returns shadow trades created between start and end.
	// Used by the evaluation engine to compute FalseNegative candidates.
	GetShadowTradesByWindow(ctx context.Context, start, end string) ([]ShadowTrade, error)

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

	// GetLatestLatencyProfile returns the most recent latency profile for the
	// given chain, or nil if no profile has been recorded.
	GetLatestLatencyProfile(ctx context.Context, chain string) (*contracts.LatencyProfileDTO, error)

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
}

// ── Domain Types ─────────────────────────────────────────────────────────────

// Config holds database connection parameters.
// Values are loaded from config/pipeline.yaml via the config package.
type Config struct {
	Engine              string // postgres
	Host                string
	Port                int
	Database            string
	User                string
	Password            string
	SSLMode             string // disable | require | verify-ca | verify-full
	MaxOpenConns        int
	MaxIdleConns        int
	ConnMaxLifetimeSecs int
	MigrationsDir       string // absolute path to database/migrations/
	ClaimTimeoutSecs    int    // seconds before a stale claim is eligible for re-claim (default 300)
}

// Event is the event bus row representation.
// See docs/db_adapter_spec.md § 2.
type Event struct {
	EventID       string  // SHA256(payload_signature)[:16]
	EventType     string  // e.g., "market_data_event"
	Payload       []byte  // canonical JSON of the DTO
	TraceID       string
	CorrelationID string
	CausationID   *string // nil only for Layer 0 root events
	VersionID     string
	CreatedAt     string  // ISO 8601
	Processed     bool
	Priority      int     // higher = processed first; default 0
	ExpiresAt     string  // ISO 8601 UTC; "" = no expiry
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

// ShadowTrade is a rejected token that was observed to pump after rejection.
// Used by the evaluation engine to compute FalseNegative candidates.
type ShadowTrade struct {
	ShadowTradeID  string
	TokenAddress   string
	Chain          string
	TraceID        string
	CorrelationID  string
	VersionID      string
	RejectReason   string
	RejectedAt     string
	PeakGainPct    float64
	ObservedAt     string
	IsFNCandidate  bool
	CreatedAt      string
}
