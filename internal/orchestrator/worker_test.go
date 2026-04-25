package orchestrator_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/orchestrator"
)

// ── worker mock adapter ──────────────────────────────────────────────────────

// workerAdapter is a minimal mock for RunWorker tests.
// It returns canned events sequentially from claimQueue, then nil.
type workerAdapter struct {
	mockAdapter

	claimQueue   []*database.Event // events to return in order
	claimIndex   int32             // atomic cursor into claimQueue
	claimErr     error             // if non-nil, ClaimNextEvent returns this after queue exhausted
	marked       []string          // EventIDs passed to MarkEventProcessed
	released     []string          // EventIDs passed to ReleaseEventClaim
	inserted     []database.Event  // events passed to InsertEvent
	markErr      error             // returned by MarkEventProcessed if set
	insertOutErr error             // returned by InsertEvent if set
}

func newWorkerAdapter(events ...*database.Event) *workerAdapter {
	w := &workerAdapter{mockAdapter: *newMock()}
	w.claimQueue = events
	return w
}

func (w *workerAdapter) ClaimNextEvent(_ context.Context, _ string, _ []string) (*database.Event, error) {
	idx := int(atomic.AddInt32(&w.claimIndex, 1)) - 1
	if idx < len(w.claimQueue) {
		return w.claimQueue[idx], nil
	}
	if w.claimErr != nil {
		return nil, w.claimErr
	}
	return nil, nil
}

func (w *workerAdapter) MarkEventProcessed(_ context.Context, eventID string) error {
	w.marked = append(w.marked, eventID)
	return w.markErr
}

func (w *workerAdapter) ReleaseEventClaim(_ context.Context, eventID string) error {
	w.released = append(w.released, eventID)
	return nil
}

func (w *workerAdapter) InsertEvent(_ context.Context, evt database.Event) error {
	if w.insertOutErr != nil {
		return w.insertOutErr
	}
	w.inserted = append(w.inserted, evt)
	return nil
}

// ── handler stubs ────────────────────────────────────────────────────────────

// returnNilHandler returns (nil, nil) — no downstream event emitted.
type returnNilHandler struct{}

func (returnNilHandler) Process(_ context.Context, _ *database.Event) (*database.Event, error) {
	return nil, nil
}

// returnOutputHandler returns a fixed output event.
type returnOutputHandler struct {
	output *database.Event
}

func (h returnOutputHandler) Process(_ context.Context, _ *database.Event) (*database.Event, error) {
	return h.output, nil
}

// errorHandler always returns an error.
type errorHandler struct{ err error }

func (h errorHandler) Process(_ context.Context, _ *database.Event) (*database.Event, error) {
	return nil, h.err
}

// panicHandler panics on every call.
type panicHandler struct{}

func (panicHandler) Process(_ context.Context, _ *database.Event) (*database.Event, error) {
	panic("handler panicked intentionally")
}

// ── tests ────────────────────────────────────────────────────────────────────

// TestRunWorker_ContextCancelled_ExitsCleanly verifies the worker exits when the
// context is already cancelled before the first iteration.
func TestRunWorker_ContextCancelled_ExitsCleanly(t *testing.T) {
	// Arrange
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	adapter := newWorkerAdapter()

	// Act
	err := orchestrator.RunWorker(ctx, adapter, "group-a", []string{"event.a"}, returnNilHandler{}, time.Millisecond, nil)

	// Assert
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

// TestRunWorker_NoEvents_ExitsOnCancel verifies the worker sleeps when the queue
// is empty and exits cleanly once the context is cancelled.
func TestRunWorker_NoEvents_ExitsOnCancel(t *testing.T) {
	// Arrange: no events in queue
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	adapter := newWorkerAdapter()

	// Act
	err := orchestrator.RunWorker(ctx, adapter, "group-b", []string{"event.b"}, returnNilHandler{}, time.Millisecond, nil)

	// Assert: must exit due to deadline/cancel, not some other error
	if err != nil && !errors.Is(err, context.DeadlineExceeded) && !errors.Is(err, context.Canceled) {
		t.Errorf("expected context error, got %v", err)
	}
}

// TestRunWorker_ProcessesEvent_MarksProcessed verifies that a claimed event is
// processed and then marked as processed, in that order.
func TestRunWorker_ProcessesEvent_MarksProcessed(t *testing.T) {
	// Arrange
	evt := &database.Event{EventID: "evt-aaa", EventType: "event.c", TraceID: "t1", CorrelationID: "c1", VersionID: "v1"}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	adapter := newWorkerAdapter(evt)

	// Act
	orchestrator.RunWorker(ctx, adapter, "group-c", []string{"event.c"}, returnNilHandler{}, time.Millisecond, nil) //nolint:errcheck

	// Assert
	if len(adapter.marked) == 0 {
		t.Fatal("expected MarkEventProcessed to be called")
	}
	if adapter.marked[0] != "evt-aaa" {
		t.Errorf("expected evt-aaa marked, got %s", adapter.marked[0])
	}
}

// TestRunWorker_HandlerEmitsOutput_InsertsOutputEvent verifies that when a handler
// returns a non-nil output event, it is inserted before marking the input processed.
func TestRunWorker_HandlerEmitsOutput_InsertsOutputEvent(t *testing.T) {
	// Arrange
	inEvt := &database.Event{EventID: "evt-in", EventType: "event.d", TraceID: "t2", CorrelationID: "c2", VersionID: "v2"}
	outEvt := &database.Event{EventID: "evt-out", EventType: "event.d.result"}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	adapter := newWorkerAdapter(inEvt)

	// Act
	orchestrator.RunWorker(ctx, adapter, "group-d", []string{"event.d"}, returnOutputHandler{output: outEvt}, time.Millisecond, nil) //nolint:errcheck

	// Assert: output event must be inserted
	if len(adapter.inserted) == 0 {
		t.Fatal("expected InsertEvent to be called for output event")
	}
	if adapter.inserted[0].EventID != "evt-out" {
		t.Errorf("expected evt-out inserted, got %s", adapter.inserted[0].EventID)
	}
	// Input event must be marked processed
	if len(adapter.marked) == 0 || adapter.marked[0] != "evt-in" {
		t.Errorf("expected evt-in marked, got %v", adapter.marked)
	}
}

// TestRunWorker_HandlerError_ReleasesClaim verifies that when a handler returns
// an error, the event claim is released and processing continues (not crashed).
func TestRunWorker_HandlerError_ReleasesClaim(t *testing.T) {
	// Arrange
	evt := &database.Event{EventID: "evt-fail", EventType: "event.e", TraceID: "t3", CorrelationID: "c3", VersionID: "v3"}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	adapter := newWorkerAdapter(evt)
	handler := errorHandler{err: errors.New("handler error")}

	// Act
	orchestrator.RunWorker(ctx, adapter, "group-e", []string{"event.e"}, handler, time.Millisecond, nil) //nolint:errcheck

	// Assert: claim must be released, event must NOT be marked processed
	if len(adapter.released) == 0 {
		t.Fatal("expected ReleaseEventClaim to be called on handler error")
	}
	if adapter.released[0] != "evt-fail" {
		t.Errorf("expected evt-fail released, got %s", adapter.released[0])
	}
	if len(adapter.marked) > 0 {
		t.Errorf("MarkEventProcessed must not be called on handler error, but was called for %v", adapter.marked)
	}
}

// TestRunWorker_HandlerPanic_ReleasesClaim verifies that a panicking handler
// does not crash the worker — the claim is released and the loop continues.
func TestRunWorker_HandlerPanic_ReleasesClaim(t *testing.T) {
	// Arrange
	evt := &database.Event{EventID: "evt-panic", EventType: "event.f", TraceID: "t4", CorrelationID: "c4", VersionID: "v4"}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	adapter := newWorkerAdapter(evt)

	// Act: must not panic the test
	orchestrator.RunWorker(ctx, adapter, "group-f", []string{"event.f"}, panicHandler{}, time.Millisecond, nil) //nolint:errcheck

	// Assert: claim must be released
	if len(adapter.released) == 0 {
		t.Fatal("expected ReleaseEventClaim to be called after handler panic")
	}
	if adapter.released[0] != "evt-panic" {
		t.Errorf("expected evt-panic released, got %s", adapter.released[0])
	}
}

// TestRunWorker_NilLogger_DoesNotPanic verifies that passing nil logger is safe.
func TestRunWorker_NilLogger_DoesNotPanic(t *testing.T) {
	// Arrange
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	adapter := newWorkerAdapter()

	// Act / Assert: must not panic
	orchestrator.RunWorker(ctx, adapter, "group-nil", []string{"event.nil"}, returnNilHandler{}, time.Millisecond, nil) //nolint:errcheck
}
