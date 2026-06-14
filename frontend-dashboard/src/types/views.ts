/** Dashboard view ids — match docs/mockups/operator-dashboard.html data-view attributes. */
export type DashboardView =
  | "overview"
  | "pipeline"
  | "positions"
  | "activity"
  | "dq"
  | "gate"
  | "mode"
  | "safety"
  | "configs";

export type NavItem = {
  view: DashboardView;
  label: string;
  icon: string;
  badge?: string;
};

export type NavSection = {
  title: string;
  items: NavItem[];
};

export const NAV_SECTIONS: NavSection[] = [
  {
    title: "Monitor",
    items: [
      { view: "overview", label: "Overview", icon: "◉" },
      { view: "pipeline", label: "Pipeline health", icon: "⇄", badge: "L0–L10" },
      { view: "positions", label: "Open positions", icon: "▦" },
      { view: "activity", label: "Recent activity", icon: "⚡" },
    ],
  },
  {
    title: "Quality",
    items: [
      { view: "dq", label: "Data quality", icon: "✓" },
      { view: "gate", label: "Gate review", icon: "⚖" },
    ],
  },
  {
    title: "Control",
    items: [
      { view: "mode", label: "Trading mode", icon: "○" },
      { view: "safety", label: "Safety", icon: "⚠" },
      { view: "configs", label: "Configs", icon: "⚙" },
    ],
  },
];

/** Views where chain / market filter UI is shown (mockup CHAIN_FILTER_VIEWS). */
export const CHAIN_FILTER_VIEWS = new Set<DashboardView>([
  "overview",
  "pipeline",
  "positions",
  "activity",
  "dq",
  "gate",
  "mode",
]);

export const VIEW_TITLES: Record<DashboardView, { title: string; subtitle: string }> = {
  overview: { title: "Overview", subtitle: "Helicopter view · high-level health only" },
  pipeline: { title: "Pipeline health", subtitle: "L0–L10 funnel and layer heartbeats" },
  positions: { title: "Open positions", subtitle: "Live positions with trace attribution" },
  activity: { title: "Recent activity", subtitle: "Event bus tail (newest first)" },
  dq: { title: "Data quality", subtitle: "Decision breakdown and top reject reasons" },
  gate: { title: "Gate review", subtitle: "Production readiness criteria" },
  mode: { title: "Trading mode", subtitle: "Operational mode — POST /api/v1/commands" },
  safety: { title: "Safety", subtitle: "Kill switch with typed confirmation" },
  configs: { title: "Configs", subtitle: "YAML manifest — filenames and keys only" },
};
