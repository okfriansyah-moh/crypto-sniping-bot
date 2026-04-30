// Phase 10 (Reference-Repo Improvements / Task D) — consecutive-pass
// debounce gate. Mirrors the CONSECUTIVE_FILTER_MATCHES pattern from
// the mux Solana sniper: an edge must pass the EV/latency checks N
// times in a row within a configured window before being promoted.
//
// State (count + window start) is owned by the orchestrator/worker; the
// module is pure — it reads PriorPassState and returns updated counters
// on the resulting ValidatedEdgeDTO.

package validation

import (
	"context"
	"fmt"
	"time"

	"crypto-sniping-bot/contracts"
)

// PriorPassState is the carry-over input for the consecutive-pass gate.
// Loaded by the worker from a side table keyed by token_lifecycle_id.
type PriorPassState struct {
	Count       int32
	WindowStart string // ISO 8601 UTC; "" = no prior window
}

// ProcessWithDebounce evaluates an EdgeDTO and applies the consecutive-pass
// debounce gate. evalAt is the explicit evaluation timestamp (replay-safe).
//
// Behaviour:
//   - If RequiredConsecutivePasses <= 1, the gate is disabled and the
//     output is identical to Process() (counters still emitted as 1/now
//     on PASS for observability).
//   - On a base PASS, increment the counter (resetting the window if it
//     has expired). When the counter is below the required threshold,
//     emit Decision="REJECT" with RejectReason="consecutive_pass_pending".
//   - On a base REJECT, reset the counter to 0 / window to "".
func (m *Module) ProcessWithDebounce(
	ctx context.Context,
	in contracts.EdgeDTO,
	prior PriorPassState,
	evalAt time.Time,
) (contracts.ValidatedEdgeDTO, error) {
	out, err := m.Process(ctx, in)
	if err != nil {
		return out, err
	}

	required := int32(0)
	windowSec := int32(0)
	if m.cfg != nil {
		required = m.cfg.RequiredConsecutivePasses
		windowSec = m.cfg.ConsecutivePassWindowSeconds
	}

	now := evalAt.UTC().Format(time.RFC3339Nano)

	if out.Decision == "REJECT" {
		// Base reject: clear counters.
		out.ConsecutivePassCount = 0
		out.ConsecutivePassWindowStart = ""
		return out, nil
	}

	// Base PASS — apply window logic.
	count := prior.Count
	windowStart := prior.WindowStart
	if windowStart == "" {
		count = 1
		windowStart = now
	} else if windowSec > 0 {
		ws, parseErr := time.Parse(time.RFC3339Nano, windowStart)
		if parseErr != nil || evalAt.Sub(ws) > time.Duration(windowSec)*time.Second {
			// Window expired — restart.
			count = 1
			windowStart = now
		} else {
			count++
		}
	} else {
		// No window cap configured.
		count++
	}

	out.ConsecutivePassCount = count
	out.ConsecutivePassWindowStart = windowStart

	if required > 1 && count < required {
		// Pending — downgrade to REJECT.
		out.Decision = "REJECT"
		out.RejectReason = fmt.Sprintf("consecutive_pass_pending:%d/%d", count, required)
		out.LatencyGatePassed = false
		// Re-derive EventID so dedupe is keyed on the pending state.
		out.EventID = contracts.ContentIDFromString(
			fmt.Sprintf("validated:%s:PENDING:%d", in.EventID, count))
	}

	return out, nil
}
