# Runbook — Backup & Restore

Qeet Logs holds state in **two** stores that must both be backed up:

| Store | Holds | Loss impact |
|---|---|---|
| **PostgreSQL 17** | Tenants, API keys, RBAC scopes, alert rules, incidents, dashboards, saved searches, webhooks, postmortems, war-room sessions, plans, transform programs, DLQ index, erasure requests | Control-plane loss — auth, alerting, and all metadata gone; the log data in ClickHouse becomes orphaned/unqueryable per-tenant |
| **ClickHouse 25+** | Logs, metrics, traces, auth_events, change_events (+ rollups) | Observability data loss (bounded by retention TTL anyway) |

NATS JetStream and Redis are **transient** — JetStream persists only in-flight messages (recovered
by the writer), Redis is cache/fan-out. Neither needs a scheduled backup; both must be re-seedable
from config on rebuild. Recovery objectives are in [../../docs/slo-sli.md](../../docs/slo-sli.md)
(target RTO < 5 min per the SOC 2 map, RPO ≤ 5 min for Postgres).

---

## PostgreSQL (metadata — highest priority)

### Backup

Continuous WAL archiving + a periodic base backup is the production standard (point-in-time
recovery). For ad-hoc/logical backups:

```bash
# Logical dump (per-DB). Run against the metadata DB.
pg_dump --format=custom --no-owner --dbname="$DATABASE_URL" \
  --file="qeet-logs-$(date -u +%Y%m%dT%H%M%SZ).dump"
```

Store dumps + WAL in an off-cluster object store (different region from the live DB). Verify a
restore into a scratch DB monthly — an untested backup is not a backup.

### Restore

```bash
# 1. Provision an empty Postgres 17 and create the database.
createdb --dbname="$ADMIN_URL" qeet-logs

# 2. Restore the dump.
pg_restore --no-owner --dbname="$RESTORE_URL" qeet-logs-<timestamp>.dump

# 3. Reconcile schema version — migrations are immutable and sequential.
make migrate-version DB_URL="$RESTORE_URL"     # confirm it matches the dump's era
make migrate-up      DB_URL="$RESTORE_URL"     # apply any migrations newer than the dump
```

RLS is enforced by the schema (`current_setting('app.tenant_id')`), so it is restored with the
migrations — no extra step. After restore, confirm the app role (`qeet_logs_app`) exists and that
`/readyz` on a query pod goes green.

---

## ClickHouse (logs / metrics / traces)

ClickHouse backup is **volume-scale** — plan for it, do not `SELECT … INTO OUTFILE` a production
table. Two supported paths:

### Path A — `BACKUP`/`RESTORE` to S3 (preferred)

```sql
-- Backup the qeet_logs database to the cold/backup bucket.
BACKUP DATABASE qeet_logs
  TO S3('https://<bucket>.s3.<region>.amazonaws.com/backups/qeet_logs/{timestamp}',
        '<access_key>', '<secret_key>');

-- Restore (into an empty cluster or a fresh database name).
RESTORE DATABASE qeet_logs
  FROM S3('https://<bucket>.s3.<region>.amazonaws.com/backups/qeet_logs/<timestamp>',
          '<access_key>', '<secret_key>');
```

### Path B — rely on the cold tier + retention semantics

Aged partitions already live on S3/MinIO via the `hot_cold` storage policy
(`clickhouse/config/storage.xml`, CH migration `0009_cold_tier.sql`). This is a **tiering**
mechanism, **not** a backup — cold data is still a single copy governed by the same per-record
`_retention_days` hard-delete TTL. Do not treat the cold tier as your disaster copy.

### Restore ordering & schema

1. Provision the ClickHouse cluster (prod = `ReplicatedMergeTree` + ClickHouse Keeper).
2. Apply DDL first if restoring into an empty cluster: `make ch-migrate` (idempotent `IF NOT
   EXISTS`), or let `RESTORE` recreate tables from the backup.
3. If the `hot_cold` policy is in use, ensure `storage.xml` (the `s3_cold` disk) is mounted
   **before** attaching tables — `MODIFY SETTING storage_policy` requires the policy to contain the
   table's current disks.

### Caveats

- **Partition/order keys are `(tenant_id, …)`** — tenant isolation survives restore because it is a
  column-level predicate, not a per-tenant object. Never restore one tenant's rows into another
  tenant's partition.
- **Insert-dedup** — the writer sets ClickHouse insert-deduplication; replaying the DLQ or
  re-ingesting after a partial restore will not double-insert the same ULID batch.
- **Erasure receipts** — an erasure completed before the backup point is baked into the ClickHouse
  data; one completed *after* the backup point but before the restore point must be **re-applied**
  after restore (re-run the erasure request) to stay DPDP-compliant. Track this explicitly during
  point-in-time restores.

---

## Coordinated (whole-platform) restore

Restore **Postgres first, ClickHouse second** — the metadata (tenants, API keys, retention config)
is what makes the ClickHouse data addressable and correctly TTL'd. After both:

```bash
curl -fsS https://api.logs.qeet.in/readyz     # all four deps green
# smoke: ingest one event via cmd/ql, then query it back
QEET_LOGS_API_KEY=<key> ql send --service smoke --level info --message "restore check"
QEET_LOGS_API_KEY=<key> ql query "SELECT service,message FROM logs WHERE service='smoke'"
```

Then re-verify any erasure requests that fell in the restore gap (see caveat above) and re-enable
the alerter singleton.
