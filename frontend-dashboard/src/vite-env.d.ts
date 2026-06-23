/// <reference types="vite/client" />

interface ImportMetaEnv {
  readonly VITE_DASHBOARD_API_KEY?: string;
  readonly VITE_DASHBOARD_OPERATOR_ID?: string;
  readonly VITE_API_BASE_URL?: string;
  readonly VITE_POLL_INTERVAL_SECONDS?: string;
}

interface ImportMeta {
  readonly env: ImportMetaEnv;
}
