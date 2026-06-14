package workers

import (
	"context"
	"log/slog"
	"time"
)

// baselineFlushDefaults are the fallbacks applied when the corresponding
// config key is 0. The validator (validate_ranges.go) rejects positive
// values below the minimums — 0 means "unset, use default".
const (
	defaultBaselineFlushIntervalSec = 30
	defaultBaselineFlushMaxWrites   = 100
)

// baselineFlushKey mirrors features.BaselineKey / edge.BaselineKey at
// the worker layer so the shared loop is package-agnostic.
type baselineFlushKey struct {
	Market string
	Signal string
}

// baselineFlusher is the integration surface between a module's
// BaselineStore and the database adapter. The features and edge workers
// each implement this against their own store + the shared adapter.
type baselineFlusher interface {
	// dirtyKeys returns the current dirty (market, signal) tuples in
	// deterministic order. Does NOT clear the dirty set.
	dirtyKeys() []baselineFlushKey
	// flushKey persists a single (market, signal) entry. On success it
	// MUST clear the dirty bit for that key. On error it MUST leave the
	// dirty bit set so the next cycle retries.
	flushKey(ctx context.Context, market, signal string) error
}

// runBaselineFlushLoop is the debounced, bounded-throughput flush loop
// shared by the features and edge workers. Blocks until ctx.Done.
//
// Per cycle:
//  1. Read the dirty key set (sorted, deterministic).
//  2. Write up to maxWrites entries via flushKey. Each successful write
//     clears its dirty bit; failed writes stay dirty for the next cycle.
//  3. If more keys were dirty than maxWrites, log a single warn — the
//     remainder is naturally picked up on the next tick.
//
// Persistence is best-effort: a failed SaveBaseline logs and continues.
// The worker's hot path is never blocked.
func runBaselineFlushLoop(
	ctx context.Context,
	logger *slog.Logger,
	module string,
	intervalSec, maxWrites int,
	f baselineFlusher,
) {
	if intervalSec <= 0 {
		intervalSec = defaultBaselineFlushIntervalSec
	}
	if maxWrites <= 0 {
		maxWrites = defaultBaselineFlushMaxWrites
	}
	ticker := time.NewTicker(time.Duration(intervalSec) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			flushBaselineCycle(ctx, logger, module, maxWrites, f)
		}
	}
}

// flushBaselineCycle runs one flush iteration. Returns counts for tests.
func flushBaselineCycle(
	ctx context.Context,
	logger *slog.Logger,
	module string,
	maxWrites int,
	f baselineFlusher,
) (writes int, deferred int) {
	keys := f.dirtyKeys()
	if len(keys) == 0 {
		return 0, 0
	}
	limit := len(keys)
	if limit > maxWrites {
		limit = maxWrites
		deferred = len(keys) - maxWrites
	}
	for i := 0; i < limit; i++ {
		if err := ctx.Err(); err != nil {
			return writes, deferred
		}
		k := keys[i]
		if err := f.flushKey(ctx, k.Market, k.Signal); err != nil {
			logger.Warn("baseline_flush_failed",
				"module", module,
				"market", k.Market,
				"signal", k.Signal,
				"error", err,
			)
			continue
		}
		writes++
	}
	if deferred > 0 {
		logger.Warn("baseline_flush_throttled",
			"module", module,
			"writes", writes,
			"deferred", deferred,
			"max_writes", maxWrites,
		)
	}
	return writes, deferred
}
