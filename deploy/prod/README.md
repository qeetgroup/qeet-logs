# Qeet Logs — production deploy overlay

Production artifacts referenced by the [GA release-cut checklist](../../docs/ga-release-checklist.md) §5
("Deploy (Helm)"). These sit on top of the base chart at [`../helm/qeet-logs`](../helm/qeet-logs).

| File | Role |
|---|---|
| [`values-prod.yaml`](values-prod.yaml) | Helm values **overlay** for production — replicas/HPA/resources, prod ingress hosts + cert-manager TLS, `secrets.existingSecret: qeet-logs-secrets`, `config` (env=production, rateLimitPerMinute). Overrides only what prod needs. |
| [`secrets.example.yaml`](secrets.example.yaml) | Template for the `qeet-logs-secrets` Secret (all keys from the chart's `values.yaml`), plus **ExternalSecret** and **SealedSecret** variants for out-of-band provisioning. **DO NOT COMMIT REAL VALUES.** |

---

## How it maps to the release checklist §5

1. **Provision the secret** (`qeet-logs-secrets`) out-of-band — do **not** apply the raw
   `secrets.example.yaml` with real values. Use one of its variants:
   - **External Secrets** (Variant A) — pull from Vault / AWS SM / GCP SM.
   - **Sealed Secrets** (Variant B) — `kubeseal` the plaintext, commit the ciphertext.
   - **SOPS / manual `kubectl`** — for air-gapped bootstrap only.

   `QEET_LOGS_SECRETS_KEY` is **required** — the query pod will not start without it
   (`openssl rand -base64 32`). The rest of the required keys are `DATABASE_URL`, `REDIS_URL`,
   `NATS_URL`, `CLICKHOUSE_URL/USER/PASSWORD`, `QEET_ID_ISSUER`. Optional feature-gated keys
   (`ANTHROPIC_API_KEY`, `SLACK_*`, `QEET_NOTIFY_*`, `INGEST_API_KEY`) can be omitted — the feature
   just returns 501 / stays off. `CLICKHOUSE_DATABASE` is **not** a secret; it comes from the
   ConfigMap (`config.clickhouseDatabase`).

2. **Review the effective manifests** (checklist §5):

   ```bash
   helm template qeet-logs deploy/helm/qeet-logs -f deploy/prod/values-prod.yaml | less
   ```

   Confirm the query / alerter / **lifecycle** / ingest workloads, the HPAs, the ingress
   (`api.logs.qeet.in` + `ingest.logs.qeet.in`, cert-manager TLS), the `/healthz`+`/readyz` probes,
   and the `/metrics` scrape annotations on the query pod.

3. **Run migrations** against the prod DBs (Postgres `migrate up`, ClickHouse `ch-migrate`) — see the
   checklist.

4. **Install / upgrade:**

   ```bash
   helm upgrade --install qeet-logs deploy/helm/qeet-logs \
     -f deploy/prod/values-prod.yaml --wait
   ```

   Then confirm rollout: all pods Ready, query HPA at its min replicas, alerter + lifecycle singletons
   running.

---

## Image tags / versioning

Images default to `Chart.yaml` `appVersion` (`ghcr.io/qeetgroup/qeet-logs-<service>:v<appVersion>`,
built by `release.yml`). The overlay deliberately does **not** pin `image.tag`, so a GA cut is driven
by bumping `appVersion` + tagging `v<appVersion>` (immutable-per-tag → clean `helm rollback`). To pin
a specific version without a chart bump:

```bash
helm upgrade --install qeet-logs deploy/helm/qeet-logs -f deploy/prod/values-prod.yaml \
  --set query.image.tag=v1.0.1 --set ingest.image.tag=v1.0.1 \
  --set alerter.image.tag=v1.0.1 --set lifecycle.image.tag=v1.0.1 --wait
```

The **OTel collector** image is *not* built by `release.yml`; `collector.enabled` stays `false` unless
you have built and pushed `qeet-logs-collector` yourself.

---

## Observability

After deploy, point Prometheus at the query API `/metrics` and import the RED dashboard — see
[`../grafana/README.md`](../grafana/README.md). The dashboard thresholds encode the SLO targets from
[`../../docs/slo-sli.md`](../../docs/slo-sli.md) (p95 < 2 s, p99 < 8 s, availability ≥ 99.9 %), which is
what checklist §8 ("Cut + monitor") watches in the first hour.
