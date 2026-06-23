import { useCallback, useEffect, useState } from "react";
import { ApiError } from "../api/client";

export type ToastKind = "success" | "error";

export interface ToastState {
  kind: ToastKind;
  message: string;
}

function commandErrorMessage(err: unknown): string {
  if (err instanceof ApiError) {
    switch (err.status) {
      case 401:
        return "Unauthorized (401) — check VITE_DASHBOARD_API_KEY";
      case 403:
        return err.message || "Forbidden (403) — operator not in allowlist";
      case 409:
        return err.message || "Conflict (409) — command could not be applied";
      default:
        return err.message || `Request failed (${err.status})`;
    }
  }
  if (err instanceof Error) {
    return err.message;
  }
  return "Unexpected error";
}

export function useToast(autoDismissMs = 6000) {
  const [toast, setToast] = useState<ToastState | null>(null);

  const dismiss = useCallback(() => setToast(null), []);

  const showSuccess = useCallback((message: string) => {
    setToast({ kind: "success", message });
  }, []);

  const showError = useCallback((err: unknown) => {
    setToast({ kind: "error", message: commandErrorMessage(err) });
  }, []);

  useEffect(() => {
    if (!toast || autoDismissMs <= 0) {
      return;
    }
    const id = window.setTimeout(dismiss, autoDismissMs);
    return () => window.clearTimeout(id);
  }, [toast, autoDismissMs, dismiss]);

  return { toast, showSuccess, showError, dismiss };
}
