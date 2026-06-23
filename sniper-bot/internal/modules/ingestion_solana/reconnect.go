package ingestion_solana

// reconnect.go — exponential backoff with full jitter for Solana reconnects.

import (
	"math"
	"math/rand"
	"time"

	"crypto-sniping-bot/internal/app/config"
)

// nextDelay returns the backoff delay for a given attempt number.
// Uses full jitter: delay = random(0, min(max_ms, initial_ms * multiplier^attempt))
// Full jitter prevents thundering-herd on reconnects.
func nextDelay(cfg config.IngestionBackoff, attempt int) time.Duration {
	initial := float64(cfg.InitialMs)
	maximum := float64(cfg.MaxMs)
	multiplier := cfg.Multiplier
	if multiplier <= 1.0 {
		multiplier = 2.0
	}

	cap := math.Min(maximum, initial*math.Pow(multiplier, float64(attempt)))
	// Full jitter: uniform random in [0, cap]
	jittered := rand.Float64() * cap //nolint:gosec // non-crypto backoff jitter
	return time.Duration(jittered) * time.Millisecond
}
