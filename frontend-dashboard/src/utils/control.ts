/** Operator issuer ID for POST /api/v1/commands (must match DASHBOARD_ALLOWED_OPERATORS). */
export function dashboardOperatorId(): string {
  return (import.meta.env.VITE_DASHBOARD_OPERATOR_ID ?? "").trim();
}

export const OPERATOR_ID_HINT =
  "Set VITE_DASHBOARD_OPERATOR_ID (must match DASHBOARD_ALLOWED_OPERATORS on the API)";

export const OPERATIONAL_MODES = [
  { id: "STRICT", label: "STRICT", description: "Hardest filters · safest" },
  { id: "BALANCED", label: "BALANCED", description: "Default day-to-day" },
  { id: "EXPLORATION", label: "EXPLORATION", description: "More trades · testing" },
  {
    id: "VERY_EXPLORATION",
    label: "VERY EXPLORATION",
    description: "Maximum aggression",
  },
] as const;

export function normalizeModeId(mode: string): string {
  return mode.trim().toUpperCase().replace(/\s+/g, "_");
}

export function drawdownTierLabel(drawdownPct: number): string {
  if (drawdownPct < 5) {
    return "Tier 0 — normal";
  }
  if (drawdownPct < 10) {
    return "Tier 1 — caution";
  }
  if (drawdownPct < 15) {
    return "Tier 2 — reduced size";
  }
  return "Tier 3 — halt threshold";
}
