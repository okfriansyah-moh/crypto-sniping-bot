package config

import (
	"encoding/json"
	"fmt"
	"log/slog"
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
	Chains       map[string]ChainConfig `yaml:"chains"` // per-chain EVM ingestion config
	Solana       SolanaConfig           `yaml:"solana"` // Solana ingestion config (Phase 7)
	Edge         EdgeConfig             `yaml:"edge"`
	Validation   ValidationConfig       `yaml:"validation"`
	Selection    SelectionConfig        `yaml:"selection"`
	Capital      CapitalConfig          `yaml:"capital"`
	Position     PositionConfig         `yaml:"position"`
	Execution    ExecutionConfig        `yaml:"execution"` // Phase 3+4 combined
	Evaluation   EvaluationConfig       `yaml:"evaluation"`
	StateMachine StateMachineConfig     `yaml:"state_machine"`
	EventWeights EventPriorityWeights   `yaml:"event_weights"`
	Models       ModelsConfig           `yaml:"models"`
	Learning     LearningConfig         `yaml:"learning"`
	Risk         RiskConfig             `yaml:"risk"`
	ModeAdaptive ModeAdaptiveConfig     `yaml:"mode_adaptive"`
	Retention    RetentionConfig        `yaml:"retention"`
	MEV          MEVConfig              `yaml:"mev"`
	Budgets      BudgetsConfig          `yaml:"budgets"`
	Hardening    HardeningConfig        `yaml:"hardening"` // Phase 8 production hardening

	// Phase 9 (Profitability Restoration) — additive runtime configs.
	// Loaded from config/data_quality.yaml, config/feature.yaml,
	// config/probability.yaml. Structs defined in:
	//   data_quality_runtime_config.go, feature_runtime_config.go,
	//   probability_runtime_config.go, capital_runtime_config.go.
	DataQualityRuntime DataQualityRuntimeConfig `yaml:"data_quality"`
	Feature            FeatureRuntimeConfig     `yaml:"feature"`
	ProbabilityRuntime ProbabilityRuntimeConfig `yaml:"probability"`

	// Residual-risk #3 closure: per-market slippage α aggregator.
	ExecutionQuality ExecutionQualityConfig `yaml:"execution_quality"`

	// Residual-risk #4 scaffolding: optional MarketDataDTO enrichment
	// stage. Default-OFF — populated only when the operator deploys a
	// simulation contract and flips probes.enabled.
	Probes ProbesConfig `yaml:"probes"`

	// Phase 10 — time-banded rescan worker (Layer 0.5).
	// Disabled by default. See docs/PLAN.md § Task 1 and
	// internal/app/config/rescan_config.go for struct definitions.
	Rescan RescanConfig `yaml:"rescan"`

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
	MinVelocityScore     float64 `yaml:"min_velocity_score"`
	MinLiquidityScore    float64 `yaml:"min_liquidity_score"`
	MaxAgeSeconds        int64   `yaml:"max_age_seconds"`
	BaseWindowMs         int32   `yaml:"base_window_ms"`
	WindowMomentumFactor float64 `yaml:"window_momentum_factor"`
	TTLSeconds           int     `yaml:"ttl_seconds"`

	// Phase 11 (Reference-Repo Improvements R2 — DETECT/EDGE).
	// MaxDevBuyPctBps: reject EdgeDTO when EdgeDTO.DevBuyPctBps exceeds
	//   this cap (m8s-lab heuristic; 0 = disabled).
	// MaxCreatorRugCount: reject when the creator has at least this
	//   many prior confirmed rugs (0 = disabled).
	// MinDevWalletAgeSeconds: reject brand-new creator wallets younger
	//   than this. AxisBot heuristic. 0 = disabled.
	MaxDevBuyPctBps        int32 `yaml:"max_dev_buy_pct_bps"`
	MaxCreatorRugCount     int32 `yaml:"max_creator_rug_count"`
	MinDevWalletAgeSeconds int64 `yaml:"min_dev_wallet_age_seconds"`

	// ── F-4 fix: edge taxonomy + adaptive momentum threshold ──────────
	// (per docs/architecture.md § 3.3 and the edge-detection /
	// momentum-detector / signal-normalizer skills).

	// NewLaunchWindowSeconds: pool-age ceiling (seconds) for the
	// NEW_LAUNCH_EDGE path. Tokens older than this fall through to the
	// MOMENTUM_EDGE path. Per skill default: 300s (5 min).
	NewLaunchWindowSeconds int64 `yaml:"new_launch_window_seconds"`

	// MinContractSafety: NEW_LAUNCH_EDGE requires this floor on
	// FeatureDTO.ContractSafety (defence-in-depth — DQ should already
	// have rejected unsafe tokens). 0 disables the floor.
	MinContractSafety float64 `yaml:"min_contract_safety"`

	// NEW_LAUNCH_EDGE strength weights (sum should be 1.0). Defaults
	// applied in code when zero.
	NewLaunchWeightLiquidity float64 `yaml:"new_launch_weight_liquidity"`
	NewLaunchWeightSafety    float64 `yaml:"new_launch_weight_safety"`
	NewLaunchWeightHolders   float64 `yaml:"new_launch_weight_holders"`
	NewLaunchWeightEntropy   float64 `yaml:"new_launch_weight_entropy"`

	// MOMENTUM_EDGE strength weights (sum should be 1.0).
	MomentumWeightPrice    float64 `yaml:"momentum_weight_price"`
	MomentumWeightVolume   float64 `yaml:"momentum_weight_volume"`
	MomentumWeightVelocity float64 `yaml:"momentum_weight_velocity"`

	// MinPriceMomentum: cold-start fallback threshold and absolute floor
	// for the adaptive PriceMomentum percentile (NEVER below this).
	MinPriceMomentum float64 `yaml:"min_price_momentum"`
	// MinVolumeMomentum: hard gate on VolumeMomentum for MOMENTUM_EDGE.
	MinVolumeMomentum float64 `yaml:"min_volume_momentum"`
	// MomentumQuantile: rolling-window quantile used to derive the
	// adaptive PriceMomentum threshold once enough samples exist
	// (default 0.7 per momentum-detector skill).
	MomentumQuantile float64 `yaml:"momentum_quantile"`
	// BaselineMinSamples: cold-start gate. Below this, adaptive threshold
	// is bypassed in favour of MinPriceMomentum.
	BaselineMinSamples int `yaml:"baseline_min_samples"`
	// BaselineMaxLen: ring-buffer cap per (market, signal). Bounded
	// memory — oldest values are evicted past this limit.
	BaselineMaxLen int `yaml:"baseline_max_len"`

	// Residual-risk #1 — debounced rolling-window baseline persistence.
	// See FeatureRuntimeConfig for field semantics. Minimums enforced
	// by validate_ranges: flush_interval_sec >= 5, flush_max_writes >= 1.
	BaselineFlushIntervalSec int `yaml:"baseline_flush_interval_sec"`
	BaselineFlushMaxWrites   int `yaml:"baseline_flush_max_writes"`

	// ModelVersion: stamped onto every emitted EdgeDTO.EdgeModelVersionID
	// for attribution and replay differencing.
	ModelVersion string `yaml:"model_version"`
}

// ValidationConfig holds Phase 2 EV gate parameters (fixed priors).
type ValidationConfig struct {
	PriorProbability float64 `yaml:"prior_probability"`
	PriorGainBps     int32   `yaml:"prior_gain_bps"`
	PriorLossBps     int32   `yaml:"prior_loss_bps"`
	PriorSlippageBps int32   `yaml:"prior_slippage_bps"`
	EvThresholdBps   int32   `yaml:"ev_threshold_bps"`
	FixedCostsBps    int32   `yaml:"fixed_costs_bps"`
	BuildSubmitP95Ms int32   `yaml:"build_submit_p95_ms"`
	TTLSeconds       int     `yaml:"ttl_seconds"`

	// Phase 10 (Reference-Repo Improvements / Task D) — consecutive-pass
	// debounce gate. Mirrors mux's CONSECUTIVE_FILTER_MATCHES + window.
	// When RequiredConsecutivePasses <= 1 the gate is disabled.
	//
	// NOTE (wiring): the pure debounce helper lives at
	// internal/modules/validation/consecutive_debounce.go
	// (Module.ProcessWithDebounce). It is intentionally NOT yet invoked
	// from ValidationWorker because durable PriorPassState persistence
	// (per token_lifecycle_id, with bounded TTL) requires an additive
	// adapter method + side table that are scheduled for the next
	// validation-pipeline phase. Until that wiring lands these fields
	// remain inert — leaving them at 0 in YAML preserves legacy
	// single-pass behaviour and is the only safe configuration today.
	RequiredConsecutivePasses    int32 `yaml:"required_consecutive_passes"`
	ConsecutivePassWindowSeconds int32 `yaml:"consecutive_pass_window_seconds"`

	// Bus dependency-wait — bounded join window for the ValidationWorker.
	// Validation triggers on edge_event but joins probability/slippage by
	// trace_id from the DB-backed event-bus state. If those upstream
	// producers haven't committed by JoinTimeoutMs, validation rejects
	// with an explicit reason ("probability_unavailable") rather than
	// silently substituting the prior into EV — that substitution
	// produces deterministic mass-rejects at large negative bps and
	// starves Layers 6–10 (see docs/architecture.md § 2 / § 3.5).
	//
	// JoinTimeoutMs        — hard cap on the bounded wait, ms.
	// JoinPollIntervalMs   — DB poll cadence within the bounded wait, ms.
	//
	// JoinTimeoutMs <= 0 disables the wait (single-shot read; legacy
	// behaviour for tests / replay). Production YAML must set both > 0.
	JoinTimeoutMs      int `yaml:"join_timeout_ms"`
	JoinPollIntervalMs int `yaml:"join_poll_interval_ms"`
}

// SelectionConfig holds Phase 2 selection parameters.
type SelectionConfig struct {
	MaxOpenPositions int `yaml:"max_open_positions"`

	// Phase 11 (Reference-Repo Improvements R2 — SELECT) — per-creator
	// dedup. mux's pattern: at most this many open positions per
	// creator wallet. 0 = disabled.
	//
	// NOTE (wiring): the pure helper lives at
	// internal/modules/selection/per_creator_dedup.go
	// (FilterByCreatorOpenPositions). It is intentionally NOT yet
	// invoked from SelectionWorker because per-creator counting
	// requires creator metadata to flow through ValidatedEdgeDTO and
	// PositionStateDTO (additive DTO fields scheduled for the next
	// selection-pipeline phase). Until that DTO+adapter wiring lands
	// this field stays inert — leaving it at 0 in YAML preserves
	// legacy behaviour and is the only safe configuration today.
	MaxPositionsPerCreator int `yaml:"max_positions_per_creator"`
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

	// Phase 9 (Profitability Restoration § 9.4) — dynamic sizing fields.
	// Mirror config/capital.yaml. Module code reads these directly; legacy
	// FixedEntrySizeUsd is retained as documented fallback only.
	UseDynamicSizing       bool                       `yaml:"use_dynamic_sizing"`
	BaseSizeUsd            float64                    `yaml:"base_size_usd"`
	MinSizeUsd             float64                    `yaml:"min_size_usd"`
	Kelly                  CapitalKellyConfig         `yaml:"kelly"`
	ModeMultipliers        map[string]float64         `yaml:"mode_multipliers"`
	ModeFreshnessSec       int                        `yaml:"mode_freshness_sec"`
	Cohort                 CapitalCohortConfig        `yaml:"cohort"`
	Exploration            CapitalExplorationConfig   `yaml:"exploration"`
	MinAggregateConfidence float64                    `yaml:"min_aggregate_confidence"`
	FailurePolicy          CapitalFailurePolicyConfig `yaml:"failure_policy"`

	// Phase 10 (Reference-Repo Improvements / Task B) — de-hardcode
	// AllocationDTO fields. Defaults preserve legacy behaviour:
	//   * DefaultMaxSlippageBps == 0 → module uses legacy 200 bps.
	//   * WalletShardCount  <= 0  → sharding disabled, wallet_shard=0
	//                                emitted (legacy ShardIndex returns 0).
	//   * DefaultCohortID   == "" → module uses legacy "default".
	DefaultMaxSlippageBps int32  `yaml:"default_max_slippage_bps"`
	WalletShardCount      int    `yaml:"wallet_shard_count"`
	DefaultCohortID       string `yaml:"default_cohort_id"`
}

// PositionConfig holds Phase 2 position management parameters.
// Phase 10 (Reference-Repo Improvements) extends this with trailing
// stop, partial-TP1 scaling and volume-staleness time exit.
type PositionConfig struct {
	Tp1Bps              int32 `yaml:"tp1_bps"`
	Tp2Bps              int32 `yaml:"tp2_bps"`
	SlBps               int32 `yaml:"sl_bps"`
	MaxHoldSeconds      int32 `yaml:"max_hold_seconds"`
	PollIntervalSeconds int   `yaml:"poll_interval_seconds"`

	// Phase 10 / Task A — Trailing stop activated AFTER TP1 hit.
	// When 0, trailing stop is disabled (legacy behaviour).
	TrailingStopBps       int32 `yaml:"trailing_stop_bps"`
	Tp1FilledPctBps       int32 `yaml:"tp1_filled_pct_bps"` // 0..10000; e.g. 5000 = sell 50 % at TP1
	TrailingActivateAtTp1 bool  `yaml:"trailing_activate_at_tp1"`

	// Phase 10 / Task E — Volume-staleness time exit. When both > 0
	// and the position has been held longer than VolumeStalenessSeconds
	// while last 24h volume increased by less than
	// VolumeStalenessMinDeltaPctBps, force exit (reason TIME_VOLUME_STALE).
	VolumeStalenessSeconds        int32 `yaml:"volume_staleness_seconds"`
	VolumeStalenessMinDeltaPctBps int32 `yaml:"volume_staleness_min_delta_pct_bps"`
}

// SolanaExecutionConfig holds Phase 7 Solana execution parameters.
type SolanaExecutionConfig struct {
	SlippageCapBps         int32    `yaml:"slippage_cap_bps"`
	ConfirmTimeoutMs       int      `yaml:"confirm_timeout_ms"`
	ReceiptPollIntervalMs  int      `yaml:"receipt_poll_interval_ms"`
	MaxSendAttempts        int      `yaml:"max_send_attempts"`
	WalletKeyPaths         []string `yaml:"wallet_key_paths"`
	ComputeUnitLimitBuffer int      `yaml:"compute_unit_limit_buffer"`
	PriorityFeeLamports    int64    `yaml:"priority_fee_lamports"`
}

// ExecutionConfig holds Phase 3+4 execution parameters: retry/replacement (Phase 3)
// and private RPC routing (Phase 4).
type ExecutionConfig struct {
	// Phase 3: retry and fee-bump parameters
	MaxRetry               int     `yaml:"max_retry"`
	MaxReplacements        int     `yaml:"max_replacements"`
	RetryBackoffMs         []int   `yaml:"retry_backoff_ms"`
	ReplacementThresholdMs int     `yaml:"replacement_threshold_ms"`
	DropTimeoutMs          int     `yaml:"drop_timeout_ms"`
	FeeBumpMultiplier      float64 `yaml:"fee_bump_multiplier"`
	PollIntervalMs         int     `yaml:"poll_interval_ms"`
	ConcurrencyLimit       int     `yaml:"concurrency_limit"`
	ConcurrencyMin         int     `yaml:"concurrency_min"`
	ConcurrencyMax         int     `yaml:"concurrency_max"`
	DefaultMaxSlippageBps  int32   `yaml:"default_max_slippage_bps"`
	// Phase 4: private RPC routing
	PrivateRouteThresholdUsd float64 `yaml:"private_route_threshold_usd"`
	// PrivateEndpoints may contain API keys embedded in URLs (e.g. Alchemy, Infura).
	// json:"-" prevents them from being serialized into Config.Snapshot() and stored
	// in the strategy_versions.config_snapshot column.
	PrivateEndpoints []string `yaml:"private_endpoints"         json:"-"`
	Mode             string   `yaml:"mode"` // "live" | "shadow" (Phase 5)
	// Gas: configurable gas limit for swap transactions.
	// Override per-chain if different token contracts require more gas.
	GasLimit uint64 `yaml:"gas_limit"`
	// TxPollIntervalSeconds is how often to poll for a transaction receipt.
	TxPollIntervalSeconds int `yaml:"tx_poll_interval_seconds"`
	// TxTimeoutSeconds is the maximum time to wait for a transaction to be confirmed.
	// Maps to tx_timeout_seconds in config/execution.yaml.
	// Takes precedence over DropTimeoutMs when set.
	TxTimeoutSeconds int `yaml:"tx_timeout_seconds"`
	// EthPriceUsd is a static ETH/USD approximation used to convert USD
	// allocation amounts to wei. Update this when ETH price moves significantly.
	// A future phase will replace this with a real-time price feed.
	EthPriceUsd float64 `yaml:"eth_price_usd"`
	// Phase 7: Solana execution parameters
	Solana SolanaExecutionConfig `yaml:"solana"`

	// Phase 10 (Reference-Repo Improvements / Task C) — adaptive priority fee.
	// When mode == "adaptive", AdaptivePriorityFeeWei scales the RPC-suggested
	// priority fee by (1 + latencyErrPct), clamped to [MinMultiplier, MaxMultiplier].
	// mode == "static" (default) leaves the suggested fee untouched.
	PriorityFee PriorityFeeConfig `yaml:"priority_fee"`
}

// PriorityFeeConfig governs the adaptive priority-fee policy used by the
// EVM execution module. See internal/modules/execution/priority_fee.go.
type PriorityFeeConfig struct {
	Mode          string  `yaml:"mode"`           // "static" | "adaptive"
	MinMultiplier float64 `yaml:"min_multiplier"` // e.g. 1.0
	MaxMultiplier float64 `yaml:"max_multiplier"` // e.g. 3.0 (cap to avoid runaway)
}

// EvaluationConfig holds Phase 3 evaluation engine parameters.
type EvaluationConfig struct {
	FPLossThresholdPct float64 `yaml:"fp_loss_threshold_pct"`
	FNGainThresholdPct float64 `yaml:"fn_gain_threshold_pct"`
	WindowSeconds      int     `yaml:"window_seconds"`

	// Phase 11 (Reference-Repo Improvements R2 — EVALUATE) — enable the
	// simulated-vs-realized variance computation when the orchestrator
	// captures a pre-trade simulation. When false, ExecutionVarianceBps
	// stays zero (legacy behaviour).
	EnableSimulationDiff bool `yaml:"enable_simulation_diff"`
}

// StateMachineConfig holds Phase 3 state machine enforcement parameters.
type StateMachineConfig struct {
	QuarantineThreshold int `yaml:"quarantine_threshold"`
}

// EventPriorityWeights maps event types to base priority values.
// Used by ComputePriority in resource_control package.
type EventPriorityWeights struct {
	PositionEventExit    int32 `yaml:"position_event_exit"`
	ExecutionReplacement int32 `yaml:"execution_replacement"`
	PositionEventOpen    int32 `yaml:"position_event_open"`
	AllocationEvent      int32 `yaml:"allocation_event"`
	ValidatedEdgeEvent   int32 `yaml:"validated_edge_event"`
	EdgeEvent            int32 `yaml:"edge_event"`
	FeatureEvent         int32 `yaml:"feature_event"`
	DataQualityEvent     int32 `yaml:"data_quality_event"`
	MarketDataEvent      int32 `yaml:"market_data_event"`
	AdjustmentEvent      int32 `yaml:"adjustment_event"`
}

// ModelsConfig holds Phase 4 model parameters (probability, slippage, latency).
// All values are loaded from config/pipeline.yaml; safe defaults are applied
// when keys are absent so existing Phase 2/3 configs remain valid.
type ModelsConfig struct {
	Probability                ProbabilityCoefficients `yaml:"probability"`
	Slippage                   SlippageModelConfig     `yaml:"slippage"`
	Latency                    LatencyModelConfig      `yaml:"latency"`
	LatencyProfileIntervalSecs int                     `yaml:"latency_profile_interval_seconds"`
	ModelJoinTimeoutMs         int                     `yaml:"model_join_timeout_ms"`
}

// ProbabilityCoefficients are the fixed weights for the Phase 4 logistic model.
type ProbabilityCoefficients struct {
	Bias                float64 `yaml:"bias"`
	WLiquidityScore     float64 `yaml:"w_liquidity_score"`
	WTxVelocityScore    float64 `yaml:"w_tx_velocity_score"`
	WHolderDistribution float64 `yaml:"w_holder_distribution"`
	WWalletEntropy      float64 `yaml:"w_wallet_entropy"`
	WContractSafety     float64 `yaml:"w_contract_safety"`
	WTokenAge           float64 `yaml:"w_token_age"`
	WVolumeMomentum     float64 `yaml:"w_volume_momentum"`
	WPriceMomentum      float64 `yaml:"w_price_momentum"`
	ModelVersionID      string  `yaml:"model_version_id"`
	BrierCalibration    float64 `yaml:"brier_calibration"`
}

// SlippageModelConfig holds the slippage CPMM-model parameters. Bucket
// fields are retained for backward-compatible YAML loading only — the
// CPMM model (F-3 fix) does not consult them.
type SlippageModelConfig struct {
	Buckets        []SlippageBucketConfig `yaml:"buckets"`
	FallbackP50Bps int32                  `yaml:"fallback_p50_bps"`
	FallbackP95Bps int32                  `yaml:"fallback_p95_bps"`
	ModelVersionID string                 `yaml:"model_version_id"`

	// CPMM model parameters (F-3 fix).
	MaxSlippageBps int32   `yaml:"max_slippage_bps"`
	VolatilityZ    float64 `yaml:"volatility_z"`
	TailBps        int32   `yaml:"tail_bps"`
	MinReserveUsd  float64 `yaml:"min_reserve_usd"`
	DefaultAlpha   float64 `yaml:"default_alpha"`
	MaxAlpha       float64 `yaml:"max_alpha"`

	// Phase 11 (Reference-Repo R2 — P/S/L MODELS) — congestion-aware
	// multiplier. When enabled, the slippage model scales BaseP95 by 1 +
	// clamp((latencyP95 - anchor) / anchor, 0, MaxMultiplier - 1).
	// Disabled = always 1.0 (legacy).
	Congestion SlippageCongestionConfig `yaml:"congestion"`
}

// SlippageCongestionConfig governs the latency-driven slippage uplift.
type SlippageCongestionConfig struct {
	Enabled         bool    `yaml:"enabled"`
	LatencyAnchorMs int32   `yaml:"latency_anchor_ms"` // baseline RPC latency (e.g. 200)
	MaxMultiplier   float64 `yaml:"max_multiplier"`    // hard cap (e.g. 2.0)
}

// SlippageBucketConfig is a single (liquidity, size) calibration entry.
type SlippageBucketConfig struct {
	LiquidityMaxUsd float64 `yaml:"liquidity_max_usd"`
	SizeMaxUsd      float64 `yaml:"size_max_usd"`
	P50Bps          int32   `yaml:"p50_bps"`
	P95Bps          int32   `yaml:"p95_bps"`
}

// LatencyModelConfig holds rolling-window settings + fallbacks.
type LatencyModelConfig struct {
	WindowSeconds int32 `yaml:"window_seconds"`
	MinSamples    int   `yaml:"min_samples"`
	FallbackP50Ms int32 `yaml:"fallback_p50_ms"`
	FallbackP95Ms int32 `yaml:"fallback_p95_ms"`
}

// LearningConfig holds Phase 5 learning engine parameters.
type LearningConfig struct {
	// EvalWindowMinutes is how often the evaluator runs (default: 60).
	EvalWindowMinutes int `yaml:"eval_window_minutes"`
	// EvalWindowSeconds is the lookback window for evaluation (default: 86400 = 24h).
	EvalWindowSeconds int `yaml:"eval_window_seconds"`
	// MinSampleSize is the minimum number of records required before updating (default: 30).
	MinSampleSize int `yaml:"min_sample_size"`
	// MaxDeltaPct is the maximum fractional change per parameter per cycle (default: 0.10).
	MaxDeltaPct float64 `yaml:"max_delta_pct"`
	// Families is the ordered list of parameter families for round-robin updates.
	Families []string `yaml:"families"`
	// ShadowWindowMinutes is the observation window before A/B promotion (default: 60).
	ShadowWindowMinutes int `yaml:"shadow_window_minutes"`
	// RollbackThresholdPct is the expectancy drop that triggers rollback (default: 0.10).
	RollbackThresholdPct float64 `yaml:"rollback_threshold_pct"`
	// PostPromotionWatchMinutes is the post-promotion monitoring window (default: 120).
	PostPromotionWatchMinutes int `yaml:"post_promotion_watch_minutes"`
	// ShadowPollIntervalSeconds is how often the shadow observer runs (default: 60).
	ShadowPollIntervalSeconds int `yaml:"shadow_poll_interval_seconds"`
	// ObservationWindowSeconds is how long to track a rejected token's return (default: 3600).
	ObservationWindowSeconds int `yaml:"observation_window_seconds"`
	// FnGainThresholdPct is the minimum return for a rejected trade to be classified FN (default: 0.10).
	FnGainThresholdPct float64 `yaml:"fn_gain_threshold_pct"`
	// RollbackCheckIntervalSeconds is how often the rollback watchdog runs (default: 300).
	RollbackCheckIntervalSeconds int `yaml:"rollback_check_interval_seconds"`

	// SybilSuspectMinWallets is the minimum UniqueWallets1m count above
	// which a losing trade with a low wash score is flagged as a probable
	// Sybil-cluster bypass (residual risk #5 / F-SEC-08). Default 50.
	SybilSuspectMinWallets int `yaml:"sybil_suspect_min_wallets"`
	// SybilSuspectMaxWashScore is the wash-score upper bound (exclusive)
	// below which the Sybil flag fires — i.e. the wash detector said the
	// token was clean. Range [0,1]. Default 0.30.
	SybilSuspectMaxWashScore float64 `yaml:"sybil_suspect_max_wash_score"`

	// Phase 11 (Reference-Repo Improvements R2 — LEARN) — creator
	// blacklist plumbing. When Enabled, every confirmed rug observation
	// for a creator increments creator_blacklist.rug_count; the Edge
	// module rejects future tokens once the count reaches
	// MinRugsForBlacklist (paired with EdgeConfig.MaxCreatorRugCount).
	CreatorBlacklist CreatorBlacklistConfig `yaml:"creator_blacklist"`
}

// CreatorBlacklistConfig governs Layer-10 creator-rug blacklist updates.
type CreatorBlacklistConfig struct {
	Enabled             bool  `yaml:"enabled"`
	MinRugsForBlacklist int32 `yaml:"min_rugs_for_blacklist"` // typically 1
}

// RiskConfig holds Phase 6 global kill-switch / drawdown risk parameters.
type RiskConfig struct {
	// CheckIntervalSeconds is how often the risk controller runs (default: 30).
	CheckIntervalSeconds int `yaml:"check_interval_seconds"`
	// DrawdownWindowHours is the look-back window for computing drawdown (default: 24).
	DrawdownWindowHours int `yaml:"drawdown_window_hours"`
	// DegradedDrawdownPct triggers DEGRADED mode (default: 0.05 = 5%).
	DegradedDrawdownPct float64 `yaml:"degraded_drawdown_pct"`
	// HaltDrawdownPct triggers HALTED mode (default: 0.10 = 10%).
	HaltDrawdownPct float64 `yaml:"halt_drawdown_pct"`
	// ResumeDrawdownPct auto-resumes to BALANCED when drawdown recovers below this (default: 0.03).
	ResumeDrawdownPct float64 `yaml:"resume_drawdown_pct"`
	// DegradedSizeMultiplier scales SizeUsd in DEGRADED mode (default: 0.5).
	DegradedSizeMultiplier float64 `yaml:"degraded_size_multiplier"`
}

// ModeAdaptiveConfig governs the adaptive risk-appetite controller that
// transitions the system between STRICT / BALANCED / EXPLORATION based on
// starvation and rug/FP-rate signals (operational-modes skill). It is
// orthogonal to the drawdown-driven safety mode controller in RiskConfig:
// the adaptive controller skips entirely when the system is DEGRADED or
// HALTED.
type ModeAdaptiveConfig struct {
	// Enabled gates the entire adaptive controller. When false, only the
	// safety-mode (drawdown) controller and manual /mode commands change
	// the mode.
	Enabled bool `yaml:"enabled"`
	// AdaptiveWindowSec is the look-back window over which rug/FP rates
	// are computed. Default 1800s (30 min).
	AdaptiveWindowSec int `yaml:"adaptive_window_sec"`
	// StarvationTriggerSec triggers an auto-upgrade once the time since
	// the last validated_edge_event exceeds this. Default 1800s.
	StarvationTriggerSec int `yaml:"starvation_trigger_sec"`
	// RugRateAutoDowngrade triggers a one-notch auto-downgrade when the
	// observed rug rate exceeds this fraction. Range [0,1]. Default 0.15.
	RugRateAutoDowngrade float64 `yaml:"rug_rate_auto_downgrade"`
	// FPRateAutoDowngrade triggers a one-notch auto-downgrade when the
	// observed false-positive rate exceeds this fraction. Range [0,1].
	// Default 0.25.
	FPRateAutoDowngrade float64 `yaml:"fp_rate_auto_downgrade"`
	// TransitionWindowSec bounds adaptive transitions to at most one per
	// window. Default 3600s (1 hour).
	TransitionWindowSec int `yaml:"transition_window_sec"`
	// DefaultStartupMode is the cold-start mode persisted on the first
	// risk-controller tick when state.Mode is empty. One of
	// {BALANCED, STRICT, EXPLORATION}. Default BALANCED.
	DefaultStartupMode string `yaml:"default_startup_mode"`
}

// RetentionConfig holds Phase 6 data retention / archival parameters.
type RetentionConfig struct {
	// IntervalHours is how often the archive worker runs (default: 24).
	IntervalHours int `yaml:"interval_hours"`
	// HotDays is the number of days to keep events in the hot table (default: 7).
	HotDays int `yaml:"hot_days"`
	// WarmDays is the maximum age of processed events before archival (default: 30).
	WarmDays int `yaml:"warm_days"`
	// BatchSize is the number of events moved per archival run (default: 10000).
	BatchSize int `yaml:"batch_size"`
}

// MEVConfig holds Phase 6 MEV-aware execution routing parameters.
type MEVConfig struct {
	// PrivateSizeThresholdUsd is the trade size above which private routing is used (default: 500).
	PrivateSizeThresholdUsd float64 `yaml:"private_size_threshold_usd"`
	// PreferredPrivate is the default private relay when above threshold (default: "flashbots").
	PreferredPrivate string `yaml:"preferred_private"`
	// SlippageGuardBps is the amountOutMin guard in basis points (default: 150).
	SlippageGuardBps int32 `yaml:"slippage_guard_bps"`
	// FrontRunWindowMs is the time window for front-run pattern detection (default: 500).
	FrontRunWindowMs int `yaml:"front_run_window_ms"`
}

// BudgetsConfig holds Phase 6 resource budget parameters.
type BudgetsConfig struct {
	// RPCRequestsPerSecond is the token bucket rate per RPC endpoint (default: 50).
	RPCRequestsPerSecond int `yaml:"rpc_requests_per_second"`
	// RPCBurstSize is the token bucket burst capacity (default: 100).
	RPCBurstSize int `yaml:"rpc_burst_size"`
	// RPCWaitMs is how long to wait when budget is exhausted before shedding (default: 200).
	RPCWaitMs int `yaml:"rpc_wait_ms"`
	// GasWalletDailyCapGwei is the per-wallet daily gas cap in gwei (default: 1_000_000).
	GasWalletDailyCapGwei int64 `yaml:"gas_wallet_daily_cap_gwei"`
	// GasSystemDailyCapGwei is the system-wide daily gas cap in gwei (default: 5_000_000).
	GasSystemDailyCapGwei int64 `yaml:"gas_system_daily_cap_gwei"`
	// ComputeMaxQueueDepth is the maximum number of pending events before shedding (default: 1000).
	ComputeMaxQueueDepth int `yaml:"compute_max_queue_depth"`
}

// HardeningConfig holds Phase 8 production hardening parameters.
type HardeningConfig struct {
	// Reconciliation worker parameters (§ 4.10.E.2).
	ReconciliationIntervalMs   int `yaml:"reconciliation_interval_ms"`   // default: 30000
	ReconciliationToleranceBps int `yaml:"reconciliation_tolerance_bps"` // default: 50 (0.5%)

	// Worker partition parameters (§ 4.11.B).
	PartitionLeaseTTLSec      int `yaml:"partition_lease_ttl_sec"`      // default: 60
	PartitionRenewIntervalSec int `yaml:"partition_renew_interval_sec"` // default: 30

	// DLQ retry policy (§ 4.10.C).
	MaxTransientRetries   int `yaml:"max_transient_retries"`   // default: 5
	MaxApplicationRetries int `yaml:"max_application_retries"` // default: 3

	// Event bus batch size for ClaimNextEvents.
	EventClaimBatchSize int `yaml:"event_claim_batch_size"` // default: 10

	// Drain timeout for PromoteStrategyVersion (seconds).
	DrainTimeoutSec int `yaml:"drain_timeout_sec"` // default: 60

	// Evaluation deadline in seconds after execution.
	EvaluationDeadlineSec int `yaml:"evaluation_deadline_sec"` // default: 3600

	// Crash recovery: grace period before marking execution lost.
	RecoveryGraceSec int `yaml:"recovery_grace_sec"` // default: 300

	// Reorg protection: max reorg depth before triggering halt.
	MaxReorgDepth int `yaml:"max_reorg_depth"` // default: 12 for EVM
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
		budgetsPath := filepath.Join(cwd, "config", "budgets.yaml")
		if _, statErr := os.Stat(budgetsPath); statErr == nil {
			paths = append(paths, budgetsPath)
		}
		executionPath := filepath.Join(cwd, "config", "execution.yaml")
		if _, statErr := os.Stat(executionPath); statErr == nil {
			paths = append(paths, executionPath)
		}
		// Phase 9 — auto-discover the four profitability-restoration configs.
		for _, name := range []string{"data_quality.yaml", "feature.yaml", "probability.yaml", "capital.yaml"} {
			p := filepath.Join(cwd, "config", name)
			if _, statErr := os.Stat(p); statErr == nil {
				paths = append(paths, p)
			}
		}
	} else if len(paths) == 1 && strings.HasSuffix(paths[0], "pipeline.yaml") {
		// When a single pipeline.yaml path is given (e.g. from findConfigPath),
		// auto-discover sibling config files so budgets.yaml, chains.yaml,
		// and execution.yaml are always merged in.
		dir := filepath.Dir(paths[0])
		for _, name := range []string{"chains.yaml", "budgets.yaml", "execution.yaml", "data_quality.yaml", "feature.yaml", "probability.yaml", "capital.yaml"} {
			p := filepath.Join(dir, name)
			if _, statErr := os.Stat(p); statErr == nil {
				paths = append(paths, p)
			}
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

	// Apply rescan defaults (Phase 10) before validation.
	applyRescanDefaults(&cfg.Rescan)

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	if err := cfg.validateRanges(slog.Default()); err != nil {
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

	switch c.Database.SSLMode {
	case "", "disable", "require", "verify-ca", "verify-full":
		// valid values
	default:
		return fmt.Errorf("config: invalid database.ssl_mode: %q (allowed: disable, require, verify-ca, verify-full)", c.Database.SSLMode)
	}

	if len(missing) > 0 {
		return fmt.Errorf("config: missing required keys: %s", strings.Join(missing, ", "))
	}
	if err := validateRescanConfig(c.Rescan); err != nil {
		return fmt.Errorf("config: %w", err)
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
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config: read %s: %w", path, err)
	}
	// Expand ${ENV_VAR} placeholders (e.g. ${SOLANA_RPC_HTTP_1} in chains.yaml).
	data := []byte(os.ExpandEnv(string(raw)))
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return fmt.Errorf("config: parse %s: %w", path, err)
	}
	if cfg.Capital.WalletPrivateKey != "" {
		return fmt.Errorf("config: wallet_private_key must not be set in config files; use SNIPER_WALLET_KEY env var")
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
