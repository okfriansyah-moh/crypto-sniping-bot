/**
 * Typed fetch client for backend-dashboard REST API (port 8090).
 * API key from VITE_DASHBOARD_API_KEY only — never hardcode or commit secrets.
 */

import type {
  ActivityEventDTO,
  ApiErrorBody,
  ConfigManifestEntryDTO,
  DashboardQueryParams,
  DQBreakdownResponseDTO,
  GateEvidenceResponseDTO,
  HealthResponseDTO,
  OverviewResponseDTO,
  PipelineStatsResponseDTO,
  PnLSummaryDTO,
  PositionRowDTO,
} from "./types";

export class ApiError extends Error {
  readonly status: number;
  readonly body: ApiErrorBody | null;

  constructor(status: number, message: string, body: ApiErrorBody | null) {
    super(message);
    this.name = "ApiError";
    this.status = status;
    this.body = body;
  }
}

const DEFAULT_API_BASE = "";

function apiBase(): string {
  const base = import.meta.env.VITE_API_BASE_URL ?? DEFAULT_API_BASE;
  return base.replace(/\/$/, "");
}

function dashboardApiKey(): string {
  return (import.meta.env.VITE_DASHBOARD_API_KEY ?? "").trim();
}

function buildQuery(params?: DashboardQueryParams): string {
  if (!params) {
    return "";
  }
  const q = new URLSearchParams();
  if (params.chain) {
    q.set("chain", params.chain);
  }
  if (params.market) {
    q.set("market", params.market);
  }
  if (params.window_hours !== undefined) {
    q.set("window_hours", String(params.window_hours));
  }
  if (params.limit !== undefined) {
    q.set("limit", String(params.limit));
  }
  const s = q.toString();
  return s ? `?${s}` : "";
}

async function parseJson<T>(res: Response): Promise<T> {
  const text = await res.text();
  if (!text) {
    return {} as T;
  }
  return JSON.parse(text) as T;
}

/**
 * Low-level GET helper — attaches X-Dashboard-Key when configured.
 */
export async function apiGet<T>(path: string, params?: DashboardQueryParams): Promise<T> {
  const url = `${apiBase()}${path}${buildQuery(params)}`;
  const headers = authHeaders();

  const res = await fetch(url, { method: "GET", headers, cache: "no-store" });
  return handleResponse<T>(res);
}

function authHeaders(): HeadersInit {
  const headers: HeadersInit = {
    Accept: "application/json",
    "Content-Type": "application/json",
  };
  const key = dashboardApiKey();
  if (key) {
    headers["X-Dashboard-Key"] = key;
  }
  return headers;
}

async function handleResponse<T>(res: Response): Promise<T> {
  if (!res.ok) {
    let body: ApiErrorBody | null = null;
    try {
      body = await parseJson<ApiErrorBody>(res);
    } catch {
      body = null;
    }
    const message = body?.error ?? res.statusText ?? "request failed";
    throw new ApiError(res.status, message, body);
  }
  return parseJson<T>(res);
}

/**
 * Low-level POST helper — attaches X-Dashboard-Key when configured.
 */
export async function apiPost<T>(path: string, payload: unknown): Promise<T> {
  const url = `${apiBase()}${path}`;
  const res = await fetch(url, {
    method: "POST",
    headers: authHeaders(),
    body: JSON.stringify(payload),
    cache: "no-store",
  });
  return handleResponse<T>(res);
}

export const dashboardApi = {
  getHealth: () => apiGet<HealthResponseDTO>("/api/v1/health"),

  getOverview: (params?: Pick<DashboardQueryParams, "chain" | "market">) =>
    apiGet<OverviewResponseDTO>("/api/v1/overview", params),

  getPipeline: (params?: Pick<DashboardQueryParams, "chain" | "market" | "window_hours">) =>
    apiGet<PipelineStatsResponseDTO>("/api/v1/pipeline", params),

  getPositions: (params?: Pick<DashboardQueryParams, "chain" | "market">) =>
    apiGet<PositionRowDTO[]>("/api/v1/positions", params),

  getPnL: (params?: Pick<DashboardQueryParams, "window_hours">) =>
    apiGet<PnLSummaryDTO>("/api/v1/pnl", params),

  getDQ: (params?: Pick<DashboardQueryParams, "chain" | "market" | "window_hours">) =>
    apiGet<DQBreakdownResponseDTO>("/api/v1/dq", params),

  getActivity: (params?: Pick<DashboardQueryParams, "chain" | "limit">) =>
    apiGet<ActivityEventDTO[]>("/api/v1/activity", params),

  getGateEvidence: () => apiGet<GateEvidenceResponseDTO>("/api/v1/gate/evidence"),

  getConfigs: () => apiGet<ConfigManifestEntryDTO[]>("/api/v1/configs"),
};

export type DashboardApi = typeof dashboardApi;
