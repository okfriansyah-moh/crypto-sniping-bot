// Package traceability validates that all pipeline DTOs carry the required
// trace fields before they are written to the event bus.
// The adapter enforces ErrMissingTraceField at write time (Phase 3+).
package traceability

import (
	"errors"
	"fmt"
	"reflect"
)

// TraceFields are the four required fields on every non-root event.
// CausationID may be empty only for Layer 0 root events (market_data_event
// emitted by the ingestion worker before any causation exists).
var TraceFields = []string{"TraceID", "CorrelationID", "VersionID"}

// ValidateTrace checks that the four trace fields are present on any DTO that
// has them. It uses reflection so it works on any struct type.
// Returns ErrMissingTraceField if any required field is empty.
// Returns nil for non-struct inputs (caller decides how to handle).
func ValidateTrace(dto any) error {
	if dto == nil {
		return fmt.Errorf("traceability: nil DTO")
	}
	v := reflect.ValueOf(dto)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return fmt.Errorf("traceability: nil pointer DTO")
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}

	t := v.Type()
	for _, fieldName := range TraceFields {
		f, ok := t.FieldByName(fieldName)
		if !ok {
			continue // DTO doesn't have this field — skip (some DTOs may be partial)
		}
		if f.Type.Kind() != reflect.String {
			continue
		}
		val := v.FieldByName(fieldName).String()
		if val == "" {
			return fmt.Errorf("traceability: %w: %s is empty", ErrMissingTraceField, fieldName)
		}
	}
	return nil
}

// ValidateTraceWithCausation checks all four fields including CausationID.
// Use this for non-root events where CausationID must also be populated.
func ValidateTraceWithCausation(dto any) error {
	if err := ValidateTrace(dto); err != nil {
		return err
	}
	if dto == nil {
		return nil
	}
	v := reflect.ValueOf(dto)
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	// Check CausationID separately (it can be a string or *string).
	t := v.Type()
	f, ok := t.FieldByName("CausationID")
	if !ok {
		return nil
	}
	fv := v.FieldByName("CausationID")
	switch f.Type.Kind() {
	case reflect.String:
		if fv.String() == "" {
			return fmt.Errorf("traceability: %w: CausationID is empty", ErrMissingTraceField)
		}
	case reflect.Ptr:
		if fv.IsNil() {
			return fmt.Errorf("traceability: %w: CausationID is nil", ErrMissingTraceField)
		}
		if fv.Elem().String() == "" {
			return fmt.Errorf("traceability: %w: CausationID is empty", ErrMissingTraceField)
		}
	}
	return nil
}

// ErrMissingTraceField is the sentinel error returned when a required trace
// field is absent. Matches database.ErrMissingTraceField.
var ErrMissingTraceField = errors.New("database: missing required trace field")
