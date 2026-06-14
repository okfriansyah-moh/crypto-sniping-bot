import { useCallback, useEffect, useMemo, useState } from "react";
import type { DashboardView } from "../types/views";
import { CHAIN_FILTER_VIEWS } from "../types/views";

export type ChainId = "all" | "solana" | "eth" | "bsc";

export type ChainOption = {
  id: ChainId;
  label: string;
  short: ChainId | "solana" | "eth" | "bsc";
  markets: string[];
};

const STORAGE_CHAIN = "dashboard-chain";
const STORAGE_MARKET = "dashboard-market";

export const CHAIN_OPTIONS: ChainOption[] = [
  {
    id: "all",
    label: "All chains",
    short: "all",
    markets: ["all"],
  },
  {
    id: "solana",
    label: "Solana",
    short: "solana",
    markets: [
      "all",
      "solana-pumpfun-amm",
      "solana-raydium-v4",
      "solana-pumpfun",
      "solana-raydium-clmm",
    ],
  },
  {
    id: "eth",
    label: "Ethereum",
    short: "eth",
    markets: ["all", "eth-uniswap-v2", "eth-uniswap-v3", "eth-sushiswap-v2", "eth-sushiswap-v3"],
  },
  {
    id: "bsc",
    label: "BNB Chain",
    short: "bsc",
    markets: ["all", "bsc-pancake-v2", "bsc-pancake-v3"],
  },
];

function readStoredChain(): ChainId {
  try {
    const v = localStorage.getItem(STORAGE_CHAIN);
    if (v === "all" || v === "solana" || v === "eth" || v === "bsc") {
      return v;
    }
  } catch {
    /* ignore */
  }
  return "solana";
}

function readStoredMarket(): string {
  try {
    const v = localStorage.getItem(STORAGE_MARKET);
    if (v) {
      return v;
    }
  } catch {
    /* ignore */
  }
  return "all";
}

export function chainFilterVisible(view: DashboardView): boolean {
  return CHAIN_FILTER_VIEWS.has(view);
}

/** API query value for chain filter (empty when all chains). */
export function chainQueryParam(chain: ChainId): string | undefined {
  return chain === "all" ? undefined : chain;
}

export function useChainFilter() {
  const [chain, setChainState] = useState<ChainId>(readStoredChain);
  const [market, setMarketState] = useState<string>(readStoredMarket);

  const chainMeta = useMemo(
    () => CHAIN_OPTIONS.find((c) => c.id === chain) ?? CHAIN_OPTIONS[1],
    [chain],
  );

  const setChain = useCallback((next: ChainId) => {
    setChainState(next);
    setMarketState("all");
    try {
      localStorage.setItem(STORAGE_CHAIN, next);
      localStorage.setItem(STORAGE_MARKET, "all");
    } catch {
      /* ignore */
    }
  }, []);

  const setMarket = useCallback(
    (next: string) => {
      setMarketState(next);
      try {
        localStorage.setItem(STORAGE_MARKET, next);
      } catch {
        /* ignore */
      }
    },
    [],
  );

  useEffect(() => {
    if (!chainMeta.markets.includes(market)) {
      setMarket("all");
    }
  }, [chainMeta, market, setMarket]);

  return {
    chain,
    market,
    chainMeta,
    setChain,
    setMarket,
    markets: chainMeta.markets,
    marketDisabled: chain === "all",
  };
}
