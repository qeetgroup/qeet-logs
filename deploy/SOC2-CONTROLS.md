# Qeet Logs — SOC 2 Control Mapping

Version 1.0 | 2026-07-02

Maps the Trust Services Criteria (TSC) CC categories to Qeet Logs implementation artifacts.

---

## CC1 — Control Environment

| Control | Implementation |
|---|---|
| CC1.1 Demonstrates commitment to integrity and competence | Code of conduct enforced via Qeet ID SSO + role-based access for all operators |
| CC1.2 Board oversight | Responsibility assigned to Qeet Group leadership; no external board yet |
| CC1.3 Organizational structure | Qeet Group monorepo structure; `logs:platform` scope = operator-only; tenant data fully partitioned |

---

## CC2 — Communication and Information

| Control | Implementation |
|---|---|
| CC2.1 Internal communication | Internal API documented in `api/openapi/*.yaml` (split by bounded context); CLAUDE.md for ops knowledge |
| CC2.2 External communication | Public API docs at `docs.qeet.in/logs`; status page planned |
| CC2.3 Reporting deficiencies | Alert rules engine (`domains/alerting/`) delivers to webhook/Qeet Notify on state change |

---

## CC3 — Risk Assessment

| Control | Implementation |
|---|---|
| CC3.1 Risk identification | PII gate (`ingest/core/pii`) runs synchronously before storage; zero PII reaches ClickHouse |
| CC3.2 Risk analysis | NATS JetStream with DLQ (`dlq_events` table) prevents silent data loss on ClickHouse failure |
| CC3.3 Risk mitigation | Rate limiting via Redis (`platform/api/middleware/ratelimit.go`) limits blast radius of compromised keys |

---

## CC4 — Monitoring Activities

| Control | Implementation |
|---|---|
| CC4.1 Ongoing monitoring | `/readyz` endpoint checks Postgres + Redis + ClickHouse + NATS; Helm `readinessProbe` blocks traffic on degraded deps |
| CC4.2 Evaluation and communication | Alerter engine (`cmd/alerter`) polls every 60 s and fires to Qeet Notify/webhook on threshold breach |

---

## CC5 — Control Activities

| Control | Implementation |
|---|---|
| CC5.1 Controls mitigate risk | Tenant isolation via `tenant_id` injected from API key resolution — never from user input (TAD §7.2) |
| CC5.2 Technology controls | Postgres RLS (`qeet_logs_app` role; `SET LOCAL app.tenant_id`) + ClickHouse `tenant_id` column predicate |
| CC5.3 Documented policies | This document; `CLAUDE.md` conventions; OpenAPI spec |

---

## CC6 — Logical and Physical Access

| Control | Implementation |
|---|---|
| CC6.1 Logical access security | `X-Qeet-Api-Key` header auth; SHA-256 key fingerprint in DB; `api_keys` table with scopes column |
| CC6.2 New user registration | API key creation only via authenticated admin call (`POST /v1/admin/api-keys`); requires `logs:admin` |
| CC6.3 Role modification | Scope changes require key revocation + re-issue (no in-place update) |
| CC6.4 User access removal | `DELETE /v1/admin/api-keys/{id}` hard-deletes; subsequent calls with revoked key → 401 |
| CC6.5 Disposal of assets | ClickHouse TTL enforced by `retention_config.retention_days`; Postgres metadata purged by migration |
| CC6.6 Logical access boundaries | `logs:platform` scope = cross-tenant ops; absent by default from all tenant API keys |
| CC6.7 Transmission encryption | TLS terminated at ingress (cert-manager / Let's Encrypt); internal cluster traffic encrypted via mTLS (Cilium/Istio — operator's responsibility) |
| CC6.8 Prevention of unauthorized access | Rate limiting (`redis`); CORS restricted in query API; NATS subjects scoped per tenant |

---

## CC7 — System Operations

| Control | Implementation |
|---|---|
| CC7.1 Detection of configuration changes | Immutable migrations (`migrations/NNNN_*.sql`); Git history is the audit trail |
| CC7.2 Monitoring for unauthorized access | Audit log (`audit_log` table) records actor, action, resource, status, IP, user-agent for all admin calls |
| CC7.3 Evaluate security events | Alert rules with `kind=threshold` can fire on elevated error rates or unauthorized-access patterns |
| CC7.4 Response to security incidents | DLQ replay (`POST /v1/admin/dlq/{id}/replay`) enables controlled data recovery |
| CC7.5 Recovery from identified incidents | NATS JetStream persistence ensures no in-flight messages lost on query-API restart |

---

## CC8 — Change Management

| Control | Implementation |
|---|---|
| CC8.1 Change management process | GitHub Actions CI runs `go build ./... && go vet ./... && go test -race ./...` on every PR; no deploy without green CI |

---

## CC9 — Risk Mitigation

| Control | Implementation |
|---|---|
| CC9.1 Vendor risk | Self-hosted ClickHouse + Postgres + NATS + Redis; no proprietary SaaS in the hot path |
| CC9.2 Business disruption | HPA (Helm `hpa.yaml`) auto-scales query (2–10) and ingest (3–20) on CPU; alerter singleton with restart policy |

---

## Availability (A1)

| Control | Implementation |
|---|---|
| A1.1 Availability commitments | Target 99.9% uptime; `/healthz` + `/readyz` probes enforce liveness |
| A1.2 Performance monitoring | Quota usage API (`GET /v1/admin/quota/usage`) provides per-tenant monthly counts for capacity planning |
| A1.3 Recovery objectives | NATS JetStream replication (N=3 recommended) + Postgres WAL-based replication; RTO < 5 min per runbook |

---

## Confidentiality (C1)

| Control | Implementation |
|---|---|
| C1.1 Identification of confidential information | PII gate (`pii/detectors.go`) identifies and masks/hashes/drops PII fields before storage |
| C1.2 Disposal of confidential information | Retention TTL purges ClickHouse data; Postgres `retention_config` enforces per-tenant windows |

---

## Privacy (P)

| Control | Implementation |
|---|---|
| P1.1 Privacy notice | Communicated via Qeet website; logs product never stores end-user PII beyond configured masking |
| P3.1 Collection limitation | PII gate is synchronous and blocking — no PII reaches ClickHouse under any code path |
| P4.1 Use limitation | `tenant_id` predicate injected server-side; no cross-tenant data leakage possible via API |
| P6.1 Disclosure | Qeet Notify delivery only; no third-party analytics in log pipeline |
| P8.1 Remediation | Retention config (`PUT /v1/admin/retention`) allows customers to trigger immediate TTL reduction |
