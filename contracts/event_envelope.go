package contracts

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
)

// EventEnvelope is the canonical payload wrapper for every event on the bus.
// It wraps the inner DTO payload with routing and traceability fields.
// Payload is a JSON string (canonical JSON of the inner DTO).
// See docs/reference/implementation_roadmap.md § 0.4 and docs/reference/dto_contracts.md § 1.
type EventEnvelope struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	Payload   string `json:"payload"` // canonical JSON of the inner DTO
	TraceFields
	CreatedAt string `json:"created_at"` // ISO 8601 UTC; caller must provide
}

// NewEventEnvelope constructs an EventEnvelope with a content-addressable EventID.
// EventID = SHA256(canonical_json(payload))[:16].
// createdAt must be an ISO 8601 UTC timestamp — callers are responsible for
// providing a deterministic value (use block_timestamp or a fixed value in tests).
func NewEventEnvelope(eventType string, payload any, trace TraceFields, createdAt string) (EventEnvelope, error) {
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return EventEnvelope{}, fmt.Errorf("marshal payload: %w", err)
	}
	eventID := contentID(payloadBytes)
	return EventEnvelope{
		EventID:     eventID,
		EventType:   eventType,
		Payload:     string(payloadBytes),
		TraceFields: trace,
		CreatedAt:   createdAt,
	}, nil
}

// DecodePayload deserializes the envelope payload into the target type.
func DecodePayload[T any](env EventEnvelope) (T, error) {
	var out T
	if err := json.Unmarshal([]byte(env.Payload), &out); err != nil {
		return out, fmt.Errorf("decode payload: %w", err)
	}
	return out, nil
}

// contentID returns SHA256(data)[:16] as lowercase hex — 16 characters.
func contentID(data []byte) string {
	hash := sha256.Sum256(data)
	return fmt.Sprintf("%x", hash[:8])
}

// ContentID is the exported version of contentID for use by other packages.
// Use this to derive content-addressable IDs from any canonical JSON representation.
func ContentID(data []byte) string {
	return contentID(data)
}

// ContentIDFromString derives a content-addressable ID from a string.
func ContentIDFromString(s string) string {
	return contentID([]byte(s))
}
