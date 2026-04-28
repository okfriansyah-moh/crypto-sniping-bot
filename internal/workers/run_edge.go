package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/app/config"
	"crypto-sniping-bot/internal/modules/edge"
)

// EdgeWorker implements Layer 3: Signal & Edge Discovery.
// Consumes: feature_event → emits: edge_event (only when edge is detected)
type EdgeWorker struct {
	adapter database.Adapter
	mod     *edge.Module
	logger  *slog.Logger
}

// NewEdgeWorker returns a new EdgeWorker.
func NewEdgeWorker(adapter database.Adapter, cfg *config.Config, logger *slog.Logger) *EdgeWorker {
	if logger == nil {
		logger = slog.Default()
	}
	return &EdgeWorker{
		adapter: adapter,
		mod:     edge.New(&cfg.Edge),
		logger:  logger,
	}
}

func (w *EdgeWorker) Process(ctx context.Context, evt *database.Event) (*database.Event, error) {
	var dto contracts.FeatureDTO
	if err := json.Unmarshal(evt.Payload, &dto); err != nil {
		return nil, fmt.Errorf("edge_worker: unmarshal: %w", err)
	}

	edgeDTO, err := w.mod.Process(ctx, dto)
	if err != nil {
		return nil, fmt.Errorf("edge_worker: module: %w", err)
	}

	w.logger.Info("edge_decision",
		"token", edgeDTO.TokenAddress,
		"edge_type", edgeDTO.EdgeType,
		"edge_strength", edgeDTO.EdgeStrength,
		"edge_confidence", edgeDTO.EdgeConfidence,
		"trace_id", edgeDTO.TraceID,
	)

	if err := w.adapter.InsertEdge(ctx, edgeDTO); err != nil {
		w.logger.Warn("edge_worker_persist_failed", "event_id", edgeDTO.EventID, "error", err)
	}

	nextState := "EDGE_DETECTED"
	if edgeDTO.EdgeType == "" {
		nextState = "REJECTED"
	}
	if err := doMandatoryTransition(ctx, w.adapter, dto.TokenLifecycleID, "FEATURE_READY", nextState, "", "edge_worker"); err != nil {
		return nil, fmt.Errorf("edge_worker: transition: %w", err)
	}

	if edgeDTO.EdgeType == "" {
		return nil, nil
	}

	return makeOutputEvent(
		edgeDTO.EventID, edgeDTO, "edge_event",
		evt.TraceID, evt.CorrelationID, evt.EventID, evt.VersionID,
	)
}
