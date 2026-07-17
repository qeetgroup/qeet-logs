# Console E2E (Playwright)

End-to-end smoke coverage for the Qeet Logs console, driven by
[Playwright](https://playwright.dev). The suite is **hermetic** — it needs no
live query API / backend. Every call the app makes to the query API (`/v1/**`)
and the health probe (`/readyz`) is intercepted with `page.route` and answered
from an in-memory map (see [`fixtures.ts`](fixtures.ts)).

## What's covered

| Spec | Covers |
|---|---|
| [`sign-in.spec.ts`](sign-in.spec.ts) | Sign-in form renders; empty key keeps submit disabled; invalid key shows an inline error; valid key lands on the app shell |
| [`shell.spec.ts`](shell.spec.ts) | Authenticated layout: sidebar (primary nav + links), header (account menu), overview heading |
| [`navigation.spec.ts`](navigation.spec.ts) | Sidebar navigation to Search, Incidents, Settings (URL + `<h1>`) |
| [`error-boundary.spec.ts`](error-boundary.spec.ts) | API failure surfaces a contained `role="alert"` error while the shell survives; unknown route hits the not-found boundary |

## Running

Playwright needs its browser once (Chromium only is used):

```bash
bunx playwright install chromium
```

Then, from `apps/console` (or the repo root with
`bun run --filter '@qeet-logs/console' <script>`):

```bash
bun run test:e2e        # headless run (list reporter)
bun run test:e2e:ui     # interactive Playwright UI mode
```

The [`playwright.config.ts`](../playwright.config.ts) `webServer` block brings
the console up on `:3020` automatically (reusing one if already running).

> **Note — runs against a production build, not the dev server.** The suite
> executes `bun run build && bun run preview`. The SSR + HMR dev server does not
> reliably re-hydrate in a headless browser (a theme / i18n hydration mismatch
> leaves the page non-interactive), so controlled inputs and client navigation
> would not work under `vite dev`. The production build hydrates
> deterministically, which is what E2E needs.

To run against an already-running instance instead, set `PLAYWRIGHT_BASE_URL`
(with `reuseExistingServer`, no build/preview is spawned):

```bash
bun run build && bun run preview --port 3020 &   # in one terminal
PLAYWRIGHT_BASE_URL=http://localhost:3020 bun run test:e2e
```

## Isolation from the Vitest unit suite

- **Vitest** only collects `src/**/*.{test,spec}.{ts,tsx}` (see
  [`vitest.config.ts`](../vitest.config.ts)); these E2E specs live in `e2e/`,
  outside `src/`, so the unit runner never picks them up.
- **Playwright** only looks in `testDir: "./e2e"` and matches `*.spec.ts`, so it
  never picks up the `*.test.tsx` unit/component tests under `src/`.

The two suites share zero files and run independently.
