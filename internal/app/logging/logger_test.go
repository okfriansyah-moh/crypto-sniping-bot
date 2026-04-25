package logging_test

import (
	"bytes"
	"strings"
	"testing"

	"crypto-sniping-bot/internal/app/logging"
)

// ── New ───────────────────────────────────────────────────────────────────────

func TestNew_ReturnsNonNilLogger(t *testing.T) {
	// Act
	logger := logging.New("info", "json")

	// Assert
	if logger == nil {
		t.Error("New returned nil logger")
	}
}

func TestNew_TextFormat_ReturnsLogger(t *testing.T) {
	// Act
	logger := logging.New("info", "text")

	// Assert
	if logger == nil {
		t.Error("New with text format returned nil logger")
	}
}

// ── NewWithWriter ─────────────────────────────────────────────────────────────

func TestNewWithWriter_JSONFormat_WritesJSON(t *testing.T) {
	// Arrange
	var buf bytes.Buffer

	// Act
	logger := logging.NewWithWriter(&buf, "info", "json")
	logger.Info("test_event", "key", "value")

	// Assert
	out := buf.String()
	if !strings.Contains(out, "test_event") {
		t.Errorf("expected log output to contain 'test_event', got: %q", out)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("expected JSON output starting with '{', got: %q", out)
	}
}

func TestNewWithWriter_TextFormat_WritesText(t *testing.T) {
	// Arrange
	var buf bytes.Buffer

	// Act
	logger := logging.NewWithWriter(&buf, "info", "text")
	logger.Info("text_event")

	// Assert
	out := buf.String()
	if !strings.Contains(out, "text_event") {
		t.Errorf("expected log output to contain 'text_event', got: %q", out)
	}
}

func TestNewWithWriter_DebugLevel_FiltersInfoBelow(t *testing.T) {
	// Arrange: set level to error — info messages should be suppressed
	var buf bytes.Buffer

	// Act
	logger := logging.NewWithWriter(&buf, "error", "json")
	logger.Info("should_be_suppressed")

	// Assert
	if buf.Len() > 0 {
		t.Errorf("expected no output for info log at error level, got: %q", buf.String())
	}
}

func TestNewWithWriter_DebugLevel_EmitsDebug(t *testing.T) {
	// Arrange
	var buf bytes.Buffer

	// Act
	logger := logging.NewWithWriter(&buf, "debug", "json")
	logger.Debug("debug_event")

	// Assert
	if !strings.Contains(buf.String(), "debug_event") {
		t.Errorf("expected debug output, got: %q", buf.String())
	}
}

// ── WithTrace ─────────────────────────────────────────────────────────────────

func TestWithTrace_AttachesTraceFields(t *testing.T) {
	// Arrange
	var buf bytes.Buffer
	base := logging.NewWithWriter(&buf, "info", "json")

	// Act
	tl := logging.WithTrace(base, "trace-001", "corr-001", "ver-001")
	tl.Info("traced_event")

	// Assert
	out := buf.String()
	for _, field := range []string{"trace-001", "corr-001", "ver-001"} {
		if !strings.Contains(out, field) {
			t.Errorf("expected log to contain %q, got: %q", field, out)
		}
	}
}

func TestWithTrace_DoesNotModifyBase(t *testing.T) {
	// Arrange
	var baseBuf bytes.Buffer
	base := logging.NewWithWriter(&baseBuf, "info", "json")

	// Act
	tl := logging.WithTrace(base, "t1", "c1", "v1")
	_ = tl
	base.Info("base_event")

	// Assert: base logger should NOT include trace fields
	out := baseBuf.String()
	if strings.Contains(out, "t1") {
		t.Errorf("base logger must not include trace fields added via WithTrace: %q", out)
	}
}

// ── Level parsing (via NewWithWriter behaviour) ───────────────────────────────

func TestParseLevel_WarnSuppressesInfo(t *testing.T) {
	// Arrange
	var buf bytes.Buffer
	logger := logging.NewWithWriter(&buf, "warn", "json")

	// Act
	logger.Info("info_suppressed")
	logger.Warn("warn_visible")

	// Assert
	out := buf.String()
	if strings.Contains(out, "info_suppressed") {
		t.Errorf("info should be suppressed at warn level")
	}
	if !strings.Contains(out, "warn_visible") {
		t.Errorf("warn should be visible at warn level")
	}
}

func TestParseLevel_WarningAlias_SuppressesInfo(t *testing.T) {
	// Arrange: "warning" is an alias for "warn"
	var buf bytes.Buffer
	logger := logging.NewWithWriter(&buf, "warning", "json")

	// Act
	logger.Info("suppressed")
	logger.Warn("visible")

	// Assert
	out := buf.String()
	if strings.Contains(out, "suppressed") {
		t.Errorf("info should be suppressed at warning level")
	}
	if !strings.Contains(out, "visible") {
		t.Errorf("warn should be visible at warning level")
	}
}

func TestParseLevel_UnknownLevel_DefaultsToInfo(t *testing.T) {
	// Arrange: unknown level should default to Info
	var buf bytes.Buffer
	logger := logging.NewWithWriter(&buf, "garbage", "json")

	// Act
	logger.Info("info_event")
	logger.Debug("debug_suppressed")

	// Assert
	out := buf.String()
	if !strings.Contains(out, "info_event") {
		t.Errorf("info should be visible at default (info) level: %q", out)
	}
	if strings.Contains(out, "debug_suppressed") {
		t.Errorf("debug should be suppressed at default (info) level")
	}
}
