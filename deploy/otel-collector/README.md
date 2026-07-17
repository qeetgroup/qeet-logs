# Qeet Logs — OpenTelemetry Collector (power-user path)

Qeet Logs commits to **OpenTelemetry as its sole ingestion contract** (PRD Design
Principle 6 / Gap 5): there is no proprietary agent that will ever be deprecated.
The **default** onboarding path is the zero-config SDKs (Module 01) pointing at
the ingest gateway. This Collector is the **escape hatch** for teams with unusual
pipelines and for cluster-wide, zero-YAML log auto-discovery (Module 01.3).

## What's here

| File | Purpose |
|---|---|
| `builder-config.yaml` | OpenTelemetry Collector Builder (ocb) manifest — the **curated, tested component subset** (Module 04.1), not the full Contrib distribution. |
| `config.yaml` | Reference runtime config: OTLP + filelog receivers → memory_limiter/k8sattributes/resourcedetection/batch → OTLP/HTTP to the gateway. |

The Helm chart ships the same config as a ConfigMap + DaemonSet + RBAC under
`deploy/helm/qeet-logs/templates/*-collector.yaml`, gated by `collector.enabled`.

## Build the distribution

```bash
go install go.opentelemetry.io/collector/cmd/builder@latest
builder --config deploy/otel-collector/builder-config.yaml   # → ./bin/qeet-logs-collector
```

## Run locally against a gateway

```bash
export QEET_LOGS_ENDPOINT=http://localhost:8101   # ingest gateway
export QEET_LOGS_API_KEY=qeel_...                 # a logs:ingest key
./bin/qeet-logs-collector --config deploy/otel-collector/config.yaml
```

## Kubernetes (auto-discovery)

```bash
helm upgrade --install qeet-logs deploy/helm/qeet-logs \
  --set collector.enabled=true
# requires INGEST_API_KEY in the qeet-logs-secrets Secret
```

The DaemonSet tails `/var/log/pods/*/*/*.log` on each node (no per-workload
config), the `k8sattributes` processor stamps `k8s.namespace.name`/`k8s.pod.name`/
`k8s.node.name`, and the gateway promotes those to first-class columns
(Module 04.3). SDKs inside the cluster can also send OTLP to the Collector's
`:4317`/`:4318` and ride the same pipelines.
