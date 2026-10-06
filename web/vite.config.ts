import { defineConfig } from "vite";

// Alias roots relative to this file. `import.meta.url` resolves the same way
// through Vite's config loader and Bun; URL + decodeURIComponent avoids the
// `node:url` import (the no-Node-imports rule) and needs no Bun-only global.
const aliasRoot = (p: string) =>
  decodeURIComponent(new URL(p, import.meta.url).pathname);

export default defineConfig({
  resolve: {
    alias: {
      "@core": aliasRoot("./src/core"),
      "@shared": aliasRoot("./src/shared"),
      "@features": aliasRoot("./src/features"),
    },
  },
  server: {
    port: 3000,
    // Fail loudly instead of silently drifting to :3001 when a stale dev
    // server still holds the port — silent port drift masks config drift
    // (e.g. an old Vite without the /media proxy answering for the new one).
    strictPort: true,
    // Proxy /health, /api/v1, and /media to the Go backend during development.
    // PUBLIC_BASE_URL defaults to http://localhost:3000, so absolute media
    // URLs emitted by the API resolve back through this proxy.
    proxy: {
      "/health": "http://localhost:8080",
      "/api/v1": "http://localhost:8080",
      "/media": "http://localhost:8080",
    },
  },
  build: {
    target: "esnext",
    outDir: "dist",
  },
});
