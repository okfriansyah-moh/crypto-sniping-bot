import { useCallback, useMemo } from "react";
import { dashboardApi } from "../api/client";
import type { DQBreakdownResponseDTO, DQRejectReasonDTO } from "../api/types";
import { ViewState } from "../components/ViewState";
import type { ChainId } from "../hooks/useChainFilter";
import { usePolling } from "../hooks/usePolling";
import { formatPct } from "../utils/format";
import { chainMarketQuery } from "../utils/query";

const WINDOW_HOURS = 24;

/** Mandatory structural hard-rejects — never bypassed by operational mode. */
export const MANDATORY_REJECT_REASONS = [
  "serial_launcher",
  "unknown_creator_count",
  "no_social_links",
  "unknown_social_links",
  "high_total_supply",
  "unknown_total_supply",
] as const;

const MANDATORY_REJECT_LABELS: Record<string, string> = {
  serial_launcher: "serial_launcher",
  unknown_creator_count: "unknown_creator_count (fail-closed)",
  no_social_links: "no_social_links",
  unknown_social_links: "unknown_social_links (fail-closed)",
  high_total_supply: "high_total_supply",
  unknown_total_supply: "unknown_total_supply (fail-closed)",
};

type DQViewProps = {
  chain: ChainId;
  market: string;
  active: boolean;
};

export function DQView({ chain, market, active }: DQViewProps) {
  const fetchDQ = useCallback(
    () =>
      dashboardApi.getDQ({
        ...chainMarketQuery(chain, market),
        window_hours: WINDOW_HOURS,
      }),
    [chain, market],
  );

  const { data, loading, error } = usePolling(fetchDQ, [chain, market], { enabled: active });

  return (
    <ViewState loading={loading} error={error} empty={data === null}>
      {data ? <DQContent data={data} /> : null}
    </ViewState>
  );
}

function DQContent({ data }: { data: DQBreakdownResponseDTO }) {
  const mandatoryCounts = useMemo(() => extractMandatoryCounts(data.top_reject_reasons), [data]);

  return (
    <>
      <div className="grid grid-3 view-section">
        <div className="card">
          <h3>Probe completeness ({data.window_hours}h)</h3>
          <ul className="reject-reason-list">
            <li>
              <span className="mono">holder_dist_known</span>
              <span>{formatPct(data.holder_dist_known_pct ?? 0)}</span>
            </li>
            <li>
              <span className="mono">lp_stats_known (supply proxy)</span>
              <span>{formatPct(data.total_supply_known_pct ?? 0)}</span>
            </li>
            <li>
              <span className="mono">creator_address set</span>
              <span>{formatPct(data.creator_count_known_pct ?? 0)}</span>
            </li>
          </ul>
        </div>
        <div className="card">
          <h3>Fair-chance SKIPs</h3>
          <div className="value">{(data.fair_chance_skip_count ?? 0).toLocaleString()}</div>
          <div className="hint">probe_partial / probe_exhausted / monitored flags</div>
        </div>
      </div>

      <div className="grid grid-3 view-section">
        <div className="card">
          <h3>Decisions ({data.window_hours}h)</h3>
          <div className="value">{data.total_decisions.toLocaleString()}</div>
          <div className="hint">
            PASS {data.pass_count} · RISKY {data.risky_pass_count} · REJECT {data.reject_count} · SKIP{" "}
            {data.skip_count}
          </div>
        </div>
        <div className="card">
          <h3>Pass rate</h3>
          <div className="value">{formatPct(data.pass_rate_pct)}</div>
          <div className="hint">Mode-adaptive thresholds apply above mandatory rejects</div>
        </div>
        <div className="card">
          <h3>Mandatory hard-rejects</h3>
          <ul className="mandatory-reject-list">
            {MANDATORY_REJECT_REASONS.map((reason) => {
              const count = mandatoryCounts.get(reason) ?? 0;
              return (
                <li key={reason}>
                  <span className="mono">{MANDATORY_REJECT_LABELS[reason] ?? reason}</span>
                  {count > 0 ? <span className="mandatory-count">{count.toLocaleString()}</span> : null}
                </li>
              );
            })}
          </ul>
        </div>
      </div>

      <div className="card view-section">
        <h3>Top reject reasons</h3>
        {data.top_reject_reasons.length === 0 ? (
          <p className="hint">No reject reasons in window.</p>
        ) : (
          <ul className="reject-reason-list">
            {data.top_reject_reasons.map((r) => (
              <li key={r.reason}>
                <span className={`mono${isMandatoryReject(r.reason) ? " mandatory-reason" : ""}`}>
                  {r.reason}
                </span>
                <span>{r.count.toLocaleString()}</span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </>
  );
}

function extractMandatoryCounts(reasons: DQRejectReasonDTO[]): Map<string, number> {
  const map = new Map<string, number>();
  for (const r of reasons) {
    if (MANDATORY_REJECT_REASONS.includes(r.reason as (typeof MANDATORY_REJECT_REASONS)[number])) {
      map.set(r.reason, r.count);
    }
  }
  return map;
}

function isMandatoryReject(reason: string): boolean {
  return (MANDATORY_REJECT_REASONS as readonly string[]).includes(reason);
}
