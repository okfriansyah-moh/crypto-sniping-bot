package orchestrator

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/internal/app/config"
)

// Orchestrator is the ONLY component that:
//   - Calls modules (modules never call each other)
//   - Manages execution order (pipeline stage sequence)
//   - Performs checkpointing (writes last_completed_stage after each stage)
//   - Writes to the database (via database.Adapter)
//   - Routes DTOs between modules
//   - Handles failures (decides retry, skip, or abort)
//
// See docs/reference/orchestrator_spec.md for the full execution model.
type Orchestrator struct {
	db        database.Adapter
	cfg       *config.Config
	logger    *slog.Logger
	registry  *Registry
	versionID string
}

// Boot initializes the orchestrator:
//  1. Applies pending migrations.
//  2. Pins the active StrategyVersion from the current config snapshot.
//  3. Returns a ready-to-run Orchestrator.
func Boot(ctx context.Context, adapter database.Adapter, cfg *config.Config, logger *slog.Logger) (*Orchestrator, error) {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	// Step 1: Run migrations.
	if err := adapter.RunMigrations(ctx); err != nil {
		return nil, fmt.Errorf("orchestrator boot: run migrations: %w", err)
	}
	logger.Info("migrations_applied")

	// Step 2: Pin StrategyVersion from config snapshot.
	snapshot, err := cfg.Snapshot()
	if err != nil {
		return nil, fmt.Errorf("orchestrator boot: config snapshot: %w", err)
	}

	versionID, err := pinStrategyVersion(ctx, adapter, snapshot, logger)
	if err != nil {
		return nil, fmt.Errorf("orchestrator boot: pin strategy version: %w", err)
	}
	logger.Info("strategy_version_pinned", "version_id", versionID)

	return &Orchestrator{
		db:        adapter,
		cfg:       cfg,
		logger:    logger,
		registry:  NewRegistry(),
		versionID: versionID,
	}, nil
}

// RegisterStage adds a stage handler for a worker group and its event types.
func (o *Orchestrator) RegisterStage(group string, handler StageHandler, eventTypes ...string) {
	o.registry.Register(group, handler, eventTypes...)
}

// Run starts all registered workers and blocks until ctx is cancelled.
// Each registered event type gets its own goroutine.
func (o *Orchestrator) Run(ctx context.Context) error {
	if o.registry.Empty() {
		o.logger.Info("no_stages_registered")
		<-ctx.Done()
		return ctx.Err()
	}

	errCh := make(chan error, o.registry.Len())

	for _, entry := range o.registry.Entries() {
		entry := entry // capture loop variable
		go func() {
			idleBackoff := time.Duration(o.cfg.Worker.IdleBackoffMs) * time.Millisecond
			if idleBackoff == 0 {
				idleBackoff = 100 * time.Millisecond
			}
			err := RunWorker(ctx, o.db, entry.Group, entry.EventTypes, entry.Handler, idleBackoff, o.logger, o.cfg.Worker.MaxRetryCount)
			if err != nil && err != ctx.Err() {
				errCh <- fmt.Errorf("worker %s: %w", entry.Group, err)
			}
		}()
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}

// VersionID returns the active StrategyVersionID pinned at boot.
func (o *Orchestrator) VersionID() string {
	return o.versionID
}

// pinStrategyVersion creates and activates a StrategyVersion from the config snapshot.
// Returns the active StrategyVersionID.
func pinStrategyVersion(ctx context.Context, adapter database.Adapter, snapshot []byte, logger *slog.Logger) (string, error) {
	versionID := contracts.ContentID(snapshot)

	now := time.Now().UTC().Format(time.RFC3339Nano)
	sv := database.StrategyVersion{
		StrategyVersionID: versionID,
		ConfigSnapshot:    snapshot,
		CreatedAt:         now,
	}

	if err := adapter.CreateStrategyVersion(ctx, sv); err != nil {
		return "", fmt.Errorf("create strategy version: %w", err)
	}

	if err := adapter.ActivateStrategyVersion(ctx, versionID); err != nil {
		return "", fmt.Errorf("activate strategy version: %w", err)
	}

	// Verify the pin.
	active, err := adapter.GetActiveStrategyVersion(ctx)
	if err != nil {
		logger.Warn("strategy_version_verify_failed", "error", err)
		return versionID, nil
	}
	if active.StrategyVersionID != versionID {
		logger.Warn("strategy_version_mismatch",
			"pinned", versionID,
			"active", active.StrategyVersionID,
		)
	}

	return versionID, nil
}

// createPipelineRun creates a run record and checkpoints it.
func (o *Orchestrator) createPipelineRun(ctx context.Context, traceID string) (database.PipelineRun, error) {
	run := database.PipelineRun{
		RunID:             contracts.ContentIDFromString(traceID + o.versionID),
		TraceID:           traceID,
		Status:            "started",
		StrategyVersionID: o.versionID,
		CreatedAt:         time.Now().UTC().Format(time.RFC3339Nano),
		UpdatedAt:         time.Now().UTC().Format(time.RFC3339Nano),
	}

	if err := o.db.CreateRun(ctx, run); err != nil {
		return database.PipelineRun{}, fmt.Errorf("create pipeline run: %w", err)
	}
	return run, nil
}
