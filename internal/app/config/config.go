package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config holds the complete application configuration loaded from YAML files.
// All thresholds and tunable parameters must come from YAML — no hardcoded values.
// See docs/implementation_roadmap.md § 0.5.
type Config struct {
	Pipeline     PipelineConfig         `yaml:"pipeline"`
	Database     DatabaseConfig         `yaml:"database"`
	Worker       WorkerConfig           `yaml:"worker"`
	Logging      LoggingConfig          `yaml:"logging"`
	Chains       map[string]ChainConfig `yaml:"chains"` // per-chain ingestion config
	Edge         EdgeConfig             `yaml:"edge"`
	Validation   ValidationConfig       `yaml:"validation"`
	Selection    SelectionConfig        `yaml:"selection"`
	Capital      CapitalConfig          `yaml:"capital"`
	Position     PositionConfig         `yaml:"position"`
	Execution    ExecutionPhase3Config  `yaml:"execution"`
	Evaluation   EvaluationConfig       `yaml:"evaluation"`
	StateMachine StateMachineConfig     `yaml:"state_machine"`
	EventWeights EventPriorityWeights   `yaml:"event_weights"`

	// SchemaVersion is set from pipeline.schema_version.
	SchemaVersion string
}

// PipelineConfig holds top-level pipeline metadata.
type PipelineConfig struct {
	SchemaVersion string `yaml:"schema_version"`
}

// DatabaseConfig holds database connection parameters.
type DatabaseConfig struct {
	Engine   string     `yaml:"engine"`
	Host     string     `yaml:"host"`
	Port     int        `yaml:"port"`
	Database string     `yaml:"database"`
	User     string     `yaml:"user"`
	SSLMode  string     `yaml:"ssl_mode"` // disable | require | verify-ca | verify-full
	Pool     PoolConfig `yaml:"pool"`
}

// PoolConfig holds connection pool settings.
type PoolConfig struct {
	MaxOpenConns        int `yaml:"max_open_conns"`
	MaxIdleConns        int `yaml:"max_idle_conns"`
	ConnMaxLifetimeSecs int `yaml:"conn_max_lifetime_seconds"`
}

// WorkerConfig holds worker loop parameters.
type WorkerConfig struct {
	IdleBackoffMs int  `yaml:"idle_backoff_ms"`
	MaxRetryCount int  `yaml:"max_retry_count"`
	PanicRecovery bool `yaml:"panic_recovery"`
}

// LoggingConfig holds structured logging settings.
type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

// EdgeConfig holds Phase 2 edge detection parameters.
type EdgeConfig struct {
	MinVelocityScore    float64 `yaml:"min_velocity_score"`
	MinLiquidityScore   float64 `yaml:"min_liquidity_score"`
	MaxAgeSeconds       int64   `yaml:"max_age_seconds"`
	BaseWindowMs        int32   `yaml:"base_window_ms"`
	WindowMomentumFactor float64 `yaml:"window_momentum_factor"`
	TTLSeconds          int     `yaml:"ttl_seconds"`
}

// ValidationConfig holds Phase 2 EV gate parameters (fixed priors).
type ValidationConfig struct {
	PriorProbability   float64 `yaml:"prior_probability"`
	PriorGainBps       int32   `yaml:"prior_gain_bps"`
	PriorLossBps       int32   `yaml:"prior_loss_bps"`
	PriorSlippageBps   int32   `yaml:"prior_slippage_bps"`
	EvThresholdBps     int32   `yaml:"ev_threshold_bps"`
	FixedCostsBps      int32   `yaml:"fixed_costs_bps"`
	BuildSubmitP95Ms   int32   `yaml:"build_submit_p95_ms"`
	TTLSeconds         int     `yaml:"ttl_seconds"`
}

// SelectionConfig holds Phase 2 selection parameters.
type SelectionConfig struct {
	MaxOpenPositions int `yaml:"max_open_positions"`
}

// CapitalConfig holds Phase 2 capital sizing parameters.
type CapitalConfig struct {
	// WalletAddress and WalletPrivateKey are excluded from JSON serialization
	// (json:"-") so that Config.Snapshot() never embeds credentials in the
	// strategy_versions.config_snapshot column. They are runtime-only values
	// loaded from env vars (SNIPER_WALLET_ADDRESS / SNIPER_WALLET_KEY) and
	// must never be stored in the database.
	WalletAddress          string  `yaml:"wallet_address"    json:"-"`
	WalletPrivateKey       string  `yaml:"wallet_private_key" json:"-"`
	FixedEntrySizeUsd      float64 `yaml:"fixed_entry_size_usd"`
	MaxTotalExposureUsd    float64 `yaml:"max_total_exposure_usd"`
	MaxConcurrentPositions int     `yaml:"max_concurrent_positions"`
	MaxSizeUsd             float64 `yaml:"max_size_usd"`
	TTLSeconds             int     `yaml:"ttl_seconds"`
}

// PositionConfig holds Phase 2 position management parameters.
type PositionConfig struct {
	Tp1Bps              int32 `yaml:"tp1_bps"`
	Tp2Bps              int32 `yaml:"tp2_bps"`
	SlBps               int32 `yaml:"sl_bps"`
	MaxHoldSeconds      int32 `yaml:"max_hold_seconds"`
	PollIntervalSeconds int   `yaml:"poll_interval_seconds"`
}

// ExecutionPhase3Config holds Phase 3 execution retry and replacement parameters.
type ExecutionPhase3Config struct {
	MaxRetry                 int     `yaml:"max_retry"`
	MaxReplacements          int     `yaml:"max_replacements"`
	RetryBackoffMs           []int   `yaml:"retry_backoff_ms"`
	ReplacementThresholdMs   int     `yaml:"replacement_threshold_ms"`
	DropTimeoutMs            int     `yaml:"drop_timeout_ms"`
	FeeBumpMultiplier        float64 `yaml:"fee_bump_multiplier"`
	PollIntervalMs           int     `yaml:"poll_interval_ms"`
	ConcurrencyLimit         int     `yaml:"concurrency_limit"`
	ConcurrencyMin           int     `yaml:"concurrency_min"`
	ConcurrencyMax           int     `yaml:"concurrency_max"`
	DefaultMaxSlippageBps    int32   `yaml:"default_max_slippage_bps"`
}

// EvaluationConfig holds Phase 3 evaluation engine parameters.
type EvaluationConfig struct {
	FPLossThresholdPct  float64 `yaml:"fp_loss_threshold_pct"`
	FNGainThresholdPct  float64 `yaml:"fn_gain_threshold_pct"`
	WindowSeconds       int     `yaml:"window_seconds"`
}

// StateMachineConfig holds Phase 3 state machine enforcement parameters.
type StateMachineConfig struct {
	QuarantineThreshold int `yaml:"quarantine_threshold"`
}

// EventPriorityWeights maps event types to base priority values.
// Used by ComputePriority in resource_control package.
type EventPriorityWeights struct {
	PositionEventExit   int32 `yaml:"position_event_exit"`
	ExecutionReplacement int32 `yaml:"execution_replacement"`
	PositionEventOpen   int32 `yaml:"position_event_open"`
	AllocationEvent     int32 `yaml:"allocation_event"`
	ValidatedEdgeEvent  int32 `yaml:"validated_edge_event"`
	EdgeEvent           int32 `yaml:"edge_event"`
	FeatureEvent        int32 `yaml:"feature_event"`
	DataQualityEvent    int32 `yaml:"data_quality_event"`
	MarketDataEvent     int32 `yaml:"market_data_event"`
	AdjustmentEvent     int32 `yaml:"adjustment_event"`
}

// Load reads configuration from one or more YAML config files.
// Files are merged in order (later files override earlier ones for shared keys).
// Environment variables can override values: SNIPER_DB_PASSWORD overrides database.password.
// Returns an error if any required key is missing or files cannot be parsed.
func Load(paths ...string) (*Config, error) {
	if len(paths) == 0 {
		// Default: load pipeline.yaml; merge chains.yaml when present.
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("config: get working directory: %w", err)
		}
		paths = []string{filepath.Join(cwd, "config", "pipeline.yaml")}
		chainsPath := filepath.Join(cwd, "config", "chains.yaml")
		if _, statErr := os.Stat(chainsPath); statErr == nil {
			paths = append(paths, chainsPath)
		}
	}

	cfg := &Config{}
	for _, path := range paths {
		if err := loadFile(path, cfg); err != nil {
			return nil, err
		}
	}

	// Apply environment variable overrides.
	applyEnvOverrides(cfg)

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	cfg.SchemaVersion = cfg.Pipeline.SchemaVersion
	return cfg, nil
}

// Validate checks that all required configuration fields are present.
func (c *Config) Validate() error {
	var missing []string

	if c.Database.Host == "" {
		missing = append(missing, "database.host")
	}
	if c.Database.Port == 0 {
		missing = append(missing, "database.port")
	}
	if c.Database.Database == "" {
		missing = append(missing, "database.database")
	}
	if c.Database.User == "" {
		missing = append(missing, "database.user")
	}

	if len(missing) > 0 {
		return fmt.Errorf("config: missing required keys: %s", strings.Join(missing, ", "))
	}
	return nil
}

// Snapshot returns canonical JSON of all tunable config parameters.
// Used to derive the StrategyVersionID.
func (c *Config) Snapshot() ([]byte, error) {
	return json.Marshal(c)
}

// DBPassword returns the database password from SNIPER_DB_PASSWORD env var.
func (c *Config) DBPassword() string {
	return os.Getenv("SNIPER_DB_PASSWORD")
}

// Port returns the HTTP server port from PORT env var (default: 8080).
func (c *Config) Port() string {
	return getEnv("PORT", "8080")
}

func loadFile(path string, cfg *Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("config: parse %s: %w", path, err)
	}
	return nil
}

func applyEnvOverrides(cfg *Config) {
	if v := os.Getenv("SNIPER_DB_HOST"); v != "" {
		cfg.Database.Host = v
	}
	if v := os.Getenv("SNIPER_DB_NAME"); v != "" {
		cfg.Database.Database = v
	}
	if v := os.Getenv("SNIPER_DB_USER"); v != "" {
		cfg.Database.User = v
	}
	if v := os.Getenv("SNIPER_DB_SSL_MODE"); v != "" {
		cfg.Database.SSLMode = v
	}
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
	if v := os.Getenv("SNIPER_WALLET_ADDRESS"); v != "" {
		cfg.Capital.WalletAddress = v
	}
	if v := os.Getenv("SNIPER_WALLET_KEY"); v != "" {
		cfg.Capital.WalletPrivateKey = v
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
