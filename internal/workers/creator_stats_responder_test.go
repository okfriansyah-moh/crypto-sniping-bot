package workers

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"crypto-sniping-bot/contracts"
	"crypto-sniping-bot/database"
)

// statsTrackingAdapter embeds the zero-value-safe stubAdapter and overrides the
// methods exercised by RunCreatorStatsResponder.
type statsTrackingAdapter struct {
	stubAdapter

	// Input controls — configure these before calling RunCreatorStatsResponder.
	nextEvent     *database.Event // returned once by ClaimNextEvent, then nil
	profileResult contracts.CreatorProfile
	profileFound  bool
	profileErr    error

	// Output captures — inspect these after the call.
	insertedEvent       *database.Event
	markProcessedCalled bool
	releaseCalled       bool
}

func (a *statsTrackingAdapter) ClaimNextEvent(
	_ context.Context,
	consumer string,
	eventTypes []string,
) (*database.Event, error) {
	evt := a.nextEvent
	a.nextEvent = nil // consume once
	return evt, nil
}

func (a *statsTrackingAdapter) GetCreatorProfile(
	_ context.Context,
	_, _ string,
) (contracts.CreatorProfile, bool, error) {
	return a.profileResult, a.profileFound, a.profileErr
}

func (a *statsTrackingAdapter) InsertEvent(_ context.Context, evt database.Event) error {
	cp := evt
	a.insertedEvent = &cp
	return nil
}

func (a *statsTrackingAdapter) MarkEventProcessed(_ context.Context, _ string) error {
	a.markProcessedCalled = true
	return nil
}

func (a *statsTrackingAdapter) ReleaseEventClaim(_ context.Context, _ string) error {
	a.releaseCalled = true
	return nil
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func makeStatsRequestEvent(chain, creator string) *database.Event {
	payload, _ := json.Marshal(creatorStatsRequest{
		Chain:       chain,
		CreatorAddr: creator,
	})
	return &database.Event{
		EventID:       "test_event_001",
		EventType:     "creator_stats_request",
		Payload:       payload,
		TraceID:       "trace-abc",
		CorrelationID: "corr-def",
	}
}

// ── Tests ────────────────────────────────────────────────────────────────────

// TestCreatorStatsResponder_EmitsResponseEvent verifies that the responder, when
// given a valid creator_stats_request event and a found creator profile, emits a
// telegram_event whose text contains the expected profile data.
func TestCreatorStatsResponder_EmitsResponseEvent(t *testing.T) {
	profile := contracts.CreatorProfile{
		Chain:           "solana",
		CreatorAddress:  "5n3LYFeABC123",
		TotalTokens:     10,
		RugTokens:       2,
		MigratedTokens:  3,
		GoldenGemTokens: 1,
		WinTokens:       4,
		LossTokens:      6,
		FirstSeenAt:     time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		LastSeenAt:      time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
	}

	adapter := &statsTrackingAdapter{
		nextEvent:     makeStatsRequestEvent("solana", "5n3LYFeABC123"),
		profileResult: profile,
		profileFound:  true,
	}

	err := RunCreatorStatsResponder(context.Background(), adapter, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if adapter.insertedEvent == nil {
		t.Fatal("expected telegram_event to be inserted, but InsertEvent was not called")
	}
	if adapter.insertedEvent.EventType != "telegram_event" {
		t.Errorf("expected event type telegram_event, got %s", adapter.insertedEvent.EventType)
	}

	var tgPayload telegramEventPayload
	if err := json.Unmarshal(adapter.insertedEvent.Payload, &tgPayload); err != nil {
		t.Fatalf("failed to unmarshal telegram_event payload: %v", err)
	}

	text := tgPayload.Text
	if !strings.Contains(text, "5n3LYFeABC123") {
		t.Errorf("expected text to contain creator address, got: %s", text)
	}
	if !strings.Contains(text, "10") {
		t.Errorf("expected text to contain TotalTokens=10, got: %s", text)
	}
	// Rug rate should be 20.0% (2/10).
	if !strings.Contains(text, "20.0") {
		t.Errorf("expected text to contain rug rate 20.0%%, got: %s", text)
	}
	if !strings.Contains(text, "Elevated rug risk") {
		t.Errorf("expected verdict to contain 'Elevated rug risk', got: %s", text)
	}

	if !adapter.markProcessedCalled {
		t.Error("expected MarkEventProcessed to be called after successful processing")
	}
}

// TestCreatorStatsResponder_NotFound_EmitsNotFoundMessage verifies that the
// responder emits an appropriate not-found message when the profile does not exist.
func TestCreatorStatsResponder_NotFound_EmitsNotFoundMessage(t *testing.T) {
	adapter := &statsTrackingAdapter{
		nextEvent:    makeStatsRequestEvent("solana", "unknown_creator"),
		profileFound: false,
	}

	err := RunCreatorStatsResponder(context.Background(), adapter, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if adapter.insertedEvent == nil {
		t.Fatal("expected telegram_event to be inserted even for not-found case")
	}
	var tgPayload telegramEventPayload
	if err := json.Unmarshal(adapter.insertedEvent.Payload, &tgPayload); err != nil {
		t.Fatalf("failed to unmarshal telegram_event payload: %v", err)
	}

	if !strings.Contains(tgPayload.Text, "not found") {
		t.Errorf("expected not-found message, got: %s", tgPayload.Text)
	}
	if !adapter.markProcessedCalled {
		t.Error("expected MarkEventProcessed to be called")
	}
}

// TestCreatorStatsResponder_ZeroTotalTokens_NoDivisionByZero verifies that the
// responder does not panic and produces a valid telegram_event when TotalTokens is 0.
func TestCreatorStatsResponder_ZeroTotalTokens_NoDivisionByZero(t *testing.T) {
	profile := contracts.CreatorProfile{
		Chain:          "solana",
		CreatorAddress: "ZeroCreator",
		TotalTokens:    0,
	}

	adapter := &statsTrackingAdapter{
		nextEvent:     makeStatsRequestEvent("solana", "ZeroCreator"),
		profileResult: profile,
		profileFound:  true,
	}

	err := RunCreatorStatsResponder(context.Background(), adapter, nil)
	if err != nil {
		t.Fatalf("unexpected error (no panic expected): %v", err)
	}
	if adapter.insertedEvent == nil {
		t.Fatal("expected telegram_event to be inserted")
	}
}

// TestCreatorStatsResponder_NoEventAvailable verifies that calling the responder
// when no events are available returns nil without inserting anything.
func TestCreatorStatsResponder_NoEventAvailable(t *testing.T) {
	adapter := &statsTrackingAdapter{nextEvent: nil}

	err := RunCreatorStatsResponder(context.Background(), adapter, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adapter.insertedEvent != nil {
		t.Error("expected no InsertEvent call when no event is available")
	}
	if adapter.markProcessedCalled {
		t.Error("expected no MarkEventProcessed when no event is available")
	}
}

// TestCreatorStatsResponder_MalformedPayload verifies graceful handling of a
// broken JSON payload — the event is marked processed (no re-delivery loop)
// and no telegram_event is inserted.
func TestCreatorStatsResponder_MalformedPayload(t *testing.T) {
	adapter := &statsTrackingAdapter{
		nextEvent: &database.Event{
			EventID:   "bad_payload",
			EventType: "creator_stats_request",
			Payload:   []byte(`{not valid json`),
		},
	}

	err := RunCreatorStatsResponder(context.Background(), adapter, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if adapter.insertedEvent != nil {
		t.Error("expected no InsertEvent for malformed payload")
	}
	if !adapter.markProcessedCalled {
		t.Error("expected MarkEventProcessed so malformed event does not re-deliver")
	}
}

// TestFormatCreatorStats_FoundProfile directly tests FormatCreatorStats for a
// profile with all outcomes set — verifies computed percentages and verdict.
func TestFormatCreatorStats_FoundProfile(t *testing.T) {
	profile := contracts.CreatorProfile{
		Chain:           "solana",
		CreatorAddress:  "ABC123",
		TotalTokens:     4,
		RugTokens:       2,
		MigratedTokens:  1,
		GoldenGemTokens: 1,
		WinTokens:       2,
		LossTokens:      2,
		FirstSeenAt:     time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC),
		LastSeenAt:      time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC),
	}

	text := FormatCreatorStats(profile, true)

	if !strings.Contains(text, "ABC123") {
		t.Errorf("expected creator address in text, got: %s", text)
	}
	// 2/4 = 50% rug rate → high rug risk verdict
	if !strings.Contains(text, "50.0") {
		t.Errorf("expected 50.0%% rug rate, got: %s", text)
	}
	if !strings.Contains(text, "High rug risk") {
		t.Errorf("expected 'High rug risk' verdict, got: %s", text)
	}
	if !strings.Contains(text, "2025-03-01") {
		t.Errorf("expected first seen date, got: %s", text)
	}
}

// TestFormatCreatorStats_NotFound verifies the not-found path does not panic
// and returns a user-friendly message.
func TestFormatCreatorStats_NotFound(t *testing.T) {
	text := FormatCreatorStats(contracts.CreatorProfile{}, false)

	if strings.TrimSpace(text) == "" {
		t.Error("expected non-empty message for not-found case")
	}
	if !strings.Contains(strings.ToLower(text), "not found") {
		t.Errorf("expected 'not found' in message, got: %s", text)
	}
}

// TestFormatCreatorStats_LowRugRate verifies the low-rug-history verdict path.
func TestFormatCreatorStats_LowRugRate(t *testing.T) {
	profile := contracts.CreatorProfile{
		Chain:          "solana",
		CreatorAddress: "LowRug123",
		TotalTokens:    10,
		RugTokens:      0, // 0% rug rate
	}

	text := FormatCreatorStats(profile, true)
	if !strings.Contains(text, "Low rug history") {
		t.Errorf("expected 'Low rug history' verdict, got: %s", text)
	}
}
