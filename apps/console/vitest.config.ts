import { fileURLToPath } from "node:url";
import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// Dedicated Vitest config. It deliberately does NOT reuse vite.config.ts: the
// TanStack Start / Nitro / devtools plugins are for the dev+build server and
// are unnecessary (and slow) under jsdom. We only need JSX transforms, the
// `@/*` path alias, and a browser-like environment.
export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
    },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/test/setup.ts"],
    include: ["src/**/*.{test,spec}.{ts,tsx}"],
    css: false,
    restoreMocks: true,
  },
});
