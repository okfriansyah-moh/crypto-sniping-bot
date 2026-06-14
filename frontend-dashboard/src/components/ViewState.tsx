import type { ReactNode } from "react";

type ViewStateProps = {
  loading: boolean;
  error: string | null;
  empty?: boolean;
  children: ReactNode;
};

export function ViewState({ loading, error, empty, children }: ViewStateProps) {
  if (error) {
    return (
      <div className="view-state view-state--error" role="alert">
        <strong>Unable to load data</strong>
        <p>{error}</p>
        <p className="hint">Check that backend-dashboard is running and VITE_DASHBOARD_API_KEY is set.</p>
      </div>
    );
  }

  if (loading && empty) {
    return <div className="view-state view-state--loading">Loading…</div>;
  }

  return <>{children}</>;
}
