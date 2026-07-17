# Runbook — Incident Response

Operational response for Qeet Logs production incidents. For availability targets and error budgets
see [../../docs/slo-sli.md](../../docs/slo-sli.md).

## Severity classification

| Sev | Definition | Examples | Response |
|---|---|---|---|
| **SEV-1** | Ingest or query fully down, or any cross-tenant data exposure | Gateway returns 5xx for all tenants; RLS bypass; ClickHouse cluster down | Page immediately, war room, exec notify |
| **SEV-2** | Major degradation, single subsystem down | Query p95 > 10× SLO; alerter not firing; DLQ growth unbounded | Page on-call, mitigate within 1h |
| **SEV-3** | Partial / single-tenant / non-urgent | One tenant's tail lagging; a Loki panel erroring | Ticket, next business day |

Cross-tenant data exposure is **always SEV-1** regardless of blast radius — see the tenant-isolation
invariant in [../../docs/data-privacy.md](../../docs/data-privacy.md).

## First 5 minutes

1. **Acknowledge** the page. Declare severity.
2. **Scope it** — is it ingest (write path), query (read path), or alerting? Check probes:
   ```bash
   curl -fsS https://api.logs.qeet.in/readyz      # query API deps
   curl -fsS https://ingest.logs.qeet.in/readyz   # gateway deps
   ```
   `/readyz` reports which backing service (Postgres / Redis / ClickHouse / NATS) is unhealthy.
3. **Establish a timeline** — note start time, first symptom, and any recent change. Qeet Logs
   ingests its own change stream; correlate with `GET /v1/deploy/culprits?service=<svc>` and
   `GET /v1/timeline?service=<svc>&since=1h`.
4. For SEV-1/2, **open a war room** — `POST /v1/admin/incidents/{id}/declare`, then log the
   investigation with `POST /v1/admin/sessions/{id}/entries` (durable, survives handoff).

## Triage by subsystem

### Ingest path (writes failing / data not landing)
- Gateway `/readyz` red on **NATS** → ingest cannot publish. Check NATS JetStream health
  (`:8223/varz`, stream `qeet-logs.>`). The gateway buffers nothing — clients get 5xx and should
  retry (SDKs buffer and retry).
- Data accepted (2xx) but not queryable → the **writer** is behind or down. Failed batches land in
  the DLQ (`dlq_events`, Postgres); inspect `GET /v1/admin/dlq` and replay with
  `POST /v1/admin/dlq/{id}/replay` once ClickHouse recovers.
- ClickHouse down → writer retries; NATS JetStream persists in-flight messages (no data loss on a
  transient ClickHouse outage). Restore ClickHouse (see
  [backup-restore.md](backup-restore.md)), then drain the DLQ.

### Query path (reads failing / slow)
- `/readyz` red on **ClickHouse** → queries fail. Check ClickHouse load, in-flight mutations
  (`system.mutations`), and merges (`system.merges`). A stuck `ALTER … DELETE` erasure mutation can
  saturate a node — see [../../docs/data-privacy.md](../../docs/data-privacy.md).
- Query p95 breached but ClickHouse healthy → check per-tenant cardinality (PromQL cardinality cap
  is `METRIC_MAX_LABEL_CARDINALITY`, default 50 000; a 400 with guidance means a tenant blew it) and
  scale query replicas (see [scaling.md](scaling.md)).
- Live-tail stalled → Redis is the fan-out; check Redis health and the `tail.{tenant}.{service}`
  channels.

### Alerting path (alerts not firing / storming)
- `cmd/alerter` is a **singleton** (one replica). If it crash-loops, no alerts fire. Check its logs
  and restart. It re-reads rules from Postgres on each cycle, so no state is lost.
- Alert storm → confidence gating (`ALERT_PAGE_MIN_CONFIDENCE`, default 0.6) should suppress
  low-confidence pages into the low-severity feed. If storming anyway, raise the gate temporarily
  and/or disable the noisy rule via `DELETE /v1/admin/alert-rules/{id}`.
- Delivery failing → alerter delivers via webhook + Qeet Notify (`domains/alerting/delivery.go`); if
  Qeet Notify is down, webhook delivery still fires. Regional-language delivery degrades to default
  locale.

## Mitigation levers

- **Scale out** query/ingest — HPA is enabled (query 2→10, ingest 3→20). Bump `minReplicas` for a
  faster floor. See [scaling.md](scaling.md).
- **Shed load** — a compromised/abusive API key is rate-limited via Redis; revoke it with
  `DELETE /v1/admin/api-keys/{id}` (takes effect immediately — next call → 401).
- **Roll back** a suspected bad deploy — `GET /v1/deploy/culprits` gives the ranked culprit + a
  rollback suggestion; execute the rollback in your deploy tooling, then verify recovery on the
  timeline. Migration rollback is a separate procedure — see
  [migration-rollback.md](migration-rollback.md).

## Post-incident

1. Resolve the incident (`GET /v1/incidents` shows it close; the alerter fires `incident.resolved`
   to registered webhooks).
2. Author a structured postmortem — `POST /v1/admin/postmortems` with detection-linked remediation
   commitments (`/commitments`). CERT-In-reportable incidents export via
   `GET /v1/admin/postmortems/{id}/cert-in-export` (6-hour window — see
   [../../docs/data-privacy.md](../../docs/data-privacy.md)).
3. Remediation commitments can be promoted to live alert rules; resolution outcomes feed continuous
   calibration (`POST /v1/admin/incidents/{id}/feedback`), tightening future confidence scoring.
