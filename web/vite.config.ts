import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// The build is embedded in the Go binary (internal/server/web/dist).
// `pnpm dev` proxies the API to a local `ax start` on the default port.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: "../internal/server/web/dist",
    emptyOutDir: true,
    assetsInlineLimit: 0,
    reportCompressedSize: true,
  },
  server: {
    proxy: { "/api": "http://127.0.0.1:27337" },
  },
});
