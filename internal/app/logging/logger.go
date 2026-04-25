package logging

import (
	"io"
	"log/slog"
	"os"
	"strings"
)

// New creates a new structured logger based on the configured level and format.
// All logs are JSON by default (format: "json") or human-readable text (format: "text").
// Unstructured console output is forbidden — use this logger everywhere.
//
// The logger emits structured key-value pairs.
// Use slog.With() to attach trace_id, correlation_id, version_id to a logger instance.
func New(level, format string) *slog.Logger {
	lvl := parseLevel(level)
	opts := &slog.HandlerOptions{Level: lvl}

	var handler slog.Handler
	switch strings.ToLower(format) {
	case "text":
		handler = slog.NewTextHandler(os.Stdout, opts)
	default:
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}

	return slog.New(handler)
}

// NewWithWriter creates a logger that writes to w — used in tests.
func NewWithWriter(w io.Writer, level, format string) *slog.Logger {
	lvl := parseLevel(level)
	opts := &slog.HandlerOptions{Level: lvl}

	var handler slog.Handler
	switch strings.ToLower(format) {
	case "text":
		handler = slog.NewTextHandler(w, opts)
	default:
		handler = slog.NewJSONHandler(w, opts)
	}

	return slog.New(handler)
}

// WithTrace attaches the mandatory traceability fields to a logger.
// Every log entry in the pipeline MUST include these fields.
//
// Usage:
//
//	logger := logging.WithTrace(base, traceID, correlationID, versionID)
//	logger.Info("stage_started")
func WithTrace(logger *slog.Logger, traceID, correlationID, versionID string) *slog.Logger {
	return logger.With(
		"trace_id", traceID,
		"correlation_id", correlationID,
		"version_id", versionID,
	)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
