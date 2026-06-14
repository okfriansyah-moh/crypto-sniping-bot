import type { ChainId } from "../hooks/useChainFilter";
import { ChainSwitch } from "./ChainSwitch";

type ChainBarProps = {
  visible: boolean;
  chain: ChainId;
  market: string;
  markets: string[];
  marketDisabled: boolean;
  onChainChange: (chain: ChainId) => void;
  onMarketChange: (market: string) => void;
};

export function ChainBar({
  visible,
  chain,
  market,
  markets,
  marketDisabled,
  onChainChange,
  onMarketChange,
}: ChainBarProps) {
  return (
    <div
      className={`chain-bar${visible ? "" : " chain-filter-hidden"}`}
      role="region"
      aria-label="Chain and market filter"
    >
      <label>Chain</label>
      <ChainSwitch activeChain={chain} onSelect={onChainChange} ariaLabel="Select chain" />
      <div className="market-filter">
        <label htmlFor="market-select">Market</label>
        <select
          id="market-select"
          aria-label="Filter by market within chain"
          disabled={marketDisabled}
          value={marketDisabled ? "" : market}
          onChange={(e) => onMarketChange(e.target.value)}
        >
          {marketDisabled ? (
            <option value="">Select a single chain to filter markets</option>
          ) : (
            markets.map((m) => (
              <option key={m} value={m}>
                {m === "all" ? "All markets on chain" : m}
              </option>
            ))
          )}
        </select>
      </div>
    </div>
  );
}
