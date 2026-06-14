package features

// Phase 11 (Reference-Repo Improvements R2 — FEATURES) — social-link
// presence extractor. Adapted from AxisBot's "is project doxxed?"
// heuristic: a token whose creator metadata advertises ≥1 social link
// (website / twitter / telegram) is materially less likely to be a
// pure rug than one with no social presence at all.
//
// Pure function. No RPC, no database. The caller resolves the metadata
// (Solana: Metaplex Metadata account; EVM: token URI / Etherscan token
// info) and passes the link strings here.

import "strings"

// SocialLinkInputs carries the raw fields. Empty string = "absent".
type SocialLinkInputs struct {
	Website  string
	Twitter  string
	Telegram string
	Discord  string
	Other    []string
}

// ComputeSocialPresence returns (hasAny, count). count is the number of
// non-empty, non-whitespace links among Website/Twitter/Telegram/Discord/Other.
func ComputeSocialPresence(in SocialLinkInputs) (bool, int32) {
	var n int32
	if strings.TrimSpace(in.Website) != "" {
		n++
	}
	if strings.TrimSpace(in.Twitter) != "" {
		n++
	}
	if strings.TrimSpace(in.Telegram) != "" {
		n++
	}
	if strings.TrimSpace(in.Discord) != "" {
		n++
	}
	for _, s := range in.Other {
		if strings.TrimSpace(s) != "" {
			n++
		}
	}
	return n > 0, n
}

// SocialPresenceScore maps a link count to a normalized [0,1] score.
// Saturates at 3 links (diminishing returns). 0 links → 0.0.
func SocialPresenceScore(count int32) float64 {
	if count <= 0 {
		return 0
	}
	if count >= 3 {
		return 1.0
	}
	return float64(count) / 3.0
}
