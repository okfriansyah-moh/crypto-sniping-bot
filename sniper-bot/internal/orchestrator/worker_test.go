package orchestrator_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"crypto-sniping-bot/shared/database"
	"crypto-sniping-bot/sniper-bot/internal/orchestrator"
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

	// DLQ instrumentation (CRIT-2 retry+DLQ wiring).
	retryCounts atomic.Int32        // per-event retry counter; bumped each IncrementEventRetry
	dlqEntries  []database.DLQEntry // entries passed to MoveToDLQ
	dlqErr      error               // returned by MoveToDLQ if set
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

// IncrementEventRetry overrides mockAdapter to monotonically count failures.
// Returns the new retry count so RunWorker can decide release-vs-DLQ.
func (w *workerAdapter) IncrementEventRetry(_ context.Context, _, _ string) (int, error) {
	return int(w.retryCounts.Add(1)), nil
}

// MoveToDLQ overrides mockAdapter to capture DLQ entries for assertions.
func (w *workerAdapter) MoveToDLQ(_ context.Context, e database.DLQEntry) error {
	if w.dlqErr != nil {
		return w.dlqErr
	}
	w.dlqEntries = append(w.dlqEntries, e)
	return nil
}
func (w *workerAdapter) CountTokensByCreator(_ context.Context, _, _ string) (int32, error) {
	return 0, nil
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

// TestRunWorker_PoisonPill_MovesToDLQ verifies that an event whose handler
// fails repeatedly is moved to dead_letter_events once retry_count exceeds
// maxRetries — the poison-pill barrier required by architecture.md § 4.10.
//
// Without this barrier, a malformed payload or persistent module bug would
// loop forever in the queue and block the pipeline.
func TestRunWorker_PoisonPill_MovesToDLQ(t *testing.T) {
	// Arrange: same event repeated until DLQ takes it.
	const maxRetries = 2
	evt := &database.Event{
		EventID:       "evt-poison",
		EventType:     "event.dlq",
		TraceID:       "t-dlq",
		CorrelationID: "c-dlq",
		VersionID:     "v-dlq",
		Payload:       []byte(`{"poison":true}`),
	}
	// Re-deliver the same event 5 times — exceeds maxRetries=2.
	adapter := newWorkerAdapter(evt, evt, evt, evt, evt)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	handler := errorHandler{err: errors.New("permanent failure")}

	// Act
	orchestrator.RunWorker(ctx, adapter, "group-dlq", []string{"event.dlq"}, handler, time.Millisecond, nil, maxRetries) //nolint:errcheck

	// Assert: at least one DLQ entry recorded.
	if len(adapter.dlqEntries) == 0 {
		t.Fatal("expected MoveToDLQ to be called once retry_count > maxRetries")
	}
	got := adapter.dlqEntries[0]
	if got.EventID != "evt-poison" {
		t.Errorf("expected DLQ EventID=evt-poison, got %q", got.EventID)
	}
	if got.Consumer != "group-dlq" {
		t.Errorf("expected DLQ Consumer=group-dlq, got %q", got.Consumer)
	}
	if got.Reason != "transient_exceeded" {
		t.Errorf("expected DLQ Reason=transient_exceeded, got %q", got.Reason)
	}
	if got.RetryCount <= maxRetries {
		t.Errorf("expected DLQ RetryCount > %d, got %d", maxRetries, got.RetryCount)
	}
	if got.ErrorMessage == "" {
		t.Error("expected DLQ ErrorMessage to be populated")
	}
	// MarkEventProcessed must NOT be called — MoveToDLQ handles that internally
	// inside the postgres adapter (UPDATE events SET processed=TRUE).
	if len(adapter.marked) != 0 {
		t.Errorf("expected MarkEventProcessed NOT to be called by RunWorker on DLQ path, got %v", adapter.marked)
	}
}

// TestRunWorker_TransientFailure_ReleasesClaim_NotDLQ verifies that a single
// handler failure (count <= maxRetries) re-releases the claim instead of
// moving to DLQ. This guards against the regression where the new retry
// counter would prematurely DLQ events on the very first failure.
func TestRunWorker_TransientFailure_ReleasesClaim_NotDLQ(t *testing.T) {
	// Arrange
	evt := &database.Event{EventID: "evt-transient", EventType: "event.t", TraceID: "tt", CorrelationID: "ct", VersionID: "vt"}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	adapter := newWorkerAdapter(evt) // single delivery; loop will sleep on subsequent calls
	handler := errorHandler{err: errors.New("blip")}

	// Act: maxRetries=5 (default-ish); a single failure must NOT DLQ.
	orchestrator.RunWorker(ctx, adapter, "group-trans", []string{"event.t"}, handler, time.Millisecond, nil, 5) //nolint:errcheck

	// Assert
	if len(adapter.dlqEntries) != 0 {
		t.Errorf("expected no DLQ entries on first failure, got %d", len(adapter.dlqEntries))
	}
	if len(adapter.released) == 0 {
		t.Fatal("expected ReleaseEventClaim on transient failure")
	}
}

// TestRunWorker_LifecycleAlreadyAdvanced_SkipsToProcessed verifies that when a
// handler returns ErrLifecycleAlreadyAdvanced (stale event from a prior session),
// RunWorker marks the event processed and does NOT invoke the retry/DLQ path.
// This guards against the 18.6× DQ replay multiplier caused by prior-session
// stale events hitting a lifecycle CAS guard and being retried until DLQ.
func TestRunWorker_LifecycleAlreadyAdvanced_SkipsToProcessed(t *testing.T) {
	// Arrange
	evt := &database.Event{
		EventID:       "evt-stale",
		EventType:     "event.stale",
		TraceID:       "t-stale",
		CorrelationID: "c-stale",
		VersionID:     "v-stale",
		Payload:       []byte(`{}`),
	}
	adapter := newWorkerAdapter(evt)
	handler := errorHandler{err: database.ErrLifecycleAlreadyAdvanced}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	// Act
	orchestrator.RunWorker(ctx, adapter, "group-stale", []string{"event.stale"}, handler, time.Millisecond, nil) //nolint:errcheck

	// Assert: event must be marked processed — not retried, not DLQ'd.
	if len(adapter.marked) == 0 {
		t.Fatal("expected MarkEventProcessed for already-advanced lifecycle")
	}
	if adapter.marked[0] != "evt-stale" {
		t.Errorf("expected marked[0]=evt-stale, got %q", adapter.marked[0])
	}
	if len(adapter.dlqEntries) != 0 {
		t.Errorf("expected no DLQ entries, got %d", len(adapter.dlqEntries))
	}
	if len(adapter.released) != 0 {
		t.Errorf("expected no ReleaseEventClaim, got %d", len(adapter.released))
	}
}
