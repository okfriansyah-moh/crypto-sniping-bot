import { useCallback } from "react";
import { dashboardApi } from "../api/client";
import type { ActivityEventDTO } from "../api/types";
import { ViewState } from "../components/ViewState";
import type { ChainId } from "../hooks/useChainFilter";
import { chainQueryParam } from "../hooks/useChainFilter";
import { usePolling } from "../hooks/usePolling";
import { formatEventTime, shortAddress } from "../utils/format";

const DEFAULT_LIMIT = 50;

type ActivityViewProps = {
  chain: ChainId;
  active: boolean;
};

export function ActivityView({ chain, active }: ActivityViewProps) {
  const fetchActivity = useCallback(
    () =>
      dashboardApi.getActivity({
        chain: chainQueryParam(chain),
        limit: DEFAULT_LIMIT,
      }),
    [chain],
  );

  const { data, loading, error } = usePolling(fetchActivity, [chain], { enabled: active });

  return (
    <ViewState loading={loading} error={error} empty={data === null}>
      {data ? <ActivityTable rows={data} /> : null}
    </ViewState>
  );
}

function ActivityTable({ rows }: { rows: ActivityEventDTO[] }) {
  return (
    <div className="table-wrap">
      <table>
        <thead>
          <tr>
            <th>Time</th>
            <th>Event</th>
            <th>Chain</th>
            <th>Token</th>
            <th>Detail</th>
          </tr>
        </thead>
        <tbody>
          {rows.length === 0 ? (
            <tr>
              <td colSpan={5} className="table-empty">
                No recent events for this filter.
              </td>
            </tr>
          ) : (
            rows.map((row) => (
              <tr key={row.event_id}>
                <td className="mono">{formatEventTime(row.created_at)}</td>
                <td className="mono">{row.event_type}</td>
                <td>
                  <span className={`chain-tag ${row.chain}`}>{row.chain}</span>
                </td>
                <td className="mono truncate" title={row.token_address}>
                  {row.token_address ? shortAddress(row.token_address) : "—"}
                </td>
                <td className="truncate" title={row.summary}>
                  {row.summary}
                </td>
              </tr>
            ))
          )}
        </tbody>
      </table>
    </div>
  );
}
