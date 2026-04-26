package traceability

import (
	"errors"
	"testing"

	"crypto-sniping-bot/contracts"
)

func TestValidateTrace_ValidDTO(t *testing.T) {
	dto := contracts.EdgeDTO{
		TraceID:       "trace-1",
		CorrelationID: "corr-1",
		VersionID:     "v1",
	}
	if err := ValidateTrace(dto); err != nil {
		t.Errorf("unexpected error for valid DTO: %v", err)
	}
}

func TestValidateTrace_MissingTraceID(t *testing.T) {
	dto := contracts.EdgeDTO{
		CorrelationID: "corr-1",
		VersionID:     "v1",
	}
	err := ValidateTrace(dto)
	if err == nil {
		t.Error("expected error for missing TraceID")
	}
	if !errors.Is(err, ErrMissingTraceField) {
		t.Errorf("expected ErrMissingTraceField, got %v", err)
	}
}

func TestValidateTrace_MissingCorrelationID(t *testing.T) {
	dto := contracts.EdgeDTO{
		TraceID:   "trace-1",
		VersionID: "v1",
	}
	err := ValidateTrace(dto)
	if err == nil {
		t.Error("expected error for missing CorrelationID")
	}
	if !errors.Is(err, ErrMissingTraceField) {
		t.Errorf("expected ErrMissingTraceField, got %v", err)
	}
}

func TestValidateTrace_MissingVersionID(t *testing.T) {
	dto := contracts.EdgeDTO{
		TraceID:       "trace-1",
		CorrelationID: "corr-1",
	}
	err := ValidateTrace(dto)
	if err == nil {
		t.Error("expected error for missing VersionID")
	}
}

func TestValidateTrace_NilInput(t *testing.T) {
	err := ValidateTrace(nil)
	if err == nil {
		t.Error("expected error for nil input")
	}
}

func TestValidateTrace_NonStruct(t *testing.T) {
	// Non-struct types are silently accepted (caller's responsibility).
	err := ValidateTrace("hello")
	if err != nil {
		t.Errorf("expected nil for non-struct, got %v", err)
	}
}

func TestValidateTraceWithCausation_Valid(t *testing.T) {
	dto := contracts.EdgeDTO{
		TraceID:       "trace-1",
		CorrelationID: "corr-1",
		CausationID:   "cause-1",
		VersionID:     "v1",
	}
	if err := ValidateTraceWithCausation(dto); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateTraceWithCausation_MissingCausation(t *testing.T) {
	dto := contracts.EdgeDTO{
		TraceID:       "trace-1",
		CorrelationID: "corr-1",
		VersionID:     "v1",
	}
	err := ValidateTraceWithCausation(dto)
	if err == nil {
		t.Error("expected error for missing CausationID")
	}
	if !errors.Is(err, ErrMissingTraceField) {
		t.Errorf("expected ErrMissingTraceField, got %v", err)
	}
}

func TestValidateTrace_Pointer(t *testing.T) {
	dto := &contracts.EdgeDTO{
		TraceID:       "trace-1",
		CorrelationID: "corr-1",
		VersionID:     "v1",
	}
	if err := ValidateTrace(dto); err != nil {
		t.Errorf("unexpected error for pointer DTO: %v", err)
	}
}

func TestValidateTrace_NilPointer(t *testing.T) {
	var dto *contracts.EdgeDTO
	err := ValidateTrace(dto)
	if err == nil {
		t.Error("expected error for nil pointer DTO")
	}
}
