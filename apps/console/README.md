# Qeet Logs — Console

The operator console for **Qeet Logs** (privacy-first, identity-aware log management). It's a
[TanStack Start](https://tanstack.com/start) app (SSR + file-based routing) built on the
**`@qeetrix/ui`** design system, talking to the Go query API (`cmd/query`, `:8100`).

- **Stack:** React 19 · TanStack Start / Router / Query · `@qeetrix/ui` (Base UI + Tailwind v4) ·
  Recharts · i18next · Vitest
- **Package manager:** Bun (this app is the sole member of the repo-root Bun workspace)
- **Dev port:** `3020`

---

## Quick start

```bash
# from the repo root
bun install
cp apps/console/.env.example apps/console/.env    # then edit if the API isn't on :8100

# run the console (needs the query API running — `make dev` at the repo root)
bun run --filter '@qeet-logs/console' dev          # → http://localhost:3020
```

## Scripts

Run from the repo root with `bun run --filter '@qeet-logs/console' <script>`, or from `apps/console`
with `bun run <script>`.

| Script | What it does |
|---|---|
| `dev` | Vite dev server on `:3020` |
| `build` | Production build (`.output/`, Nitro server) |
| `preview` | Preview the production build |
| `typecheck` | `tsc --noEmit` |
| `test` / `test:run` | Run the Vitest suite once (jsdom) |
| `test:watch` | Vitest in watch mode |

Lint/format is centralised: run **`bun run check`** (Biome) from the repo root.

## Environment

Config is validated in [`src/env.ts`](src/env.ts) via `@t3-oss/env-core`. Client vars must be
prefixed `VITE_`.

| Variable | Default | Purpose |
|---|---|---|
| `VITE_API_URL` | `http://localhost:8100` | Base URL of the query API. The live-tail WebSocket URL is derived by swapping `http(s)`→`ws(s)`. |
| `VITE_APP_TITLE` | `Qeet Logs` | Window title / visible app name. |
| `SERVER_URL` | — | Optional internal URL for SSR/server functions. |

## Auth model

Unlike other Qeet products (OIDC/JWT), the query API authenticates **every request with an
`X-Qeet-Api-Key` header**. The console treats "holding a valid key" as being signed in.

- The key is stored **only** in this browser's `localStorage` (`qeet-logs.api-key`) and attached to
  every non-anonymous request in [`src/lib/api.ts`](src/lib/api.ts).
- **Sign-in** ([`/sign-in`](src/routes/sign-in.tsx)) validates a pasted key against
  `GET /v1/admin/api-keys` (requires the `logs:admin` scope) before storing it.
- A `401` from any call clears the key and bounces to `/sign-in`.
- The live-tail WebSocket can't send custom headers, so the key is passed as the `api_key` query
  param (see `wsURL`).
- The auth guard lives in the `_app` layout as a `useEffect` (not `beforeLoad`) because the key is in
  `localStorage` and therefore invisible to the SSR pass.

## Route map

All application screens sit under the authenticated `_app` layout (sidebar + header). `/sign-in` is
the only public route.

| Path | Screen | Path | Screen |
|---|---|---|---|
| `/` | Overview | `/analytics` | Analytics (TTFIQ) |
| `/tail` | Live Tail (WS) | `/saved-searches` | Saved Searches |
| `/topology` | Service Topology | `/audit` | Audit Log |
| `/timeline` | Timeline | `/alerts` | Alert Rules |
| `/dashboards` | Dashboards | `/postmortems` | Postmortems (CERT-In export) |
| `/search` | Log Search (LogQL++) | `/business-context` | Business Context |
| `/incidents` | Incidents (RCA / deploys / impact) | `/api-keys` | API Keys |
| `/changes` | Changes | `/webhooks` | Webhooks |
| `/export` | Export | `/settings` | Retention & PII masking |
| `/sign-in` | Sign in (public) | | |

## Project structure

```
src/
  routes/                 File-based routes (__root, _app layout, _app/*, sign-in)
  components/             Shared UI: page-header, results-table, error-state, confirm-dialog, data-table/*
  features/dashboard/     Sidebar, header user menu, breadcrumb, theme toggle
  config/navigation.tsx   Sidebar/breadcrumb tree (i18n keys → labels)
  lib/                    api (HTTP client) · auth · query · incidents · format · domain · list-view
  i18n/                   i18next instance + locales/en/common.json
  integrations/tanstack-query/  QueryClient + global toast wiring
  test/setup.ts           Vitest jsdom setup (RTL cleanup + browser polyfills)
```

## Production conventions

- **Error handling.** `__root` and `_app` declare `errorComponent`s that render a branded
  `ErrorState` with retry (re-runs loaders via `router.invalidate()`); `__root` also sets
  `notFoundComponent`. Content-area render errors are caught by a `CatchBoundary` around the router
  `Outlet` so the sidebar/header survive. Backend errors from mutations/queries surface as a single
  toast (see `integrations/tanstack-query/root-provider.tsx`); `4xx`/validation and `silent` queries
  opt out. Net effect: a failure never collapses to a blank screen.
- **Loading / empty.** Every query-backed surface uses `@qeetrix/ui`'s `<DataState>` (skeleton /
  error / empty / data) with `<EmptyState>` zero-states. The generic query envelope renders through
  `components/results-table.tsx`.
- **i18n.** User-facing strings resolve through `t()` / `<Trans>` (react-i18next). Keys live in
  `src/i18n/locales/en/common.json`; the setup mirrors the qeet-id console so more locales drop in
  without touching call sites. English is the only shipped catalog today.
- **Accessibility.** Single `<main>` landmark (the sidebar inset), a labelled `<nav>` landmark with
  `aria-current="page"` on the active item, a skip-to-content link, `aria-label`s on icon-only
  buttons, `aria-sort` on sortable table headers, and keyboard-accessible dialogs/sheets (Base UI).

## Testing

Vitest + Testing Library run under jsdom ([`vitest.config.ts`](vitest.config.ts)).

- **Unit:** `lib/format`, `lib/api` (+ `wsURL`, `keyStore`), `lib/query` (hook, mocked transport),
  `lib/auth` (`validateKey`).
- **Component:** `page-header`, `results-table`, `data-table/sort-header`.

```bash
bun run --filter '@qeet-logs/console' test:run
```
