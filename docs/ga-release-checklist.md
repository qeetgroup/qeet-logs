# GA release-cut checklist

The steps to cut and ship a Qeet Logs release. Assumes the
[staging bring-up](staging-bringup.md) passed on an environment that mirrors
production. Automation lives in [`../.github/workflows/`](../.github/workflows/)
(`ci.yml`, `security.yml`, `release.yml`) and [`../deploy/helm/`](../deploy/helm/).

## Versioning

- **App version** = the platform release (`Chart.yaml` `appVersion`, the image tag). Semantic: `MAJOR.MINOR.PATCH`.
- **Chart version** (`Chart.yaml` `version`) bumps independently when the chart changes.
- Tag format `v<appVersion>` (e.g. `v1.0.0`) — this is what `release.yml` triggers on.

---

## 1. Pre-flight gates

- [ ] [Staging bring-up](staging-bringup.md) §5–§7 all green on prod-mirror infra
- [ ] `develop` green in CI: **ci.yml** (Go vet/race-test/build · console typecheck/build/biome · Rust · migration up→down→up) and **security.yml** (govulncheck/gosec/CodeQL/bun-audit/Trivy) with no unresolved criticals
- [ ] `make ci` clean locally; `bun run check` clean
- [ ] [PHASE2-GAP-REGISTER.md](../PHASE2-GAP-REGISTER.md) reviewed — gated items (ONNX RCA GA, Qeet Pay billing, Teams inbound) are **not** in the release scope / not advertised
- [ ] Security review of the deployed surface done (see §7)
- [ ] Merge `develop` → `main` (release cuts from `main`)

---

## 2. Version stamp

- [ ] Bump `deploy/helm/qeet-logs/Chart.yaml`: `appVersion` (+ `version` if the chart changed)
- [ ] Update `README.md` status + [AS-BUILT-NOTES](../../qeet-files/qeet-logs/research/AS-BUILT-NOTES.md) if scope changed
- [ ] Write release notes (highlights + the honest "gated / not-yet-GA" list)
- [ ] Commit the version stamp to `main`

---

## 3. Tag → build → publish (automated)

- [ ] Tag and push: `git tag v<x.y.z> && git push origin v<x.y.z>`
- [ ] **release.yml** runs on the tag and, per service in the matrix
      (`query`, `alerter`, `lifecycle`, `mcp`, `ingest`):
  - builds a multi-arch (`linux/amd64,linux/arm64`) image
  - pushes to `ghcr.io/qeetgroup/qeet-logs-<service>:v<x.y.z>`
  - attaches a keyless build-provenance attestation
  - generates a per-image SPDX SBOM
  - then cuts the GitHub release with the SBOMs attached
- [ ] Confirm the workflow succeeded (all 5 image jobs + the release job)

> Prereqs (one-time, org/repo settings): **Actions → Workflow permissions** allow
> GHCR package writes (`packages: write`); code scanning enabled so
> gosec/CodeQL/Trivy SARIF uploads land; `id-token`/`attestations: write` for
> provenance. `mcp` is published but not run as a daemon (stdio server).

---

## 4. Verify artifacts

- [ ] Each image pulls: `docker pull ghcr.io/qeetgroup/qeet-logs-query:v<x.y.z>` (repeat for alerter/lifecycle/mcp/ingest)
- [ ] SBOMs attached to the GitHub release
- [ ] Provenance verifies: `gh attestation verify oci://ghcr.io/qeetgroup/qeet-logs-query:v<x.y.z> --owner qeetgroup`
- [ ] Images are the expected digests + multi-arch (`docker manifest inspect`)

---

## 5. Deploy (Helm)

- [ ] Create/rotate the K8s secret `qeet-logs-secrets` with the real values (see `deploy/helm/qeet-logs/values.yaml` `secrets:` for the full key list):
      `DATABASE_URL`, `REDIS_URL`, `NATS_URL`, `CLICKHOUSE_URL/DATABASE/USER/PASSWORD`, `QEET_ID_ISSUER`, **`QEET_LOGS_SECRETS_KEY`** (required), and optional `QEET_NOTIFY_URL/QEET_NOTIFY_API_KEY`, `ANTHROPIC_API_KEY`, `SLACK_*`
- [ ] Review effective values: `helm template deploy/helm/qeet-logs -f values-prod.yaml | less` — confirm query/alerter/**lifecycle**/ingest workloads, HPA, ingress (`api.logs.qeet.in` + `ingest.logs.qeet.in`, cert-manager TLS), and the `/healthz`+`/readyz` probes + `/metrics` scrape annotation
- [ ] Run migrations against prod DBs (as a Job or one-shot): Postgres `migrate up`, ClickHouse `ch-migrate` (mount `clickhouse/config/storage.xml` + create the cold bucket first if tiering is on)
- [ ] `helm upgrade --install qeet-logs deploy/helm/qeet-logs -f values-prod.yaml --wait`
- [ ] Confirm rollout: all pods Ready, query HPA min replicas up, alerter + lifecycle singletons running

---

## 6. Post-deploy smoke (prod)

- [ ] Repeat staging **§5 (smoke)** and **§6 (security invariants)** against the prod hostnames
- [ ] `/readyz` green through the ingress with TLS; `/metrics` scraped by Prometheus
- [ ] A canary ingest → query roundtrip returns the row for a real (or throwaway) tenant
- [ ] Alert delivery path exercised (fire a test rule → confirm channel delivery)

---

## 7. Security sign-off

- [ ] TLS valid (cert-manager issued, correct SANs); HSTS present
- [ ] `QEET_LOGS_SECRETS_KEY` set in prod (bot tokens encrypted at rest); secret rotation runbook in place
- [ ] Rate limits active (`X-RateLimit-*` on responses)
- [ ] Cross-tenant isolation re-verified in prod (the integration `tenant_isolation` invariants, or a manual probe with two tenants)
- [ ] Pentest / dependency-scan results triaged (govulncheck + Trivy from `security.yml`); no open highs
- [ ] SOC 2 control mapping current ([`../deploy/SOC2-CONTROLS.md`](../deploy/SOC2-CONTROLS.md))

---

## 8. Cut + monitor

- [ ] Announce GA; publish the GitHub release
- [ ] Watch for the first hour: `qeet_logs_http_requests_total{status=~"5.."}` error ratio, `request_duration_seconds` p95 vs [SLO](slo-sli.md), ingest lag, `/readyz` flaps
- [ ] Prefer a gradual rollout (HPA + surge) or a canary namespace before full traffic

---

## 9. Rollback (if needed)

- [ ] App: `helm rollback qeet-logs <previous-revision>` (images are immutable per tag)
- [ ] DB: follow [migration-rollback runbook](../deploy/runbooks/migration-rollback.md) — Postgres `migrate down` is reversible; **ClickHouse DDL is largely forward-only** (a `0009` cold-tier rollback removes the move-TTL but data already on cold stays — plan accordingly)
- [ ] Incident: [incident-response runbook](../deploy/runbooks/incident-response.md); DR: [disaster-recovery runbook](../deploy/runbooks/disaster-recovery.md)

---

## 10. Post-cut

- [ ] Update [AS-BUILT-NOTES](../../qeet-files/qeet-logs/research/AS-BUILT-NOTES.md) with the shipped version
- [ ] Close the register items delivered in this cut; keep the gated list honest
- [ ] Publish/refresh the SDKs in `qeet-sdks/` if the API surface changed (note: the legacy in-repo `sdk/` is not the source of truth)
- [ ] Schedule the next milestone (e.g. ONNX RCA GA once a labeled corpus exists; Qeet Pay billing when that API ships)

---

### What this release does NOT include (state it plainly)

These ship as code but return a clear **501/error** until their dependency exists — do not advertise them as GA:

- Trained **RCA learned-ranker** + **ONNX Tier-1 anomaly** models (need a labeled corpus)
- **Qeet Pay** charging + GST invoicing (compute/preview only)
- **Teams** inbound ChatOps (Slack two-way ships; Teams inbound + signed OAuth `state` pending)
- End-to-end **cold-tier** + **Slack two-way** require live ClickHouse-cluster/MinIO + registered Slack secrets to exercise
