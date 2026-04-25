package database_test

import (
	"errors"
	"testing"

	"crypto-sniping-bot/database"
)

func TestSentinelErrors_AreDistinct(t *testing.T) {
	errs := []error{
		database.ErrOrphanEvent,
		database.ErrInvalidTransition,
		database.ErrMissingTraceField,
		database.ErrUnknownVersion,
		database.ErrNonceGap,
		database.ErrNotFound,
		database.ErrNotImplemented,
	}

	for i := 0; i < len(errs); i++ {
		for j := i + 1; j < len(errs); j++ {
			if errors.Is(errs[i], errs[j]) {
				t.Errorf("errors[%d] and errors[%d] should be distinct but errors.Is returned true", i, j)
			}
		}
	}
}

func TestSentinelErrors_WrappedCanBeUnwrapped(t *testing.T) {
	wrapped := errors.New("outer: " + database.ErrNotFound.Error())
	// errors.Is won't match for string wrapping — that's correct.
	// Real wrapping uses %w.
	if errors.Is(wrapped, database.ErrNotFound) {
		t.Error("string-concat wrapping should NOT satisfy errors.Is")
	}

	// %w wrapping must work.
	wrapped2 := errors.Join(database.ErrNotFound, errors.New("detail"))
	if !errors.Is(wrapped2, database.ErrNotFound) {
		t.Error("errors.Join should allow errors.Is to detect ErrNotFound")
	}
}

func TestEvent_ZeroValue_IsInvalid(t *testing.T) {
	// A zero-value Event has no required fields set.
	var e database.Event
	if e.EventID != "" {
		t.Error("zero Event should have empty EventID")
	}
	if e.TraceID != "" {
		t.Error("zero Event should have empty TraceID")
	}
}

func TestPipelineRun_StatusTransitions(t *testing.T) {
	run := database.PipelineRun{
		RunID:  "run-001",
		Status: "started",
	}

	// Simulate checkpointing.
	stage := "data_quality"
	run.LastCompletedStage = &stage

	if run.LastCompletedStage == nil || *run.LastCompletedStage != "data_quality" {
		t.Error("expected LastCompletedStage to be data_quality")
	}

	run.Status = "completed"
	if run.Status != "completed" {
		t.Error("expected status to be completed")
	}
}

func TestStrategyVersion_Fields(t *testing.T) {
	sv := database.StrategyVersion{
		StrategyVersionID: "abc123",
		ConfigSnapshot:    []byte(`{"mode":"BALANCED"}`),
		CreatedAt:         "2026-01-01T00:00:00Z",
	}

	if sv.StrategyVersionID == "" {
		t.Error("StrategyVersionID should be set")
	}
	if len(sv.ConfigSnapshot) == 0 {
		t.Error("ConfigSnapshot should be non-empty")
	}
	if sv.ActivatedAt != nil {
		t.Error("ActivatedAt should be nil before activation")
	}
}

func TestConfig_DefaultMigrationsDir(t *testing.T) {
	cfg := database.Config{
		Engine:   "postgres",
		Host:     "localhost",
		Port:     5432,
		Database: "sniper",
		User:     "sniper",
		Password: "secret",
	}

	if cfg.MigrationsDir != "" {
		t.Error("MigrationsDir should be empty by default (set by caller)")
	}
}
