-- Cold-tier storage lifecycle (TAD §6, PRD Module 6 hot/warm/cold tiering /
-- P2-G2). Recent data stays on fast local disk (hot); data older than the hot
-- window moves to a cheap S3-backed volume (cold, MinIO in dev / any S3 in prod);
-- data past the per-tenant retention boundary is hard-deleted by the SAME
-- per-record `_retention_days` TTL that already governs deletion (Module 3.3 —
-- no shadow copies). This migration only adds a hot→cold MOVE step ahead of that
-- existing DELETE; the delete semantics are unchanged.
--
-- PREREQUISITE (server config, NOT DDL): a `hot_cold` storage policy must be
-- defined on the ClickHouse server via clickhouse/config/storage.xml (mounted to
-- /etc/clickhouse-server/config.d/). It declares an `s3_cold` disk (MinIO) and a
-- policy whose HOT volume is the existing `default` disk and whose COLD volume is
-- `s3_cold`. Because the new policy's first volume IS the current default disk,
-- `MODIFY SETTING storage_policy` is a safe superset change (ClickHouse requires
-- the new policy to contain the table's current disks).
--
-- The 3-day hot window below is a GLOBAL floor expressed at the table level (a
-- table TTL cannot read per-tenant config). Per-tenant hot windows are refined by
-- cmd/lifecycle, which proactively MOVEs a specific tenant's aged partitions to
-- the cold volume based on tenant_tiers.hot_days (Postgres migration 0020).
--
-- Requires a ClickHouse cluster + reachable S3/MinIO to APPLY (`make infra-up`
-- then `make ch-migrate`); it is not exercised in the unit sandbox.

-- Attach the tiering policy to each substrate table.
ALTER TABLE qeet_logs.logs    MODIFY SETTING storage_policy = 'hot_cold';
ALTER TABLE qeet_logs.metrics MODIFY SETTING storage_policy = 'hot_cold';
ALTER TABLE qeet_logs.traces  MODIFY SETTING storage_policy = 'hot_cold';

-- logs: move to cold after 3 days, delete at the per-tenant retention boundary.
ALTER TABLE qeet_logs.logs MODIFY TTL
    toDateTime(timestamp) + toIntervalDay(3)               TO VOLUME 'cold',
    toDateTime(timestamp) + toIntervalDay(_retention_days) DELETE;

-- metrics: same tiering; metrics default to 30-day retention.
ALTER TABLE qeet_logs.metrics MODIFY TTL
    toDateTime(timestamp) + toIntervalDay(3)               TO VOLUME 'cold',
    toDateTime(timestamp) + toIntervalDay(_retention_days) DELETE;

-- traces: same tiering; traces default to 30-day retention.
ALTER TABLE qeet_logs.traces MODIFY TTL
    toDateTime(timestamp) + toIntervalDay(3)               TO VOLUME 'cold',
    toDateTime(timestamp) + toIntervalDay(_retention_days) DELETE;
