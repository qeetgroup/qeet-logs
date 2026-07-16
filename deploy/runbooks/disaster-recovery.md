# Runbook — Disaster Recovery

Recovery from catastrophic loss — a lost region, a destroyed cluster, or corruption across a store.
For routine single-store restores use [backup-restore.md](backup-restore.md); this runbook is the
whole-platform rebuild.

## Objectives

| Metric | Target | Basis |
|---|---|---|
| **RTO** (time to service restored) | < 5 min for a warm-standby failover; < 2 h for a cold rebuild | SOC 2 map A1.3 |
| **RPO** (max data loss) | ≤ 5 min for Postgres (WAL); ≤ backup interval for ClickHouse; bounded by retention TTL regardless | `deploy/SOC2-CONTROLS.md` |

Data classes and their loss tolerance:

| Data | Store | On total loss |
|---|---|---|
| Control plane (tenants, keys, RBAC, rules, incidents, dashboards) | Postgres | **Must** be restored — nothing works without it |
| Observability data (logs/metrics/traces) | ClickHouse | Restore from backup; residual gap ≤ RPO; retention TTL caps the max data ever held |
| In-flight ingest | NATS JetStream | Recovered by the writer if the JetStream volume survives; otherwise lost (clients/SDKs retry + buffer) |
| Cache / tail fan-out | Redis | Rebuilt from live traffic; no restore needed |

## Failure scenarios

### 1. Query API / gateway loss (stateless tiers gone)
Lowest severity — no data at risk. Re-deploy from the Helm chart; HPA and probes bring capacity
back. Confirm `/readyz` green on both hosts. **RTO: minutes.**

### 2. Redis loss
Live-tail and rate-limit state gone; no persistent data lost. Re-provision Redis, point
`REDIS_URL` at it, restart query pods. Tail resumes as new logs arrive. **RTO: minutes.**

### 3. NATS JetStream loss
- **Volume survived** → the writer resumes draining; no action beyond restart.
- **Volume lost** → in-flight (not-yet-written) messages are lost; already-written data is safe in
  ClickHouse. SDK client buffers and retries cover most of the gap. Re-provision NATS, run
  `EnsureStreams` (the query/alerter processes do this on boot), resume ingest.

### 4. ClickHouse cluster loss
Observability data lost until restored. Rebuild the cluster
(prod = `ReplicatedMergeTree` + ClickHouse Keeper), mount `storage.xml` (the `s3_cold` disk) **first**
if tiering is used, apply DDL (`make ch-migrate`), then `RESTORE` from the S3 backup
([backup-restore.md](backup-restore.md), Path A). Re-apply any erasure requests that fell in the
restore gap (DPDP). **RTO: hours** (volume-dependent). Ingest can resume before restore completes —
new data lands; historical data backfills.

### 5. PostgreSQL loss (control-plane disaster)
**Highest priority.** Without metadata, API keys don't resolve, RLS has no tenant context, and the
ClickHouse data is unaddressable per-tenant. Restore Postgres from WAL/PITR (RPO ≤ 5 min) or the
latest dump, then `make migrate-up` to the current version. Only after Postgres is healthy does the
rest of the platform become usable.

### 6. Full region loss
Rebuild in the standby region in dependency order (below). This is why backups (Postgres WAL +
ClickHouse `BACKUP … TO S3`) **must live in a different region** from the primary — the cold tier is
tiering, not a DR copy.

## Whole-platform rebuild order

Dependency-ordered — each step must be green before the next:

1. **Infrastructure** — K8s cluster, storage classes, secrets (`qeet-logs-secrets`), DNS
   (`api.logs.qeet.in`, `ingest.logs.qeet.in`), ingress + cert-manager.
2. **PostgreSQL** — provision, restore, `make migrate-up`. Verify the `qeet_logs_app` role + RLS.
3. **ClickHouse** — provision (Keeper + `ReplicatedMergeTree`), mount `storage.xml`, `make
   ch-migrate`, `RESTORE` from S3.
4. **NATS + Redis** — provision; streams auto-created on query/alerter boot.
5. **Ingest** (gateway + writer) — deploy; confirm `ingest.logs.qeet.in/readyz`.
6. **Query API** — deploy; confirm `api.logs.qeet.in/readyz` (all four deps green).
7. **Alerter** — deploy the **singleton**; confirm a clean evaluation cycle.
8. **Lifecycle + console** — deploy last (non-critical path).

## Verification & closeout

```bash
curl -fsS https://api.logs.qeet.in/readyz
curl -fsS https://ingest.logs.qeet.in/readyz
# end-to-end: ingest then query a probe event
QEET_LOGS_API_KEY=<key> ql send  --service dr-check --level info --message "dr restore ok"
QEET_LOGS_API_KEY=<key> ql query "SELECT service,message FROM logs WHERE service='dr-check'"
```

Then: re-verify erasure requests in the restore gap, confirm retention TTLs are intact, drain any
DLQ backlog (`GET /v1/admin/dlq`), re-enable alerting, and write the postmortem
([incident-response.md](incident-response.md)). Schedule a DR game-day to keep RTO/RPO honest — an
untested DR plan is a hope, not a plan.
