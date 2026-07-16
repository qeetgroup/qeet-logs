import { defineConfig, devices } from "@playwright/test";

// Base URL of the console under test. Playwright builds + previews the console
// on :3020 via the `webServer` block below; override with PLAYWRIGHT_BASE_URL
// to point at an already-running instance.
const BASE_URL = process.env.PLAYWRIGHT_BASE_URL ?? "http://localhost:3020";
const PORT = new URL(BASE_URL).port || "3020";

// Dedicated Playwright config. It is intentionally isolated from Vitest:
//
//   - Vitest only ever collects `src/**/*.{test,spec}.{ts,tsx}` (see
//     vitest.config.ts). The E2E specs live in `e2e/`, OUTSIDE `src/`, so the
//     unit runner never picks them up.
//   - Playwright only looks in `testDir: "./e2e"`, so it never picks up the
//     `*.test.tsx` unit/component tests under `src/`.
//
// The two suites therefore share zero files and can be run independently.
export default defineConfig({
  testDir: "./e2e",
  // Belt-and-braces: even if a spec were moved, only *.spec.ts is a Playwright
  // test — the Vitest suites use *.test.tsx.
  testMatch: "**/*.spec.ts",
  fullyParallel: true,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 2 : 0,
  // A single preview server backs all workers; cap parallelism to keep it
  // responsive.
  workers: process.env.CI ? 1 : 2,
  // Generous timeouts: the webServer may cold-build the app on the first run,
  // and query-backed screens resolve asynchronously after mount.
  timeout: 60_000,
  expect: { timeout: 15_000 },
  reporter: "list",
  use: {
    baseURL: BASE_URL,
    trace: "on-first-retry",
  },
  projects: [
    {
      name: "chromium",
      use: {
        ...devices["Desktop Chrome"],
        // Wide enough that the collapsible sidebar renders expanded (labels
        // visible), so nav links are clickable in the navigation tests.
        viewport: { width: 1280, height: 800 },
      },
    },
  ],
  // Bring up the console for the tests unless one is already running.
  //
  // We run against a *production build* (`build` + `preview`), not the dev
  // server: the SSR + HMR dev server does not reliably re-hydrate here (a theme
  // / i18n hydration mismatch leaves the page non-interactive), whereas the
  // production build hydrates deterministically — which is what an E2E suite
  // needs to exercise real client behaviour (controlled inputs, client nav).
  //
  // The suite mocks every API call (see e2e/fixtures.ts), so no live query API /
  // backend is required. VITE_API_URL is baked to the app's own origin so the
  // mocked calls are same-origin (no CORS preflight); the fixture also returns
  // permissive CORS headers, so a cross-origin default still works.
  webServer: {
    command: `bun run build && bun run preview --port ${PORT}`,
    url: BASE_URL,
    reuseExistingServer: !process.env.CI,
    timeout: 180_000,
    env: {
      VITE_API_URL: BASE_URL,
      VITE_APP_TITLE: "Qeet Logs",
    },
  },
});
