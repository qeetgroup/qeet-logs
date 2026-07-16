// Shared helpers for the console E2E suite.
//
// The suite is fully hermetic: no live query API / backend is required. Every
// call the console makes to the query API (`/v1/**`) and the health probe
// (`/readyz`) is intercepted with `page.route` and answered from an in-memory
// map, so the tests are deterministic and run offline.
//
// Response shapes mirror the real API envelopes the app decodes in
// `src/lib/api.ts`:
//   - success: the `{ columns, count, rows }` query envelope, or the relevant
//     admin JSON payload
//   - failure: the flat `{ error: string }` envelope

import { expect, type Page, type Route } from "@playwright/test";

/** localStorage key the console stores the API key under (see src/lib/api.ts). */
const API_KEY_STORAGE = "qeet-logs.api-key";

/** A stand-in API key used to seed an authenticated session. */
export const API_KEY = "qlog_live_e2e_testkey";

/** A single mocked response: HTTP status (default 200) + JSON body. */
export type MockSpec = { status?: number; json?: unknown };

/** Per-path overrides, keyed by exact request pathname. */
export type Overrides = Record<string, MockSpec>;

// Sensible empty/healthy defaults so any authenticated screen renders without a
// backend. Individual tests override the paths they care about.
const DEFAULTS: Overrides = {
  "/readyz": { json: { healthy: true, status: "ok" } },
  "/v1/admin/api-keys": { json: [] },
  "/v1/query": { json: { columns: [], count: 0, rows: [] } },
  "/v1/incidents": { json: [] },
  "/v1/changes": { json: [] },
  "/v1/admin/audit": { json: { entries: [], total: 0 } },
  "/v1/admin/quota/usage": { json: {} },
  "/v1/admin/retention": { json: { retention_days: 7, masking_actions: {} } },
};

// Permissive CORS headers so the mocks work whether the app calls the API on
// its own origin (default here) or cross-origin on :8100.
const CORS_HEADERS: Record<string, string> = {
  "access-control-allow-origin": "*",
  "access-control-allow-methods": "GET,POST,PUT,PATCH,DELETE,OPTIONS",
  "access-control-allow-headers": "*",
};

function specFor(pathname: string, overrides: Overrides): MockSpec {
  if (pathname in overrides) return overrides[pathname];
  if (pathname in DEFAULTS) return DEFAULTS[pathname];
  return { json: {} };
}

/**
 * Intercept every query-API / health-probe request and answer it from the
 * `DEFAULTS` map merged with the given `overrides`. Call once per test (before
 * navigating).
 */
export async function mockApi(page: Page, overrides: Overrides = {}): Promise<void> {
  await page.route(
    (url) => {
      const p = url.pathname;
      return p.startsWith("/v1/") || p === "/readyz" || p === "/healthz" || p === "/livez";
    },
    async (route: Route) => {
      const request = route.request();

      // Answer CORS preflight so a cross-origin call (X-Qeet-Api-Key header
      // makes it non-simple) is not blocked by the browser.
      if (request.method() === "OPTIONS") {
        await route.fulfill({ status: 204, headers: CORS_HEADERS });
        return;
      }

      const { pathname } = new URL(request.url());
      const spec = specFor(pathname, overrides);
      await route.fulfill({
        status: spec.status ?? 200,
        contentType: "application/json",
        headers: CORS_HEADERS,
        body: JSON.stringify(spec.json ?? {}),
      });
    },
  );
}

/**
 * Fill the API key and submit the sign-in form.
 *
 * The console is SSR-hydrated, so a controlled input filled before hydration
 * completes can be reset by the client render. Retry the fill until the value
 * sticks and the submit button enables, then click.
 */
export async function signInWith(page: Page, key: string): Promise<void> {
  const input = page.getByLabel("API key");
  const submit = page.getByRole("button", { name: "Continue" });
  await expect(async () => {
    await input.fill(key);
    await expect(submit).toBeEnabled({ timeout: 1_000 });
  }).toPass({ timeout: 15_000 });
  await submit.click();
}

/**
 * Seed an authenticated session by writing the API key to localStorage before
 * any app code runs. The `_app` layout treats "a key is present" as signed in.
 */
export async function seedKey(page: Page, key: string = API_KEY): Promise<void> {
  await page.addInitScript(
    ([storageKey, value]) => {
      window.localStorage.setItem(storageKey, value);
    },
    [API_KEY_STORAGE, key] as const,
  );
}
