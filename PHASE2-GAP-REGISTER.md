# Qeet Logs — Phase 2 Gap Register

**Purpose:** The ordered worklist for **PRD Phase 2** ("AI Root Cause Intelligence GA + Business Context", [Product_Requirement_Document.md §10](../qeet-files/qeet-logs/Product_Requirement_Document.md), Q3 2027–Q2 2028). Phase 1 shipped the logs+metrics+traces platform ([PHASE1-GAP-REGISTER.md](PHASE1-GAP-REGISTER.md), G1–G18 done). Phase 2 turns raw signals into **confidence-gated RCA GA**, **business-context correlation**, **incident/war-room collaboration**, and **cost/compliance** surfaces. We close gaps one by one, in dependency order.

**Legend:** ✅ done · 🚧 in progress · ⬜ todo · 🔒 gated (needs infra not yet built) · effort S/M/L/XL

---

## Net new infra Phase 2 requires (tracked separately; several gaps are 🔒 on these)

1. **ONNX Runtime (Tier-1)** — self-hosted anomaly/clustering/correlation models (TAD §3.2). *Not built* (`domains/anomaly/anomaly.go` = statistical floor only).
2. **Tier-2 LLM gateway** — governed, PII-masked, per-tenant-opt-in Anthropic path. Today only a raw call in `handler/nl_query.go`.
3. **Cold-tier storage wiring** — ClickHouse S3 `storage_policy` + lifecycle manager. MinIO container present (`docker-compose.yml`), unconfigured.
4. **Collaboration-app infra** — Slack/Teams OAuth apps + bot; PagerDuty/Opsgenie (Modules 18/19).
5. **Read-only CRM/billing connectors** (Stripe/Chargebee/Salesforce) for Module 16.
6. **Learned-ranker training pipeline + labeled corpus** (fed by Module 20 postmortems) + cross-tenant consent.
7. **External products** — Qeet Pay (GST/billing 27.4/33.5), Qeet Notify (regional-language 27.5).

---

## Wave P2-A — Structural signal foundation (non-AI; unblocks RCA GA)

**P2-G1 · Deployment Intelligence GA** — Module 15 (15.2 P0 ranked culprit, 15.3 P1 health correlation, 15.4 P1 rollback suggestion; 15.1 contract shipped in G8) — ✅ **DONE** — **M**
- *Why first:* deploy proximity is the #1 structural signal the Phase-2 RCA ranker (11.2) consumes (TAD §9.2); it also feeds alert correlation (Module 13) and the timeline (Module 09). Pure Go/ClickHouse over data already landing (`change_events` G8, topology G9, enrichment G5, incidents G11) — **no gated infra**.
- *Built:* `domains/deploy/deploy.go` — `RankCulprits(service, window)` scores every recent change (deploy/flag/config/rollback) by **recency × change-type weight × post-change error-rate delta**; per-change `HealthDelta` compares the error rate in the window before vs after the change (15.3); the top degraded **deploy** gets a one-click `Rollback` suggestion to the preceding deploy on that service (15.4). Handler `GET /v1/deploy/culprits?service=|incident_id=&since=` (`handler/deploy.go`), wired in `cmd/query`. Health-rate/degradation + scoring extracted to pure helpers.
- *Verified:* `go build`/`go vet` clean; `go test ./domains/deploy` green (`deploy_test.go` — health-delta rates + degradation verdict, recency/type/health ranking order). *ClickHouse-backed `RankCulprits`/`deployHealth` queries compile against the real client + `change_events`/`logs` schema but need `make infra-up` to exercise end-to-end (deferred, like the Phase-1 integration suite).*
- *Deps:* G8, G9, G5, G11 (all done).

**P2-G2 · Cold-tier lifecycle + cost-transparent retention** — Modules 06.4 / 28.1–28.3 — ⬜🔒(cold-tier wiring) — **L**
- Hot/warm/cold ClickHouse `storage_policy` (S3/MinIO) + `qlogs-lifecycle` mover; per-tenant tier TTLs; pre-ingestion cost meter + budget guardrails. Extends `domains/retention`.
- *Deps:* MinIO (present), metering (33.1 done).

**P2-G3 · Webhooks (in/out) + change-event connectors** — Modules 30.4 / 31.3 / 31.4 — ⬜ — **M**
- Outbound webhooks on alert/incident state; inbound change-event contract (GitHub Actions/GitLab CI, LaunchDarkly/Flagsmith, Terraform/Argo) feeding Module 15.
- *Deps:* P2-G1.

## Wave P2-B — Business + incident substrate (mostly non-AI)

**P2-G4 · Business Context Correlation Layer** — Module 16 (16.1/16.2 P0, 16.3/16.4 P1) — ⬜ — **L**
- Business-context schema + affected-customer/plan-tier tagging on incidents; revenue-at-risk (qualified range) + SLA exposure; enables timeline 09.5 overlay. Read-only CRM/billing connectors are I/O (Stripe/CSV).
- *Deps:* incidents (G11), RBAC (done).

**P2-G5 · Incident Management & War Room** — Module 18 (18.1/18.2/18.4 P0, 18.3/18.5 P1; +09.4 sessions) — ⬜🔒(Slack/Teams for two-way sync) — **L**
- Incident declaration, live investigation timeline, incident roles, failure-domain isolation (09.3), post-incident handoff, incident-scoped sessions store (09.4). Core is Postgres; two-way chat sync needs collaboration infra.
- *Deps:* G10, G11.

**P2-G6 · Postmortem & Knowledge Graph** — Module 20 (20.1 incl. CERT-In export, 20.2) — ⬜ — **M**
- Structured postmortems; detection-linked remediation commitments → live alert rules. Produces the **labeled corpus** for the RCA ranker (11.2) and 13.3 calibration.
- *Deps:* P2-G5.

## Wave P2-C — Collaboration + compliance surfacing

**P2-G7 · ChatOps two-way** — Module 19 (19.3 P0 app install → 19.1/19.2 P1) — ⬜🔒(collaboration suite) — **M**
**P2-G8 · Compliance/India** — 27.2 CERT-In 6h export · 27.4 GST (Qeet Pay) · 27.5 regional-language (Qeet Notify) · full DPDP erasure (crypto-shred) — ⬜🔒(external products) — **M**
**P2-G9 · Continuous calibration** — Module 13.3 (consumes 18/20 resolution outcomes) — ⬜ — **S**

## Wave P2-D — AI GA (🔒 ONNX Tier-1 + LLM gateway + corpus from P2-G6)

**P2-G10 · RCA GA** — Module 11.2 learned ranker → 11.4 published accuracy → 11.6 feedback loop — ⬜🔒 — **XL**
**P2-G11 · AI Copilot GA** — Module 12.2/12.3 (real Tier-2 LLM gateway, PII-mask, per-tenant opt-in) — ⬜🔒 — **L**
**P2-G12 · Anomaly ONNX tiers + predictive groundwork** — Module 14.1/14.2 (14.3/14.4 = Phase 3) — ⬜🔒 — **L**

## Wave P2-E — Dashboards / cost / DX (mostly non-AI, parallelizable)

**P2-G13 · Grafana-compatible data source** — Module 22.4 (PromQL surface exists, G4) — ⬜ — **M**
**P2-G14 · Auto-generated dashboards** — Module 23.1/23.2 (query-usage telemetry, heuristic) — ⬜ — **M**
**P2-G15 · MCP server** — Module 29.3 (+ Ruby/PHP/.NET SDKs, HTTP auto-instrumentation) — ⬜ — **M**
**P2-G16 · Small deferred APIs** — 2.5 custom/business metrics · 4.5 pipeline dry-run · 7.5 TTFIQ metric · 8.5 export/programmatic · 10.4 deploy-churn freshness · 17.4 routing simulation — ⬜ — **M**
**P2-G17 · Environments/plans/billing** — Module 33.2–33.5 — ⬜🔒(Qeet Pay) — **M**

---

## Build order

```
A: P2-G1 → P2-G2, P2-G3        structural signal + cold-tier + webhooks
B: P2-G4, P2-G5 → P2-G6        business context + war room → postmortem corpus
C: P2-G7, P2-G8, P2-G9         chatops + compliance + calibration
D: (ONNX + LLM gateway) → P2-G10 → P2-G11, P2-G12    AI GA (gated)
E: P2-G13..G17                 dashboards / cost / DX (parallel)
```

**Recommended first: P2-G1** (Deployment Intelligence) — additive, non-gated, and directly unblocks the Phase-2 headline goal (RCA GA).

---

## Progress

**P2-G1 · Deployment Intelligence GA** 🚧 — see below as it lands.
