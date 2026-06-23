import { useCallback, useEffect } from "react";
import { dashboardApi } from "../api/client";
import type { ChainStatusDTO, FortressPostureDTO, OverviewResponseDTO } from "../api/types";
import { ViewState } from "../components/ViewState";
import type { ChainId } from "../hooks/useChainFilter";
import { usePolling } from "../hooks/usePolling";
import type { DashboardView } from "../types/views";
import { chainMarketQuery } from "../utils/query";
import { formatPct, formatUsd, formatUsdSigned } from "../utils/format";

type OverviewViewProps = {
  chain: ChainId;
  market: string;
  active: boolean;
  onNavigate: (view: DashboardView) => void;
  onChainStatuses?: (statuses: ChainStatusDTO[]) => void;
};

export function OverviewView({
  chain,
  market,
  active,
  onNavigate,
  onChainStatuses,
}: OverviewViewProps) {
  const fetchOverview = useCallback(
    () => dashboardApi.getOverview(chainMarketQuery(chain, market)),
    [chain, market],
  );
  const fetchPosture = useCallback(() => dashboardApi.getPosture(), []);

  const overview = usePolling(fetchOverview, [chain, market], { enabled: active });
  const posture = usePolling(fetchPosture, [], { enabled: active });

  useEffect(() => {
    if (overview.data?.chain_statuses && onChainStatuses) {
      onChainStatuses(overview.data.chain_statuses);
    }
  }, [overview.data?.chain_statuses, onChainStatuses]);

  const loading = overview.loading && overview.data === null;
  const error = overview.error ?? posture.error;

  return (
    <ViewState loading={loading} error={error} empty={overview.data === null}>
      {overview.data ? (
        <OverviewContent
          data={overview.data}
          posture={posture.data}
          postureLoading={posture.loading && posture.data === null}
          onNavigate={onNavigate}
        />
      ) : null}
    </ViewState>
  );
}

function OverviewContent({
  data,
  posture,
  postureLoading,
  onNavigate,
}: {
  data: OverviewResponseDTO;
  posture: FortressPostureDTO | null;
  postureLoading: boolean;
  onNavigate: (view: DashboardView) => void;
}) {
  const exposurePct =
    data.max_exposure_usd > 0 ? (data.total_exposure_usd / data.max_exposure_usd) * 100 : 0;
  const pnlClass = data.pnl_today_usd >= 0 ? "value--ok" : "value--bad";

  return (
    <>
      {postureLoading ? (
        <div className="fortress-banner fortress-banner--loading hint">Loading fortress posture…</div>
      ) : posture ? (
        <FortressPostureBanner posture={posture} onNavigate={onNavigate} />
      ) : null}

      <div className="pill-row" aria-label="System status">
        <span className="pill ok">
          <span className="dot" /> Bot running
        </span>
        <span className={`pill ${data.execution_mode === "live" ? "ok" : "warn"}`}>
          <span className="dot" /> {data.execution_mode === "live" ? "Live mode" : "Shadow mode"}
        </span>
        <span className="pill">{data.mode}</span>
      </div>

      {data.chain_statuses.length > 0 ? (
        <div className="chain-status-strip" aria-label="Chain health summary">
          {data.chain_statuses.map((c) => (
            <div key={c.chain} className="chain-status-card">
              <div className="row">
                <strong>{c.label || c.chain}</strong>
                <span className={`chain-tag ${c.chain}`}>{c.chain}</span>
              </div>
              <div className="meta">
                L0: {c.ingestion_per_hour}/hr · {c.open_positions} open · {c.throughput_verdict}
              </div>
            </div>
          ))}
        </div>
      ) : null}

      {data.alert_banner ? (
        <div className={`banner ${data.alert_banner.severity} compact`}>
          <div>
            <h3>{data.alert_banner.code ?? "Alert"}</h3>
            <p>{data.alert_banner.message}</p>
          </div>
          {data.alert_banner.code === "KILL_SWITCH" ? (
            <button type="button" className="btn-link" onClick={() => onNavigate("safety")}>
              Open safety →
            </button>
          ) : null}
        </div>
      ) : null}

      <div className="grid grid-4 view-section">
        <div className="card">
          <h3>
            Today&apos;s PnL <span className="help" title="Realized PnL today">?</span>
          </h3>
          <div className={`value ${pnlClass}`}>{formatUsdSigned(data.pnl_today_usd)}</div>
          <div className="hint">
            {data.pnl_today_wins} wins · {data.pnl_today_losses} losses
          </div>
        </div>
        <div className="card">
          <h3>Open positions</h3>
          <div className="value">{data.open_positions}</div>
          <div className="hint">{formatUsd(data.total_exposure_usd)} deployed</div>
        </div>
        <div className="card">
          <h3>Exposure</h3>
          <div className="value">
            {formatUsd(data.total_exposure_usd)} / {formatUsd(data.max_exposure_usd)}
          </div>
          <div className="progress-bar">
            <span style={{ width: `${Math.min(100, exposurePct)}%` }} />
          </div>
          <div className="hint">{formatPct(exposurePct, 0)} of max</div>
        </div>
        <div className="card">
          <h3>Win rate (7d)</h3>
          <div className="value">{formatPct(data.win_rate_7d, 0)}</div>
          <div className="hint">{data.closed_trades_7d} closed</div>
        </div>
      </div>

      {data.shadow_gate ? (
        <div className="card view-section">
          <h3>Shadow live-flip gate</h3>
          <div className="value value--sm">
            {data.shadow_gate.trade_count}{" "}
            <span className="muted-inline">/ {data.shadow_gate.min_trades} trades</span>
          </div>
          <p className="hint">
            Aggregate PnL: {data.shadow_gate.aggregate_pnl_bps.toFixed(0)} bps ·{" "}
            {data.shadow_gate.pass ? (
              <span className="tag pass">PASS</span>
            ) : (
              <span className="tag skip">FAIL</span>
            )}
          </p>
        </div>
      ) : null}

      <h3 className="section-label">Drill down</h3>
      <div className="grid grid-3">
        <GlanceCard
          title="Pipeline health"
          meta={`L0: ${data.chain_statuses[0]?.ingestion_per_hour ?? "—"}/hr · view funnel`}
          onClick={() => onNavigate("pipeline")}
        />
        <GlanceCard
          title="Ingestion"
          meta={posture?.ingestion_delivery ?? "delivery modes · programs"}
          onClick={() => onNavigate("ingestion")}
        />
        <GlanceCard
          title="Executions"
          meta={`${data.execution_mode} mode · execution trail`}
          onClick={() => onNavigate("executions")}
        />
        <GlanceCard
          title="Open positions"
          meta={`${data.open_positions} active · ${formatUsd(data.total_exposure_usd)} deployed`}
          onClick={() => onNavigate("positions")}
        />
        <GlanceCard
          title="Recent activity"
          meta="Event bus tail — live feed"
          onClick={() => onNavigate("activity")}
        />
        <GlanceCard
          title="Data quality"
          meta="DQ breakdown — live"
          onClick={() => onNavigate("dq")}
        />
        <GlanceCard
          title="Gate review"
          meta={data.shadow_gate?.pass ? "Shadow gate passing" : "Review gate criteria"}
          onClick={() => onNavigate("gate")}
        />
        <GlanceCard
          title="Configuration"
          meta={`Strategy ${data.strategy_version_id.slice(0, 8)}…`}
          onClick={() => onNavigate("configs")}
        />
      </div>
    </>
  );
}

function FortressPostureBanner({
  posture,
  onNavigate,
}: {
  posture: FortressPostureDTO;
  onNavigate: (view: DashboardView) => void;
}) {
  const bannerClass = postureBannerClass(posture.readiness_state);
  const blockers = posture.blockers.slice(0, 3);

  return (
    <div className={`banner fortress-banner ${bannerClass}`} aria-label="Fortress posture">
      <div className="fortress-banner-body">
        <h3>Fortress posture · {posture.readiness_state.replace(/_/g, " ")}</h3>
        {blockers.length > 0 ? (
          <ul className="fortress-blockers">
            {blockers.map((b) => (
              <li key={b}>{b}</li>
            ))}
          </ul>
        ) : (
          <p className="hint">No active blockers.</p>
        )}
        {posture.next_action ? (
          <p className="fortress-next-action">
            <strong>Next:</strong> {posture.next_action}
          </p>
        ) : null}
      </div>
      <div className="fortress-banner-meta">
        <span className={`pill ${posturePillClass(posture.readiness_state)}`}>
          {posture.readiness_state}
        </span>
        <p className="hint">
          {posture.ingestion_delivery} · {posture.throughput_verdict}
        </p>
        <button type="button" className="btn-link" onClick={() => onNavigate("ingestion")}>
          Ingestion →
        </button>
      </div>
    </div>
  );
}

function postureBannerClass(state: string): string {
  switch (state) {
    case "BLOCKED":
      return "bad";
    case "SHADOW_READY":
    case "LIVE_READY":
      return "info";
    default:
      return "warn";
  }
}

function posturePillClass(state: string): string {
  switch (state) {
    case "BLOCKED":
      return "bad";
    case "SHADOW_READY":
    case "LIVE_READY":
      return "ok";
    default:
      return "warn";
  }
}

function GlanceCard({
  title,
  meta,
  onClick,
}: {
  title: string;
  meta: string;
  onClick: () => void;
}) {
  return (
    <div
      className="card glance-card"
      role="button"
      tabIndex={0}
      onClick={onClick}
      onKeyDown={(e) => {
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onClick();
        }
      }}
    >
      <div className="glance-title">{title}</div>
      <div className="glance-meta">{meta}</div>
      <div className="glance-action">Open view →</div>
    </div>
  );
}
