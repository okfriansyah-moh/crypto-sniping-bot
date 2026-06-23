import { useCallback } from "react";
import { dashboardApi } from "../api/client";
import type { ExecutionRowDTO } from "../api/types";
import { ViewState } from "../components/ViewState";
import { usePolling } from "../hooks/usePolling";
import { formatEventTime, shortAddress } from "../utils/format";

const DEFAULT_LIMIT = 50;

type ExecutionsViewProps = {
  active: boolean;
};

export function ExecutionsView({ active }: ExecutionsViewProps) {
  const fetchExecutions = useCallback(
    () => dashboardApi.getExecutions({ limit: DEFAULT_LIMIT }),
    [],
  );

  const { data, loading, error } = usePolling(fetchExecutions, [], { enabled: active });

  return (
    <ViewState loading={loading} error={error} empty={data === null}>
      {data ? <ExecutionsTable rows={data.rows} /> : null}
    </ViewState>
  );
}

function ExecutionsTable({ rows }: { rows: ExecutionRowDTO[] }) {
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Time</th>
            <th>Token</th>
            <th>Mode</th>
            <th>Status</th>
            <th>Tx</th>
            <th>Execution ID</th>
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 ? (
            <tr>
              <td colSpan={6} className="table-empty">
                No recent executions in the trail.
              </td>
            </tr>
          ) : (
            rows.map((row) => <ExecutionRow key={row.execution_id} row={row} />)
          )}
        </tbody>
      </table>
    </div>
  );
}

function ExecutionRow({ row }: { row: ExecutionRowDTO }) {
  return (
    <tr>
      <td className="mono">{formatEventTime(row.timestamp)}</td>
      <td className="mono">{shortAddress(row.token)}</td>
      <td>
        {row.shadow ? (
          <span className="tag skip">shadow</span>
        ) : (
          <span className="tag pass">live</span>
        )}
      </td>
      <td>{row.status || "—"}</td>
      <td className="mono">{row.tx_hash ? shortAddress(row.tx_hash) : "—"}</td>
      <td className="mono">{shortAddress(row.execution_id)}</td>
    </tr>
  );
}
