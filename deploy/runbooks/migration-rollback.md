# Runbook — Migration Rollback

Qeet Logs has **two** independent schema stores with **different** rollback stories. Know which one
you are touching.

| Store | Tool | Migrations | Down migrations? |
|---|---|---|---|
| PostgreSQL 17 | `golang-migrate` (`cmd/migrate`) | `migrations/NNNN_*.{up,down}.sql` (0001–0021) | ✅ paired `.down.sql` |
| ClickHouse 25+ | `make ch-migrate` (raw DDL via `clickhouse-client`) | `clickhouse/migrations/NNNN_*.sql` (0001–0009) | ❌ **forward-only, no down files** |

**Golden rule:** migrations are immutable once applied. You roll *back* by applying a down migration
(Postgres) or a compensating forward migration (ClickHouse) — never by editing an applied file.

---

## PostgreSQL rollback (golang-migrate)

### Check state first

```bash
make migrate-version DB_URL="$DB_URL"    # prints the current version; flags "dirty" state
```

If the version is **dirty**, a previous migration failed mid-apply. Do **not** blindly roll back —
inspect the partial change, fix the data/DDL, then `force` to the correct version before continuing:

```bash
# golang-migrate: force sets the version WITHOUT running SQL — use only to clear a dirty flag
go run ./cmd/migrate -url "$DB_URL" force <version>
```

### Roll back N migrations

```bash
make migrate-down DB_URL="$DB_URL"          # rolls back the last 1
make migrate-down DB_URL="$DB_URL" n=3      # rolls back the last 3
```

Each `down` runs the paired `NNNN_*.down.sql`. Review the down SQL **before** running it in prod —
many down migrations `DROP` tables/columns and are **destructive** (e.g. rolling back
`0011_business_context` drops the `business_context` table and its data). Take a Postgres backup
first (see [backup-restore.md](backup-restore.md)).

### Deploy-safe ordering

- **Roll back code before schema** when a new column/table is being removed, so no running pod
  references a dropped object.
- **Roll back schema before code** is rarely needed; the query API tolerates additive columns.
- Migrations are additive by convention (new tables/columns), which makes most forward deploys
  backward-compatible with the previous pod generation during a rolling update — prefer fixing
  forward over rolling schema back.

---

## ClickHouse "rollback" (forward-only)

There are **no `.down.sql` files** for ClickHouse. `make ch-migrate` applies every
`clickhouse/migrations/*.sql` in order; the statements are idempotent (`IF NOT EXISTS`,
`MODIFY SETTING`). To undo a ClickHouse change you write a **new, higher-numbered** compensating
migration.

Caveats that make ClickHouse rollback different from Postgres:

- **Dropping a column is a mutation**, not a metadata flip — `ALTER TABLE … DROP COLUMN` rewrites
  parts in the background and is expensive at volume. Watch `system.mutations`.
- **`MODIFY SETTING storage_policy` is a superset-only change.** The `0009_cold_tier` policy works
  because its first volume *is* the existing `default` disk. You cannot swap to a policy that omits
  a disk the table currently uses. To "undo" tiering, you must add a compensating policy that still
  contains the current disks and then move parts back with `ALTER TABLE … MOVE PARTITION … TO
  VOLUME`.
- **TTL changes** (`0009` move-TTL, per-record `_retention_days` delete-TTL) take effect on the next
  merge; already-deleted data is **gone** (true hard-delete, no shadow copies — see
  [../../docs/data-privacy.md](../../docs/data-privacy.md)). A TTL rollback cannot resurrect deleted
  rows; restore from backup if you need them.
- **Materialized views** (`metrics_5m`/`metrics_1h`, `0008`) only populate on *new* inserts. Dropping
  and recreating an MV does not backfill historical aggregates.

### Rolling back a bad ClickHouse migration

1. Stop the writers (or pause ingest) if the migration affected an actively-written table.
2. Author `clickhouse/migrations/00NN_revert_<thing>.sql` with the compensating DDL.
3. Apply it: `make ch-migrate` (safe to re-run — earlier idempotent statements no-op).
4. Verify against `system.tables` / `system.columns` and re-enable ingest.

---

## Verifying a rollback

```bash
make migrate-version DB_URL="$DB_URL"      # Postgres at the expected version, not dirty
curl -fsS https://api.logs.qeet.in/readyz  # deps green
# functional smoke: ingest + query a probe event (see backup-restore.md)
```

If a rollback leaves the platform inconsistent (e.g. code expects a dropped column), prefer
**forward recovery** — deploy a fixed migration + matching code — over chasing the rollback.
