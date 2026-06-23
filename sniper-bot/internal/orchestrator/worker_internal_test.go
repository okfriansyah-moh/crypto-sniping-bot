// Package orchestrator internal tests cover unexported helpers: safeProcess
// and outputEventID. These cannot be reached from the external test package.
package orchestrator

import (
	"context"
	"errors"
	"testing"

	"crypto-sniping-bot/shared/database"
)

// ── safeProcess ──────────────────────────────────────────────────────────────

// internalNilHandler returns (nil, nil).
type internalNilHandler struct{}

func (internalNilHandler) Process(_ context.Context, _ *database.Event) (*database.Event, error) {
	return nil, nil
}

// internalOutputHandler returns a fixed event.
type internalOutputHandler struct{ evt *database.Event }

func (h internalOutputHandler) Process(_ context.Context, _ *database.Event) (*database.Event, error) {
	return h.evt, nil
}

// internalErrorHandler returns an error.
type internalErrorHandler struct{ err error }

func (h internalErrorHandler) Process(_ context.Context, _ *database.Event) (*database.Event, error) {
	return nil, h.err
}

// internalPanicHandler panics.
type internalPanicHandler struct{ msg string }

func (h internalPanicHandler) Process(_ context.Context, _ *database.Event) (*database.Event, error) {
	panic(h.msg)
}

func TestSafeProcess_HappyPath_ReturnsOutput(t *testing.T) {
	// Arrange
	ctx := context.Background()
	evt := &database.Event{EventID: "e1"}
	out := &database.Event{EventID: "e2"}
	logger := noopLogger()

	// Act
	result, err := safeProcess(ctx, internalOutputHandler{evt: out}, evt, logger)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil || result.EventID != "e2" {
		t.Errorf("expected output event e2, got %v", result)
	}
}

func TestSafeProcess_HandlerError_PropagatesError(t *testing.T) {
	// Arrange
	ctx := context.Background()
	evt := &database.Event{EventID: "e3"}
	sentinel := errors.New("processing failed")
	logger := noopLogger()

	// Act
	result, err := safeProcess(ctx, internalErrorHandler{err: sentinel}, evt, logger)

	// Assert
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result on error, got %v", result)
	}
}

func TestSafeProcess_HandlerPanic_ReturnsStagePanic(t *testing.T) {
	// Arrange
	ctx := context.Background()
	evt := &database.Event{EventID: "e4"}
	logger := noopLogger()

	// Act: must NOT propagate the panic
	result, err := safeProcess(ctx, internalPanicHandler{msg: "boom"}, evt, logger)

	// Assert
	if !errors.Is(err, database.ErrStagePanic) {
		t.Errorf("expected ErrStagePanic, got %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result on panic, got %v", result)
	}
}

func TestSafeProcess_NilOutput_ReturnsNil(t *testing.T) {
	// Arrange
	ctx := context.Background()
	evt := &database.Event{EventID: "e5"}
	logger := noopLogger()

	// Act
	result, err := safeProcess(ctx, internalNilHandler{}, evt, logger)

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

// ── outputEventID ────────────────────────────────────────────────────────────

func TestOutputEventID_NilEvent_ReturnsEmpty(t *testing.T) {
	// Arrange / Act / Assert
	if got := outputEventID(nil); got != "" {
		t.Errorf("expected empty string for nil event, got %q", got)
	}
}

func TestOutputEventID_NonNilEvent_ReturnsID(t *testing.T) {
	// Arrange
	evt := &database.Event{EventID: "evt-xyz"}

	// Act
	got := outputEventID(evt)

	// Assert
	if got != "evt-xyz" {
		t.Errorf("expected evt-xyz, got %q", got)
	}
}
