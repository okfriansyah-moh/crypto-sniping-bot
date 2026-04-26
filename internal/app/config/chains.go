package config

// ChainConfig holds per-chain RPC and factory configuration.
// Values come from config/chains.yaml — no hardcoded values.
type ChainConfig struct {
Name              string          `yaml:"name"`
ChainID           uint64          `yaml:"chain_id"`
RPCEndpoints      []string        `yaml:"rpc_endpoints"`
WSEndpoints       []string        `yaml:"ws_endpoints"`
ConfirmationDepth uint32          `yaml:"confirmation_depth"`
BaseTokens        []BaseToken     `yaml:"base_tokens"`
Factories         []FactoryConfig `yaml:"factories"`
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
}

// SolanaProgramConfig is a tracked Solana program (Raydium, Pump.fun, etc.).
type SolanaProgramConfig struct {
ProgramID string `yaml:"program_id"`
Family    string `yaml:"family"` // raydium-v4 | pumpfun | raydium-clmm
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
ChainID                 string              `yaml:"chain_id"`
RPCEndpoints            []SolanaRPCEndpoint `yaml:"rpc"`
Programs                []SolanaProgramConfig `yaml:"programs"`
ConfirmationCommitment  string              `yaml:"confirmation_commitment"`
BlockhashRefreshMs      int                 `yaml:"blockhash_refresh_ms"`
IngestionBackoff        IngestionBackoff    `yaml:"ingestion_backoff"`
WSHeartbeatTimeoutMs    int                 `yaml:"ws_heartbeat_timeout_ms"`
GapRecoveryMaxSlots     uint64              `yaml:"gap_recovery_max_slots"`
PublishBufferSize       int                 `yaml:"publish_buffer_size"`
PreferredRegion         string              `yaml:"preferred_region"`
ProvidersRequired       int                 `yaml:"providers_required"`
Health                  SolanaHealthConfig  `yaml:"health"`
}
