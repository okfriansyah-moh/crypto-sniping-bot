package database

import "crypto-sniping-bot/contracts"

// Adapter is the single database access entry point.
// All modules MUST use this interface — no direct driver imports in modules.
// Only the orchestrator calls the adapter; modules never touch the database.
type Adapter interface {
	// PipelineRun persistence
	CreatePipelineRun(run contracts.PipelineRun) error
	GetPipelineRun(runID string) (contracts.PipelineRun, error)
	UpdatePipelineRun(run contracts.PipelineRun) error

	// EntityResult persistence
	UpsertEntityResult(result contracts.EntityResult) error
	GetEntityResult(entityID string) (contracts.EntityResult, error)
	ListEntityResults(runID string) ([]contracts.EntityResult, error)

	// Lifecycle
	Close() error
	Migrate() error
}
