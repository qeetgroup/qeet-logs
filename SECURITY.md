# Security Policy

## Reporting a vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Report privately to **security@qeet.in**. Please include:

- A description of the issue and its impact.
- Steps to reproduce (a proof-of-concept if you have one).
- Affected component (ingest gateway, query API, alerter, console, an SDK) and version
  (`GET /version`).
- Whether the issue involves **cross-tenant data access** — flag this explicitly; it is our highest
  severity.

We aim to acknowledge within **2 business days** and to provide a remediation timeline after triage.
Please give us a reasonable disclosure window before publishing. We credit reporters who wish to be
named.

## Supported versions

Qeet Logs is pre-1.0 and released from `develop`. Security fixes target the latest released build
only; there is no long-term-support branch yet. Confirm the running build with `GET /version`
(embeds the git SHA). This policy will formalize supported-version windows at 1.0 GA.

## Threat model summary

The security posture rests on four load-bearing invariants. Full detail is in
[docs/data-privacy.md](docs/data-privacy.md); the control-to-implementation mapping is in
[deploy/SOC2-CONTROLS.md](deploy/SOC2-CONTROLS.md).

### 1. Multi-tenant isolation (highest priority)
- `tenant_id` is **derived from the authenticated identity, never from request input** — the query
  layer injects the tenant predicate server-side (TAD §7.2).
- **PostgreSQL Row-Level Security** keyed on `current_setting('app.tenant_id')` enforces isolation
  at the database, not in application code (migration `0004_enable_rls`).
- **ClickHouse** tables are partitioned/ordered on `(tenant_id, …)` and every query carries the
  injected predicate; NATS subjects and Redis tail channels are per-tenant.
- Cross-tenant access requires the `logs:platform` RBAC scope (Qeet operators only), absent by
  default from every tenant key. **Any suspected cross-tenant exposure is a SEV-1 incident.**

### 2. API-key handling
- Auth is via the `X-Qeet-Api-Key` header (tenant data plane) or a Qeet ID OIDC Bearer JWT
  (console/admin). Qeet Logs is an OIDC **relying party** — it does not roll its own auth.
- Keys are stored as a **SHA-256 fingerprint**, never in plaintext (`platform/security/hash.go`,
  `api_keys` table). Lookup is by fingerprint.
- Scopes: `logs:{ingest,read,query,export,admin,platform}`. Scope changes require **revoke +
  re-issue** (no in-place scope edit). Revocation (`DELETE /v1/admin/api-keys/{id}`) is immediate —
  the next call with the revoked key returns 401.
- Redis-backed **rate limiting** bounds the blast radius of a compromised key.
- Admin routes require `logs:admin`; ChatOps Slack routes are **not** API-key authed — they verify
  Slack's request signature (HMAC v0 + replay window) and resolve the tenant from the installation
  record.

### 3. PII gate (data minimization)
- A **synchronous, pre-storage** PII gate (`ingest/core/pii.rs`) masks/hashes/drops sensitive fields
  **before** any event is stored. PII never reaches ClickHouse under any code path.
- Reduces breach impact to near-zero for end-user PII: a compromised store, mis-scoped query, or
  leaked backup cannot expose data that was never written.
- The governed LLM copilot is opt-in per tenant, PII-masks prompts, and audits every call
  (migration `0016`).

### 4. Data lifecycle & compliance
- **True hard-delete** retention (per-record `_retention_days` TTL, no shadow copies).
- **DPDP/GDPR erasure** via `POST /v1/admin/erasure` (pseudonymous `user_linkage_key`; async
  `ALTER … DELETE` across logs/metrics/traces with a completion receipt).
- **Audit log** (`audit_log` table) records actor/action/resource/status/IP/user-agent for all
  admin calls.
- **CERT-In** incident export via `GET /v1/admin/postmortems/{id}/cert-in-export`.

### Transport & network
- TLS terminated at the ingress (cert-manager / Let's Encrypt) for `api.logs.qeet.in` and
  `ingest.logs.qeet.in`; cookies scoped to `.logs.qeet.in`, never the parent `.qeet.in` zone.
- Internal cluster mTLS (Cilium/Istio) is the operator's responsibility (SOC 2 CC6.7).
- CORS is restricted on the query API; NATS subjects are per-tenant.

## Out-of-scope / known limitations

- KMS BYOK, third-party pentest, and formal conformance are external-ops hardening items, not yet
  completed.
- Slack ChatOps returns `501` until app secrets are configured; Teams inbound is not yet built.
- ML-based PII detection (beyond regex) is a Phase-2 item.

## Compliance

SOC 2 Trust Services Criteria mapping: [deploy/SOC2-CONTROLS.md](deploy/SOC2-CONTROLS.md). Privacy
invariants: [docs/data-privacy.md](docs/data-privacy.md).
