import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// The app is embedded in remnantd and served from /. During development,
// `npm run dev` proxies the API to a local remnantd.
export default defineConfig({
  plugins: [react()],
  build: {
    outDir: "dist",
    emptyOutDir: true,
    assetsDir: "assets",
    // Everything ships as files: the CSP forbids inline script and style.
    assetsInlineLimit: 0,
    modulePreload: { polyfill: false },
    sourcemap: false,
  },
  server: {
    proxy: { "/api": process.env.REMNANT_DEV_API ?? "http://127.0.0.1:8080" },
  },
});
