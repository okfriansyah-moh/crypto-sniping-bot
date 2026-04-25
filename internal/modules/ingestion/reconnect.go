package ingestion

import "time"

// BackoffConfig holds exponential backoff parameters for RPC reconnects.
type BackoffConfig struct {
	InitialMs  int     // starting delay in milliseconds
	MaxMs      int     // maximum delay cap in milliseconds
	Multiplier float64 // delay growth factor per attempt
}

// defaultBackoff is used when no explicit BackoffConfig is provided.
var defaultBackoff = BackoffConfig{InitialMs: 100, MaxMs: 30000, Multiplier: 2.0}

// NextDelay returns the exponential backoff delay for the nth attempt (0-indexed).
// Delay = min(InitialMs × Multiplier^attempt, MaxMs).
func NextDelay(cfg BackoffConfig, attempt int) time.Duration {
	if cfg.InitialMs <= 0 {
		cfg = defaultBackoff
	}
	delay := float64(cfg.InitialMs)
	for i := 0; i < attempt; i++ {
		delay *= cfg.Multiplier
		if delay >= float64(cfg.MaxMs) {
			delay = float64(cfg.MaxMs)
			break
		}
	}
	return time.Duration(delay) * time.Millisecond
}

// SelectEndpoint returns endpoints[attempt % len(endpoints)] for round-robin failover.
// Returns "" if endpoints is empty.
func SelectEndpoint(endpoints []string, attempt int) string {
	if len(endpoints) == 0 {
		return ""
	}
	return endpoints[attempt%len(endpoints)]
}
