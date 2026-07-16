# qeet-logs test suite

End-to-end tests that exercise a **running** qeet-logs stack over real HTTP /
WebSocket — they do not import the server packages.

```
test/
  integration/   Go black-box integration tests (build tag: integration)
  load/          k6 load tests (query + ingest)  — see load/README.md
```

## Integration tests (`test/integration/`)

Every file carries `//go:build integration`, so the default `go test ./...`
**never** compiles or runs them (they need live infra). They talk to the query
API purely over HTTP + WS.

### Prerequisites

```bash
make infra-up      # ClickHouse, Postgres, NATS, Redis, MinIO
make migrate-up    # Postgres metadata
make ch-migrate    # ClickHouse DDL (logs/auth_events tables) — needed for query reads
make dev           # query API on :8100
```

You also need at least one API key. There is no `cmd/seed` in this repo (the
Makefile's `seed` target references a `./cmd/seed/` package that does not exist),
so mint keys via the admin API (with an existing admin key or a Qeet ID JWT):

```bash
curl -s -X POST http://localhost:8100/v1/admin/api-keys \
  -H "X-Qeet-Api-Key: $ADMIN_KEY" \
  -d '{"name":"integration","scopes":["logs:admin","logs:read","logs:query","logs:export"]}'
# → returns {"Key":"qeel_…", …} once
```

### Environment

| Var | Default | Purpose |
|---|---|---|
| `QEET_LOGS_API_URL` | `http://localhost:8100` | query API base URL |
| `QEET_LOGS_API_KEY` | — | **primary key** — should carry `logs:admin` + `logs:read`/`logs:query` + `logs:export` for full coverage |
| `QEET_LOGS_API_KEY_READONLY` | *(optional)* | a key **without** `logs:admin`; the auth scope test mints+revokes one automatically if unset |
| `QEET_LOGS_API_KEY_B` | *(optional)* | a **second tenant's** admin key; enables the cross-tenant isolation tests |
| `QEET_LOGS_INGEST_URL` | `http://localhost:8101` | ingest gateway (load tests) |

Every test **self-skips** (`t.Skip`) when the API is unreachable or a required
key/scope is absent — so the suite is safe to run with no infra (it just skips).

### Run

```bash
# Everything
go test -tags=integration ./test/integration/...

# Full run with race detector + fresh results (mirrors `make test-integration`)
go test -race -count=1 -tags integration ./test/integration/...

# The headline tenant-isolation invariant only
go test -tags=integration ./test/integration/ -run 'Tenant|CrossTenant' -v

# Cross-tenant resource isolation (needs the 2nd-tenant key)
QEET_LOGS_API_KEY_B=qeel_tenantB... go test -tags=integration ./test/integration/ -run CrossTenant -v
```

### What each test asserts

| File / test | Invariant |
|---|---|
| `health_test.go` · `TestHealthz` | `/healthz` is unauthenticated and returns `status:"ok"`. |
| `health_test.go` · `TestReadyz` | `/readyz` reports per-dependency checks (postgres/redis/clickhouse/nats); 200 ⇒ `ready`, 503 ⇒ degraded. |
| `health_test.go` · `TestVersion` | `/version` is unauthenticated and stamped (non-empty). |
| `auth_test.go` · `TestMissingAPIKeyUnauthorized` | No `X-Qeet-Api-Key` ⇒ 401 with an `{"error"}` body. |
| `auth_test.go` · `TestInvalidAPIKeyUnauthorized` | Unknown key ⇒ 401 (never resolved to a tenant). |
| `auth_test.go` · `TestValidKeyResolves` | Primary key authenticates — not 401/403 on a read route. |
| `auth_test.go` · `TestAdminScopeEnforced` | A key lacking `logs:admin` ⇒ **403** on an admin route (RBAC breach if 200). |
| `tenant_isolation_test.go` · `TestCraftedQueryCannotEscapeTenant` | **Headline invariant.** A crafted `q` (forged `tenant_id`/`tenant`, `OR`-injection, parenthesised) can never return rows for a foreign tenant, and all returned rows share one `tenant_id`. The tenant predicate comes from identity, never user input (TAD §7.2). |
| `tenant_isolation_test.go` · `TestCrossTenantResourceIsolation` | Tenant A cannot GET/list/DELETE tenant B's dashboards or saved searches (404 + absent from listings). *(needs `QEET_LOGS_API_KEY_B`)* |
| `tenant_isolation_test.go` · `TestCrossTenantIncidentsDisjoint` | Two tenants' incident feeds never share an incident id. *(needs `QEET_LOGS_API_KEY_B`)* |
| `query_test.go` · `TestQueryJSONRoundtrip` | `/v1/query` returns the `{columns,count,rows}` envelope; default logs projection; `count == len(rows)`. |
| `query_test.go` · `TestQueryCSVFormat` / `TestQueryNDJSONFormat` | `?format=csv` ⇒ `text/csv` with the compiled header row; `?format=ndjson` ⇒ `application/x-ndjson`, each line a JSON object. |
| `query_test.go` · `TestQueryMissingParam` / `TestQueryTailRejectedOnSyncEndpoint` / `TestQueryUnknownTableRejected` | Missing `q`, a `TAIL` on the sync endpoint, and an unknown table are each 400. |
| `query_test.go` · `TestLiveTailWebSocketConnect` | The live-tail WebSocket upgrades (HTTP 101) for a valid `TAIL` statement + valid key, then closes cleanly. |
| `query_test.go` · `TestLiveTailRejectsNonTailStatement` | A non-`TAIL` statement is rejected **before** upgrade (handshake ⇒ HTTP 400, not 101). |
| `query_test.go` · `TestExportJSON` / `TestExportNDJSON` | `/v1/export` returns the query envelope / NDJSON and marks it `Content-Disposition: attachment`. |
| `admin_crud_test.go` · `TestAdmin*CRUD` | Create → read/list → update → delete roundtrips for api-keys, alert-rules, dashboards, saved-searches, retention, business-context, postmortems (+ commitments), webhooks; DLQ list envelope; quota-usage shape. All under `/v1/admin` (require `logs:admin`). |

## Load tests (`test/load/`)

See [`load/README.md`](load/README.md). k6 scripts for `/v1/query` (`query_load.js`)
and `/v1/ingest` (`ingest_load.js`), each with a p95 latency threshold and error-rate gates.
