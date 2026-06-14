// Package state_machine — quarantine helpers.
// Quarantine is triggered after N consecutive CAS violations on the same token.
package state_machine

import (
	"context"
	"fmt"
	"log/slog"
)

// Quarantiner is a narrow interface for token quarantine operations.
// Workers inject a concrete adapter; the module only sees this interface.
type Quarantiner interface {
	QuarantineToken(ctx context.Context, tokenAddress string, reason string) error
}

// QuarantineChecker tracks CAS violation counts per lifecycle and triggers
// quarantine when the configured threshold is exceeded.
type QuarantineChecker struct {
	threshold  int
	violations map[string]int // lifecycleID → violation count
	q          Quarantiner
	logger     *slog.Logger
}

// NewQuarantineChecker returns a QuarantineChecker.
// threshold is the number of violations before QuarantineToken is called.
func NewQuarantineChecker(threshold int, q Quarantiner, logger *slog.Logger) *QuarantineChecker {
	if logger == nil {
		logger = slog.Default()
	}
	return &QuarantineChecker{
		threshold:  threshold,
		violations: make(map[string]int),
		q:          q,
		logger:     logger,
	}
}

// RecordViolation increments the violation count for lifecycleID.
// When the count reaches the threshold, QuarantineToken is called.
// Returns an error only when quarantine itself fails.
func (q *QuarantineChecker) RecordViolation(ctx context.Context, lifecycleID, tokenAddress, reason string) error {
	q.violations[lifecycleID]++
	count := q.violations[lifecycleID]

	q.logger.Warn("state_machine_violation",
		"lifecycle_id", lifecycleID,
		"token", tokenAddress,
		"reason", reason,
		"violation_count", count,
		"threshold", q.threshold,
	)

	if count >= q.threshold {
		quarantineReason := fmt.Sprintf("violation_threshold_exceeded:%s", reason)
		if err := q.q.QuarantineToken(ctx, tokenAddress, quarantineReason); err != nil {
			q.logger.Error("quarantine_failed",
				"lifecycle_id", lifecycleID,
				"token", tokenAddress,
				"error", err,
			)
			return fmt.Errorf("quarantine token %s: %w", tokenAddress, err)
		}
		// Reset after quarantine so a re-listed token starts fresh.
		delete(q.violations, lifecycleID)
		q.logger.Warn("token_quarantined",
			"token", tokenAddress,
			"reason", quarantineReason,
		)
	}
	return nil
}

// ViolationCount returns the current violation count for a lifecycle.
func (q *QuarantineChecker) ViolationCount(lifecycleID string) int {
	return q.violations[lifecycleID]
}
