import { useCallback } from "react";
import { dashboardApi } from "../api/client";
import type { IngestionStatusDTO, RescanStatusResponseDTO } from "../api/types";
import { ViewState } from "../components/ViewState";
import { usePolling } from "../hooks/usePolling";

const HELIUS_DOC_PATH = "docs/guides/HELIUS_WEBHOOK_SETUP.md";

type IngestionViewProps = {
  active: boolean;
};

export function IngestionView({ active }: IngestionViewProps) {
  const fetchIngestion = useCallback(() => dashboardApi.getIngestion(), []);
  const fetchRescan = useCallback(() => dashboardApi.getRescan(), []);

  const ingestion = usePolling(fetchIngestion, [], { enabled: active });
  const rescan = usePolling(fetchRescan, [], { enabled: active });

  const loading = ingestion.loading || rescan.loading;
  const error = ingestion.error ?? rescan.error;
  const empty = ingestion.data === null && rescan.data === null;

  return (
    <ViewState loading={loading} error={error} empty={empty}>
      {ingestion.data ? (
        <IngestionContent data={ingestion.data} rescan={rescan.data} />
      ) : null}
    </ViewState>
  );
}

function IngestionContent({
  data,
  rescan,
}: {
  data: IngestionStatusDTO;
  rescan: RescanStatusResponseDTO | null;
}) {
  const deliveries = ["stream", "webhook", "hybrid"] as const;

  return (
    <>
      <div className="card view-section">
        <h3>Global delivery mode</h3>
        <div className="delivery-pills" role="list" aria-label="Delivery modes">
          {deliveries.map((mode) => (
            <span
              key={mode}
              role="listitem"
              className={`delivery-pill${data.global_delivery === mode ? " active" : ""}`}
            >
              {mode}
            </span>
          ))}
        </div>
        <p className="hint">
          Transport: <code className="mono">{data.transport_mode}</code>
          {data.webhook_active ? (
            <>
              {" "}
              · Webhook ingress <span className="tag pass">active</span>
            </>
          ) : (
            <>
              {" "}
              · Webhook ingress <span className="tag skip">off</span>
            </>
          )}
        </p>
        <p className="hint">
          Helius endpoint: <code className="mono">POST /webhooks/helius</code> — configure per{" "}
          <a
            href={`/${HELIUS_DOC_PATH}`}
            target="_blank"
            rel="noopener noreferrer"
            className="btn-link"
          >
            HELIUS_WEBHOOK_SETUP.md
          </a>
        </p>
      </div>

      <div className="card view-section">
        <h3>Per-program delivery</h3>
        <div className="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Family</th>
                <th>Program ID</th>
                <th>Delivery</th>
                <th>Status</th>
              </tr>
            </thead>
            <tbody>
              {data.programs.length === 0 ? (
                <tr>
                  <td colSpan={4} className="table-empty">
                    No Solana programs configured.
                  </td>
                </tr>
              ) : (
                data.programs.map((prog) => (
                  <tr key={prog.program_id}>
                    <td>{prog.family || "—"}</td>
                    <td className="mono">{shortProgramId(prog.program_id)}</td>
                    <td>
                      <span className={`delivery-pill compact${deliveryActive(data.global_delivery, prog.delivery) ? " active" : ""}`}>
                        {prog.delivery}
                      </span>
                    </td>
                    <td>
                      {prog.disabled ? (
                        <span className="tag skip">disabled</span>
                      ) : (
                        <span className="tag pass">enabled</span>
                      )}
                    </td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      </div>

      {rescan ? <RescanBandTable data={rescan} /> : null}
    </>
  );
}

function RescanBandTable({ data }: { data: RescanStatusResponseDTO }) {
  return (
    <div className="card view-section">
      <h3>Rescan worker (L0.5)</h3>
      <p className="hint">
        Re-emits eligible tokens at configured age bands (15m → 48h).{" "}
        {data.enabled ? (
          <span className="tag pass">enabled</span>
        ) : (
          <span className="tag skip">disabled</span>
        )}
        {data.total_emitted_24h > 0 ? (
          <> · {data.total_emitted_24h} emissions (24h)</>
        ) : null}
      </p>
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              <th>Band</th>
              <th>Phase</th>
              <th>Emitted (24h)</th>
            </tr>
          </thead>
          <tbody>
            {data.bands.length === 0 ? (
              <tr>
                <td colSpan={3} className="table-empty">
                  No rescan bands configured.
                </td>
              </tr>
            ) : (
              data.bands.map((band) => (
                <tr key={band.band}>
                  <td className="mono">rescan_{band.band}</td>
                  <td>{phaseLabel(band.phase)}</td>
                  <td>{band.emitted_24h}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function shortProgramId(id: string): string {
  if (id.length <= 12) {
    return id;
  }
  return `${id.slice(0, 6)}…${id.slice(-4)}`;
}

function deliveryActive(globalMode: string, programMode: string): boolean {
  if (globalMode === "hybrid") {
    return programMode === "webhook" || programMode === "stream";
  }
  return globalMode === programMode;
}

function phaseLabel(phase: string): string {
  switch (phase) {
    case "A":
      return "A — organic momentum (0–8h)";
    case "B":
      return "B — reversal (12–24h)";
    case "C":
      return "C — CEX catalyst (36–48h)";
    default:
      return phase || "—";
  }
}
