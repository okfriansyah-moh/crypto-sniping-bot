package contracts

// contracts — Immutable DTO definitions.
// All inter-module communication flows through these types.
// DTOs are value objects — no methods that mutate state.
// This package is the ONLY coupling between modules.

// PipelineRun represents the state of a pipeline execution.
type PipelineRun struct {
	RunID              string `json:"run_id"`
	InputPath          string `json:"input_path"`
	Status             string `json:"status"`
	LastCompletedStage string `json:"last_completed_stage"`
	ConfigHash         string `json:"config_hash"`
}

// EntityResult represents a processed entity.
type EntityResult struct {
	EntityID string  `json:"entity_id"`
	Name     string  `json:"name"`
	Status   string  `json:"status"`
	Score    float64 `json:"score"`
}
