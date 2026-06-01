package config

// ChainConfig holds per-chain RPC and factory configuration.
// Values come from config/chains.yaml — no hardcoded values.
type ChainConfig struct {
	Name                string           `yaml:"name"`
	ChainID             uint64           `yaml:"chain_id"`
	RPCEndpoints        []string         `yaml:"rpc_endpoints"`
	WSEndpoints         []string         `yaml:"ws_endpoints"`
	ConfirmationDepth   uint32           `yaml:"confirmation_depth"`
	BaseTokens          []BaseToken      `yaml:"base_tokens"`
	Factories           []FactoryConfig  `yaml:"factories"`
	Backoff             IngestionBackoff `yaml:"backoff"`
	PollIntervalMs      int              `yaml:"poll_interval_ms"`
	HeartbeatIntervalMs int              `yaml:"heartbeat_interval_ms"`
	HeartbeatTimeoutMs  int              `yaml:"heartbeat_timeout_ms"`
}

// BaseToken is a known base-side token (WETH, USDT, WBNB, etc.).
type BaseToken struct {
	Address string `yaml:"address"`
	Symbol  string `yaml:"symbol"`
}

// FactoryConfig is a DEX factory contract address and protocol label.
type FactoryConfig struct {
	Address  string `yaml:"address"`
	Protocol string `yaml:"protocol"`
	Market   string `yaml:"market"`
}

// IngestionBackoff holds exponential backoff parameters for RPC reconnects.
type IngestionBackoff struct {
	InitialMs  int     `yaml:"initial_ms"`
	MaxMs      int     `yaml:"max_ms"`
	Multiplier float64 `yaml:"multiplier"`
}

// SolanaRPCEndpoint is a single Solana RPC endpoint with priority and kind.
type SolanaRPCEndpoint struct {
	URL      string `yaml:"url"`
	Priority int    `yaml:"priority"` // 1 = primary; higher = lower priority
	Kind     string `yaml:"kind"`     // ws | http
	Region   string `yaml:"region"`
	// Provider identifies the RPC provider for dialect selection.
	// Supported values: "quicknode" (or "qn"), "helius".
	// When empty the provider is auto-detected from the endpoint URL.
	Provider string `yaml:"provider"`
}

// SolanaProgramConfig is a tracked Solana program (Raydium, Pump.fun, etc.).
type SolanaProgramConfig struct {
	ProgramID string `yaml:"program_id"`
	Family    string `yaml:"family"`   // raydium-v4 | pumpfun | raydium-clmm
	Disabled  bool   `yaml:"disabled"` // when true the WS subscription is skipped at startup
	// SubscriptionMethod controls the WebSocket subscription type for this program.
	// Empty or "logsSubscribe" uses the standard logsSubscribe path.
	// "transactionSubscribe" switches to a Helius-extended transactionSubscribe
	// filtered by AccountFilter, reducing credit burn for high-volume programs
	// (e.g. Raydium V4 switches from ~2M credits/day via logsSubscribe to ~1k/day).
	SubscriptionMethod string `yaml:"subscription_method"`
	// AccountFilter is the account public key passed to accountInclude when
	// SubscriptionMethod is "transactionSubscribe". Must be the required-signer
	// account for pool-creation transactions (not a swap-only account) so that
	// only new-pool initialization events are received. Ignored for logsSubscribe.
	AccountFilter string `yaml:"account_filter"`
}

// SolanaHealthConfig holds endpoint health-scoring parameters.
type SolanaHealthConfig struct {
	ScoreRefreshIntervalMs         int     `yaml:"score_refresh_interval_ms"`
	LatencyNormalizerMs            int     `yaml:"latency_normalizer_ms"`
	LatencyFailoverThresholdMs     int     `yaml:"latency_failover_threshold_ms"`
	ErrorRateFailoverThreshold     float64 `yaml:"error_rate_failover_threshold"`
	ConsecutiveWSFailuresThreshold int     `yaml:"consecutive_ws_failures_threshold"`
	CircuitOpenCooldownMs          int     `yaml:"circuit_open_cooldown_ms"`
}

// SolanaConfig holds Solana-specific ingestion parameters.
// Values come from config/chains.yaml — no hardcoded values.
type SolanaConfig struct {
	ChainID                string                `yaml:"chain_id"`
	RPCEndpoints           []SolanaRPCEndpoint   `yaml:"rpc"`
	Programs               []SolanaProgramConfig `yaml:"programs"`
	ConfirmationCommitment string                `yaml:"confirmation_commitment"`
	BlockhashRefreshMs     int                   `yaml:"blockhash_refresh_ms"`
	IngestionBackoff       IngestionBackoff      `yaml:"ingestion_backoff"`
	WSHeartbeatTimeoutMs   int                   `yaml:"ws_heartbeat_timeout_ms"`
	GapRecoveryMaxSlots    uint64                `yaml:"gap_recovery_max_slots"`
	PublishBufferSize      int                   `yaml:"publish_buffer_size"`
	PreferredRegion        string                `yaml:"preferred_region"`
	ProvidersRequired      int                   `yaml:"providers_required"`
	Health                 SolanaHealthConfig    `yaml:"health"`
	// GetTransactionRPS caps how many getTransaction HTTP calls per second the
	// client may make. Set to match your RPC provider account tier.
	// QuickNode free = 15 req/s; set to 12 to leave headroom for other calls.
	GetTransactionRPS int `yaml:"get_transaction_rps"`
	// RateLimitBackoffMs is how long (ms) to suppress getTransaction calls
	// after receiving an RPC -32003 daily-quota error. Default: 60000 (60s).
	RateLimitBackoffMs int `yaml:"rate_limit_backoff_ms"`
	// WsSubscribeStaggerMs is the per-program startup delay (ms) applied before
	// the first logsSubscribe attempt for each program. Staggering prevents all
	// programs from connecting simultaneously and triggering a burst 429.
	// Program i waits i*WsSubscribeStaggerMs before its first attempt.
	// 0 disables staggering (all connect immediately).
	WsSubscribeStaggerMs int `yaml:"ws_subscribe_stagger_ms"`
	// ProcessingWorkers controls how many concurrent goroutines may process
	// notifications per program (calling getTransaction + normalize + emit).
	// 0 or negative falls back to a safe default. Pump.fun in log-decode mode
	// does not consume worker slots.
	ProcessingWorkers int `yaml:"processing_workers"`
	// PumpfunDecodeFromLogs toggles the log-only decoding path for Pump.fun
	// CreateEvent. When true, no getTransaction RPC is issued for pumpfun
	// notifications — all DTO fields are derived from the WS log payload.
	// Default true (set explicitly in config/chains.yaml).
	PumpfunDecodeFromLogs bool `yaml:"pumpfun_decode_from_logs"`
	// PumpfunVirtualSolLamports is the pump.fun bonding-curve virtual SOL
	// reserve at launch (BondingCurveProgress=0). Protocol constant: 30 SOL
	// = 30_000_000_000 lamports. Injected into MarketDataDTO.ReserveBaseRaw
	// and MarketDataDTO.LiquidityUsd on the log-only ingest path where no
	// on-chain reserve data is available. Set 0 to disable injection.
	PumpfunVirtualSolLamports uint64 `yaml:"pumpfun_virtual_sol_lamports"`
	// SolEstimatedPriceUsd is a conservative SOL price fallback (USD) used
	// only when no live price feed is available. Used to compute LiquidityUsd
	// = PumpfunVirtualSolLamports / 1e9 × SolEstimatedPriceUsd.
	SolEstimatedPriceUsd float64 `yaml:"sol_estimated_price_usd"`

	// PythSolUsdAccount is the on-chain Pyth price account for SOL/USD.
	// Mainnet: H6ARHf6YXhGYeQfUzQNGk6rDNnLBQKrenN712K4AQJEG.
	// Empty disables the live price feed and forces the static
	// SolEstimatedPriceUsd fallback. Phase 3 (recovery).
	PythSolUsdAccount string `yaml:"pyth_sol_usd_account"`
	// PythCacheTTLSeconds is the cache freshness window for Pyth quotes.
	// Default 5s when zero. Higher values reduce RPC load at the cost of
	// staler liquidity estimates.
	PythCacheTTLSeconds int `yaml:"pyth_cache_ttl_seconds"`
	// PythStaleAfterSeconds is the grace window during which a cached
	// quote is still served (with Stale=true) when the RPC is failing.
	// Default 60s when zero.
	PythStaleAfterSeconds int `yaml:"pyth_stale_after_seconds"`

	// Phase 11 (Reference-Repo Improvements R2 — INGEST) — hybrid
	// transport. Mode "rpc" (default, legacy) uses the existing
	// websocket+RPC stack. Mode "grpc" prefers a Yellowstone/Geyser
	// gRPC stream; on N consecutive errors it falls back to RPC when
	// FallbackOnError is true. GrpcEndpoint is empty in legacy mode.
	Transport IngestionTransportConfig `yaml:"transport"`

	// PreFilter holds the L0 pre-cohort filter configuration (Task 25).
	// When PreFilter.Enabled is true, tokens from creators whose prior
	// launch count exceeds MaxCreatorPrevTokenCount are dropped at L0
	// before probe calls are issued, reducing Helius credit burn.
	PreFilter IngestionPreFilterConfig `yaml:"pre_filter"`
}

// IngestionTransportConfig governs Solana streaming transport selection.
// NOTE: GrpcAuthToken is intentionally absent from this struct. The gRPC auth
// token MUST be supplied via SOLANA_GRPC_TOKEN env var only — never in YAML
// config files to prevent accidental secret commit to git.
type IngestionTransportConfig struct {
	Mode            string `yaml:"mode"`              // "rpc" | "grpc" | "hybrid"
	GrpcEndpoint    string `yaml:"grpc_endpoint"`     // host:port
	FallbackOnError bool   `yaml:"fallback_on_error"` // hybrid → fall back to rpc
	FallbackErrorN  int    `yaml:"fallback_error_n"`  // consecutive errors before fallback
}

// IngestionPreFilterConfig controls the L0 pre-cohort filter (Task 25).
// When Enabled is true, the ingestion module consults the injected
// CreatorProfileReader before emitting a MarketDataDTO.  Tokens whose
// creator has launched more than MaxCreatorPrevTokenCount prior tokens
// are silently dropped at L0, saving downstream probe and DQ budget.
//
// The threshold (default 25) is intentionally above VERY_EXPLORATION's
// max_creator_prev_token_count=10 so this filter never overrides a DQ
// mode decision — it only drops the strict super-set that would always
// SKIP/REJECT under every mode.
//
// Disabled by default (Enabled: false) — operator opt-in after Task 24
// PIPELINE_PROOF confirmation.
type IngestionPreFilterConfig struct {
	// Enabled gates the entire pre-filter. False = fail-open (all tokens pass).
	Enabled bool `yaml:"enabled"`
	// MaxCreatorPrevTokenCount is the inclusive upper bound on prior launches
	// before the token is dropped at L0. 0 means filter disabled regardless of Enabled.
	MaxCreatorPrevTokenCount int32 `yaml:"max_creator_prev_token_count"`
}
