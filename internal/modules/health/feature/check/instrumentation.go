package check

import "log/slog"

// Instrumentation provides observability for the health check feature.

// LogCheck logs a health check execution.
func LogCheck(status string) {
	slog.Info("health_check_executed", "status", status)
}
