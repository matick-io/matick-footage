import { fileURLToPath } from "node:url";
import { realpathSync } from "node:fs";
import { defineConfig, searchForWorkspaceRoot } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// matick-ui is linked from a matick checkout until it's published (make link).
const matick = realpathSync(fileURLToPath(new URL(".matick", import.meta.url)));

export default defineConfig({
  plugins: [react(), tailwindcss()],
  resolve: {
    // The linked packages carry their own node_modules; these must be one copy.
    dedupe: ["react", "react-dom", "@tanstack/react-query", "three"],
  },
  server: {
    port: 5173,
    fs: { allow: [searchForWorkspaceRoot(process.cwd()), matick] },
    proxy: { "/api": "http://localhost:8420" },
  },
});
