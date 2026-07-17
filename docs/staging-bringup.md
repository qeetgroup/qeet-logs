# Staging bring-up checklist

Stand up a full Qeet Logs environment from scratch and verify it end-to-end. This
is the gate before a [GA release cut](ga-release-checklist.md): everything below
must pass on a staging environment that mirrors production before you tag.

> Local dev and staging use the same steps; staging just points at real infra
> hosts instead of the docker-compose stack. Ports and env keys are from
> [`../CLAUDE.md`](../CLAUDE.md) and [`../.env.example`](../.env.example).

---

## 0. Prerequisites

- [ ] **Go 1.25** (`go version` matches `go.mod`)
- [ ] **bun 1.3.14** (`bun --version`) — for the console
- [ ] **Docker + Compose** (local infra) or access to managed Postgres 17 / ClickHouse / NATS JetStream / Redis 7 / S3-or-MinIO (staging)
- [ ] **Rust toolchain** (`rustup`) — only if running the ingest gateway (`cmd`/Rust); the Go query plane does not need it
- [ ] **k6** (`k6 version`) — for load tests
- [ ] `psql` + a ClickHouse client (or use the compose one-shots)

---

## 1. Configuration

- [ ] `cp .env.example .env`
- [ ] Generate the secrets-at-rest key: `openssl rand -base64 32` → set `QEET_LOGS_SECRETS_KEY` (**required for prod**; unset = plaintext bot tokens)
- [ ] Set real `DATABASE_URL`, `CLICKHOUSE_URL`, `NATS_URL`, `REDIS_URL` for staging (defaults target local compose)
- [ ] Leave the optional integrations unset unless testing them — each degrades to a clear **501**, never a fake success:
  - `ANTHROPIC_API_KEY` → AI Copilot (`/v1/query/copilot*`)
  - `SLACK_CLIENT_ID/SECRET/SIGNING_SECRET/REDIRECT_URL` → two-way ChatOps (`/v1/chatops/slack/*`)
  - `QEET_NOTIFY_URL/QEET_NOTIFY_API_KEY` → regional-language alert delivery
- [ ] (optional) `RATE_LIMIT_PER_MINUTE` (default 600/tenant), `LIFECYCLE_INTERVAL` (default 6h)

---

## 2. Infrastructure

- [ ] `make infra-up` — ClickHouse, Postgres, NATS, Redis, MinIO (local) — or provision the managed equivalents
- [ ] Wait for health: `docker compose ps` shows all services healthy (query/CH ports: 8123/9100, PG 5434, NATS 4223, Redis 6380, MinIO 9002/9003)
- [ ] **Cold-tier only** (Module 6 / P2-G2) — if exercising hot→cold tiering:
  - [ ] Create the cold bucket: `mc mb local/qeet-logs-cold` (or S3 equivalent)
  - [ ] Mount [`../clickhouse/config/storage.xml`](../clickhouse/config/storage.xml) to `/etc/clickhouse-server/config.d/` so the `hot_cold` storage policy exists
  - [ ] ⚠️ Without the storage policy, ClickHouse migration `0009_cold_tier` fails on `MODIFY SETTING storage_policy`. If you are **not** testing tiering in staging, either mount the policy or hold `0009` (apply `0001`–`0008` only)

---

## 3. Migrations

- [ ] `make migrate-up` — Postgres metadata (0001–0021)
- [ ] `make migrate-version` — confirm version = 21, dirty = false
- [ ] `make ch-migrate` — ClickHouse DDL (0001–0009; see the cold-tier caveat above for 0009)
- [ ] Reversibility spot-check (staging only, destructive): `migrate down 1 && migrate up 1` on a scratch DB — CI already enforces full up→down→up

---

## 4. Seed + run

- [ ] `make seed` — creates two demo tenants (`demo`, `demo-b`) + a full-scope API key each + 40 sample log rows. **Copy the printed keys** — they are shown once:
  ```
  export QEET_LOGS_API_URL=http://localhost:8100
  export QEET_LOGS_API_KEY=<tenant A key>
  export QEET_LOGS_API_KEY_B=<tenant B key>
  ```
- [ ] `make dev` — query API on `:8100`
- [ ] `make dev-alerter` — alerter engine (correlation/incidents/delivery)
- [ ] `make dev-lifecycle` — cold-tier mover (no-op until data ages; needs CH + S3)
- [ ] (optional) `make dev-ingest` — Rust ingest gateway on `:8101` + OTLP `:4318`
- [ ] (optional) `make dev-console` — console on `:3020`

---

## 5. Smoke tests (query plane)

- [ ] `curl -fsS $QEET_LOGS_API_URL/healthz` → `{"status":"ok"}`
- [ ] `curl -fsS $QEET_LOGS_API_URL/readyz` → 200 with postgres/redis/clickhouse/nats all `ok` (503 if any degraded)
- [ ] `curl -fsS $QEET_LOGS_API_URL/version` → stamped version
- [ ] `curl -fsS $QEET_LOGS_API_URL/metrics | grep qeet_logs_http` → Prometheus RED metrics present
- [ ] Authenticated query returns seeded rows:
  ```
  curl -fsS -H "X-Qeet-Api-Key: $QEET_LOGS_API_KEY" \
    "$QEET_LOGS_API_URL/v1/query?q=SELECT%20*%20FROM%20logs%20LIMIT%205"
  ```
  → `{"columns":[...],"count":N,"rows":[...]}`

---

## 6. Security verification (the invariants that gate GA)

- [ ] **AuthN**: request without `X-Qeet-Api-Key` → **401**; unknown key → **401**
- [ ] **AuthZ (scope)**: a `logs:read`-only key on `/v1/admin/api-keys` → **403**
- [ ] **Rate limit**: exceed `RATE_LIMIT_PER_MINUTE` on `/v1/query` → **429** + `Retry-After` + `X-RateLimit-*` headers (lower the limit temporarily to test)
- [ ] **Security headers**: responses carry `X-Content-Type-Options`, `X-Frame-Options`, `Content-Security-Policy`; `Strict-Transport-Security` present when behind TLS
- [ ] **Cross-tenant isolation** (headline invariant): tenant A's key cannot read tenant B's logs/incidents/dashboards, and a forged `tenant_id`/`OR`-injection in `q` cannot escape the identity-injected predicate
- [ ] **Secrets at rest**: after a Slack install (if testing ChatOps), `chatops_installations.bot_token` is `v1:`-prefixed ciphertext, not the raw `xoxb-` token

---

## 7. Automated suites

- [ ] Unit + vet + race (should already be green from CI): `make ci`
- [ ] **Integration suite** (needs the running stack + the exported keys):
  ```
  make test-integration
  # or, with the second tenant for cross-tenant tests:
  QEET_LOGS_API_KEY_B=$QEET_LOGS_API_KEY_B go test -tags=integration ./test/integration/... -v
  ```
- [ ] **Load test** (record p95 vs the SLO in [slo-sli.md](slo-sli.md)):
  ```
  k6 run -e API_URL=$QEET_LOGS_API_URL -e API_KEY=$QEET_LOGS_API_KEY test/load/query_load.js
  k6 run -e INGEST_URL=http://localhost:8101 test/load/ingest_load.js   # if ingest is up
  ```

---

## 8. Console (if deploying the UI)

- [ ] `bun install && bun run --filter '@qeet-logs/console' build`
- [ ] Serve the SSR output; sign in with a seeded API key; verify Overview, Search, Live-tail (WS), Incidents load and degrade cleanly (EmptyState/ErrorState) against the staging API

---

## 9. Observability wiring

- [ ] Point Prometheus at `:8100/metrics` (or the pod scrape annotation once on K8s)
- [ ] Confirm `qeet_logs_http_request_duration_seconds` histogram populates under load
- [ ] Wire the SLO alerts from [slo-sli.md](slo-sli.md); confirm structured (zerolog) logs + request IDs flow to your log sink

---

## Exit criteria → proceed to release

- [ ] All of §5–§7 green on staging
- [ ] p95 query latency within the [SLO](slo-sli.md)
- [ ] No open critical/security findings
- [ ] Gated features (ONNX RCA GA, Qeet Pay billing, Teams ChatOps) are **not** advertised in this cut — they return 501 by design

➡️ Continue to the [**GA release-cut checklist**](ga-release-checklist.md).
