package orchestrator

import (
	"fmt"
	"log/slog"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
)

// Orchestrator is the ONLY component that:
//   - Calls modules (modules never call each other)
//   - Manages execution order (pipeline stage sequence)
//   - Performs checkpointing (writes last_completed_stage after each stage)
//   - Writes to the database (via database.Adapter)
//   - Routes DTOs between modules
//   - Handles failures (decides retry, skip, or abort)
type Orchestrator struct {
	db     database.Adapter
	logger *slog.Logger
}

// New creates a new pipeline orchestrator.
func New(db database.Adapter, logger *slog.Logger) *Orchestrator {
	return &Orchestrator{db: db, logger: logger}
}

// Run executes the full pipeline for a given run.
// Resumes from last successful checkpoint on restart.
func (o *Orchestrator) Run(run contracts.PipelineRun) error {
	o.logger.Info("pipeline_started", "run_id", run.RunID, "input_path", run.InputPath)

	// TODO: Add pipeline stages as they are implemented.
	// Each stage follows this pattern:
	//
	//   if run.LastCompletedStage < "stage_name" {
	//       result, err := module.Process(input)
	//       if err != nil { return fmt.Errorf("stage_name: %w", err) }
	//       if err := o.db.UpsertEntityResult(result); err != nil { return err }
	//       run.LastCompletedStage = "stage_name"
	//       if err := o.db.UpdatePipelineRun(run); err != nil { return err }
	//       o.logger.Info("stage_completed", "stage", "stage_name", "run_id", run.RunID)
	//   }

	run.Status = "completed"
	if err := o.db.UpdatePipelineRun(run); err != nil {
		return fmt.Errorf("finalize run: %w", err)
	}

	o.logger.Info("pipeline_completed", "run_id", run.RunID)
	return nil
}
