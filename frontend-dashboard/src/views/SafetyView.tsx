import { useCallback, useState } from "react";
import { dashboardApi } from "../api/client";
import { submitDestructiveCommand, submitForceCloseCommand } from "../api/commands";
import type { OverviewResponseDTO } from "../api/types";
import { ConfirmModal } from "../components/ConfirmModal";
import { Toast } from "../components/Toast";
import { ViewState } from "../components/ViewState";
import { usePolling } from "../hooks/usePolling";
import { useToast } from "../hooks/useToast";
import { OPERATOR_ID_HINT, dashboardOperatorId, drawdownTierLabel } from "../utils/control";
import { formatPct } from "../utils/format";

type SafetyViewProps = {
  active: boolean;
};

type PendingAction = "kill" | "resume" | "force_close" | null;

export function SafetyView({ active }: SafetyViewProps) {
  const fetchOverview = useCallback(() => dashboardApi.getOverview(), []);

  const { data, loading, error, refresh } = usePolling(fetchOverview, [], { enabled: active });

  return (
    <ViewState loading={loading} error={error} empty={data === null}>
      {data ? <SafetyContent data={data} onActionApplied={refresh} /> : null}
    </ViewState>
  );
}

function SafetyContent({
  data,
  onActionApplied,
}: {
  data: OverviewResponseDTO;
  onActionApplied: () => void;
}) {
  const halted = data.alert_banner?.code === "KILL_SWITCH";
  const issuerId = dashboardOperatorId();
  const { toast, showSuccess, showError, dismiss } = useToast();
  const [pending, setPending] = useState<PendingAction>(null);
  const [busy, setBusy] = useState(false);
  const [forceCloseToken, setForceCloseToken] = useState("");

  const controlsReady = Boolean(issuerId);

  const runDestructive = async (action: "kill" | "resume") => {
    if (!issuerId) {
      showError(new Error(OPERATOR_ID_HINT));
      return;
    }
    setBusy(true);
    try {
      const resp = await submitDestructiveCommand(action, issuerId);
      showSuccess(
        `${action === "kill" ? "Kill switch" : "Resume"} accepted (${resp.command_id?.slice(0, 8) ?? "ok"})`,
      );
      setPending(null);
      onActionApplied();
    } catch (err) {
      showError(err);
    } finally {
      setBusy(false);
    }
  };

  const runForceClose = async () => {
    const token = forceCloseToken.trim();
    if (!token) {
      showError(new Error("Enter a token address or prefix"));
      return;
    }
    if (!issuerId) {
      showError(new Error(OPERATOR_ID_HINT));
      return;
    }
    setBusy(true);
    try {
      const resp = await submitForceCloseCommand(token, issuerId);
      showSuccess(`Force close accepted (${resp.command_id?.slice(0, 8) ?? "ok"})`);
      setPending(null);
      setForceCloseToken("");
      onActionApplied();
    } catch (err) {
      showError(err);
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <Toast toast={toast} onDismiss={dismiss} />

      <ConfirmModal
        open={pending === "kill"}
        title="Kill switch"
        description="Stops all new trading immediately. Open positions remain monitored; use force-close via Telegram if needed."
        confirmPhrase="KILL"
        confirmLabel="Kill trading"
        busy={busy}
        onClose={() => !busy && setPending(null)}
        onConfirm={() => runDestructive("kill")}
      />

      <ConfirmModal
        open={pending === "resume"}
        title="Resume trading"
        description="Clears the global halt and allows the pipeline to execute again."
        confirmPhrase="RESUME"
        confirmLabel="Resume trading"
        busy={busy}
        onClose={() => !busy && setPending(null)}
        onConfirm={() => runDestructive("resume")}
      />

      <ConfirmModal
        open={pending === "force_close"}
        title="Force close positions"
        description={`Force-exit all open positions for token prefix "${forceCloseToken.trim()}". Bypasses TP/SL — logged and gated.`}
        confirmPhrase="FORCE_CLOSE"
        confirmLabel="Force close"
        busy={busy}
        onClose={() => !busy && setPending(null)}
        onConfirm={() => runForceClose()}
      />

      <div className="card view-section">
        <p className="safety-intro">
          Destructive actions require typed confirmation. Commands emit to the event bus and are
          applied by the sniper operator worker (same safety model as Telegram).
        </p>
        {!controlsReady ? (
          <p className="hint warn-inline">{OPERATOR_ID_HINT}</p>
        ) : null}
        <div className="actions">
          <button
            className="btn btn-danger"
            type="button"
            disabled={!controlsReady || busy || halted}
            onClick={() => setPending("kill")}
          >
            Kill switch — stop all trading
          </button>
          <button
            className="btn btn-ghost"
            type="button"
            disabled={!controlsReady || busy || !halted}
            onClick={() => setPending("resume")}
          >
            Resume trading
          </button>
        </div>
        <p className="hint" style={{ marginTop: "0.75rem" }}>
          Kill switch:{" "}
          {halted ? (
            <span className="pill bad">
              <span className="dot" /> Halted — {data.alert_banner?.message}
            </span>
          ) : (
            <span className="pill ok">
              <span className="dot" /> Active (not halted)
            </span>
          )}
        </p>
      </div>

      <div className="card view-section">
        <h3>Force close</h3>
        <p className="hint">
          Closes all open positions matching a token address prefix. Same safety model as Telegram{" "}
          <code className="mono">/force_close</code>.
        </p>
        <label className="modal-label">
          Token address or prefix
          <input
            className="modal-input"
            type="text"
            value={forceCloseToken}
            onChange={(e) => setForceCloseToken(e.target.value)}
            placeholder="Ab3c… or full mint address"
            disabled={!controlsReady || busy}
            spellCheck={false}
          />
        </label>
        <div className="actions" style={{ marginTop: "0.75rem" }}>
          <button
            className="btn btn-danger"
            type="button"
            disabled={!controlsReady || busy || !forceCloseToken.trim()}
            onClick={() => setPending("force_close")}
          >
            Force close positions
          </button>
        </div>
      </div>

      <div className="grid grid-2 view-section">
        <div className="card">
          <h3>Drawdown tier</h3>
          <div className="value value--sm">{drawdownTierLabel(data.drawdown_pct)}</div>
          <p className="hint">
            Current drawdown {formatPct(data.drawdown_pct)} · from{" "}
            <code className="mono">config/budgets.yaml</code> risk controller
          </p>
        </div>
        <div className="card">
          <h3>Execution mode</h3>
          <div className="value value--sm">{data.execution_mode || "—"}</div>
          <p className="hint">
            Shadow gate:{" "}
            {data.shadow_gate?.pass ? (
              <span className="tag pass">passing</span>
            ) : (
              <span className="tag skip">not ready</span>
            )}
          </p>
        </div>
      </div>
    </>
  );
}
