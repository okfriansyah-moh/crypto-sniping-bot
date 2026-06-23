package telegram

import (
	"errors"
	"io"
	"testing"
)

// ── isPollerTransient ─────────────────────────────────────────────────────────

func TestIsPollerTransient_Nil_ReturnsFalse(t *testing.T) {
	// Arrange / Act / Assert
	if isPollerTransient(nil) {
		t.Error("isPollerTransient(nil): want false, got true")
	}
}

func TestIsPollerTransient_EOF_ReturnsTrue(t *testing.T) {
	// Arrange / Act / Assert
	if !isPollerTransient(io.EOF) {
		t.Error("isPollerTransient(io.EOF): want true, got false")
	}
}

func TestIsPollerTransient_EOFString_ReturnsTrue(t *testing.T) {
	// Arrange
	err := errors.New("read tcp: EOF")

	// Act / Assert
	if !isPollerTransient(err) {
		t.Error("isPollerTransient(message containing EOF): want true, got false")
	}
}

func TestIsPollerTransient_ConnectionReset_ReturnsTrue(t *testing.T) {
	// Arrange
	err := errors.New("read tcp: connection reset by peer")

	// Act / Assert
	if !isPollerTransient(err) {
		t.Error("isPollerTransient(connection reset): want true, got false")
	}
}

func TestIsPollerTransient_ClosedNetwork_ReturnsTrue(t *testing.T) {
	// Arrange
	err := errors.New("read tcp: use of closed network connection")

	// Act / Assert
	if !isPollerTransient(err) {
		t.Error("isPollerTransient(closed network): want true, got false")
	}
}

func TestIsPollerTransient_UnrelatedError_ReturnsFalse(t *testing.T) {
	// Arrange
	err := errors.New("unexpected status code 429")

	// Act / Assert
	if isPollerTransient(err) {
		t.Error("isPollerTransient(non-transient): want false, got true")
	}
}

// ── sanitizeToken ─────────────────────────────────────────────────────────────

func TestSanitizeToken_Nil_ReturnsNil(t *testing.T) {
	// Arrange / Act / Assert
	if got := sanitizeToken(nil, "mytoken"); got != nil {
		t.Errorf("sanitizeToken(nil, token): want nil, got %v", got)
	}
}

func TestSanitizeToken_EmptyToken_ReturnsOriginalError(t *testing.T) {
	// Arrange
	original := errors.New("some error with token123")

	// Act
	got := sanitizeToken(original, "")

	// Assert: empty token should not modify the error.
	if got != original {
		t.Errorf("sanitizeToken with empty token: want original error, got %v", got)
	}
}

func TestSanitizeToken_TokenPresentInMessage_IsRedacted(t *testing.T) {
	// Arrange
	secret := "abc123secrettoken"
	err := errors.New("failed to call https://api.telegram.org/bot" + secret + "/getUpdates")

	// Act
	sanitized := sanitizeToken(err, secret)

	// Assert: the secret must not appear in the sanitized error message.
	if sanitized == nil {
		t.Fatal("sanitizeToken: want non-nil error")
	}
	if msg := sanitized.Error(); containsStr(msg, secret) {
		t.Errorf("sanitizeToken: secret leaked in error message: %q", msg)
	}
}

func TestSanitizeToken_TokenNotInMessage_ReturnsOriginal(t *testing.T) {
	// Arrange: error message does not contain the token.
	original := errors.New("network timeout")

	// Act
	got := sanitizeToken(original, "mytoken999")

	// Assert: original error must be returned unchanged.
	if got != original {
		t.Errorf("sanitizeToken: want original error when token not in message")
	}
}

// ── helpText ──────────────────────────────────────────────────────────────────

func TestHelpText_NonEmpty(t *testing.T) {
	// Arrange / Act
	text := helpText()

	// Assert: must return a non-empty help string.
	if text == "" {
		t.Error("helpText(): want non-empty string, got empty")
	}
}

func TestHelpText_ContainsStatusCommand(t *testing.T) {
	// Arrange / Act
	text := helpText()

	// Assert: /status must always be documented.
	if !containsStr(text, "/status") {
		t.Error("helpText() does not mention /status command")
	}
}

// containsStr is a package-local helper to avoid importing strings in test.
func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}
