package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/execution"
)

// ExecutionWorker implements Layer 8: Execution Engine.
// Consumes: allocation_event → emits: execution_result_event
//
// Phase 2 simplification: if no EVMClient is provided (nil), the worker
// emits a Simulated execution result and skips actual on-chain submission.
// Phase 7: when alloc.Chain == "solana" and solanaExecutor != nil, the
// worker delegates directly to the Solana execution path via processSolanaAlloc.
type ExecutionWorker struct {
	adapter        database.Adapter
	evmClient      execution.EVMClient
	privKey        string
	chainID        int64
	router         string
	cfg            *config.Config
	logger         *slog.Logger
	circuitBreaker *execution.CircuitBreaker
	rpcEndpoint    string                   // canonical endpoint label for circuit-breaker tracking
	walletShards   []execution.WalletConfig // multi-wallet shards; nil means single-wallet mode
	solanaExec     execution.SolanaExecutor // optional; nil disables Solana execution path
}

// NewExecutionWorker returns a new ExecutionWorker.
// evmClient may be nil for testing / paper-trade mode.
// walletShards is the multi-wallet shard list; pass nil or empty to use single-wallet mode.
func NewExecutionWorker(
	adapter database.Adapter,
	cfg *config.Config,
	evmClient execution.EVMClient,
	privKey string,
	chainID int64,
	routerAddress string,
	walletShards []execution.WalletConfig,
	logger *slog.Logger,
) *ExecutionWorker {
	if logger == nil {
		logger = slog.Default()
	}

	// Initialise circuit breaker from config. Defaults are conservative:
	// open after 3 consecutive RPC errors, recover after 30s cooldown.
	cbFailureThreshold := 3
	cbCooldown := 30 * time.Second
	if cfg != nil && cfg.Execution.MaxRetry > 0 {
		cbFailureThreshold = cfg.Execution.MaxRetry
	}
	if cfg != nil && cfg.Execution.DropTimeoutMs > 0 {
		cbCooldown = time.Duration(cfg.Execution.DropTimeoutMs) * time.Millisecond
	}
	cb := execution.NewCircuitBreaker(cbFailureThreshold, cbCooldown)

	rpcEndpoint := "default"
	if cfg != nil && len(cfg.Execution.PrivateEndpoints) > 0 {
		// Sanitize before storing: private endpoint URLs may contain embedded API keys.
		rpcEndpoint = sanitizeURL(cfg.Execution.PrivateEndpoints[0])
	}

	return &ExecutionWorker{
		adapter:        adapter,
		evmClient:      evmClient,
		privKey:        privKey,
		chainID:        chainID,
		router:         routerAddress,
		cfg:            cfg,
		logger:         logger,
		circuitBreaker: cb,
		rpcEndpoint:    rpcEndpoint,
		walletShards:   walletShards,
	}
}

// WithSolanaExecutor wires in an optional Solana execution module.
// When set, allocations with Chain=="solana" are routed to this executor.
func (w *ExecutionWorker) WithSolanaExecutor(exec execution.SolanaExecutor) *ExecutionWorker {
	w.solanaExec = exec
	return w
}

func (w *ExecutionWorker) Process(ctx context.Context, evt *database.Event) (*database.Event, error) {
	var alloc contracts.AllocationDTO
	if err := json.Unmarshal(evt.Payload, &alloc); err != nil {
		return nil, fmt.Errorf("execution_worker: unmarshal: %w", err)
	}

	// Exactly-once execution gate (architecture.md § 4.10.D, certification § D+E).
	// ClaimExecution atomically reserves execution_id via INSERT ... ON CONFLICT DO NOTHING
	// BEFORE any nonce allocation or transaction submission. On a duplicate redelivery
	// of the same allocation event (e.g. claim_token expiry, MarkEventProcessed failure,
	// worker crash mid-flow), the second attempt observes claimed=false and returns
	// without re-broadcasting the transaction. This is the only barrier preventing
	// duplicate on-chain trades against real capital.
	claimed, claimErr := w.adapter.ClaimExecution(ctx, alloc)
	if claimErr != nil {
		return nil, fmt.Errorf("execution_worker: claim: %w", claimErr)
	}
	if !claimed {
		// Duplicate redelivery: side effects (InsertExecutionResult + lifecycle
		// transition) were already applied on the first delivery. We must NOT
		// re-broadcast the transaction or re-transition the lifecycle. However,
		// we MUST re-emit the same downstream execution_result_event so that
		// a crash window between InsertExecutionResult and outer InsertEvent
		// (worker.go) cannot leave the pipeline stuck with no position ever
		// opened. InsertEvent is idempotent via ON CONFLICT (event_id),
		// so re-emission is safe.
		prior, lookupErr := w.adapter.GetExecutionByLifecycle(ctx, alloc.TokenLifecycleID)
		if lookupErr != nil || prior == nil {
			w.logger.Warn("execution_worker_duplicate_lookup_failed",
				"execution_id", alloc.ExecutionID,
				"event_id", evt.EventID,
				"trace_id", evt.TraceID,
				"correlation_id", evt.CorrelationID,
				"token_lifecycle_id", alloc.TokenLifecycleID,
				"error", lookupErr,
			)
			// Return an error so the orchestrator retries or DLQ's this event
			// rather than calling MarkEventProcessed and permanently dropping it.
			return nil, fmt.Errorf("execution_worker: duplicate redelivery lookup failed for lifecycle %s: %w", alloc.TokenLifecycleID, lookupErr)
		}
		// A stale reservation row (status="reserved") means ClaimExecution succeeded
		// but InsertExecutionResult never ran. Its EventID equals the current allocation
		// event_id, so re-emitting it would collide in the event bus and never produce
		// an execution_result_event. Treat as a hard failure so the event is retried
		// or DLQ'd for manual recovery of the stale reservation.
		if prior.Status == "reserved" {
			w.logger.Warn("execution_worker_stale_reservation",
				"execution_id", alloc.ExecutionID,
				"event_id", evt.EventID,
				"trace_id", evt.TraceID,
				"correlation_id", evt.CorrelationID,
				"token_lifecycle_id", alloc.TokenLifecycleID,
			)
			return nil, fmt.Errorf("execution_worker: stale reserved execution for lifecycle %s — needs recovery before retry", alloc.TokenLifecycleID)
		}
		w.logger.Info("execution_worker_duplicate_reemit",
			"execution_id", alloc.ExecutionID,
			"event_id", evt.EventID,
			"prior_event_id", prior.EventID,
			"trace_id", evt.TraceID,
			"correlation_id", evt.CorrelationID,
			"token_lifecycle_id", alloc.TokenLifecycleID,
		)
		return makeOutputEvent(
			prior.EventID, *prior, "execution_result_event",
			evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
		)
	}

	// Kill-switch pre-check (Phase 6): only exit events pass through HALTED mode.
	// Allocation events are new entry events and must be blocked when HALTED.
	if state, stateErr := w.adapter.GetSystemState(ctx); stateErr == nil && state != nil && state.Mode == "HALTED" {
		w.logger.Info("execution_worker_halted",
			"mode", state.Mode,
			"event_id", evt.EventID,
			"token_lifecycle_id", alloc.TokenLifecycleID,
		)
		now := time.Now().UTC().Format(time.RFC3339Nano)
		result := haltedExecResult(alloc, now)
		if err := w.adapter.InsertExecutionResult(ctx, result); err != nil {
			w.logger.Warn("execution_worker_persist_failed", "event_id", result.EventID, "error", err)
		}
		if transErr := doMandatoryTransition(ctx, w.adapter, alloc.TokenLifecycleID, "SELECTED", "REJECTED", "system_halted", "execution_worker"); transErr != nil {
			return nil, fmt.Errorf("execution_worker: halted transition: %w", transErr)
		}
		return makeOutputEvent(
			result.EventID, result, "execution_result_event",
			evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
		)
	}

	// Apply MEV routing (Phase 6).
	mevRoute := "public"
	if w.cfg != nil {
		// Use a zero LatencyProfileDTO when none available — PickRoute will use size threshold only.
		mevRoute = execution.PickRoute(alloc, contracts.LatencyProfileDTO{}, w.cfg.MEV)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	var result contracts.ExecutionResultDTO

	if alloc.Rejected {
		result = rejectedExecResult(alloc, now)
	} else if alloc.Chain == "solana" {
		// Solana execution path (Phase 7).
		result = w.processSolanaAlloc(ctx, alloc, now)
	} else if w.evmClient == nil || w.privKey == "" {
		result = simulatedExecResult(alloc, now)
	} else {
		// Circuit-breaker pre-check: refuse to attempt execution if the RPC
		// endpoint has recorded too many consecutive failures.
		if !w.circuitBreaker.Allow(w.rpcEndpoint) {
			w.logger.Error("execution_worker_circuit_open",
				"endpoint", w.rpcEndpoint,
				"token_lifecycle_id", alloc.TokenLifecycleID,
			)
			// Back off before returning so the worker does not thrash the DB/RPC
			// in a tight retry loop while the circuit remains open.
			const circuitOpenBackoff = 500 * time.Millisecond
			timer := time.NewTimer(circuitOpenBackoff)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-timer.C:
			}
			return nil, fmt.Errorf("execution_worker: circuit open for endpoint %q: %w",
				w.rpcEndpoint, fmt.Errorf("rpc_endpoint_unavailable"))
		}

		// Wallet sharding: select the wallet deterministically by token address.
		// Falls back to the single privKey if no shards are configured.
		privKey := w.privKey
		if len(w.walletShards) > 0 {
			shard, pickErr := execution.PickWallet(alloc.TokenAddress, w.walletShards)
			if pickErr != nil {
				return nil, fmt.Errorf("execution_worker: pick wallet: %w", pickErr)
			}
			privKey = shard.PrivateKey
			alloc.WalletAddress = shard.Address
			alloc.WalletShard = int32(shard.ShardIndex)
		}

		nonce, nonceErr := w.adapter.AllocateNonce(ctx, alloc.WalletAddress, alloc.Chain)
		if nonceErr != nil {
			return nil, fmt.Errorf("execution_worker: allocate nonce: %w", nonceErr)
		}

		baseToken := chainBaseToken(w.cfg, alloc.Chain)
		mod, err := execution.New(&w.cfg.Capital, &w.cfg.Execution, w.evmClient, privKey, w.chainID, baseToken)
		if err != nil {
			return nil, fmt.Errorf("execution_worker: init module: %w", err)
		}

		var modErr error
		result, modErr = mod.Process(ctx, alloc, nonce, w.router)
		if modErr != nil {
			// Record RPC failure so the circuit breaker can open when threshold is exceeded.
			w.circuitBreaker.RecordFailure(w.rpcEndpoint)
			return nil, fmt.Errorf("execution_worker: module: %w", modErr)
		}
		// Successful execution: record success to keep or close circuit.
		w.circuitBreaker.RecordSuccess(w.rpcEndpoint)
	}

	// Override MEV routing fields on the result (Phase 6).
	// MempoolRoute uses the canonical DTO namespace: "public" | "private_flashbots" | "private_beaverbuild".
	// ExecutionPath holds the raw relay name (e.g., "flashbots", "beaverbuild", "eden") for routing/logging.
	result.MempoolRoute = mevRouteToNamespace(mevRoute)
	result.ExecutionPath = mevRoute
	if mevRoute != "public" {
		result.MEVProtected = true
	}

	if err := w.adapter.InsertExecutionResult(ctx, result); err != nil {
		w.logger.Warn("execution_worker_persist_failed", "event_id", result.EventID, "error", err)
	}

	nextState := "EXECUTED"
	if !result.Success {
		nextState = "FAILED"
	}
	if err := doMandatoryTransition(ctx, w.adapter, alloc.TokenLifecycleID, "SELECTED", nextState, result.ErrorCode, "execution_worker"); err != nil {
		return nil, fmt.Errorf("execution_worker: transition: %w", err)
	}

	return makeOutputEvent(
		result.EventID, result, "execution_result_event",
		evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
	)
}

// processSolanaAlloc delegates to the Solana execution path.
// If no SolanaExecutor is configured, falls back to simulated result.
// On real execution the submitted signature is persisted via InsertSolanaSignature
// so the solana_signatures table is the idempotency and observability record.
func (w *ExecutionWorker) processSolanaAlloc(ctx context.Context, alloc contracts.AllocationDTO, now string) contracts.ExecutionResultDTO {
	if w.solanaExec == nil {
		w.logger.Info("execution_worker_solana_simulated",
			"execution_id", alloc.ExecutionID,
			"reason", "no_solana_executor_configured",
		)
		return simulatedExecResult(alloc, now)
	}
	result, err := w.solanaExec.Execute(ctx, alloc, "", "")
	if err != nil {
		w.logger.Error("execution_worker_solana_failed",
			"execution_id", alloc.ExecutionID,
			"error", err,
		)
		// Persist a failed signature record for observability / dedup.
		sigRec := database.SolanaSignature{
			ExecutionID: alloc.ExecutionID,
			Signature:   "",
			Status:      "failed",
			ErrMsg:      err.Error(),
			CreatedAt:   now,
		}
		if insErr := w.adapter.InsertSolanaSignature(ctx, sigRec); insErr != nil {
			w.logger.Warn("execution_worker_solana_sig_persist_failed",
				"execution_id", alloc.ExecutionID,
				"error", insErr,
			)
		}
		// Return a failed result rather than propagating — consistent with EVM path.
		return contracts.ExecutionResultDTO{
			EventID:          contracts.ContentIDFromString("exec-sol-err:" + alloc.EventID),
			TraceID:          alloc.TraceID,
			CorrelationID:    alloc.CorrelationID,
			CausationID:      alloc.EventID,
			VersionID:        alloc.VersionID,
			TokenLifecycleID: alloc.TokenLifecycleID,
			ExecutionID:      alloc.ExecutionID,
			AllocationID:     alloc.EventID,
			Status:           "failed",
			Success:          false,
			ErrorCode:        "SOLANA_EXECUTION_FAILED",
			WalletAddress:    alloc.WalletAddress,
			CompletedAt:      now,
		}
	}

	// Persist the submitted signature for idempotency and confirmation tracking.
	sigStatus := "confirmed"
	if !result.Success {
		sigStatus = "failed"
	}
	sigRec := database.SolanaSignature{
		ExecutionID: alloc.ExecutionID,
		Signature:   result.TxHash,
		Status:      sigStatus,
		Slot:        int64(result.BlockNumber),
		CreatedAt:   now,
	}
	if insErr := w.adapter.InsertSolanaSignature(ctx, sigRec); insErr != nil {
		w.logger.Warn("execution_worker_solana_sig_persist_failed",
			"execution_id", alloc.ExecutionID,
			"error", insErr,
		)
	}
	return result
}

func simulatedExecResult(alloc contracts.AllocationDTO, now string) contracts.ExecutionResultDTO {
	eventID := contracts.ContentIDFromString(fmt.Sprintf("exec-sim:%s", alloc.EventID))
	return contracts.ExecutionResultDTO{
		EventID:       eventID,
		TraceID:       alloc.TraceID,
		CorrelationID: alloc.CorrelationID,
		CausationID:   alloc.EventID,
		VersionID:     alloc.VersionID,

		TokenLifecycleID: alloc.TokenLifecycleID,
		ExecutionID:      alloc.ExecutionID,
		AllocationID:     alloc.EventID,

		Status:        "confirmed",
		Success:       true,
		Simulated:     true,
		MempoolRoute:  "public",
		WalletAddress: alloc.WalletAddress,
		CompletedAt:   now,
	}
}

func rejectedExecResult(alloc contracts.AllocationDTO, now string) contracts.ExecutionResultDTO {
	eventID := contracts.ContentIDFromString(fmt.Sprintf("exec-rej:%s", alloc.EventID))
	return contracts.ExecutionResultDTO{
		EventID:       eventID,
		TraceID:       alloc.TraceID,
		CorrelationID: alloc.CorrelationID,
		CausationID:   alloc.EventID,
		VersionID:     alloc.VersionID,

		TokenLifecycleID: alloc.TokenLifecycleID,
		ExecutionID:      alloc.ExecutionID,
		AllocationID:     alloc.EventID,

		Status:          "rejected",
		Success:         false,
		RejectionReason: alloc.RejectReason,
		MempoolRoute:    "public",
		WalletAddress:   alloc.WalletAddress,
		CompletedAt:     now,
	}
}

// haltedExecResult creates a rejected execution result for events dropped due to HALTED mode.
func haltedExecResult(alloc contracts.AllocationDTO, now string) contracts.ExecutionResultDTO {
	eventID := contracts.ContentIDFromString(fmt.Sprintf("exec-halt:%s", alloc.EventID))
	return contracts.ExecutionResultDTO{
		EventID:       eventID,
		TraceID:       alloc.TraceID,
		CorrelationID: alloc.CorrelationID,
		CausationID:   alloc.EventID,
		VersionID:     alloc.VersionID,

		TokenLifecycleID: alloc.TokenLifecycleID,
		ExecutionID:      alloc.ExecutionID,
		AllocationID:     alloc.EventID,

		Status:          "rejected",
		Success:         false,
		RejectionReason: "system_halted",
		MempoolRoute:    "public",
		WalletAddress:   alloc.WalletAddress,
		CompletedAt:     now,
	}
}

// mevRouteToNamespace maps raw relay names returned by PickRoute to the canonical
// MempoolRoute namespace defined in ExecutionResultDTO:
//
//	"public"      → "public"
//	"flashbots"   → "private_flashbots"
//	"beaverbuild" → "private_beaverbuild"
//	"eden"        → "private_flashbots"  (eden uses Flashbots-compatible relay semantics)
//	<unknown>     → "public"
func mevRouteToNamespace(relayName string) string {
	switch relayName {
	case "flashbots", "eden":
		return "private_flashbots"
	case "beaverbuild":
		return "private_beaverbuild"
	default:
		return "public"
	}
}
