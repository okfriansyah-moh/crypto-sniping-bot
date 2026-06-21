// Package data_quality implements Layer 1: Data Quality Engine.
// Consumes MarketDataDTO and emits DataQualityDTO.
// No database imports — pure business logic only.
package data_quality

import (
	"context"
	"fmt"
	"log/slog"
	"math/big"
	"sort"
	"time"

	"crypto-sniping-bot/shared/contracts"
	"crypto-sniping-bot/internal/app/config"
	dqproviders "crypto-sniping-bot/sniper-bot/internal/modules/data_quality/providers"
)

// Config holds data quality thresholds loaded from pipeline.yaml.
// All values sourced from config — no hardcoded magic numbers.
type Config struct {
	MaxBuyTaxBps      int32
	MaxSellTaxBps     int32
	MinLPHolderCount  int32
	MinReserveBaseWei string
}

// DefaultConfig returns safe defaults that align with pipeline.yaml.
// Phase 9 (§ 9.1): when a top-level Config is supplied, threshold values
// are sourced from cfg.DataQualityRuntime.Thresholds (mirrors
// config/data_quality.yaml). Module-side defaults remain only as fallback
// for tests / partial configs.
func DefaultConfig(cfg *config.Config) Config {
	out := Config{
		MaxBuyTaxBps:      1000, // 10%
		MaxSellTaxBps:     1500, // 15%
		MinLPHolderCount:  1,
		MinReserveBaseWei: "1000000000000000", // 0.001 ETH in wei
	}
	if cfg == nil {
		return out
	}
	t := cfg.DataQualityRuntime.Thresholds
	if t.TaxBuyMaxBps > 0 {
		out.MaxBuyTaxBps = t.TaxBuyMaxBps
	}
	if t.TaxSellMaxBps > 0 {
		out.MaxSellTaxBps = t.TaxSellMaxBps
	}
	return out
}

// CreatorProfileReader is the read-only interface for creator history lookups.
// It is defined in the consuming module per Go dependency-inversion idiom.
// The adapter-backed implementation lives outside this package (internal/workers/).
// Fail-closed contract: when known==false or err!=nil, callers MUST NOT change
// existing CreatorPrevTokenCountKnown semantics — the serial-launcher check
// degrades as if no profile data were available.
type CreatorProfileReader interface {
	GetCount(ctx context.Context, chain, creator string) (count int32, known bool, err error)
}

// Module is the data quality engine.
// It is a pure function: no state, no DB, no side effects on shared mutable state.
type Module struct {
	cfg                  Config
	runtime              *config.DataQualityRuntimeConfig // Phase 9 (§ 9.1) — optional runtime config.
	logger               *slog.Logger
	providers            *dqproviders.Aggregator // optional external provider layer (P1)
	creatorProfileReader CreatorProfileReader    // optional creator profile reader (Task 9)
}

// New creates a new data quality Module.
func New(cfg Config, logger *slog.Logger) *Module {
	if logger == nil {
		logger = slog.Default()
	}
	return &Module{cfg: cfg, logger: logger}
}

// WithRuntimeConfig attaches Phase 9 detector toggles, weights, and
// thresholds (mirrors config/data_quality.yaml). Returns the receiver for
// fluent wiring.
func (m *Module) WithRuntimeConfig(rt *config.DataQualityRuntimeConfig) *Module {
	m.runtime = rt
	return m
}

// WithProviders attaches an optional external provider Aggregator (P1).
// When non-nil the aggregator is called after internal detectors and its
// ExternalRiskScore blends into the final RiskScore unless shadow_mode is true.
// Calling with nil is a no-op (providers disabled).
func (m *Module) WithProviders(agg *dqproviders.Aggregator) *Module {
	m.providers = agg
	return m
}

// WithCreatorProfileReader attaches an optional reader for the creator_profiles
// table (Task 9). When non-nil, ProcessForMode augments the serial-launcher
// check with the accumulated per-wallet token count from the persistent table.
// Calling with nil is a no-op — the module falls back to probe-only semantics.
func (m *Module) WithCreatorProfileReader(r CreatorProfileReader) *Module {
	m.creatorProfileReader = r
	return m
}

// Process evaluates a MarketDataDTO and returns a DataQualityDTO.
//
// This is the back-compat entry point — it routes through ProcessForMode
// using the BALANCED profile. Production callers (the worker) MUST use
// ProcessForMode and supply the active SystemState.Mode so the decision
// band reflects the operator's risk posture.
//
// Deterministic: same input → same output.
func (m *Module) Process(ctx context.Context, in contracts.MarketDataDTO) (contracts.DataQualityDTO, error) {
	return m.ProcessForMode(ctx, in, "BALANCED")
}

// ProcessForMode evaluates a MarketDataDTO under the supplied operational
// mode (STRICT / BALANCED / EXPLORATION / VERY_EXPLORATION). All five canonical detectors run.
// Detectors whose upstream inputs are not populated emit a `dq_unknown_*`
// flag and degrade per the active profile's UnknownFactor:
//   - STRICT       → unknown counts as half-weight risk
//   - BALANCED     → unknown is neutral (no contribution)
//   - EXPLORATION  → unknown is ignored
//
// Hard-reject flags (HONEYPOT_SELL_FAIL, SELL_BLOCKED, HONEYPOT_BUY_FAIL)
// always force Decision = REJECT regardless of the aggregated score.
//
// Task 13 — Mode-aware serial launcher:
// STRICT/BALANCED (profile.MaxCreatorPrevTokenCount==0) → hard REJECT (unchanged).
// EXPLORATION/VERY_EXPLORATION (profile.MaxCreatorPrevTokenCount>0) → RISKY_PASS or SKIP.
// SKIP outcome must NOT emit a data_quality_event and MUST NOT contribute to reject-rate.
func (m *Module) ProcessForMode(ctx context.Context, in contracts.MarketDataDTO, mode string) (contracts.DataQualityDTO, error) {
	now := time.Now().UTC().Format(time.RFC3339Nano)

	profileName, profile := resolveProfile(mode, m.runtime)
	shadowMode := m.runtime != nil && m.runtime.Providers.ShadowMode
	slProfile := effectiveSerialLauncherProfile(profile, shadowMode)

	// serialLauncherPendingRiskCheck is set to true when an EXPLORATION/
	// VERY_EXPLORATION token has passed the non-risk quality gates and is
	// awaiting the post-aggregation riskScore gate (SerialLauncherMaxRiskScore).
	var serialLauncherPendingRiskCheck bool

	// ── Structural rejects (cheap pre-checks) ───────────────────────────
	var rejectReasons []string
	flags := []string{}

	isNewLaunch := in.EventTopic == "PumpFunCreate"

	if !isNewLaunch && (in.ReserveBaseRaw == "" || in.ReserveBaseRaw == "0") {
		rejectReasons = append(rejectReasons, "missing_reserves")
	}
	isFakeLiquidityStructural := false
	if !isNewLaunch && in.ReserveBaseRaw != "" && in.ReserveBaseRaw != "0" {
		reserveBase, ok := new(big.Int).SetString(in.ReserveBaseRaw, 10)
		minReserve, _ := new(big.Int).SetString(m.cfg.MinReserveBaseWei, 10)
		if ok && minReserve != nil && reserveBase.Cmp(minReserve) < 0 {
			isFakeLiquidityStructural = true
			rejectReasons = append(rejectReasons, "insufficient_liquidity")
		}
	}

	if in.Reorged {
		rejectReasons = append(rejectReasons, "reorged_event")
	}
	if in.TokenAddress == "" {
		rejectReasons = append(rejectReasons, "missing_token_address")
	}
	if m.runtime != nil &&
		m.runtime.Thresholds.MaxBondingCurveProgressBps > 0 &&
		in.BondingCurveProgressBps > m.runtime.Thresholds.MaxBondingCurveProgressBps {
		rejectReasons = append(rejectReasons, "bonding_curve_too_advanced")
	}
	if m.runtime != nil &&
		m.runtime.Thresholds.MaxTotalSupply > 0 &&
		in.TotalSupplyKnown &&
		in.TotalSupply > m.runtime.Thresholds.MaxTotalSupply {
		rejectReasons = append(rejectReasons, "high_total_supply")
	}
	if m.runtime != nil &&
		m.runtime.Thresholds.MaxTotalSupply > 0 &&
		!in.TotalSupplyKnown {
		if m.runtime.Thresholds.RejectUnknownTotalSupply {
			// Fail-closed: LP probe didn't run (RPC unhealthy, bonding-curve
			// unreadable, etc.). Reject rather than pass a token whose supply
			// may exceed the configured limit. Use reject_unknown_total_supply=false
			// on chains where supply is legitimately unfetchable.
			rejectReasons = append(rejectReasons, "unknown_total_supply")
		} else {
			// Soft path: log and continue. Operators should monitor this
			// event to detect LP-probe failures before they accumulate.
			m.logger.Warn("dq_total_supply_unknown",
				"token_address", in.TokenAddress,
				"max_total_supply_threshold", m.runtime.Thresholds.MaxTotalSupply,
			)
		}
	}
	// Creator-profile enrichment (Task 9): augment CreatorPrevTokenCount
	// from the persistent creator_profiles table before the serial-launcher
	// check. This path runs ONLY when the injected reader is present and the
	// creator address is known. Fail-closed: any error or known==false leaves
	// CreatorPrevTokenCountKnown unchanged — the downstream check degrades
	// as if no profile data were available.
	if m.creatorProfileReader != nil && in.CreatorAddress != "" {
		profileCount, profileKnown, profileErr := m.creatorProfileReader.GetCount(ctx, in.Chain, in.CreatorAddress)
		if profileErr != nil {
			m.logger.Warn("dq_creator_profile_read_failed",
				"token", in.TokenAddress,
				"creator", in.CreatorAddress,
				"chain", in.Chain,
				"error", profileErr,
			)
			// Fail-closed: leave CreatorPrevTokenCountKnown unchanged.
		} else if profileKnown {
			// Use the larger of probe count vs persistent profile count.
			// The profile aggregator counts every ingested market_data_event
			// for this creator — it may already know about more launches than
			// the probe reported (especially for non-pump.fun creators).
			if profileCount > in.CreatorPrevTokenCount {
				in.CreatorPrevTokenCount = profileCount
				in.CreatorPrevTokenCountKnown = true
			} else if !in.CreatorPrevTokenCountKnown && profileCount > 0 {
				// Profile knows this creator even if the probe did not set Known.
				in.CreatorPrevTokenCount = profileCount
				in.CreatorPrevTokenCountKnown = true
			}
		}
		// profileKnown==false: leave CreatorPrevTokenCountKnown unchanged (fail-closed).
	}

	// ── Mode-aware serial launcher check (Task 13) ───────────────────────
	// effectiveMax is the per-mode serial-launcher threshold.
	//   STRICT/BALANCED:         profile.MaxCreatorPrevTokenCount==0 → use global (=1)
	//   EXPLORATION:             profile.MaxCreatorPrevTokenCount==5
	//   VERY_EXPLORATION:        profile.MaxCreatorPrevTokenCount==10
	//
	// STRICT/BALANCED behaviour is UNCHANGED: hard REJECT for known serial
	// launchers and fail-closed REJECT for unknown creator history.
	//
	// EXPLORATION/VERY_EXPLORATION: tokens whose creator count ≥ effectiveMax
	// are evaluated against quality gates:
	//   • Social links confirmed (HasSocialLinks+SocialLinksKnown=true)
	//   • HolderCount ≥ SerialLauncherMinHolderCount
	//   • RiskScore < SerialLauncherMaxRiskScore  ← deferred to post-aggregation
	// All non-risk gates pass → set serialLauncherPendingRiskCheck=true and continue.
	// Any non-risk gate fails → return buildSkipResult immediately (SKIP, no event).
	// Unknown creator in EXPLORATION → SKIP (not a quality failure, not a reject).
	if m.runtime != nil && m.runtime.Thresholds.MaxCreatorPrevTokenCount > 0 {
		effectiveMax := slProfile.MaxCreatorPrevTokenCount
		usingGlobal := effectiveMax == 0
		if usingGlobal {
			effectiveMax = m.runtime.Thresholds.MaxCreatorPrevTokenCount
		}

		if in.CreatorPrevTokenCountKnown && in.CreatorPrevTokenCount >= effectiveMax {
			if usingGlobal {
				// STRICT/BALANCED: hard REJECT — unchanged from prior behaviour.
				// The dev-reputation weight alone (0.25) cannot push the aggregate
				// over the BALANCED 0.50 barrier, so a structural reject is required.
				rejectReasons = append(rejectReasons, "serial_launcher")
			} else {
				// EXPLORATION/VERY_EXPLORATION: check non-risk quality gates.
				socialLinksOK := !slProfile.SerialLauncherRequiresSocialLinks ||
					(in.SocialLinksKnown && in.HasSocialLinks)
				holderCountOK := slProfile.SerialLauncherMinHolderCount == 0 ||
					(in.HolderDistKnown && in.HolderCount >= slProfile.SerialLauncherMinHolderCount)

				if !socialLinksOK || !holderCountOK {
					if evaluateMomentumOverride(in, slProfile.SerialLauncherMomentumOverride) {
						serialLauncherPendingRiskCheck = true
					} else {
						var skipFlag string
						switch {
						case !socialLinksOK:
							skipFlag = contracts.FlagSerialLauncherSkippedNoSocial
						case !in.HolderDistKnown && in.CreatorAddress == "":
							skipFlag = contracts.FlagSerialLauncherSkippedCreatorUnknown
						case !in.HolderDistKnown:
							skipFlag = contracts.FlagSerialLauncherSkippedHolderUnknown
						default:
							skipFlag = contracts.FlagSerialLauncherSkippedLowHolders
						}
						m.logger.Info("serial_launcher_skip",
							"token", in.TokenAddress,
							"chain", in.Chain,
							"creator", in.CreatorAddress,
							"creator_count", in.CreatorPrevTokenCount,
							"effective_max", effectiveMax,
							"social_links_ok", socialLinksOK,
							"holder_count_ok", holderCountOK,
							"skip_flag", skipFlag,
							"mode", profileName,
						)
						return buildSkipResult(in, []string{skipFlag}, profileName), nil
					}
				} else {
					// Non-risk gates passed; defer the SerialLauncherMaxRiskScore check
					// to post-aggregation (riskScore is not yet computed at this point).
					serialLauncherPendingRiskCheck = true
				}
			}
		}

		// Unknown creator history: mode-aware fail-closed handling.
		if !in.CreatorPrevTokenCountKnown {
			if usingGlobal {
				// STRICT/BALANCED: probe failure must not equal approval.
				// An unknown dev in BALANCED mode contributes 0 risk (UnknownFactor=0),
				// making a serial developer with hundreds of launches look like a new dev.
				if m.runtime.Thresholds.RejectUnknownCreatorCount {
					rejectReasons = append(rejectReasons, "unknown_creator_count")
				}
			} else {
				// EXPLORATION/VERY_EXPLORATION: unknown history is not a quality failure —
				// the mode explicitly tolerates more risk. Drop silently so the token does
				// not pollute reject-rate statistics in Layer 10.
				m.logger.Info("serial_launcher_skip_unknown",
					"token", in.TokenAddress,
					"chain", in.Chain,
					"creator", in.CreatorAddress,
					"mode", profileName,
				)
				return buildSkipResult(in, []string{contracts.FlagSerialLauncherSkippedCreatorUnknown}, profileName), nil
			}
		}
	}
	// Structural reject: confirmed no social links (mandatory criterion).
	// When SocialLinksKnown=true (metadata probe ran) and HasSocialLinks=false
	// (no profile-level Twitter/Telegram/website found) and RejectNoSocialLinks=true,
	// reject immediately.
	if m.runtime != nil &&
		m.runtime.Thresholds.RejectNoSocialLinks &&
		in.SocialLinksKnown && !in.HasSocialLinks {
		rejectReasons = append(rejectReasons, "no_social_links")
	}
	// Structural reject: unknown social link status (mandatory fail-closed).
	// When SocialLinksKnown=false (metadata probe timed out, fetch error, or
	// probe disabled) and RejectUnknownSocialLinks=true, reject rather than
	// allowing the token to pass with unverified social presence. A token
	// whose social links cannot be validated is as dangerous as one with none.
	if m.runtime != nil &&
		m.runtime.Thresholds.RejectUnknownSocialLinks &&
		!in.SocialLinksKnown {
		rejectReasons = append(rejectReasons, "unknown_social_links")
	}
	// Structural reject: pre-probe duplicate name flag.
	// Set by the MarketProbesWorker pre-probe guard when a token with the same
	// lowercased, trimmed name has already been ingested for this chain.
	// Tokens with duplicate names are near-certainly low-quality cash-grabs;
	// rejecting them before any detector runs saves all downstream compute.
	if m.runtime != nil && m.runtime.Detectors.NameDuplicate && in.IsNameDuplicate {
		rejectReasons = append(rejectReasons, "duplicate_name")
	}
	// Structural reject: pre-probe copycat flag.
	// Set by the MarketProbesWorker pre-probe guard when the token name matches
	// a well-known/famous token entry in the configured known_tokens list.
	// Copycats of established tokens (PEPE, DOGE, WIF, TRUMP, BTC, SOL, …) are
	// impersonation attempts and must be rejected before capital allocation.
	if m.runtime != nil && m.runtime.Detectors.CopycatName && in.IsCopycat {
		rejectReasons = append(rejectReasons, "copycat_name")
	}
	// Structural reject: insufficient confirmed holder count.
	// Brand-new launches (PumpFunCreate events) are exempt — holder distribution
	// takes time to settle and would produce false rejections at the moment of
	// token creation.
	if !isNewLaunch && m.runtime != nil && m.runtime.Thresholds.MinHolderCount > 0 {
		if in.HolderDistKnown && in.HolderCount < m.runtime.Thresholds.MinHolderCount {
			rejectReasons = append(rejectReasons, "insufficient_holders")
		} else if !in.HolderDistKnown && m.runtime.Thresholds.RejectUnknownHolderCount {
			rejectReasons = append(rejectReasons, "unknown_holder_count")
		}
	}
	if m.runtime != nil && m.runtime.Thresholds.MinTokenAgeSeconds > 0 {
		// Hard-reject tokens younger than the minimum age. Tokens under this
		// threshold have incomplete data: holder distribution is not settled,
		// wash patterns are not yet visible, and metadata probes may not have
		// propagated. The rescan pipeline re-evaluates when age ≥ threshold.
		// Unknown age (empty timestamps) returns -1 → check is skipped to
		// avoid false rejections on tokens whose timestamps were not populated.
		//
		// Mode profile override: profile.MinTokenAgeSeconds=-1 disables the
		// age check for this mode (EXPLORATION/VERY_EXPLORATION — new-launch
		// sniping must catch tokens at the moment of creation, not 15 min
		// later). profile.MinTokenAgeSeconds>0 overrides with a mode-specific
		// floor. profile.MinTokenAgeSeconds=0 falls through to the global.
		effectiveMinAge := m.runtime.Thresholds.MinTokenAgeSeconds
		if profile.MinTokenAgeSeconds < 0 {
			effectiveMinAge = 0 // disabled for this mode
		} else if profile.MinTokenAgeSeconds > 0 {
			effectiveMinAge = profile.MinTokenAgeSeconds
		}
		if effectiveMinAge > 0 {
			age := tokenAgeSeconds(in.BlockTimestamp, in.IngestedAt)
			if age >= 0 && age < int64(effectiveMinAge) {
				rejectReasons = append(rejectReasons, "token_too_young")
			}
		}
	}

	// Optional: market cap range filter.
	// Guards on > 0 on the input field ensure brand-new tokens not yet indexed
	// by DEXScreener are not incorrectly rejected.
	if m.runtime != nil {
		thresholds := m.runtime.Thresholds
		if thresholds.MinMarketCapUsd > 0 && in.MarketCapUsd > 0 {
			if in.MarketCapUsd < thresholds.MinMarketCapUsd {
				m.logger.Info("market_cap_reject",
					"token", in.TokenAddress,
					"chain", in.Chain,
					"market_cap_usd", in.MarketCapUsd,
					"min_market_cap_usd", thresholds.MinMarketCapUsd,
				)
				rejectReasons = append(rejectReasons, "market_cap_too_low")
			}
		}
		if thresholds.MaxMarketCapUsd > 0 && in.MarketCapUsd > 0 {
			if in.MarketCapUsd > thresholds.MaxMarketCapUsd {
				m.logger.Info("market_cap_reject",
					"token", in.TokenAddress,
					"chain", in.Chain,
					"market_cap_usd", in.MarketCapUsd,
					"max_market_cap_usd", thresholds.MaxMarketCapUsd,
				)
				rejectReasons = append(rejectReasons, "market_cap_too_high")
			}
		}
		if thresholds.MinVolumeUsd1h > 0 && in.VolumeUsd1h > 0 {
			if in.VolumeUsd1h < thresholds.MinVolumeUsd1h {
				m.logger.Info("volume_reject",
					"token", in.TokenAddress,
					"chain", in.Chain,
					"volume_usd_1h", in.VolumeUsd1h,
					"min_volume_usd_1h", thresholds.MinVolumeUsd1h,
				)
				rejectReasons = append(rejectReasons, "volume_too_low")
			}
		}
	}

	// ── Detector toggles + weights ──────────────────────────────────────
	rugEnabled := true
	washEnabled := true
	taxEnabled := true
	honeypotEnabled := true
	fakeLiqEnabled := true
	devReputationEnabled := true
	if m.runtime != nil {
		rugEnabled = m.runtime.Detectors.RugAuthority || m.runtime.Detectors.LpLock
		washEnabled = m.runtime.Detectors.WashTrading
		taxEnabled = m.runtime.Detectors.TaxAnomaly
		honeypotEnabled = m.runtime.Detectors.HoneypotSimulation
		// FakeLiquidity has no dedicated YAML toggle yet; tie to LpLock so
		// operators can disable both signals together.
		fakeLiqEnabled = m.runtime.Detectors.LpLock
		devReputationEnabled = m.runtime.Detectors.DevReputation
	}

	weights := defaultRiskWeights
	if m.runtime != nil && !isZeroWeights(m.runtime.RiskWeights) {
		weights = m.runtime.RiskWeights
	}
	fakeLiqWeight := weights.FakeLiquidity
	if fakeLiqWeight <= 0 {
		fakeLiqWeight = 0.20 // legacy structural contribution
	}
	devReputationWeight := weights.DevReputation
	if devReputationWeight <= 0 {
		devReputationWeight = 0.25 // default: meaningful but below honeypot/rug
	}

	// ── Detector thresholds ─────────────────────────────────────────────
	maxBuyTaxBps := m.cfg.MaxBuyTaxBps
	maxSellTaxBps := m.cfg.MaxSellTaxBps
	totalTaxCapBps := int32(0)
	holderConcentrationCap := 0.40
	minLiquidityUsd := 5000.0
	minUniqueWallets := int32(5)
	maxCreatorPrevTokens := int32(5)
	noSocialLinksRisk := 0.40
	if m.runtime != nil {
		if m.runtime.Thresholds.MinLiquidityUsd > 0 {
			minLiquidityUsd = m.runtime.Thresholds.MinLiquidityUsd
		}
		if m.runtime.Thresholds.TaxBuyMaxBps > 0 {
			maxBuyTaxBps = m.runtime.Thresholds.TaxBuyMaxBps
		}
		if m.runtime.Thresholds.TaxSellMaxBps > 0 {
			maxSellTaxBps = m.runtime.Thresholds.TaxSellMaxBps
		}
		totalTaxCapBps = m.runtime.Thresholds.TaxTotalMaxBps
		if m.runtime.Thresholds.MaxCreatorPrevTokenCount > 0 {
			maxCreatorPrevTokens = m.runtime.Thresholds.MaxCreatorPrevTokenCount
		}
		if m.runtime.Thresholds.NoSocialLinksRiskScore > 0 {
			noSocialLinksRisk = m.runtime.Thresholds.NoSocialLinksRiskScore
		}
	}

	// ── Run detectors ───────────────────────────────────────────────────
	var (
		honeypot      HoneypotResult
		rug           RugResult
		wash          WashTradingResult
		fakeLiq       FakeLiquidityResult
		tax           TaxResult
		devReputation DevReputationResult
	)
	if honeypotEnabled {
		honeypot = DetectHoneypot(in)
	}
	if rugEnabled {
		rug = DetectRugPull(in, holderConcentrationCap)
	}
	if washEnabled {
		wash = DetectWashTradingDTO(in, minUniqueWallets, 0, 0, 0)
	}
	if fakeLiqEnabled {
		fakeLiq = DetectFakeLiquidity(in, minLiquidityUsd)
	}
	if taxEnabled {
		tax = DetectTaxManipulation(in, maxBuyTaxBps, maxSellTaxBps, totalTaxCapBps)
	}
	if devReputationEnabled {
		devReputation = DetectDevReputation(in, maxCreatorPrevTokens, noSocialLinksRisk)
	}

	// ── Aggregate ──────────────────────────────────────────────────────
	hardReject := honeypot.HardReject

	riskScore := 0.0
	riskScore += weightedDetector(honeypot.Score, weights.Honeypot, honeypot.Unknown, profile.UnknownFactor)
	riskScore += weightedDetector(rug.Score, weights.RugAuthority, rug.Unknown, profile.UnknownFactor)
	riskScore += weightedDetector(wash.Score, weights.WashTrading, wash.Unknown, profile.UnknownFactor)
	riskScore += weightedDetector(fakeLiq.Score, fakeLiqWeight, fakeLiq.Unknown, profile.UnknownFactor)
	riskScore += weightedDetector(tax.Score, weights.TaxAnomaly, tax.Unknown, profile.UnknownFactor)
	riskScore += weightedDetector(devReputation.Score, devReputationWeight, devReputation.Unknown, profile.UnknownFactor)

	// Structural-failure base contribution mirrors the legacy aggregator
	// (missing_reserves / reorged / missing_token / insufficient_liquidity
	// each adds 0.25 / 4 of the base; we keep them in rejectReasons so
	// they trigger the deterministic REJECT path even at score 0).
	if isFakeLiquidityStructural {
		// Already counted via insufficient_liquidity reject reason; do not
		// double-add to riskScore.
	}

	// AI narrative gate — only fires when NarrativeKnown=true
	// (the probe completed successfully and returned a classification).
	// Fail-open: NarrativeKnown=false → entire block is skipped, no rejection.
	if in.NarrativeKnown {
		// Copy-paste description: reject only when narrative quality is poor.
		// A copy-paste style with a high NarrativeScore means the token has a
		// genuine, compelling meme story — for meme tokens the narrative IS
		// the edge, so we pass it. Only low-scoring boilerplate is rejected.
		if m.runtime != nil && m.runtime.Detectors.AICopyPasteDesc && in.IsCopyPasteDesc {
			minScore := m.runtime.Thresholds.AICopyPasteDescMinNarrativeScore
			if minScore <= 0 {
				minScore = 6.0 // built-in default: 0–5 is weak narrative, 6–10 is good
			}
			if in.NarrativeScore < minScore {
				rejectReasons = append(rejectReasons, "ai_copy_paste_desc")
			}
		}
		// Impersonation: always reject regardless of narrative score — a token
		// that mimics a known project name/symbol is a scam, not a meme.
		if m.runtime != nil && m.runtime.Detectors.AIImpersonation && in.IsImpersonation {
			rejectReasons = append(rejectReasons, "ai_impersonation")
		}
		if in.IsCopyPasteDesc || in.IsImpersonation {
			m.logger.Info("ai_narrative_dq_gate",
				"token", in.TokenAddress,
				"copy_paste", in.IsCopyPasteDesc,
				"impersonation", in.IsImpersonation,
				"narrative_score", in.NarrativeScore,
				"narrative_type", in.NarrativeType,
				"narrative_quality_pass", in.IsCopyPasteDesc && !in.IsImpersonation && func() bool {
					min := 6.0
					if m.runtime != nil && m.runtime.Thresholds.AICopyPasteDescMinNarrativeScore > 0 {
						min = m.runtime.Thresholds.AICopyPasteDescMinNarrativeScore
					}
					return in.NarrativeScore >= min
				}(),
			)
		}
	}

	if riskScore < 0 {
		riskScore = 0
	}
	if riskScore > 1 {
		riskScore = 1
	}

	// ── Post-aggregation serial launcher risk score gate (Task 13) ────────
	// SerialLauncherMaxRiskScore requires the aggregated riskScore which is
	// only available here, after all detectors and external providers have run.
	// This block fires only for EXPLORATION/VERY_EXPLORATION tokens that passed
	// the non-risk quality gates in the pre-check block above.
	if serialLauncherPendingRiskCheck {
		maxRisk := slProfile.SerialLauncherMaxRiskScore
		if slProfile.SerialLauncherMomentumOverride.Enabled && slProfile.SerialLauncherMomentumOverride.MaxRiskScore > 0 {
			maxRisk = slProfile.SerialLauncherMomentumOverride.MaxRiskScore
		}
		if maxRisk > 0 && riskScore >= maxRisk {
			m.logger.Info("serial_launcher_skip_risk",
				"token", in.TokenAddress,
				"chain", in.Chain,
				"creator_count", in.CreatorPrevTokenCount,
				"risk_score", riskScore,
				"max_risk", maxRisk,
				"mode", profileName,
			)
			return buildSkipResult(in, []string{contracts.FlagSerialLauncherSkippedRisk}, profileName), nil
		}
		// All gates passed: add the monitoring flag.
		// makeDecision will produce PASS or RISKY_PASS based on riskScore;
		// we force RISKY_PASS minimum below (see "Promote PASS → RISKY_PASS").
		flags = append(flags, contracts.FlagSerialLauncherMonitored)
		m.logger.Info("serial_launcher_monitored",
			"token", in.TokenAddress,
			"chain", in.Chain,
			"creator", in.CreatorAddress,
			"creator_count", in.CreatorPrevTokenCount,
			"risk_score", riskScore,
			"mode", profileName,
		)
	}

	// ── External providers (P1) ────────────────────────────────────────
	extScore := 0.0
	providerFlags := []string{}
	providersDegraded := false
	var creatorRiskScore, lpLockPct float64
	structurallyRejected := len(rejectReasons) > 0

	if m.providers != nil &&
		m.runtime != nil &&
		m.runtime.Providers.Enabled &&
		!structurallyRejected {

		aggResult := m.providers.Evaluate(ctx, in.TokenAddress, in.Chain)
		extScore = aggResult.ExternalRiskScore
		providerFlags = aggResult.Flags
		providersDegraded = aggResult.Degraded
		creatorRiskScore = aggResult.CreatorRiskScore
		lpLockPct = aggResult.LpLockPct

		// Blend external score unless shadow_mode is active.
		if !m.runtime.Providers.ShadowMode {
			w := m.runtime.Providers.ExternalWeight
			if w > 0 && w <= 1.0 {
				riskScore = (1.0-w)*riskScore + w*extScore
				if riskScore < 0 {
					riskScore = 0
				}
				if riskScore > 1 {
					riskScore = 1
				}
			}
		}

		m.logger.Debug("dq_providers_evaluated",
			"token", in.TokenAddress,
			"chain", in.Chain,
			"external_score", extScore,
			"degraded", providersDegraded,
			"shadow_mode", m.runtime.Providers.ShadowMode,
		)
	}

	// ── Collect detector flags ─────────────────────────────────────────
	flags = append(flags, honeypot.Flags...)
	flags = append(flags, rug.Flags...)
	flags = append(flags, wash.Flags...)
	flags = append(flags, fakeLiq.Flags...)
	flags = append(flags, tax.Flags...)
	flags = append(flags, devReputation.Flags...)
	if honeypot.Unknown {
		flags = append(flags, honeypot.UnknownFlag)
	}
	if rug.Unknown {
		flags = append(flags, rug.UnknownFlag)
	}
	if wash.Unknown {
		flags = append(flags, wash.UnknownFlag)
	}
	if fakeLiq.Unknown {
		flags = append(flags, fakeLiq.UnknownFlag)
	}
	if tax.Unknown {
		flags = append(flags, tax.UnknownFlag)
	}
	if devReputation.Unknown {
		flags = append(flags, devReputation.UnknownFlag)
	}

	// MaxIndeterminateCount: if too many detectors are Unknown, reject
	// per the failure_policy block (configurable; 0 = disabled).
	if m.runtime != nil && m.runtime.FailurePolicy.MaxIndeterminateCount > 0 {
		unknownCount := 0
		if honeypot.Unknown {
			unknownCount++
		}
		if rug.Unknown {
			unknownCount++
		}
		if wash.Unknown {
			unknownCount++
		}
		if fakeLiq.Unknown {
			unknownCount++
		}
		if tax.Unknown {
			unknownCount++
		}
		if devReputation.Unknown {
			unknownCount++
		}
		if unknownCount >= m.runtime.FailurePolicy.MaxIndeterminateCount && profileName == "STRICT" {
			rejectReasons = append(rejectReasons, "too_many_indeterminate_detectors")
		}
	}

	sort.Strings(rejectReasons)
	flags = dedupSorted(flags)
	sort.Strings(flags)

	// Fair DQ: EXPLORATION+ unknown fields → SKIP/rescan or monitored RISKY_PASS.
	if m.runtime != nil {
		thresholds := m.runtime.Thresholds
		rejectReasons, flags = applyNoSocialMonitoring(mode, in, rejectReasons, flags, profile.NoSocialMonitoring, thresholds)
		var fairSkip bool
		in, flags, rejectReasons, fairSkip = applyFairUnknownEvaluation(mode, in, isNewLaunch, flags, rejectReasons, profile)
		if fairSkip {
			return buildSkipResult(in, flags, profileName), nil
		}
	}

	// ── Decision ───────────────────────────────────────────────────────
	decision := makeDecision(riskScore, hardReject, profile)
	if hardReject && !containsString(rejectReasons, "honeypot") {
		rejectReasons = append(rejectReasons, "honeypot")
	}
	if len(rejectReasons) > 0 {
		decision = "REJECT"
		sort.Strings(rejectReasons)
	}

	// Promote PASS → RISKY_PASS for serial-launcher-monitored tokens (Task 13).
	// The creator has launched previous tokens which represents elevated baseline
	// risk even when the aggregate score falls below the RISKY_PASS threshold.
	// This forces Layer 7 to apply a smaller allocation and Layer 9 tighter exits.
	if containsString(flags, contracts.FlagSerialLauncherMonitored) && decision == contracts.DecisionPass {
		decision = contracts.DecisionRiskyPass
	}

	eventID := contracts.ContentIDFromString(fmt.Sprintf("dq:%s:%s:%s", in.EventID, profileName, decision))

	// Booleans on the DTO mirror per-detector verdicts (>0 score) — kept
	// for back-compat with downstream consumers reading the *DTO booleans.
	return contracts.DataQualityDTO{
		EventID:       eventID,
		TraceID:       in.TraceID,
		CorrelationID: in.CorrelationID,
		CausationID:   in.EventID,
		VersionID:     in.VersionID,

		TokenLifecycleID: contracts.ContentIDFromString(in.TokenAddress + ":" + in.Chain),
		TokenAddress:     in.TokenAddress,
		Chain:            in.Chain,

		Decision:  decision,
		RiskScore: riskScore,

		IsHoneypot:      honeypot.HardReject || honeypot.Score > 0,
		IsFakeLiquidity: isFakeLiquidityStructural || fakeLiq.Score > 0,
		IsWashTrading:   wash.Score > 0,
		IsRugRisk:       rug.Score > 0,
		IsTaxAnomaly:    tax.Score > 0,

		BuyTaxBps:        in.BuyTaxBps,
		SellTaxBps:       in.SellTaxBps,
		LpLocked:         in.LpLockKnown && in.LpLocked,
		LpHolderCount:    0,
		ContractVerified: in.ContractVerifiedKnown && in.ContractVerified,

		HoneypotScore: honeypot.Score,
		RugScore:      rug.Score,
		WashScore:     wash.Score,
		FakeLiqScore:  fakeLiq.Score,
		TaxScore:      tax.Score,
		Profile:       profileName,
		Flags:         flags,

		RejectReasons: rejectReasons,
		EvaluatedAt:   now,

		ExternalProviderScore: extScore,
		ProviderFlags:         providerFlags,
		ProvidersDegraded:     providersDegraded,
		CreatorRiskScore:      creatorRiskScore,
		LpLockPct:             lpLockPct,
	}, nil
}

// weightedDetector returns the contribution of a detector to the aggregate
// risk score, applying the per-profile UnknownFactor when the detector did
// not have upstream data.
func weightedDetector(score, weight float64, unknown bool, unknownFactor float64) float64 {
	if weight <= 0 {
		return 0
	}
	if unknown {
		return applyUnknownContribution(weight, unknownFactor)
	}
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score * weight
}

func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// clampFloat clamps v to [lo, hi].
func clampFloat(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// tokenAgeSeconds computes the token's age in seconds relative to time.Now().
// It uses BlockTimestamp as the canonical reference (on-chain creation time),
// falling back to IngestedAt when BlockTimestamp is empty (log-only pump.fun
// path where BlockTimestamp is not populated until a full tx fetch occurs).
// Returns -1 when neither field can be parsed — the caller treats -1 as
// "unknown age; skip the check" rather than as a reject.
func tokenAgeSeconds(blockTimestamp, ingestedAt string) int64 {
	ref := blockTimestamp
	if ref == "" {
		ref = ingestedAt
	}
	if ref == "" {
		return -1
	}
	t, err := time.Parse(time.RFC3339Nano, ref)
	if err != nil {
		t2, err2 := time.Parse(time.RFC3339, ref)
		if err2 != nil {
			return -1
		}
		t = t2
	}
	age := int64(time.Since(t).Seconds())
	if age < 0 {
		// Clock skew guard: block timestamp slightly in the future is treated
		// as age=0 (brand new) rather than -1 (unknown).
		return 0
	}
	return age
}

// buildSkipResult constructs a DataQualityDTO with Decision=SKIP (Task 13).
// Called by ProcessForMode when EXPLORATION/VERY_EXPLORATION mode determines
// that a serial-launcher token should be silently dropped rather than rejected.
//
// SKIP contract (enforced by the calling worker, not this function):
//   - No data_quality_event is emitted (unlike REJECT or PASS).
//   - Does NOT contribute to reject-rate statistics in Layer 10.
//   - token_lifecycle transitions to DQ_SKIPPED (terminal state, audit trail preserved).
//   - RejectionReasons is always nil for SKIP — it is not a quality failure.
func buildSkipResult(in contracts.MarketDataDTO, flags []string, profileName string) contracts.DataQualityDTO {
	return contracts.DataQualityDTO{
		EventID:          contracts.ContentIDFromString(fmt.Sprintf("dq-skip:%s:%s", in.EventID, profileName)),
		TraceID:          in.TraceID,
		CorrelationID:    in.CorrelationID,
		CausationID:      in.EventID,
		VersionID:        in.VersionID,
		TokenLifecycleID: contracts.ContentIDFromString(in.TokenAddress + ":" + in.Chain),
		TokenAddress:     in.TokenAddress,
		Chain:            in.Chain,
		Decision:         contracts.DecisionSkip,
		Flags:            flags,
		RejectReasons:    nil, // SKIP is not a quality failure; no rejection reasons.
		Profile:          profileName,
		EvaluatedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}
}
