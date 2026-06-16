package config

// ProbesConfig configures the optional market-data enrichment stage
// (residual-risk #4). All probes default OFF — the framework ships
// dormant. Enable per-probe by setting `enabled: true` AND providing
// the deployment-specific config (e.g. simulation_contract address).
type ProbesConfig struct {
	// Enabled toggles the entire probes worker. When false, the worker
	// is not registered and the data_quality stage continues to consume
	// the raw market_data_event directly.
	Enabled bool `yaml:"enabled"`

	// MaxProbesPerHour is a hard ceiling on the number of tokens that
	// trigger Helius RPC probe calls per rolling one-hour window. When the
	// cap is reached, subsequent tokens are emitted with all Known=false
	// flags; the DQ layer's fail-closed rules (reject_unknown_social_links,
	// reject_unknown_total_supply, reject_unknown_creator_count) safely
	// reject them without spending any RPC credits.
	//
	// Credit math (Helius free tier, 1M credits/month):
	//   350 probes/hr × 3 credits × 720 hr = 756k credits/month for probes
	//   + Pyth getAccountInfo (30s TTL):       86k credits/month
	//   + trades (15/day × 28 credits):        13k credits/month
	//   + raydium-v4 getTransaction (300/day): ~9k credits/month  (1 cr/call)
	//   Total range: ~864k credits/month
	//
	// Set to 0 to disable the cap (unlimited probes). Not recommended on
	// Helius free tier — observed rate without filtering is ~65k credits/hr.
	MaxProbesPerHour int `yaml:"max_probes_per_hour"`

	// MaxProbeCreditsPerHour is a credit-aware ceiling on Helius RPC usage per
	// rolling hour. When > 0, takes precedence over token-only MaxProbesPerHour
	// for budget exhaustion decisions. Tokens over budget are enqueued in
	// probe_pending_queue instead of forwarded to DQ with Known=false.
	MaxProbeCreditsPerHour int `yaml:"max_probe_credits_per_hour"`

	// ProbeCreditCosts maps probe names to estimated Helius credits per call.
	// Used when MaxProbeCreditsPerHour > 0.
	ProbeCreditCosts map[string]int `yaml:"probe_credit_costs"`

	// RateLimitBuckets optionally splits the hourly token budget between fresh
	// ingest and rescan events. Zero values share the global MaxProbesPerHour pool.
	RateLimitBuckets ProbeRateLimitBuckets `yaml:"rate_limit_buckets"`

	// PendingQueue configures the DB-backed deferral queue for rate-limited tokens.
	PendingQueue ProbePendingQueueConfig `yaml:"pending_queue"`

	// BatchAccounts enables a single getMultipleAccounts call for the
	// solana_authorities (mint) + solana_pumpfun_lp (bonding curve) probes
	// on new-token ingest events. Saves ~1 Helius credit per pump.fun token
	// versus two separate getAccountInfo calls. Rescan events are excluded.
	BatchAccounts bool `yaml:"batch_accounts"`

	// RescanSkipPumpfunLpPhase2 skips solana_pumpfun_lp on Phase 2 rescan
	// bands (12h–48h) where bonding-curve reserves change slowly relative
	// to the DQ signals already captured at ingest. Phase 1 bands (15m–8h)
	// still re-probe liquidity.
	RescanSkipPumpfunLpPhase2 bool `yaml:"rescan_skip_pumpfun_lp_phase2"`

	// HoneypotSim configures the honeypot_sim probe. See
	// internal/modules/probes/honeypot_sim.go.
	HoneypotSim HoneypotSimYAML `yaml:"honeypot_sim"`

	// SolanaAuthorities configures the SPL mint/freeze authority probe.
	SolanaAuthorities SolanaProbeYAML `yaml:"solana_authorities"`

	// SolanaPumpfunLp configures the pump.fun bonding-curve reserve probe.
	SolanaPumpfunLp SolanaProbeYAML `yaml:"solana_pumpfun_lp"`

	// SolanaHolderDist configures the SPL holder-concentration probe.
	SolanaHolderDist SolanaHolderDistYAML `yaml:"solana_holder_dist"`

	// SolanaDASAsset configures the Helius DAS getAsset enrichment probe.
	// When enabled it fetches supply and social links in a single DAS call
	// before the other Solana probes run. DAS getAsset costs 10 credits per
	// call (helius.dev/docs/billing/credits) — more expensive than standard
	// RPC methods (1 credit), but consolidates 2+ separate lookups into one.
	// Disabled by default — enable only on Helius endpoints (non-Helius RPC
	// will return an unsupported-method error which the probe treats as fail-open).
	SolanaDASAsset SolanaProbeYAML `yaml:"solana_das_asset"`

	// SolanaMetadata configures the off-chain metadata fetch probe.
	// Fetches the MetadataURI (IPFS/Arweave/HTTPS) and sets
	// SocialLinksKnown + HasSocialLinks. No RPC client required.
	SolanaMetadata SolanaMetadataYAML `yaml:"solana_metadata"`

	// SolanaCreatorReputation configures the pump.fun creator history probe.
	// Queries the pump.fun public API to set CreatorPrevTokenCountKnown and
	// CreatorPrevTokenCount with ground-truth data. Closes the cold-start
	// serial-launcher gap (BLOCKER-2 from gate review 2026-05-10).
	SolanaCreatorReputation SolanaCreatorReputationYAML `yaml:"solana_creator_reputation"`

	// EVMPairReserves configures the Uniswap-V2 getReserves probe.
	EVMPairReserves EVMPairReservesYAML `yaml:"evm_pair_reserves"`
}

// ProbeRateLimitBuckets splits probe budget between event sources.
type ProbeRateLimitBuckets struct {
	FreshTokensPerHour  int `yaml:"fresh_tokens_per_hour"`
	RescanTokensPerHour int `yaml:"rescan_tokens_per_hour"`
}

// ProbePendingQueueConfig configures deferred probe processing.
type ProbePendingQueueConfig struct {
	Enabled             bool `yaml:"enabled"`
	DrainIntervalSeconds int `yaml:"drain_interval_seconds"`
	MaxAttempts         int  `yaml:"max_attempts"`
	TTLHours            int  `yaml:"ttl_hours"`
	DrainBatchSize      int  `yaml:"drain_batch_size"`
}

// HoneypotSimYAML mirrors probes.HoneypotSimConfig but lives in the
// config package to avoid importing probes into config (and creating an
// import cycle when validate_ranges ranges over probe-specific bounds).
type HoneypotSimYAML struct {
	Enabled            bool   `yaml:"enabled"`
	SimulationContract string `yaml:"simulation_contract"`
	TimeoutMs          int    `yaml:"timeout_ms"`
}

// SolanaProbeYAML is the common shape for Solana enrichment probes that
// only need a timeout + commitment (authorities, pumpfun_lp).
type SolanaProbeYAML struct {
	Enabled    bool   `yaml:"enabled"`
	TimeoutMs  int    `yaml:"timeout_ms"`
	Commitment string `yaml:"commitment"`
}

// SolanaHolderDistYAML adds a top-K knob on top of the common shape.
type SolanaHolderDistYAML struct {
	Enabled    bool   `yaml:"enabled"`
	TimeoutMs  int    `yaml:"timeout_ms"`
	Commitment string `yaml:"commitment"`
	TopK       int    `yaml:"top_k"`
	// FallbackEnabled activates the getTokenSupply + getProgramAccounts fallback
	// when getTokenLargestAccounts times out. Costs +11 Helius credits per use.
	FallbackEnabled bool `yaml:"fallback_enabled"`
	// FallbackTimeoutMs is the timeout per fallback RPC call (default 2500 ms).
	FallbackTimeoutMs int32 `yaml:"fallback_timeout_ms"`
	// FallbackMaxProgramAccounts caps accounts processed during fallback (default 200).
	FallbackMaxProgramAccounts int32 `yaml:"fallback_max_program_accounts"`
}

// EVMPairReservesYAML configures the Uniswap-V2 pair getReserves probe.
type EVMPairReservesYAML struct {
	Enabled   bool `yaml:"enabled"`
	TimeoutMs int  `yaml:"timeout_ms"`
}

// SolanaMetadataYAML configures the off-chain metadata fetch probe.
// The probe resolves IPFS/Arweave URIs via the configured gateway and
// parses the JSON for social link fields.
type SolanaMetadataYAML struct {
	Enabled      bool   `yaml:"enabled"`
	TimeoutMs    int    `yaml:"timeout_ms"`
	IPFSGateway  string `yaml:"ipfs_gateway"`
	MaxBodyBytes int64  `yaml:"max_body_bytes"`
}

// SolanaCreatorReputationYAML configures the pump.fun creator history probe.
// The probe queries the pump.fun public API to determine how many tokens
// a Solana wallet has previously launched. This provides ground-truth creator
// history that is not available in the local database at cold start.
type SolanaCreatorReputationYAML struct {
	// Enabled toggles the probe. Default true (enabled by default so that
	// the cold-start serial-launcher gap is closed from the first run).
	Enabled bool `yaml:"enabled"`

	// TimeoutMs is the HTTP deadline for the pump.fun API request.
	// Valid range: [500, 10000]. Probe internal default: 3000.
	// (config/pipeline.yaml ships an explicit 5000ms override to accommodate
	// the pump.fun + Helius DAS fallback path.)
	TimeoutMs int `yaml:"timeout_ms"`

	// BaseURL is the pump.fun creator API root. Must be HTTPS.
	// Default: "https://frontend-api-v3.pump.fun".
	BaseURL string `yaml:"base_url"`

	// MaxBodyBytes caps the API response body (bytes).
	// Valid range: [1024, 1048576]. Default: 131072 (128 KiB).
	MaxBodyBytes int64 `yaml:"max_body_bytes"`

	// PageLimit is the ?limit= parameter sent to pump.fun (max coins per
	// page). Valid range: [1, 200]. Default: 50.
	PageLimit int `yaml:"page_limit"`

	// HeliusRPCURL is NOT read from YAML (yaml:"-"). It is populated
	// programmatically in cmd/server.go from the first Helius HTTP
	// endpoint found in cfg.Solana.RPCEndpoints. The URL embeds the
	// Helius API key as a query parameter (sourced from SOLANA_RPC_HTTP_2
	// env var). Empty string disables the Helius DAS fallback.
	// NEVER set this field directly in pipeline.yaml — it contains an API key.
	HeliusRPCURL string `yaml:"-"`
}

// applyProbesDefaults fills zero-value probe config fields with safe defaults.
func applyProbesDefaults(p *ProbesConfig) {
	if p == nil {
		return
	}
	if len(p.ProbeCreditCosts) == 0 {
		p.ProbeCreditCosts = map[string]int{
			"solana_authorities":        1,
			"solana_pumpfun_lp":           1,
			"solana_holder_dist":          11,
			"solana_metadata":             0,
			"solana_creator_reputation":   0,
			"evm_pair_reserves":           1,
			"honeypot_sim":                1,
		}
	}
	if p.PendingQueue.DrainIntervalSeconds == 0 {
		p.PendingQueue.DrainIntervalSeconds = 60
	}
	if p.PendingQueue.MaxAttempts == 0 {
		p.PendingQueue.MaxAttempts = 3
	}
	if p.PendingQueue.TTLHours == 0 {
		p.PendingQueue.TTLHours = 24
	}
	if p.PendingQueue.DrainBatchSize == 0 {
		p.PendingQueue.DrainBatchSize = 50
	}
}
