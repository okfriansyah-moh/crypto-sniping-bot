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
type ExecutionWorker struct {
	adapter   database.Adapter
	evmClient execution.EVMClient
	privKey   string
	chainID   int64
	router    string
	cfg       *config.Config
	logger    *slog.Logger
}

// NewExecutionWorker returns a new ExecutionWorker.
// evmClient may be nil for testing / paper-trade mode.
func NewExecutionWorker(
	adapter database.Adapter,
	cfg *config.Config,
	evmClient execution.EVMClient,
	privKey string,
	chainID int64,
	routerAddress string,
	logger *slog.Logger,
) *ExecutionWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &ExecutionWorker{
		adapter:   adapter,
		evmClient: evmClient,
		privKey:   privKey,
		chainID:   chainID,
		router:    routerAddress,
		cfg:       cfg,
		logger:    logger,
	}
}

func (w *ExecutionWorker) Process(ctx context.Context, evt *database.Event) (*database.Event, error) {
	var alloc contracts.AllocationDTO
	if err := json.Unmarshal(evt.Payload, &alloc); err != nil {
		return nil, fmt.Errorf("execution_worker: unmarshal: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	var result contracts.ExecutionResultDTO

	if alloc.Rejected {
		result = rejectedExecResult(alloc, now)
	} else if w.evmClient == nil || w.privKey == "" {
		result = simulatedExecResult(alloc, now)
	} else {
		nonce, nonceErr := w.adapter.AllocateNonce(ctx, alloc.WalletAddress, alloc.Chain)
		if nonceErr != nil {
			return nil, fmt.Errorf("execution_worker: allocate nonce: %w", nonceErr)
		}

		baseToken := chainBaseToken(w.cfg, alloc.Chain)
		mod, err := execution.New(&w.cfg.Capital, w.evmClient, w.privKey, w.chainID, baseToken)
		if err != nil {
			return nil, fmt.Errorf("execution_worker: init module: %w", err)
		}

		var modErr error
		result, modErr = mod.Process(ctx, alloc, nonce, w.router)
		if modErr != nil {
			return nil, fmt.Errorf("execution_worker: module: %w", modErr)
		}
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

		Status:        "simulated",
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
