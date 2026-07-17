# api/openapi/

The Qeet Logs REST contract, split into **four self-contained, bounded-context
OpenAPI 3.1 documents**. There is no monolithic `openapi.yaml` — these files are
the source of truth.

| File | Context | Host | Surface |
|---|---|---|---|
| [ingest.yaml](ingest.yaml) | ingest | `ingest.logs.qeet.in` (:8101, +:4318 OTLP) | Log/metric/trace ingestion — single, batch (NDJSON), GELF, syslog, OTLP, Prometheus remote_write |
| [query.yaml](query.yaml) | query | `api.logs.qeet.in` (:8100) | LogQL++ query, live-tail (WebSocket), NL→query, PromQL, change events, topology, timeline, incidents, RCA, dashboard overlays, public shared dashboards |
| [admin.yaml](admin.yaml) | admin | `api.logs.qeet.in` (:8100) | API keys, alert rules, retention, transform, erasure (DPDP/GDPR), dashboards, saved searches, audit, DLQ, quota |
| [operations.yaml](operations.yaml) | operations | `api.logs.qeet.in` (:8100) | Health/readiness/version probes |

Each file carries its own `components` (the transitive `$ref` closure of what its
paths use) plus the shared `securitySchemes` (`ApiKey` → the `X-Qeet-Api-Key`
header, and `BearerJWT` → a Qeet ID OIDC token) and the document-level `security`
requirement, so **every file validates standalone**.

## Conventions

- `operationId` and full request/response bodies are carried on the launch-critical
  families; remaining admin CRUD routes carry accurate path/method/params/security.
- Auth: `X-Qeet-Api-Key` **or** a Bearer JWT on every operation except the public
  ones (`/healthz`, `/readyz`, `/version`, `/shared/**`), which set `security: []`.
- Ingest operations pin their `servers` to the ingest gateway host; everything else
  targets the query/admin API host.
- **Live tail is a WebSocket** (`GET /v1/query/tail?q=<TAIL …>`), not SSE — the
  contract reflects the real handler in `platform/api/handler/tail.go`.

## Adding or changing routes

1. Edit the file for the relevant bounded context (match the operation's `tags`).
2. Keep `cmd/query` (chi router) and the Postman collection (`api/postman/`) in sync.
3. Run `verify` (below) to confirm the file stays self-contained.

## Tooling — `tools/openapi-split/`

The splitter lives in its own nested Go module (keeps `gopkg.in/yaml.v3` out of the
production server's dependency graph):

```bash
cd tools/openapi-split
go run . verify   # assert each split file is self-contained (no dangling $ref)
go run . merge    # combine the four into one document on stdout (for codegen/Swagger UI)
go run . split    # ONE-TIME migration: re-split api/openapi/openapi.yaml into the four files
```

`split` was run once to produce these files from the original monolith and then
removed it; it is retained for reproducibility. `merge` is the single-document
feeder for any tool (codegen, Swagger UI) that wants one file.
