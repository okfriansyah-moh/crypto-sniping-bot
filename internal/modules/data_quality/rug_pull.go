package data_quality

import (
	"sort"

	"crypto-sniping-bot/contracts"
)

// RugResult is the structured output of the rug-pull detector.
type RugResult struct {
	Score       float64
	Flags       []string
	Unknown     bool
	UnknownFlag string
}

// privilegeWeights expresses the relative severity of an owner-privilege
// selector in a smart contract. Per the anti-manipulation skill, the
// canonical weights are:
//
//	mint        → 1.0   (issuer can dilute holders)
//	blacklist   → 0.9   (issuer can freeze sellers)
//	pause       → 0.7   (issuer can stop trading)
//	set_tax     → 0.7   (issuer can spike sell tax)
//	set_max_tx  → 0.5   (issuer can throttle exits)
//	upgrade     → 0.6   (proxy upgrade trapdoor)
//
// Unknown selectors contribute 0 — anti-pattern would be to invent a weight.
var privilegeWeights = map[string]float64{
	"mint":       1.0,
	"blacklist":  0.9,
	"pause":      0.7,
	"set_tax":    0.7,
	"setmaxtx":   0.5,
	"set_max_tx": 0.5,
	"upgrade":    0.6,
}

// DetectRugPull aggregates rug-pull risk signals from upstream telemetry.
// Pure function — no RPC, no state.
//
// Three independent sub-signals, each clamped to [0,1]:
//
//  1. LP-lock weakness:   1 - LpLockStrength when LpLockKnown.
//  2. Owner privileges:   sum of canonical privilege weights, clamped to 1.
//  3. Holder concentration: Top5HolderPct / cap (cap = 0.40 by default).
//
// When a signal's *Known bit is false, that signal contributes 0 and its
// `dq_unknown_rug_*` code is appended to the unknown flag list. The overall
// detector is Unknown only if every signal is Unknown — in that case the
// orchestrator can degrade per profile.
func DetectRugPull(in contracts.MarketDataDTO, holderConcentrationCap float64) RugResult {
	if holderConcentrationCap <= 0 {
		holderConcentrationCap = 0.40 // skill canonical default
	}

	knownCount := 0

	// ── LP lock ──────────────────────────────────────────────────────────
	lockRisk := 0.0
	if in.LpLockKnown {
		knownCount++
		strength := in.LpLockStrength
		if strength < 0 {
			strength = 0
		}
		if strength > 1 {
			strength = 1
		}
		lockRisk = 1.0 - strength
	}

	// ── Owner privileges ────────────────────────────────────────────────
	privilegeScore := 0.0
	flags := []string{}
	if in.OwnerPrivilegesKnown {
		knownCount++
		privilegeScore = computePrivilegeScore(in.OwnerPrivileges)
		if privilegeScore > 0.5 {
			flags = append(flags, "OWNER_PRIVILEGED")
		}
	}

	// ── Solana mint/freeze authority (additive — not all chains) ────────
	if in.SolanaAuthoritiesKnown {
		knownCount++
		if !in.MintAuthorityRenounced {
			privilegeScore += 0.4
			flags = append(flags, "SOLANA_MINT_AUTH")
		}
		if !in.FreezeAuthorityRenounced {
			privilegeScore += 0.3
			flags = append(flags, "SOLANA_FREEZE_AUTH")
		}
	}
	if privilegeScore > 1 {
		privilegeScore = 1
	}

	// ── Holder concentration ────────────────────────────────────────────
	concentrationRisk := 0.0
	if in.HolderDistKnown {
		knownCount++
		ratio := in.Top5HolderPct / holderConcentrationCap
		if ratio < 0 {
			ratio = 0
		}
		if ratio > 1 {
			ratio = 1
		}
		concentrationRisk = ratio
		if in.Top5HolderPct > holderConcentrationCap {
			flags = append(flags, "HOLDER_CONCENTRATED")
		}
	}

	// Average the available signals — equal weight inside the rug detector.
	score := 0.0
	div := 0
	if in.LpLockKnown {
		score += lockRisk
		div++
	}
	if in.OwnerPrivilegesKnown || in.SolanaAuthoritiesKnown {
		score += privilegeScore
		div++
	}
	if in.HolderDistKnown {
		score += concentrationRisk
		div++
	}
	if div > 0 {
		score /= float64(div)
	}
	if score > 1 {
		score = 1
	}

	if in.LpLockKnown && in.LpLockStrength < 0.10 {
		flags = append(flags, "LP_UNLOCKED")
	}

	sort.Strings(flags)

	if knownCount == 0 {
		return RugResult{Unknown: true, UnknownFlag: "dq_unknown_rug"}
	}
	return RugResult{Score: score, Flags: flags}
}

// computePrivilegeScore maps owner-privilege selectors to a [0,1] severity.
// Unknown selectors contribute zero (anti-pattern: never invent a weight).
func computePrivilegeScore(privileges []string) float64 {
	score := 0.0
	for _, p := range privileges {
		score += privilegeWeights[p]
	}
	if score > 1 {
		score = 1
	}
	return score
}
