package learning

// Phase 11 (Reference-Repo Improvements R2 — LEARN) — pure helpers
// that decide when a confirmed-rug LearningRecord should produce a
// creator-blacklist observation. The orchestrator owns the database
// write — this module only builds the immutable observation value.
//
// Adapted from m8s-lab/solana-sniping-bot's "burn list" of repeat-rug
// dev wallets. Sample-gated and threshold-bounded per the Phase 11
// learning-safety rule (config-driven).
//
// Module purity rule: this file does NOT import database/. The
// orchestrator translates CreatorRugObservation into the database
// adapter's type at the boundary.

import "crypto-sniping-bot/contracts"

// CreatorRugObservation is the pure value the learning module emits.
// The orchestrator maps it into database.CreatorRugObservation before
// calling the adapter.
type CreatorRugObservation struct {
	CreatorAddress    string
	Chain             string
	TokenAddress      string
	StrategyVersionID string
}

// IsConfirmedRug returns true when a LearningRecord represents a
// confirmed rug outcome that should bump the creator's rug counter.
//
// Definition: Outcome=="RUG" AND not Shadow AND not Simulated
// (we never blacklist on shadow trades — they're observation-only).
func IsConfirmedRug(rec contracts.LearningRecordDTO) bool {
	if rec.Shadow || rec.Simulated {
		return false
	}
	return rec.Outcome == "RUG"
}

// BuildCreatorRugObservation extracts the creator + chain identity
// from a LearningRecord. Returns (zero, false) when the record lacks a
// creator address or chain. The caller MUST NOT persist a zero value.
func BuildCreatorRugObservation(rec contracts.LearningRecordDTO, chain string) (CreatorRugObservation, bool) {
	creator := rec.EdgeSnapshot.CreatorAddress
	if creator == "" || chain == "" {
		return CreatorRugObservation{}, false
	}
	return CreatorRugObservation{
		CreatorAddress:    creator,
		Chain:             chain,
		TokenAddress:      rec.EdgeSnapshot.TokenAddress,
		StrategyVersionID: rec.VersionID,
	}, true
}
