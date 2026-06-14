import type { ChainId } from "../hooks/useChainFilter";
import { CHAIN_OPTIONS } from "../hooks/useChainFilter";

type ChainSwitchProps = {
  activeChain: ChainId;
  onSelect: (chain: ChainId) => void;
  ariaLabel: string;
  compact?: boolean;
};

export function ChainSwitch({ activeChain, onSelect, ariaLabel, compact }: ChainSwitchProps) {
  return (
    <div
      className={`chain-switch${compact ? " chain-switch--compact" : ""}`}
      role="group"
      aria-label={ariaLabel}
    >
      {CHAIN_OPTIONS.map((c) => (
        <button
          key={c.id}
          type="button"
          className={c.id === activeChain ? "active" : ""}
          aria-pressed={c.id === activeChain}
          onClick={() => onSelect(c.id)}
        >
          <span className={`chain-dot ${c.id === "all" ? "all" : c.short}`} aria-hidden />
          <span className="chain-label">{c.label}</span>
        </button>
      ))}
    </div>
  );
}
