package traceability

// causation_test.go — additional coverage for ValidateTraceWithCausation paths
// not exercised by validator_test.go: pointer CausationID, struct without
// CausationID field, non-struct input, and nil-after-pass guard.

import (
	"errors"
	"testing"
)

// dtoWithPtrCausation has a *string CausationID so we can test the pointer branch.
type dtoWithPtrCausation struct {
	TraceID       string
	CorrelationID string
	VersionID     string
	CausationID   *string
}

// dtoWithoutCausation has none of the optional CausationID field.
type dtoWithoutCausation struct {
	TraceID       string
	CorrelationID string
	VersionID     string
}

// ── ValidateTraceWithCausation — pointer CausationID ─────────────────────────

func TestValidateTraceWithCausation_PtrCausation_Nil_ReturnsError(t *testing.T) {
	t.Parallel()
	dto := dtoWithPtrCausation{
		TraceID:       "trace-1",
		CorrelationID: "corr-1",
		VersionID:     "v1",
		CausationID:   nil, // nil pointer → must fail
	}
	err := ValidateTraceWithCausation(dto)
	if err == nil {
		t.Fatal("expected error for nil *string CausationID, got nil")
	}
	if !errors.Is(err, ErrMissingTraceField) {
		t.Errorf("expected ErrMissingTraceField, got %v", err)
	}
}

func TestValidateTraceWithCausation_PtrCausation_EmptyString_ReturnsError(t *testing.T) {
	t.Parallel()
	empty := ""
	dto := dtoWithPtrCausation{
		TraceID:       "trace-1",
		CorrelationID: "corr-1",
		VersionID:     "v1",
		CausationID:   &empty, // non-nil pointer but empty string → must fail
	}
	err := ValidateTraceWithCausation(dto)
	if err == nil {
		t.Fatal("expected error for empty *string CausationID, got nil")
	}
	if !errors.Is(err, ErrMissingTraceField) {
		t.Errorf("expected ErrMissingTraceField, got %v", err)
	}
}

func TestValidateTraceWithCausation_PtrCausation_Valid_ReturnsNil(t *testing.T) {
	t.Parallel()
	cause := "cause-abc"
	dto := dtoWithPtrCausation{
		TraceID:       "trace-1",
		CorrelationID: "corr-1",
		VersionID:     "v1",
		CausationID:   &cause,
	}
	if err := ValidateTraceWithCausation(dto); err != nil {
		t.Errorf("unexpected error for valid *string CausationID: %v", err)
	}
}

// ── ValidateTraceWithCausation — struct without CausationID field ─────────────

func TestValidateTraceWithCausation_NoCausationField_Valid_ReturnsNil(t *testing.T) {
	t.Parallel()
	// DTOs without CausationID must pass — root events legitimately lack it.
	dto := dtoWithoutCausation{
		TraceID:       "trace-2",
		CorrelationID: "corr-2",
		VersionID:     "v2",
	}
	if err := ValidateTraceWithCausation(dto); err != nil {
		t.Errorf("unexpected error for DTO without CausationID field: %v", err)
	}
}

// ── ValidateTraceWithCausation — non-struct and nil inputs ────────────────────

func TestValidateTraceWithCausation_NonStruct_ReturnsNil(t *testing.T) {
	t.Parallel()
	// Non-struct types pass silently (same as ValidateTrace).
	if err := ValidateTraceWithCausation("a plain string"); err != nil {
		t.Errorf("expected nil for non-struct, got %v", err)
	}
}

func TestValidateTraceWithCausation_Nil_ReturnsError(t *testing.T) {
	t.Parallel()
	// Nil input propagates through ValidateTrace which returns an error first.
	if err := ValidateTraceWithCausation(nil); err == nil {
		t.Fatal("expected error for nil input, got nil")
	}
}

// ── ValidateTraceWithCausation — base fields missing (delegates to ValidateTrace) ──

func TestValidateTraceWithCausation_MissingBaseFields_ReturnsError(t *testing.T) {
	t.Parallel()
	cause := "cause-xyz"
	dto := dtoWithPtrCausation{
		// TraceID deliberately empty
		CorrelationID: "corr-1",
		VersionID:     "v1",
		CausationID:   &cause,
	}
	err := ValidateTraceWithCausation(dto)
	if err == nil {
		t.Fatal("expected error for missing TraceID, got nil")
	}
	if !errors.Is(err, ErrMissingTraceField) {
		t.Errorf("expected ErrMissingTraceField, got %v", err)
	}
}
