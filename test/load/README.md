# Load tests (k6)

Load tests for the qeet-logs data plane, written for [k6](https://k6.io).

| Script | Target | What it drives |
|---|---|---|
| `query_load.js` | query API `/v1/query` (`:8100`) | Ramps VUs across representative LogQL++ queries; p95 latency + error-rate thresholds. |
| `ingest_load.js` | ingest gateway `/v1/ingest` (`:8101`) | Ramping arrival-rate writes; asserts `202 Accepted`; p95 latency + error-rate thresholds. |

## Install k6

```bash
brew install k6            # macOS
# or see https://grafana.com/docs/k6/latest/set-up/install-k6/
```

## Run

Bring up infra and the services first (`make infra-up`, `make dev`, and the Rust
`make dev-ingest` for the ingest test), then point the scripts at them with an
API key that has the right scope.

```bash
# Query load (needs a key with logs:read or logs:query)
QEET_LOGS_API_URL=http://localhost:8100 \
QEET_LOGS_API_KEY=qeel_xxx \
k6 run test/load/query_load.js

# Ingest load (needs a key with logs:ingest)
QEET_LOGS_INGEST_URL=http://localhost:8101 \
QEET_LOGS_API_KEY=qeel_xxx \
k6 run test/load/ingest_load.js
```

## Tunables (environment variables)

| Var | Default | Applies to | Meaning |
|---|---|---|---|
| `QEET_LOGS_API_URL` | `http://localhost:8100` | query | Query API base URL |
| `QEET_LOGS_INGEST_URL` | `http://localhost:8101` | ingest | Ingest gateway base URL |
| `QEET_LOGS_API_KEY` | — (required) | both | `X-Qeet-Api-Key` credential |
| `MAX_VUS` | `50` | query | Peak virtual users |
| `TARGET_RPS` | `500` | ingest | Peak requests/second |
| `P95_MS` | `800` (query) / `200` (ingest) | both | p95 latency budget (ms) |

## Thresholds

Each script fails (non-zero exit) if a threshold is breached — suitable for CI
gating:

- **query**: `p95(/v1/query) < P95_MS`, query error rate `< 1%`, HTTP failures `< 1%`.
- **ingest**: `p95(/v1/ingest) < P95_MS`, ingest error rate `< 1%`, HTTP failures `< 1%`.

Adjust the `stages` in each script's `options` to shape the ramp (warm-up →
ramp → sustain → ramp-down).

## Notes

- Both planes enforce tenant isolation from the API key, so a load run only ever
  reads/writes the key's own tenant — safe to run against a shared dev stack.
- The ingest gateway returns `202 Accepted` for records that the PII gate drops,
  so a high drop rate will still pass the status check (watch the gateway's own
  `accepted`/`dropped` counters for that signal).
