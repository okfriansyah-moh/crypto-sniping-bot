import { useCallback, useState } from "react";
import { dashboardApi } from "../api/client";
import { submitModeChange } from "../api/commands";
import type { OverviewResponseDTO } from "../api/types";
import { Toast } from "../components/Toast";
import { ViewState } from "../components/ViewState";
import type { ChainId } from "../hooks/useChainFilter";
import { usePolling } from "../hooks/usePolling";
import { useToast } from "../hooks/useToast";
import {
  OPERATIONAL_MODES,
  OPERATOR_ID_HINT,
  dashboardOperatorId,
  normalizeModeId,
} from "../utils/control";
import { chainMarketQuery } from "../utils/query";

type ModeViewProps = {
  chain: ChainId;
  market: string;
  active: boolean;
};

export function ModeView({ chain, market, active }: ModeViewProps) {
  const fetchOverview = useCallback(
    () => dashboardApi.getOverview(chainMarketQuery(chain, market)),
    [chain, market],
  );

  const { data, loading, error, refresh } = usePolling(fetchOverview, [chain, market], {
    enabled: active,
  });

  return (
    <ViewState loading={loading} error={error} empty={data === null}>
      {data ? <ModeContent data={data} chain={chain} onModeApplied={refresh} /> : null}
    </ViewState>
  );
}

function ModeContent({
  data,
  chain,
  onModeApplied,
}: {
  data: OverviewResponseDTO;
  chain: ChainId;
  onModeApplied: () => void;
}) {
  const current = normalizeModeId(data.mode);
  const issuerId = dashboardOperatorId();
  const { toast, showSuccess, showError, dismiss } = useToast();
  const [pendingMode, setPendingMode] = useState<string | null>(null);

  const handleModeClick = async (modeId: string) => {
    if (modeId === current) {
      return;
    }
    if (!issuerId) {
      showError(new Error(OPERATOR_ID_HINT));
      return;
    }
    setPendingMode(modeId);
    try {
      const resp = await submitModeChange(modeId, issuerId);
      showSuccess(`Mode change accepted (${resp.command_id?.slice(0, 8) ?? "ok"})`);
      onModeApplied();
    } catch (err) {
      showError(err);
    } finally {
      setPendingMode(null);
    }
  };

  const controlsReady = Boolean(issuerId);

  return (
    <>
      <Toast toast={toast} onDismiss={dismiss} />

      <div className="mode-header-extra">
        <span className="pill">
          {chain} · {data.mode}
        </span>
        {!controlsReady ? (
          <span className="hint warn-inline">{OPERATOR_ID_HINT}</span>
        ) : (
          <span className="hint">Writes via POST /api/v1/commands (one transition per window)</span>
        )}
      </div>

      <div className="mode-grid" role="group" aria-label="Operational modes">
        {OPERATIONAL_MODES.map((mode) => {
          const selected = current === mode.id;
          const busy = pendingMode === mode.id;
          const disabled = !controlsReady || selected || pendingMode !== null;
          return (
            <button
              key={mode.id}
              type="button"
              className={`mode-btn${selected ? " selected" : ""}${disabled && !selected ? " mode-btn--idle" : ""}`}
              disabled={disabled}
              onClick={() => void handleModeClick(mode.id)}
              aria-pressed={selected}
              aria-busy={busy}
            >
              <strong>{mode.label}</strong>
              <span>{busy ? "Submitting…" : mode.description}</span>
            </button>
          );
        })}
      </div>

      <div className="card view-section">
        <h3>Mode thresholds (from config)</h3>
        <p className="hint">
          Loaded from <code className="mono">config/pipeline.yaml</code> →{" "}
          <code className="mono">operational_modes</code>. Active strategy version:{" "}
          <code className="mono">{data.strategy_version_id.slice(0, 12)}…</code>
        </p>
        <p className="hint">Overview refreshes on poll interval after a successful mode change.</p>
      </div>
    </>
  );
}
