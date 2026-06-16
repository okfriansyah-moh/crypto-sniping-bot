/**
 * TypeScript mirrors of contracts/operator_api.go (dashboard REST v1).
 * Canonical source: contracts/operator_api.go — keep in sync on DTO changes.
 */

export type ThroughputVerdict =
  | "CODE_DEFECT"
  | "MARKET_QUIET"
  | "GUARDRAILS_ACTIVE"
  | "HEALTHY"
  | string;

export type ChainStatusLevel = "ok" | "warn" | "bad" | string;
export type AlertSeverity = "info" | "warn" | "bad" | string;
export type LayerHeartbeatStatus = "ok" | "warn" | "stalled" | string;

export interface ShadowGateBlockDTO {
  pass: boolean;
  blocked: boolean;
  reason?: string;
  trade_count: number;
  aggregate_pnl_bps: number;
  avg_pnl_bps: number;
  min_trades: number;
  min_window_days: number;
  min_aggregate_pnl_bps: number;
  execution_mode: string;
  live_flip_hint?: string;
}

export interface ChainStatusDTO {
  chain: string;
  label: string;
  ingestion_per_hour: number;
  open_positions: number;
  throughput_verdict: ThroughputVerdict;
  status: ChainStatusLevel;
}

export interface AlertBannerDTO {
  severity: AlertSeverity;
  message: string;
  code?: string;
}

export interface OverviewResponseDTO {
  mode: string;
  execution_mode: string;
  drawdown_pct: number;
  open_positions: number;
  total_exposure_usd: number;
  max_exposure_usd: number;
  pnl_today_usd: number;
  pnl_today_wins: number;
  pnl_today_losses: number;
  win_rate_7d: number;
  closed_trades_7d: number;
  shadow_gate?: ShadowGateBlockDTO;
  chain_statuses: ChainStatusDTO[];
  alert_banner?: AlertBannerDTO;
  strategy_version_id: string;
  updated_at: string;
}

export interface PnLSummaryDTO {
  lookback_hours: number;
  realized_pnl_usd: number;
  unrealized_pnl_usd: number;
  open_exposure_usd: number;
  drawdown_pct: number;
  wins: number;
  losses: number;
  win_rate_pct: number;
  open_positions: number;
  stuck_positions: number;
}

export interface PipelineFunnelDTO {
  detected: number;
  dq_passed: number;
  feature_ready: number;
  edge_detected: number;
  validated: number;
  selected: number;
  executed: number;
  position_open: number;
  evaluated: number;
  rejected: number;
  failed: number;
}

export interface LayerHeartbeatDTO {
  layer: string;
  stage: string;
  worker_name?: string;
  count_24h: number;
  drop_pct?: string;
  status: LayerHeartbeatStatus;
  last_seen_at: string;
}

export interface ProbePendingStatsDTO {
  pending_count: number;
  due_now: number;
  expired_24h: number;
  deferred_24h: number;
}

export interface PipelineStatsResponseDTO {
  window_hours: number;
  chain?: string;
  funnel: PipelineFunnelDTO;
  layer_heartbeats: LayerHeartbeatDTO[];
  throughput_verdict?: ThroughputVerdict;
  probe_pending?: ProbePendingStatsDTO;
}

export interface PositionRowDTO {
  position_id: string;
  token_address: string;
  chain: string;
  market: string;
  entry_price_usd: number;
  current_price_usd: number;
  pnl_pct: number;
  size_usd: number;
  age_seconds: number;
  trace_id: string;
  strategy_version_id: string;
}

export interface ActivityEventDTO {
  event_id: string;
  event_type: string;
  chain: string;
  token_address?: string;
  summary: string;
  trace_id?: string;
  created_at: string;
}

export interface DQRejectReasonDTO {
  reason: string;
  count: number;
}

export interface DQBreakdownResponseDTO {
  window_hours: number;
  chain?: string;
  total_decisions: number;
  pass_count: number;
  risky_pass_count: number;
  reject_count: number;
  skip_count: number;
  pass_rate_pct: number;
  top_reject_reasons: DQRejectReasonDTO[];
}

export interface GateCriterionDTO {
  label: string;
  value: string;
  passed: boolean;
}

export interface GateEvidenceResponseDTO {
  timestamp: string;
  detected_mode?: string;
  wsol_token_address_emitted: number;
  ingestion_valid_token_ratio: number;
  market_probes_backlog_ratio: number;
  dq_pass_or_risky_pass: number;
  traces_completed: number;
  shadow_observer_failed: number;
  throughput_verdict: ThroughputVerdict;
  criteria?: GateCriterionDTO[];
}

export interface ConfigManifestEntryDTO {
  filename: string;
  sha256_prefix: string;
  last_modified: string;
  top_level_keys?: string[];
}

/** GET /api/v1/health — optional auth; mirrors health module JSON. */
export interface HealthResponseDTO {
  status: string;
  version: string;
  shadow_gate?: {
    pass: boolean;
    trade_count: number;
    aggregate_pnl_bps: number;
    avg_pnl_bps: number;
    min_trades: number;
    min_window_days: number;
    min_aggregate_pnl_bps: number;
    execution_mode: string;
    live_flip_hint?: string;
  };
}

export interface ApiErrorBody {
  error: string;
}

/** Common query params for chain-aware dashboard endpoints. */
export interface DashboardQueryParams {
  chain?: string;
  market?: string;
  window_hours?: number;
  limit?: number;
}
