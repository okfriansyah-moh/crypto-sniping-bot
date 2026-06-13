package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/selection"
)

// SelectionWorker implements Layer 6: Selection Engine.
// Consumes: validated_edge_event → emits: selection_event (Selected only)
type SelectionWorker struct {
	adapter database.Adapter
	mod     *selection.Module
	cfg     *config.Config
	logger  *slog.Logger
	now     func() time.Time

	mu      sync.Mutex
	batches map[string]*selectionChainBatch
}

type selectionChainBatch struct {
	items []*pendingSelection
	timer *time.Timer
}

type pendingSelection struct {
	evt     *database.Event
	dto     contracts.ValidatedEdgeDTO
	chain   string
	creator string
}

// NewSelectionWorker returns a new SelectionWorker.
func NewSelectionWorker(adapter database.Adapter, cfg *config.Config, logger *slog.Logger) *SelectionWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &SelectionWorker{
		adapter: adapter,
		mod:     selection.New(&cfg.Selection),
		cfg:     cfg,
		logger:  logger,
		now:     time.Now,
		batches: make(map[string]*selectionChainBatch),
	}
}

func (w *SelectionWorker) Process(ctx context.Context, evt *database.Event) (*database.Event, error) {
	var dto contracts.ValidatedEdgeDTO
	if err := json.Unmarshal(evt.Payload, &dto); err != nil {
		return nil, fmt.Errorf("selection_worker: unmarshal: %w", err)
	}

	state, stateErr := w.adapter.GetSystemState(ctx)
	if stateErr == nil && state != nil && state.Mode == "HALTED" {
		w.logger.Info("selection_worker_halted",
			"mode", state.Mode,
			"event_id", evt.EventID,
			"token_lifecycle_id", dto.TokenLifecycleID,
		)
		if transErr := doMandatoryTransition(ctx, w.adapter, dto.TokenLifecycleID, "VALIDATED", "REJECTED", "system_halted", "selection_worker"); transErr != nil {
			return nil, fmt.Errorf("selection_worker: halted transition: %w", transErr)
		}
		return nil, nil
	}

	chain := chainFromCorrelation(ctx, w.adapter, evt.CorrelationID, w.logger)
	creator := creatorFromCorrelation(ctx, w.adapter, evt.CorrelationID, w.logger)
	item := &pendingSelection{
		evt:     evt,
		dto:     dto,
		chain:   chain,
		creator: creator,
	}

	window := w.batchWindow()
	if window <= 0 {
		return w.flushItems(ctx, chain, []*pendingSelection{item}, evt.EventID)
	}

	w.mu.Lock()
	batch := w.batches[chain]
	if batch == nil {
		batch = &selectionChainBatch{}
		w.batches[chain] = batch
	}
	batch.items = append(batch.items, item)

	if len(batch.items) >= 2 {
		if batch.timer != nil {
			batch.timer.Stop()
			batch.timer = nil
		}
		items := batch.items
		batch.items = nil
		w.mu.Unlock()
		return w.flushItems(ctx, chain, items, evt.EventID)
	}

	if batch.timer != nil {
		batch.timer.Stop()
	}
	triggerID := evt.EventID
	batch.timer = time.AfterFunc(window, func() {
		w.mu.Lock()
		items := batch.items
		batch.items = nil
		batch.timer = nil
		w.mu.Unlock()
		if len(items) == 0 {
			return
		}
		outEvt, err := w.flushItems(context.Background(), chain, items, triggerID)
		if err != nil {
			w.logger.Warn("selection_batch_flush_failed", "chain", chain, "error", err)
			return
		}
		if outEvt != nil {
			if insertErr := w.adapter.InsertEvent(context.Background(), *outEvt); insertErr != nil {
				w.logger.Warn("selection_batch_emit_failed", "chain", chain, "event_id", outEvt.EventID, "error", insertErr)
			}
		}
	})
	w.mu.Unlock()

	return nil, nil
}

func (w *SelectionWorker) batchWindow() time.Duration {
	if w.cfg == nil || w.cfg.Selection.BatchWindowMs <= 0 {
		return 0
	}
	return time.Duration(w.cfg.Selection.BatchWindowMs) * time.Millisecond
}

func (w *SelectionWorker) flushItems(
	ctx context.Context,
	chain string,
	items []*pendingSelection,
	returnEventID string,
) (*database.Event, error) {
	if len(items) == 0 {
		return nil, nil
	}

	openPositions, err := w.adapter.GetOpenPositions(ctx)
	if err != nil {
		w.logger.Warn("selection_worker_open_positions_failed", "error", err)
		openPositions = nil
	}

	chainOpenCount := 0
	openByCreator := make(map[string]int32)
	for _, p := range openPositions {
		if p.Chain == chain {
			chainOpenCount++
		}
	}

	thresholds := w.resolveModeThresholds(ctx)
	batchItems := make([]selection.BatchItem, len(items))
	for i, item := range items {
		batchItems[i] = selection.BatchItem{
			Edge:           item.dto,
			CreatorAddress: item.creator,
		}
	}

	outputs, err := w.mod.ProcessBatch(ctx, batchItems, chainOpenCount, thresholds, openByCreator)
	if err != nil {
		return nil, fmt.Errorf("selection_worker: module: %w", err)
	}

	var returnEvt *database.Event
	for i, item := range items {
		selDTO := outputs[i]
		w.logger.Info("selection_decision",
			"token", selDTO.TokenAddress,
			"selected", selDTO.Selected,
			"rank", selDTO.Rank,
			"combined_score", selDTO.CombinedScore,
			"is_exploration", selDTO.IsExploration,
			"edge_strength_min_mode", thresholds.Mode,
			"max_positions", thresholds.MaxPositions,
			"reject_reason", selDTO.RejectReason,
			"trace_id", selDTO.TraceID,
			"version_id", selDTO.VersionID,
		)

		if err := w.adapter.InsertSelection(ctx, selDTO); err != nil {
			w.logger.Warn("selection_worker_persist_failed", "event_id", selDTO.EventID, "error", err)
		}

		nextState := "SELECTED"
		if !selDTO.Selected {
			nextState = "REJECTED"
		}
		if err := doMandatoryTransition(ctx, w.adapter, item.dto.TokenLifecycleID, "VALIDATED", nextState, selDTO.RejectReason, "selection_worker"); err != nil {
			return nil, fmt.Errorf("selection_worker: transition: %w", err)
		}

		if !selDTO.Selected {
			continue
		}

		outEvt, mkErr := makeOutputEvent(
			selDTO.EventID, selDTO, "selection_event",
			item.evt.TraceID, item.evt.CorrelationID, item.evt.EventID, item.evt.VersionID,
		)
		if mkErr != nil {
			return nil, fmt.Errorf("selection_worker: make output: %w", mkErr)
		}
		if item.evt.EventID == returnEventID {
			returnEvt = outEvt
			continue
		}
		if err := w.adapter.InsertEvent(ctx, *outEvt); err != nil {
			w.logger.Warn("selection_worker_emit_failed", "event_id", selDTO.EventID, "error", err)
		}
	}

	return returnEvt, nil
}

func (w *SelectionWorker) resolveModeThresholds(ctx context.Context) config.ModeThresholds {
	sysMode := "balanced"
	if w.cfg != nil && w.cfg.Priority.ActiveMode != "" {
		sysMode = w.cfg.Priority.ActiveMode
	}
	if state, err := w.adapter.GetSystemState(ctx); err == nil && state != nil && state.Mode != "" {
		sysMode = state.Mode
	}
	if w.cfg == nil {
		return config.ModeThresholds{MaxPositions: 1}
	}
	return w.cfg.ResolveModeThresholds(sysMode)
}
