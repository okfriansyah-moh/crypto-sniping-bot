package orchestrator_test

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"log/slog"

	"crypto-sniping-bot/database"
	"crypto-sniping-bot/internal/orchestrator"
)

// recordDecisionHandler is a stub that calls RecordDecision with configured
// (status, reason) and returns the configured output (typically nil for the
// reject path, non-nil for the emit path).
type recordDecisionHandler struct {
	output *database.Event
	status string
	reason string
}

func (h recordDecisionHandler) Process(ctx context.Context, _ *database.Event) (*database.Event, error) {
	if h.status != "" {
		orchestrator.RecordDecision(ctx, h.status, h.reason)
	}
	return h.output, nil
}

// captureLogs builds a slog.Logger that writes JSON records into buf so the
// test can parse and assert on individual structured fields.
func captureLogs(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelInfo}))
}

// findStageCompletedRecord scans the captured JSON-line log buffer and returns
// the parsed map for the (last) stage_completed record. Fails the test if
// none is present.
func findStageCompletedRecord(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var found map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec["msg"] == "stage_completed" {
			found = rec
		}
	}
	if found == nil {
		t.Fatalf("no stage_completed record in logs:\n%s", buf.String())
	}
	return found
}

// TestRunWorker_StageCompleted_EmittedHasOutputID verifies that on the ACCEPT
// path the stage_completed log line carries output_status=emitted and a
// non-empty output_event_id, with no decision_reason. This is the canonical
// trace correlation case (F-8: emitted side).
func TestRunWorker_StageCompleted_EmittedHasOutputID(t *testing.T) {
	in := &database.Event{EventID: "evt-in-emit", EventType: "event.x", TraceID: "t-e", CorrelationID: "c-e", VersionID: "v-e"}
	out := &database.Event{EventID: "evt-out-emit", EventType: "event.x.result"}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	adapter := newWorkerAdapter(in)
	var buf bytes.Buffer
	logger := captureLogs(&buf)
	handler := recordDecisionHandler{output: out}

	_ = orchestrator.RunWorker(ctx, adapter, "g-emit", []string{"event.x"}, handler, time.Millisecond, logger)

	rec := findStageCompletedRecord(t, &buf)
	if got := rec[orchestrator.LogFieldOutputStatus]; got != orchestrator.StageStatusEmitted {
		t.Errorf("output_status = %v, want %s", got, orchestrator.StageStatusEmitted)
	}
	if got := rec[orchestrator.LogFieldOutputEventID]; got != "evt-out-emit" {
		t.Errorf("output_event_id = %v, want evt-out-emit", got)
	}
	if got, _ := rec[orchestrator.LogFieldDecisionReason].(string); got != "" {
		t.Errorf("decision_reason on emit must be empty, got %q", got)
	}
}

// TestRunWorker_StageCompleted_RejectedHasReason verifies that on the REJECT
// path (handler returns nil and calls RecordDecision) the stage_completed
// log line carries output_status=rejected, output_event_id="", and a
// non-empty decision_reason — the F-8 fix.
func TestRunWorker_StageCompleted_RejectedHasReason(t *testing.T) {
	in := &database.Event{EventID: "evt-in-rej", EventType: "event.y", TraceID: "t-r", CorrelationID: "c-r", VersionID: "v-r"}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	adapter := newWorkerAdapter(in)
	var buf bytes.Buffer
	logger := captureLogs(&buf)
	handler := recordDecisionHandler{
		output: nil,
		status: orchestrator.StageStatusRejected,
		reason: "honeypot,fake_lp",
	}

	_ = orchestrator.RunWorker(ctx, adapter, "g-rej", []string{"event.y"}, handler, time.Millisecond, logger)

	rec := findStageCompletedRecord(t, &buf)
	if got := rec[orchestrator.LogFieldOutputStatus]; got != orchestrator.StageStatusRejected {
		t.Errorf("output_status = %v, want %s", got, orchestrator.StageStatusRejected)
	}
	if got, _ := rec[orchestrator.LogFieldOutputEventID].(string); got != "" {
		t.Errorf("output_event_id on reject must be empty, got %q", got)
	}
	if got := rec[orchestrator.LogFieldDecisionReason]; got != "honeypot,fake_lp" {
		t.Errorf("decision_reason = %v, want honeypot,fake_lp", got)
	}
	// Input event must still be marked processed.
	if len(adapter.marked) == 0 || adapter.marked[0] != "evt-in-rej" {
		t.Errorf("expected evt-in-rej marked, got %v", adapter.marked)
	}
}

// TestRunWorker_StageCompleted_LegacyNilDefaultsToFiltered verifies the
// backwards-compatible path: a handler that returns (nil, nil) without
// calling RecordDecision still produces a parseable stage_completed record
// with output_status=filtered. This is the legacy default and must remain
// stable so older handlers do not silently regress to a blank discriminator.
func TestRunWorker_StageCompleted_LegacyNilDefaultsToFiltered(t *testing.T) {
	in := &database.Event{EventID: "evt-in-legacy", EventType: "event.z", TraceID: "t-l", CorrelationID: "c-l", VersionID: "v-l"}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	adapter := newWorkerAdapter(in)
	var buf bytes.Buffer
	logger := captureLogs(&buf)

	_ = orchestrator.RunWorker(ctx, adapter, "g-legacy", []string{"event.z"}, returnNilHandler{}, time.Millisecond, logger)

	rec := findStageCompletedRecord(t, &buf)
	if got := rec[orchestrator.LogFieldOutputStatus]; got != orchestrator.StageStatusFiltered {
		t.Errorf("output_status = %v, want %s", got, orchestrator.StageStatusFiltered)
	}
	if got, _ := rec[orchestrator.LogFieldOutputEventID].(string); got != "" {
		t.Errorf("output_event_id must be empty, got %q", got)
	}
}

// TestRunWorker_StageCompleted_DistinctLogLines verifies the emit and reject
// cases produce structurally distinct, parseable log lines — the F-8 acceptance
// criterion: two adjacent stage_completed records can be disambiguated by a
// downstream parser using only documented field constants.
func TestRunWorker_StageCompleted_DistinctLogLines(t *testing.T) {
	emitIn := &database.Event{EventID: "ev-e-in", EventType: "event.q", TraceID: "tq", CorrelationID: "cq", VersionID: "vq"}
	rejIn := &database.Event{EventID: "ev-r-in", EventType: "event.q", TraceID: "tq2", CorrelationID: "cq2", VersionID: "vq"}
	emitOut := &database.Event{EventID: "ev-e-out", EventType: "event.q.result"}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Two-pass adapter: first claim returns emitIn, second returns rejIn.
	adapter := newWorkerAdapter(emitIn, rejIn)
	var buf bytes.Buffer
	logger := captureLogs(&buf)

	// Stateful handler: first call emits, second call rejects.
	calls := 0
	handler := dispatchHandler{fn: func(ctx context.Context, _ *database.Event) (*database.Event, error) {
		calls++
		if calls == 1 {
			return emitOut, nil
		}
		orchestrator.RecordDecision(ctx, orchestrator.StageStatusRejected, "ev_below_threshold")
		return nil, nil
	}}

	_ = orchestrator.RunWorker(ctx, adapter, "g-dist", []string{"event.q"}, handler, time.Millisecond, logger)

	// Collect both stage_completed records.
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			continue
		}
		if rec["msg"] == "stage_completed" {
			records = append(records, rec)
		}
	}
	if len(records) < 2 {
		t.Fatalf("expected 2 stage_completed records, got %d:\n%s", len(records), buf.String())
	}

	emit, rej := records[0], records[1]
	if emit[orchestrator.LogFieldOutputStatus] != orchestrator.StageStatusEmitted {
		t.Errorf("first record output_status = %v, want emitted", emit[orchestrator.LogFieldOutputStatus])
	}
	if emit[orchestrator.LogFieldOutputEventID] != "ev-e-out" {
		t.Errorf("first record output_event_id = %v, want ev-e-out", emit[orchestrator.LogFieldOutputEventID])
	}
	if rej[orchestrator.LogFieldOutputStatus] != orchestrator.StageStatusRejected {
		t.Errorf("second record output_status = %v, want rejected", rej[orchestrator.LogFieldOutputStatus])
	}
	if got, _ := rej[orchestrator.LogFieldOutputEventID].(string); got != "" {
		t.Errorf("second record output_event_id must be empty, got %q", got)
	}
	if rej[orchestrator.LogFieldDecisionReason] != "ev_below_threshold" {
		t.Errorf("second record decision_reason = %v, want ev_below_threshold", rej[orchestrator.LogFieldDecisionReason])
	}
}

// dispatchHandler routes Process to a configurable function — used by the
// distinct-log-lines test to alternate emit/reject behaviour without needing
// a second handler type.
type dispatchHandler struct {
	fn func(ctx context.Context, evt *database.Event) (*database.Event, error)
}

func (h dispatchHandler) Process(ctx context.Context, evt *database.Event) (*database.Event, error) {
	return h.fn(ctx, evt)
}
