import { useCallback } from "react";
import { dashboardApi } from "../api/client";
import type { GateEvidenceResponseDTO, ThroughputVerdict } from "../api/types";
import { ViewState } from "../components/ViewState";
import type { ChainId } from "../hooks/useChainFilter";
import { usePolling } from "../hooks/usePolling";
import { formatEventTime } from "../utils/format";

type GateViewProps = {
  chain: ChainId;
  active: boolean;
};

export function GateView({ chain, active }: GateViewProps) {
  const fetchGate = useCallback(() => dashboardApi.getGateEvidence(), []);

  const { data, loading, error } = usePolling(fetchGate, [], { enabled: active });

  return (
    <ViewState loading={loading} error={error} empty={data === null}>
      {data ? <GateContent data={data} chain={chain} /> : null}
    </ViewState>
  );
}

function GateContent({ data, chain }: { data: GateEvidenceResponseDTO; chain: ChainId }) {
  const verdict = data.throughput_verdict || "MARKET_QUIET";
  const verdictClass = verdictPillClass(verdict);
  const bannerClass = verdict === "CODE_DEFECT" ? "bad" : verdict === "HEALTHY" ? "info" : "warn";
  const production = productionDecision(data);
  const showWsolNote = chain === "solana" || chain === "all";

  return (
    <>
      <div className={`banner ${bannerClass}`}>
        <div className="gate-banner-body">
          <h3>{gateBannerTitle(verdict, data.traces_completed)}</h3>
          <p>{gateBannerText(verdict, data.traces_completed)}</p>
          {data.criteria && data.criteria.length > 0 ? (
            <div className="criteria-grid" aria-label="Gate criteria">
              {data.criteria.map((c) => (
                <div key={c.label} className="criterion">
                  <span>{c.label}</span>
                  <span className={c.passed ? "status-ok" : "status-bad"}>
                    {c.value} {c.passed ? "✓" : "✗"}
                  </span>
                </div>
              ))}
            </div>
          ) : null}
          {showWsolNote ? (
            <p className="hint gate-wsol-note">
              <strong>WSOL check</strong> applies to Solana only (
              <code className="mono">config/chains.yaml</code>).
            </p>
          ) : null}
        </div>
        <div className="gate-verdict-col">
          <span className={`pill ${verdictClass}`}>THROUGHPUT: {verdict}</span>
          <p className="hint gate-timestamp">Evidence {formatEventTime(data.timestamp)}</p>
        </div>
      </div>

      <div className="card view-section">
        <div className="grid grid-2">
          <div>
            <strong className="field-label">PRODUCTION DECISION</strong>
            <p className="field-value">{production}</p>
          </div>
          <div>
            <strong className="field-label">MODE DETECTED</strong>
            <p className="field-value">{data.detected_mode || "—"}</p>
          </div>
        </div>
        <div className="grid grid-3 view-section gate-metrics">
          <div className="card card--nested">
            <h3>Traces completed</h3>
            <div className="value value--sm">{data.traces_completed}</div>
          </div>
          <div className="card card--nested">
            <h3>DQ pass / risky</h3>
            <div className="value value--sm">{data.dq_pass_or_risky_pass}</div>
          </div>
          <div className="card card--nested">
            <h3>Shadow observer errors</h3>
            <div className={`value value--sm${data.shadow_observer_failed > 0 ? " value--bad" : ""}`}>
              {data.shadow_observer_failed}
            </div>
          </div>
        </div>
        <div className="actions">
          <button className="btn btn-primary" type="button" disabled title="Use Telegram / CLI for now">
            Run 30m gate collection
          </button>
          <button className="btn btn-ghost" type="button" disabled title="Use Telegram / CLI for now">
            Download brief (.txt)
          </button>
        </div>
      </div>
    </>
  );
}

function verdictPillClass(verdict: ThroughputVerdict): string {
  switch (verdict) {
    case "HEALTHY":
      return "ok";
    case "MARKET_QUIET":
    case "GUARDRAILS_ACTIVE":
      return "warn";
    case "CODE_DEFECT":
      return "bad";
    default:
      return "";
  }
}

function productionDecision(data: GateEvidenceResponseDTO): string {
  if (data.traces_completed < 1) {
    return "NOT_READY";
  }
  if (data.throughput_verdict === "CODE_DEFECT" || data.shadow_observer_failed > 0) {
    return "NOT_READY";
  }
  if (data.wsol_token_address_emitted > 0) {
    return "NOT_READY";
  }
  return "SHADOW_READY";
}

function gateBannerTitle(verdict: ThroughputVerdict, traces: number): string {
  if (traces < 1) {
    return "Pipeline proof not complete yet";
  }
  switch (verdict) {
    case "CODE_DEFECT":
      return "Throughput indicates a code defect";
    case "MARKET_QUIET":
      return "Market quiet — low ingestion volume";
    case "GUARDRAILS_ACTIVE":
      return "Guardrails active — throughput constrained";
    case "HEALTHY":
      return "Gate criteria look healthy";
    default:
      return "Gate review";
  }
}

function gateBannerText(verdict: ThroughputVerdict, traces: number): string {
  if (traces < 1) {
    return "No full L0→L10 trace in the evidence window. Run make phase2-proof MINS=30 after fixes deploy.";
  }
  switch (verdict) {
    case "CODE_DEFECT":
      return "WSOL emissions, probe backlog, shadow observer failures, or low valid-token ratio detected.";
    case "MARKET_QUIET":
      return "Ingestion volume is low — may be normal market conditions rather than a defect.";
    case "GUARDRAILS_ACTIVE":
      return "Operational guardrails are limiting throughput; review mode and DQ thresholds.";
    case "HEALTHY":
      return "Throughput metrics within expected bounds for the current window.";
    default:
      return "Review criteria below.";
  }
}
