# Data Privacy

Qeet Logs is privacy-first by construction, not by policy bolt-on. This document describes the four
load-bearing invariants: the **PII gate**, **per-tenant retention + cold tier**, **DPDP/GDPR
erasure**, and **tenant isolation**. It is the privacy companion to
[../deploy/SOC2-CONTROLS.md](../deploy/SOC2-CONTROLS.md) (CC3, CC5, CC6, C1, P1–P8).

## 1. PII gate — masked at the gate, before storage

The single most important privacy property: **PII never reaches ClickHouse**.

- The gate is **synchronous and blocking**, run in the Rust ingest gateway (`ingest/core/pii.rs`)
  *before* the event is published to NATS — not a background scrubber, not a query-time redaction.
  There is no code path by which raw PII is stored and later cleaned.
- Phase-0 detection is **regex-based** (emails, phone numbers, common secrets/tokens, etc.);
  matched fields are **masked / hashed / dropped** per policy. ML-based detection is a Phase-2 item
  (not yet shipped — see [../PHASE2-GAP-REGISTER.md](../PHASE2-GAP-REGISTER.md)).
- The stored row records **which** fields were masked in `logs.pii_detected` (an array), giving an
  audit trail of gate activity without retaining the sensitive values themselves.
- The transform/remap engine can additionally `redact` fields in-flight
  (`ingest/core/remap.rs`), fail-open (a bad rule is skipped, never drops the event).

**Consequence:** a compromised ClickHouse node, a mis-scoped query, or a leaked backup cannot expose
end-user PII, because it was never written.

## 2. Retention — true hard-delete + per-tenant cold tier

- Each row carries `_retention_days` (ClickHouse), set from the tenant's retention config. At the
  boundary the data is **hard-deleted** by TTL — **no shadow copies, no soft-delete, no
  tombstones** (`clickhouse/migrations/0001_logs.sql`, Module 3.3).
- Operators tune retention via `PUT /v1/admin/retention`; `GET /v1/admin/retention/cost` previews
  the storage-cost impact of a retention change (cost-transparent retention, P2-G2).
- **Cold tier:** aged partitions move from fast local disk to an S3/MinIO **cold** volume via the
  `hot_cold` storage policy (`clickhouse/config/storage.xml`) + `cmd/lifecycle`, refining a global
  table-level move-TTL with per-tenant hot windows (`tenant_tiers.hot_days`, Postgres migration
  `0020`). **The cold tier is tiering, not a second copy** — the same per-record hard-delete TTL
  still governs final deletion. Cold data is not exempt from erasure or retention.

## 3. DPDP / GDPR erasure — right to be forgotten

- **Pseudonymous linkage:** logs carry a `user_linkage_key` (never raw identity) so an erasure can
  target a subject without Qeet Logs ever storing who they are.
- **API:** `POST /v1/admin/erasure` accepts a `user_linkage_key` and/or a `time_from`/`time_to`
  window; `GET /v1/admin/erasure` lists prior requests (`platform/api/handler/erasure.go`, PRD
  06.5). The request is recorded (`erasure_requests`, pending), then fires async ClickHouse
  `ALTER TABLE logs/metrics/traces DELETE WHERE …` mutations across all signal tables, and the row
  is updated to completed with a JSON receipt.
- **Operational caveat:** erasure mutations are heavy at volume — watch `system.mutations`
  ([../deploy/runbooks/on-call.md](../deploy/runbooks/on-call.md)). During point-in-time restores,
  an erasure that completed *after* the backup point must be **re-applied** after restore to remain
  compliant ([../deploy/runbooks/backup-restore.md](../deploy/runbooks/backup-restore.md)).
- **Crypto-shred / full DPDP erasure** (beyond mutation-delete) and **CERT-In** obligations are
  tracked: postmortem CERT-In export ships
  (`GET /v1/admin/postmortems/{id}/cert-in-export`, the 6-hour incident-reporting export); the
  remaining India-compliance items (GST via Qeet Pay, full crypto-shred) are gated Phase-2 items.

## 4. Tenant isolation — architecturally impossible to cross

The invariant: **`tenant_id` is derived from identity, never from user input.**

- **Resolution:** the tenant is resolved from `X-Qeet-Api-Key` → SHA-256 → `api_keys` lookup
  (`platform/database/apikeys.go`, `platform/security/hash.go`), or from the Qeet ID JWT for the
  console. The query layer **always** injects the `tenant_id` predicate from the resolved identity
  and **never** from a request parameter (TAD §7.2).
- **Postgres:** domain tables enforce **Row-Level Security** keyed on
  `current_setting('app.tenant_id')`, set per-transaction by `database.WithTenant`
  (migration `0004_enable_rls`). Cross-tenant reads are blocked at the database, not in application
  code.
- **ClickHouse:** every table is partitioned and ordered on `(tenant_id, …)`, and every query
  carries the injected `tenant_id` predicate.
- **NATS:** subjects are per-tenant (`qeet-logs.{tenant_id}.{logs|metrics|traces}`); Redis tail
  channels are `tail.{tenant_id}.{service}`.
- **Cross-tenant access** is reserved to the `logs:platform` RBAC scope (Qeet operators only),
  **absent by default** from every tenant API key.

**Any suspected cross-tenant leakage is a SEV-1 incident** regardless of blast radius — see
[../deploy/runbooks/incident-response.md](../deploy/runbooks/incident-response.md).

## Governed AI & data flow

The LLM copilot (`domains/aigateway`) is **opt-in per tenant**, PII-masks prompts before sending,
and writes an audit record of every call (migration `0016`). It reuses the same PII gate philosophy:
sensitive data is masked before it leaves the trust boundary. The larger-model ("Tier-2") routing
is a config choice, not a data-flow change. No third-party analytics touch the log pipeline (SOC 2
P6.1).

## Summary of guarantees

| Guarantee | Mechanism | Where |
|---|---|---|
| No PII at rest | Synchronous pre-storage gate | `ingest/core/pii.rs` |
| Bounded retention | Per-record hard-delete TTL, no shadow copies | `clickhouse/migrations/0001,0009` |
| Right to erasure | `ALTER … DELETE` across all signal tables + receipt | `handler/erasure.go` |
| No cross-tenant access | Identity-derived `tenant_id` + RLS + partition predicate | `middleware/auth.go`, migration `0004` |
| Opt-in, audited AI | Per-tenant opt-in + PII-mask + audit log | `domains/aigateway`, migration `0016` |
