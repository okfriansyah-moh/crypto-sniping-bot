import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Dev server proxies /api → backend-dashboard on :8090 (see config/dashboard.yaml CORS).
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5175,
    proxy: {
      "/api": {
        target: "http://localhost:8090",
        changeOrigin: true,
      },
    },
  },
});
