import { useCallback, useMemo } from "react";
import { dashboardApi } from "../api/client";
import type { PipelineFunnelDTO, PipelineStatsResponseDTO } from "../api/types";
import { ViewState } from "../components/ViewState";
import type { ChainId } from "../hooks/useChainFilter";
import { usePolling } from "../hooks/usePolling";
import { chainMarketQuery } from "../utils/query";

const WINDOW_HOURS = 24;

type PipelineViewProps = {
  chain: ChainId;
  market: string;
  active: boolean;
};

export function PipelineView({ chain, market, active }: PipelineViewProps) {
  const fetchPipeline = useCallback(
    () =>
      dashboardApi.getPipeline({
        ...chainMarketQuery(chain, market),
        window_hours: WINDOW_HOURS,
      }),
    [chain, market],
  );

  const { data, loading, error } = usePolling(fetchPipeline, [chain, market], { enabled: active });

  return (
    <ViewState loading={loading} error={error} empty={data === null}>
      {data ? <PipelineContent data={data} /> : null}
    </ViewState>
  );
}

function PipelineContent({ data }: { data: PipelineStatsResponseDTO }) {
  const verdict = data.throughput_verdict ?? "HEALTHY";
  const verdictClass = verdict === "HEALTHY" ? "ok" : verdict === "MARKET_QUIET" ? "warn" : "bad";

  const funnelSteps = useMemo(() => buildFunnelSteps(data.funnel), [data.funnel]);
  const stalledLayer = findStalledLayer(data);

  return (
    <>
      <div className="pipeline-header-extra">
        <span className={`pill ${verdictClass}`}>{verdict}</span>
        <span className="hint">Window: last {data.window_hours}h</span>
      </div>

      <div className="card view-section">
        <h3>Stage funnel (last {data.window_hours}h)</h3>
        <div className="funnel" role="list" aria-label="Pipeline stages">
          {funnelSteps.map((step, i) => (
            <FunnelStep key={step.key} step={step} showArrow={i < funnelSteps.length - 1} />
          ))}
        </div>
        {stalledLayer ? (
          <p className="hint funnel-hint">
            <strong className="text-warn">Stalled at {stalledLayer}?</strong> Check worker heartbeats
            below.
          </p>
        ) : null}
      </div>

      <div className="card view-section">
        <h3>Layer detail (L0 → L10)</h3>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Layer</th>
                <th>Stage</th>
                <th>Count ({data.window_hours}h)</th>
                <th>Drop-off</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              {data.layer_heartbeats.length === 0 ? (
                <tr>
                  <td colSpan={5} className="table-empty">
                    No layer heartbeats in window.
                  </td>
                </tr>
              ) : (
                data.layer_heartbeats.map((row) => (
                  <tr key={`${row.layer}-${row.stage}`}>
                    <td className="mono">{row.layer}</td>
                    <td>{row.stage}</td>
                    <td>{row.count_24h}</td>
                    <td>{row.drop_pct ?? "—"}</td>
                    <td>
                      <span className={`tag heartbeat-${row.status}`}>{row.status}</span>
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>
    </>
  );
}

type FunnelStepDef = {
  key: string;
  layer: string;
  label: string;
  count: number;
  stalled?: boolean;
};

function buildFunnelSteps(funnel: PipelineFunnelDTO): FunnelStepDef[] {
  return [
    { key: "l0", layer: "L0", label: "Detected", count: funnel.detected },
    { key: "l1", layer: "L1", label: "DQ pass", count: funnel.dq_passed },
    { key: "l2", layer: "L2", label: "Features", count: funnel.feature_ready },
    { key: "l3", layer: "L3", label: "Edge", count: funnel.edge_detected },
    { key: "l5", layer: "L5", label: "Validated", count: funnel.validated },
    { key: "l6", layer: "L6", label: "Selected", count: funnel.selected },
    { key: "l8", layer: "L8", label: "Executed", count: funnel.executed },
    { key: "l9", layer: "L9", label: "Open", count: funnel.position_open },
    {
      key: "l10",
      layer: "L10",
      label: "Evaluated",
      count: funnel.evaluated,
      stalled: funnel.position_open > 0 && funnel.evaluated === 0,
    },
  ];
}

function findStalledLayer(data: PipelineStatsResponseDTO): string | null {
  const stalled = data.layer_heartbeats.find((h) => h.status === "stalled");
  return stalled?.layer ?? null;
}

function FunnelStep({
  step,
  showArrow,
}: {
  step: FunnelStepDef;
  showArrow: boolean;
}) {
  return (
    <>
      <div
        className={`funnel-step${step.stalled ? " stalled" : step.count > 0 ? " active" : ""}`}
        role="listitem"
      >
        <div className="layer">{step.layer}</div>
        <div className="count">{step.count}</div>
        <div className="label">{step.label}</div>
      </div>
      {showArrow ? <span className="funnel-arrow" aria-hidden="true">→</span> : null}
    </>
  );
}
