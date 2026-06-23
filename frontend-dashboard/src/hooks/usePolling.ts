import { useCallback, useEffect, useState, type DependencyList } from "react";
import { ApiError } from "../api/client";

const DEFAULT_POLL_SECONDS = 30;

/** Poll interval from VITE_POLL_INTERVAL_SECONDS (default 30s, clamped 5–300). */
export function pollIntervalMs(): number {
  const raw = import.meta.env.VITE_POLL_INTERVAL_SECONDS;
  const parsed = raw ? Number(raw) : DEFAULT_POLL_SECONDS;
  if (!Number.isFinite(parsed)) {
    return DEFAULT_POLL_SECONDS * 1000;
  }
  const sec = Math.min(300, Math.max(5, parsed));
  return sec * 1000;
}

export type PollingState<T> = {
  data: T | null;
  error: string | null;
  loading: boolean;
  refresh: () => Promise<void>;
};

/**
 * Polls a fetcher on an interval while enabled. Re-runs when deps change.
 */
export function usePolling<T>(
  factory: () => Promise<T>,
  deps: DependencyList,
  options?: { enabled?: boolean; intervalMs?: number },
): PollingState<T> {
  const enabled = options?.enabled !== false;
  const intervalMs = options?.intervalMs ?? pollIntervalMs();

  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(enabled);

  const refresh = useCallback(async () => {
    try {
      const result = await factory();
      setData(result);
      setError(null);
    } catch (e) {
      const message =
        e instanceof ApiError ? e.message : e instanceof Error ? e.message : "Failed to load data";
      setError(message);
    } finally {
      setLoading(false);
    }
  }, deps);

  useEffect(() => {
    if (!enabled) {
      return;
    }
    setLoading(true);
    void refresh();
    const id = window.setInterval(() => {
      void refresh();
    }, intervalMs);
    return () => window.clearInterval(id);
  }, [enabled, intervalMs, refresh]);

  return { data, error, loading, refresh };
}
