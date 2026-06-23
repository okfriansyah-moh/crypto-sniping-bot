package selection

// Phase 11 (Reference-Repo Improvements R2 — SELECT) — per-creator
// dedup gate. mux/solana-trading-bot enforces "at most K open positions
// per dev wallet" to avoid concentrated rug exposure when a single
// creator launches multiple tokens in a window.
//
// Pure function. Caller supplies:
//   * candidates  — already-ranked SelectionOutputDTOs (Selected=true).
//   * openByCreator — current count of open positions per creator.
//   * maxPerCreator — config cap; 0 disables the gate.
//
// Returns the same slice with rejected entries flipped to Selected=false
// and RejectReason=RejectReasonCreatorAlreadyOpen. Order is preserved
// (deterministic). Multiple candidates from the same creator within
// the same batch share the cap counter.

import "crypto-sniping-bot/shared/contracts"

const RejectReasonCreatorAlreadyOpen = "creator_already_open"

// FilterByCreatorOpenPositions enforces MaxPositionsPerCreator across
// the candidate set. Returns the modified slice (same backing array).
func FilterByCreatorOpenPositions(
	candidates []contracts.SelectionOutputDTO,
	openByCreator map[string]int32,
	maxPerCreator int32,
) []contracts.SelectionOutputDTO {
	if maxPerCreator <= 0 || len(candidates) == 0 {
		return candidates
	}
	// Local counter that combines existing + this-batch picks.
	counts := make(map[string]int32, len(openByCreator))
	for k, v := range openByCreator {
		counts[k] = v
	}
	for i := range candidates {
		c := &candidates[i]
		if !c.Selected || c.CreatorAddress == "" {
			continue
		}
		if counts[c.CreatorAddress] >= maxPerCreator {
			c.Selected = false
			if c.RejectReason == "" {
				c.RejectReason = RejectReasonCreatorAlreadyOpen
			}
			continue
		}
		counts[c.CreatorAddress]++
	}
	return candidates
}
