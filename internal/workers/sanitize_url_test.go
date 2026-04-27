package workers

import (
	"testing"
)

// ── sanitizeURL ───────────────────────────────────────────────────────────────

func TestSanitizeURL_InfuraPathKey_Redacted(t *testing.T) {
	// Arrange
	raw := "wss://mainnet.infura.io/ws/v3/abcdef1234567890abcdef1234"

	// Act
	got := sanitizeURL(raw)

	// Assert
	if got == raw {
		t.Error("expected URL to be sanitized, got original back")
	}
	if containsKey(got, "abcdef1234567890abcdef1234") {
		t.Errorf("API key still present in sanitized URL: %q", got)
	}
}

func TestSanitizeURL_AlchemyPathKey_Redacted(t *testing.T) {
	raw := "https://eth-mainnet.g.alchemy.com/v2/MyAlchemyApiKey12345678901"
	got := sanitizeURL(raw)
	if containsKey(got, "MyAlchemyApiKey12345678901") {
		t.Errorf("API key still present: %q", got)
	}
}

func TestSanitizeURL_QueryParamKey_Redacted(t *testing.T) {
	raw := "https://example.com/rpc?apikey=supersecretkey123"
	got := sanitizeURL(raw)
	if containsKey(got, "supersecretkey123") {
		t.Errorf("query key still present: %q", got)
	}
}

func TestSanitizeURL_TokenQueryParam_Redacted(t *testing.T) {
	raw := "https://example.com/rpc?token=mysecrettoken456"
	got := sanitizeURL(raw)
	if containsKey(got, "mysecrettoken456") {
		t.Errorf("token still present: %q", got)
	}
}

func TestSanitizeURL_NoKey_PassesThrough(t *testing.T) {
	raw := "wss://localhost:8545"
	got := sanitizeURL(raw)
	if got != raw {
		t.Errorf("expected passthrough for clean URL, got %q", got)
	}
}

func TestSanitizeURL_Empty_ReturnsEmpty(t *testing.T) {
	got := sanitizeURL("")
	if got != "" {
		t.Errorf("expected empty for empty input, got %q", got)
	}
}

func TestSanitizeURL_RedactedMarkerPresent(t *testing.T) {
	raw := "wss://mainnet.infura.io/ws/v3/abcdef1234567890abcdef1234"
	got := sanitizeURL(raw)
	if !containsKey(got, "[REDACTED]") {
		t.Errorf("expected [REDACTED] marker in sanitized URL: %q", got)
	}
}

// containsKey is a simple helper to check if a string contains a substring.
func containsKey(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstring(s, substr))
}

func containsSubstring(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
