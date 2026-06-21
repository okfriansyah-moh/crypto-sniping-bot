package contracts

// operator_api — JSON response DTOs for the operator dashboard REST API.
// These types are additive-only; they do not replace pipeline event-bus DTOs.
// Producers: internal/operator (Phase 1+), backend-dashboard HTTP handlers (Phase 2+).
// Consumers: frontend-dashboard typed API client.
//
// Source file: contracts/operator_api.go
// Canonical registry: docs/reference/dto_contracts.md § 3.20

// OverviewResponseDTO is the payload for GET /api/v1/overview.
// Maps to the mockup overview KPI grid, chain status strip, and alert banner.
type OverviewResponseDTO struct {
	Mode              string              `json:"mode"`
	ExecutionMode     string              `json:"execution_mode"` // shadow | live
	DrawdownPct       float64             `json:"drawdown_pct"`
	OpenPositions     int32               `json:"open_positions"`
	TotalExposureUsd  float64             `json:"total_exposure_usd"`
	MaxExposureUsd    float64             `json:"max_exposure_usd"`
	PnLTodayUsd       float64             `json:"pnl_today_usd"`
	PnLTodayWins      int32               `json:"pnl_today_wins"`
	PnLTodayLosses    int32               `json:"pnl_today_losses"`
	WinRate7d         float64             `json:"win_rate_7d"`
	ClosedTrades7d    int32               `json:"closed_trades_7d"`
	ShadowGate        *ShadowGateBlockDTO `json:"shadow_gate,omitempty"`
	ChainStatuses     []ChainStatusDTO    `json:"chain_statuses"`
	AlertBanner       *AlertBannerDTO     `json:"alert_banner,omitempty"`
	StrategyVersionID string              `json:"strategy_version_id"`
	UpdatedAt         string              `json:"updated_at"` // ISO 8601 UTC
}

// PnLSummaryDTO is the payload for GET /api/v1/pnl.
type PnLSummaryDTO struct {
	LookbackHours    int     `json:"lookback_hours"`
	RealizedPnLUsd   float64 `json:"realized_pnl_usd"`
	UnrealizedPnLUsd float64 `json:"unrealized_pnl_usd"`
	OpenExposureUsd  float64 `json:"open_exposure_usd"`
	DrawdownPct      float64 `json:"drawdown_pct"`
	Wins             int32   `json:"wins"`
	Losses           int32   `json:"losses"`
	WinRatePct       float64 `json:"win_rate_pct"`
	OpenPositions    int32   `json:"open_positions"`
	StuckPositions   int32   `json:"stuck_positions"`
}

// ShadowGateBlockDTO surfaces live-flip readiness from the shadow gate evaluator.
type ShadowGateBlockDTO struct {
	Pass               bool    `json:"pass"`
	Blocked            bool    `json:"blocked"`
	Reason             string  `json:"reason,omitempty"`
	TradeCount         int     `json:"trade_count"`
	AggregatePnlBps    float64 `json:"aggregate_pnl_bps"`
	AvgPnlBps          float64 `json:"avg_pnl_bps"`
	MinTrades          int     `json:"min_trades"`
	MinWindowDays      int     `json:"min_window_days"`
	MinAggregatePnlBps float64 `json:"min_aggregate_pnl_bps"`
	ExecutionMode      string  `json:"execution_mode"`
	LiveFlipHint       string  `json:"live_flip_hint,omitempty"`
}

// ChainStatusDTO is one card in the overview chain status strip.
type ChainStatusDTO struct {
	Chain             string `json:"chain"`
	Label             string `json:"label"`
	IngestionPerHour  int64  `json:"ingestion_per_hour"`
	OpenPositions     int32  `json:"open_positions"`
	ThroughputVerdict string `json:"throughput_verdict"` // CODE_DEFECT | MARKET_QUIET | GUARDRAILS_ACTIVE | HEALTHY
	Status            string `json:"status"`             // ok | warn | bad
}

// AlertBannerDTO is an optional top-of-overview warning (e.g. CODE_DEFECT).
type AlertBannerDTO struct {
	Severity string `json:"severity"` // info | warn | bad
	Message  string `json:"message"`
	Code     string `json:"code,omitempty"`
}

// PipelineStatsResponseDTO is the payload for GET /api/v1/pipeline.
type PipelineStatsResponseDTO struct {
	WindowHours       int                    `json:"window_hours"`
	Chain             string                 `json:"chain,omitempty"`
	Funnel            PipelineFunnelDTO        `json:"funnel"`
	LayerHeartbeats   []LayerHeartbeatDTO      `json:"layer_heartbeats"`
	ThroughputVerdict string                 `json:"throughput_verdict,omitempty"`
	ProbePending      *ProbePendingStatsDTO    `json:"probe_pending,omitempty"`
}

// ProbePendingStatsDTO is queue depth for deferred probe tokens.
type ProbePendingStatsDTO struct {
	PendingCount int64 `json:"pending_count"`
	DueNow       int64 `json:"due_now"`
	Expired24h   int64 `json:"expired_24h"`
	Deferred24h  int64 `json:"deferred_24h"`
}

// PipelineFunnelDTO holds cumulative L0–L10 funnel counts for the pipeline view.
// Count semantics match database.PipelineStats (cumulative except Rejected/Failed).
type PipelineFunnelDTO struct {
	Detected     int64 `json:"detected"`      // L0
	DQPassed     int64 `json:"dq_passed"`     // L1
	FeatureReady int64 `json:"feature_ready"` // L2
	EdgeDetected int64 `json:"edge_detected"` // L3
	Validated    int64 `json:"validated"`     // L5 (L4 models grouped in UI)
	Selected     int64 `json:"selected"`      // L6
	Executed     int64 `json:"executed"`      // L8
	PositionOpen int64 `json:"position_open"` // L9
	Evaluated    int64 `json:"evaluated"`     // L10
	Rejected     int64 `json:"rejected"`
	Failed       int64 `json:"failed"`
}

// LayerHeartbeatDTO is one row in the pipeline layer detail table.
type LayerHeartbeatDTO struct {
	Layer      string `json:"layer"`
	Stage      string `json:"stage"`
	WorkerName string `json:"worker_name,omitempty"`
	Count24h   int64  `json:"count_24h"`
	DropPct    string `json:"drop_pct,omitempty"`
	Status     string `json:"status"`       // ok | warn | stalled
	LastSeenAt string `json:"last_seen_at"` // ISO 8601 UTC
}

// PositionRowDTO is one open position row for GET /api/v1/positions.
type PositionRowDTO struct {
	PositionID        string  `json:"position_id"`
	TokenAddress      string  `json:"token_address"`
	Chain             string  `json:"chain"`
	Market            string  `json:"market"`
	EntryPriceUsd     float64 `json:"entry_price_usd"`
	CurrentPriceUsd   float64 `json:"current_price_usd"`
	PnLPct            float64 `json:"pnl_pct"`
	SizeUsd           float64 `json:"size_usd"`
	AgeSeconds        int64   `json:"age_seconds"`
	TraceID           string  `json:"trace_id"`
	StrategyVersionID string  `json:"strategy_version_id"`
}

// ActivityEventDTO is one entry in the recent activity feed (GET /api/v1/activity).
type ActivityEventDTO struct {
	EventID      string `json:"event_id"`
	EventType    string `json:"event_type"`
	Chain        string `json:"chain"`
	TokenAddress string `json:"token_address,omitempty"`
	Summary      string `json:"summary"`
	TraceID      string `json:"trace_id,omitempty"`
	CreatedAt    string `json:"created_at"` // ISO 8601 UTC
}

// DQBreakdownResponseDTO is the payload for GET /api/v1/dq.
type DQBreakdownResponseDTO struct {
	WindowHours      int                 `json:"window_hours"`
	Chain            string              `json:"chain,omitempty"`
	TotalDecisions   int64               `json:"total_decisions"`
	PassCount        int64               `json:"pass_count"`
	RiskyPassCount   int64               `json:"risky_pass_count"`
	RejectCount      int64               `json:"reject_count"`
	SkipCount        int64               `json:"skip_count"`
	PassRatePct      float64             `json:"pass_rate_pct"`
	TopRejectReasons []DQRejectReasonDTO `json:"top_reject_reasons"`
	// Probe completeness (% of market_data rows with Known flags set).
	SocialLinksKnownPct   float64 `json:"social_links_known_pct,omitempty"`
	TotalSupplyKnownPct   float64 `json:"total_supply_known_pct,omitempty"`
	CreatorCountKnownPct  float64 `json:"creator_count_known_pct,omitempty"`
	HolderDistKnownPct    float64 `json:"holder_dist_known_pct,omitempty"`
	FairChanceSkipCount   int64   `json:"fair_chance_skip_count,omitempty"`
}

// DQRejectReasonDTO is one row in the DQ top-reject-reasons list.
type DQRejectReasonDTO struct {
	Reason string `json:"reason"`
	Count  int64  `json:"count"`
}

// GateEvidenceResponseDTO is the payload for GET /api/v1/gate/evidence.
// Field names align with gate_review_collect.sh / gate_phase2_pass_evidence.json.
type GateEvidenceResponseDTO struct {
	Timestamp                string             `json:"timestamp"` // ISO 8601 or evidence file stamp
	DetectedMode             string             `json:"detected_mode,omitempty"`
	WSOLTokenAddressEmitted  int64              `json:"wsol_token_address_emitted"`
	IngestionValidTokenRatio float64            `json:"ingestion_valid_token_ratio"`
	MarketProbesBacklogRatio float64            `json:"market_probes_backlog_ratio"`
	DQPassOrRiskyPass        int64              `json:"dq_pass_or_risky_pass"`
	TracesCompleted          int64              `json:"traces_completed"`
	ShadowObserverFailed     int64              `json:"shadow_observer_failed"`
	ThroughputVerdict        string             `json:"throughput_verdict"` // CODE_DEFECT | MARKET_QUIET | GUARDRAILS_ACTIVE | HEALTHY
	Criteria                 []GateCriterionDTO `json:"criteria,omitempty"`
}

// GateCriterionDTO is one row in the gate review criteria grid.
type GateCriterionDTO struct {
	Label  string `json:"label"`
	Value  string `json:"value"`
	Passed bool   `json:"passed"`
}

// ConfigManifestEntryDTO is one row in GET /api/v1/configs (no secret values).
type ConfigManifestEntryDTO struct {
	Filename     string   `json:"filename"`
	SHA256Prefix string   `json:"sha256_prefix"` // first 8 hex chars of content hash
	LastModified string   `json:"last_modified"` // ISO 8601 UTC
	TopLevelKeys []string `json:"top_level_keys,omitempty"`
}
