import type { ChainId } from "../hooks/useChainFilter";
import { chainQueryParam } from "../hooks/useChainFilter";
import type { DashboardQueryParams } from "../api/types";

/** Chain + optional market query params for dashboard API calls. */
export function chainMarketQuery(chain: ChainId, market: string): Pick<DashboardQueryParams, "chain" | "market"> {
  return {
    chain: chainQueryParam(chain),
    market: market !== "all" ? market : undefined,
  };
}
