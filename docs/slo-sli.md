# SLOs & SLIs

Service-level objectives for Qeet Logs. These are **proposed targets** for the GA service — they set
the error budget the [runbooks](../deploy/runbooks/) defend. They are aspirational until the
in-process metrics endpoint (see [observability.md](observability.md)) is scraping and a full
measurement window exists.

## Service-level indicators

| SLI | Definition | Measured at |
|---|---|---|
| **Query latency** | Server-side p95/p99 of `GET /v1/query` and `GET|POST /api/v1/query` (excludes client network) | Query API |
| **Ingest availability** | Ratio of `2xx` to total accepted ingest requests at the gateway (`/v1/ingest*`, `/v1/logs`, `/v1/metrics`, `/v1/traces`, `/api/v1/write`) | Ingest gateway |
| **Ingest durability** | Fraction of accepted events that reach ClickHouse (not stuck/dropped) | Writer + DLQ depth |
| **Tail lag** | Time from event accepted at the gateway to appearing in `/v1/query/tail` | Gateway → Redis → tail |
| **Alert delivery latency** | Time from threshold/absence/anomaly condition true to notification dispatched (webhook/Qeet Notify) | Alerter |
| **Availability** | `/readyz` green (all four backing deps healthy) | Query API + gateway |

## Proposed objectives

| SLO | Target | Window | Rationale |
|---|---|---|---|
| Query latency p95 | **< 2 s** for interactive queries over the hot tier | 30-day rolling | ClickHouse columnar + bloom/minmax indexes on the hot working set |
| Query latency p99 | **< 8 s** | 30-day rolling | Wide time ranges / cold-tier reads have higher tails |
| Ingest availability | **≥ 99.9 %** `2xx` | 30-day rolling | Stateless gateway + HPA (3→20); NATS absorbs writer/ClickHouse hiccups |
| Ingest durability | **≥ 99.99 %** events land or DLQ | 30-day rolling | DLQ + JetStream persistence mean transient ClickHouse loss ≠ data loss |
| Tail lag | **p95 < 3 s** | 7-day rolling | Live-tail is a UX affordance, not a durability guarantee |
| Alert delivery | **p95 < 60 s** from condition→dispatch | 7-day rolling | Alerter polls on a ~60 s cycle; confidence gating adds no material delay |
| Availability | **≥ 99.9 %** (SOC 2 A1.1) | 30-day rolling | Documented in `deploy/SOC2-CONTROLS.md` |

## Recovery objectives (DR)

| Objective | Target | Source |
|---|---|---|
| **RTO** | < 5 min warm failover; < 2 h cold rebuild | SOC 2 A1.3 / [disaster-recovery.md](../deploy/runbooks/disaster-recovery.md) |
| **RPO** | ≤ 5 min (Postgres WAL); ≤ ClickHouse backup interval | [backup-restore.md](../deploy/runbooks/backup-restore.md) |

## Error budget & policy

At 99.9 % availability the monthly budget is ~43 minutes. Suggested policy:

- **Budget healthy** → ship freely; feature work proceeds.
- **Budget < 25 % remaining** → freeze risky changes; prioritize reliability work; require a rollback
  plan for every deploy.
- **Budget exhausted** → change freeze except reliability fixes until the window resets.

## What is *not* yet an SLO (honest gaps)

- **RCA / AI accuracy** — the learned-to-rank RCA model and ONNX anomaly tiers are gated on a
  labeled corpus that does not exist yet (see [PHASE2-GAP-REGISTER.md](../PHASE2-GAP-REGISTER.md)).
  Only the statistical floor ships today; no accuracy SLO is published.
- **Cross-signal correlation latency** and **cold-tier query latency** need real production telemetry
  before targets are set.
- All numbers above are **proposed**; they become committed SLOs once the metrics pipeline is live
  and a 30-day baseline exists.
