import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

const target = process.env.API_PROXY_TARGET ?? "http://127.0.0.1:8080";

export default defineConfig({
  plugins: [react()],
  server: {
    host: "127.0.0.1",
    port: 4180,
    strictPort: true,
    proxy: {
      "/api": { target, changeOrigin: false },
      "/healthz": { target, changeOrigin: false },
      "/readyz": { target, changeOrigin: false },
    },
  },
});
