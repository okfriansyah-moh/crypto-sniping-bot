import { useCallback, useMemo } from "react";
import { dashboardApi } from "../api/client";
import type { PositionRowDTO } from "../api/types";
import { ViewState } from "../components/ViewState";
import type { ChainId } from "../hooks/useChainFilter";
import { usePolling } from "../hooks/usePolling";
import { formatAge, formatPct, formatUsd, formatUsdSigned, shortAddress } from "../utils/format";
import { chainMarketQuery } from "../utils/query";

type PositionsViewProps = {
  chain: ChainId;
  market: string;
  active: boolean;
};

export function PositionsView({ chain, market, active }: PositionsViewProps) {
  const fetchPositions = useCallback(
    () => dashboardApi.getPositions(chainMarketQuery(chain, market)),
    [chain, market],
  );

  const { data, loading, error } = usePolling(fetchPositions, [chain, market], { enabled: active });

  const rows = useMemo(() => {
    if (!data) {
      return [];
    }
    if (market === "all") {
      return data;
    }
    return data.filter((r) => r.market === market);
  }, [data, market]);

  const summary = useMemo(() => summarizePositions(rows), [rows]);

  return (
    <ViewState loading={loading} error={error} empty={data === null}>
      {data ? (
        <>
          <div className="table-wrap">
            <table>
              <thead>
                <tr>
                  <th>Token</th>
                  <th>Chain</th>
                  <th>Market</th>
                  <th>Entry</th>
                  <th>Current</th>
                  <th>PnL</th>
                  <th>Age</th>
                  <th>Trace</th>
                </tr>
              </thead>
              <tbody>
                {rows.length === 0 ? (
                  <tr>
                    <td colSpan={8} className="table-empty">
                      No open positions for this filter.
                    </td>
                  </tr>
                ) : (
                  rows.map((row) => <PositionRow key={row.position_id} row={row} />)
                )}
              </tbody>
            </table>
          </div>

          <div className="grid grid-3 view-section">
            <div className="card">
              <h3>Deployed capital</h3>
              <div className="value">{formatUsd(summary.deployed)}</div>
            </div>
            <div className="card">
              <h3>Unrealized PnL</h3>
              <div className={`value ${summary.unrealized >= 0 ? "value--ok" : "value--bad"}`}>
                {formatUsdSigned(summary.unrealized)}
              </div>
            </div>
            <div className="card">
              <h3>Oldest position</h3>
              <div className="value value--sm">
                {summary.oldestSeconds > 0 ? formatAge(summary.oldestSeconds) : "—"}
              </div>
            </div>
          </div>
        </>
      ) : null}
    </ViewState>
  );
}

function PositionRow({ row }: { row: PositionRowDTO }) {
  const pnlClass = row.pnl_pct >= 0 ? "tag pnl-pos" : "tag pnl-neg";
  return (
    <tr>
      <td className="mono truncate" title={row.token_address}>
        {shortAddress(row.token_address)}
      </td>
      <td>
        <span className={`chain-tag ${row.chain}`}>{row.chain}</span>
      </td>
      <td className="mono truncate" title={row.market}>
        {row.market}
      </td>
      <td>{formatUsd(row.entry_price_usd)}</td>
      <td>{formatUsd(row.current_price_usd)}</td>
      <td>
        <span className={pnlClass}>{formatPct(row.pnl_pct)}</span>
      </td>
      <td>{formatAge(row.age_seconds)}</td>
      <td className="mono truncate" title={row.trace_id}>
        {row.trace_id.slice(0, 8)}…
      </td>
    </tr>
  );
}

function summarizePositions(rows: PositionRowDTO[]) {
  let deployed = 0;
  let unrealized = 0;
  let oldestSeconds = 0;

  for (const r of rows) {
    deployed += r.size_usd;
    unrealized += r.size_usd * (r.pnl_pct / 100);
    if (r.age_seconds > oldestSeconds) {
      oldestSeconds = r.age_seconds;
    }
  }

  return { deployed, unrealized, oldestSeconds };
}
