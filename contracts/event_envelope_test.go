package contracts_test

import (
	"testing"

	"crypto-sniping-bot/contracts"
)

// ── ContentID ─────────────────────────────────────────────────────────────────

func TestContentID_Deterministic(t *testing.T) {
	// Arrange
	data := []byte(`{"chain":"eth","token":"0xABC"}`)

	// Act
	id1 := contracts.ContentID(data)
	id2 := contracts.ContentID(data)

	// Assert
	if id1 != id2 {
		t.Errorf("ContentID non-deterministic: %q vs %q", id1, id2)
	}
}

func TestContentID_Length(t *testing.T) {
	// Arrange / Act
	id := contracts.ContentID([]byte(`{"x":1}`))

	// Assert: SHA256[:8] → 16 hex chars
	if len(id) != 16 {
		t.Errorf("expected 16 chars, got %d: %q", len(id), id)
	}
}

func TestContentID_DistinctForDifferentInputs(t *testing.T) {
	// Arrange
	a := contracts.ContentID([]byte(`{"chain":"eth"}`))
	b := contracts.ContentID([]byte(`{"chain":"bsc"}`))

	// Assert
	if a == b {
		t.Error("different payloads produced the same ContentID")
	}
}

func TestContentIDFromString_Deterministic(t *testing.T) {
	// Arrange
	s := "trace-001:v1"

	// Act
	id1 := contracts.ContentIDFromString(s)
	id2 := contracts.ContentIDFromString(s)

	// Assert
	if id1 != id2 {
		t.Errorf("ContentIDFromString non-deterministic: %q vs %q", id1, id2)
	}
}

func TestContentIDFromString_DistinctForDifferentInputs(t *testing.T) {
	// Arrange / Act
	a := contracts.ContentIDFromString("run-a")
	b := contracts.ContentIDFromString("run-b")

	// Assert
	if a == b {
		t.Error("different strings produced the same ContentID")
	}
}

// ── NewEventEnvelope ──────────────────────────────────────────────────────────

type samplePayload struct {
	Chain string `json:"chain"`
	Token string `json:"token"`
}

func TestNewEventEnvelope_Happy(t *testing.T) {
	// Arrange
	trace := contracts.NewTraceFields("tr-1", "corr-1", "cause-1", "ver-1")
	payload := samplePayload{Chain: "eth", Token: "0xDEAD"}

	// Act
	env, err := contracts.NewEventEnvelope("market_data_event", payload, trace, "2026-01-01T00:00:00Z")

	// Assert
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if env.EventID == "" {
		t.Error("EventID must not be empty")
	}
	if env.EventType != "market_data_event" {
		t.Errorf("unexpected EventType: %q", env.EventType)
	}
	if env.TraceID != "tr-1" {
		t.Errorf("TraceID not propagated: %q", env.TraceID)
	}
	if env.CorrelationID != "corr-1" {
		t.Errorf("CorrelationID not propagated: %q", env.CorrelationID)
	}
	if env.CreatedAt == "" {
		t.Error("CreatedAt must be set")
	}
}

func TestNewEventEnvelope_ContentAddressable(t *testing.T) {
	// Same payload must produce the same EventID (deterministic).
	// Arrange
	trace := contracts.NewTraceFields("tr-2", "corr-2", "cause-2", "ver-2")
	payload := samplePayload{Chain: "eth", Token: "0xBEEF"}

	// Act
	env1, err1 := contracts.NewEventEnvelope("evt", payload, trace, "2026-01-01T00:00:00Z")
	env2, err2 := contracts.NewEventEnvelope("evt", payload, trace, "2026-01-01T00:00:00Z")

	// Assert
	if err1 != nil || err2 != nil {
		t.Fatalf("unexpected errors: %v, %v", err1, err2)
	}
	if env1.EventID != env2.EventID {
		t.Errorf("same payload → different EventIDs: %q vs %q", env1.EventID, env2.EventID)
	}
}

func TestNewEventEnvelope_DifferentPayloads_DifferentIDs(t *testing.T) {
	// Arrange
	trace := contracts.NewTraceFields("tr-3", "corr-3", "", "ver-3")

	// Act
	env1, _ := contracts.NewEventEnvelope("evt", samplePayload{Chain: "eth"}, trace, "2026-01-01T00:00:00Z")
	env2, _ := contracts.NewEventEnvelope("evt", samplePayload{Chain: "bsc"}, trace, "2026-01-01T00:00:00Z")

	// Assert
	if env1.EventID == env2.EventID {
		t.Error("different payloads must not share EventID")
	}
}

// ── DecodePayload ─────────────────────────────────────────────────────────────

func TestDecodePayload_Happy(t *testing.T) {
	// Arrange
	trace := contracts.NewTraceFields("tr-4", "corr-4", "c4", "v4")
	want := samplePayload{Chain: "eth", Token: "0xCAFE"}
	env, err := contracts.NewEventEnvelope("test_event", want, trace, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("NewEventEnvelope: %v", err)
	}

	// Act
	got, err := contracts.DecodePayload[samplePayload](env)

	// Assert
	if err != nil {
		t.Fatalf("DecodePayload error: %v", err)
	}
	if got.Chain != want.Chain || got.Token != want.Token {
		t.Errorf("decoded %+v, want %+v", got, want)
	}
}

func TestDecodePayload_InvalidJSON(t *testing.T) {
	// Arrange: craft an envelope with broken JSON payload
	env := contracts.EventEnvelope{
		EventID:   "bad-001",
		EventType: "bad_event",
		Payload:   `{not valid json`,
	}

	// Act
	_, err := contracts.DecodePayload[samplePayload](env)

	// Assert
	if err == nil {
		t.Error("expected error for invalid JSON, got nil")
	}
}
