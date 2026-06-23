import type { ChainStatusDTO, ChainStatusLevel } from "../api/types";
import type { ChainId } from "../hooks/useChainFilter";
import { CHAIN_OPTIONS } from "../hooks/useChainFilter";

type ChainSwitchProps = {
  activeChain: ChainId;
  onSelect: (chain: ChainId) => void;
  ariaLabel: string;
  compact?: boolean;
  chainStatuses?: ChainStatusDTO[];
};

export function ChainSwitch({
  activeChain,
  onSelect,
  ariaLabel,
  compact,
  chainStatuses,
}: ChainSwitchProps) {
  const statusByChain = buildStatusMap(chainStatuses);

  return (
    <div
      className={`chain-switch${compact ? " chain-switch--compact" : ""}`}
      role="group"
      aria-label={ariaLabel}
    >
      {CHAIN_OPTIONS.map((c) => {
        const status = c.id === "all" ? undefined : statusByChain[c.id];
        return (
          <button
            key={c.id}
            type="button"
            className={c.id === activeChain ? "active" : ""}
            aria-pressed={c.id === activeChain}
            onClick={() => onSelect(c.id)}
          >
            <span
              className={`chain-dot ${c.id === "all" ? "all" : c.short}${status ? ` status-${status}` : ""}`}
              aria-hidden
            />
            <span className="chain-label">{c.label}</span>
          </button>
        );
      })}
    </div>
  );
}

function buildStatusMap(statuses?: ChainStatusDTO[]): Partial<Record<ChainId, ChainStatusLevel>> {
  if (!statuses?.length) {
    return {};
  }
  const out: Partial<Record<ChainId, ChainStatusLevel>> = {};
  for (const s of statuses) {
    const chain = s.chain.toLowerCase() as ChainId;
    out[chain] = s.status;
  }
  return out;
}
