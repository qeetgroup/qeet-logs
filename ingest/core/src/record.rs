//! The canonical log record (TAD §6.2) and the build pipeline that turns a
//! lenient client payload into a normalised, PII-masked record.

use std::collections::BTreeSet;

use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};
use sha2::{Digest, Sha256};
use ulid::Ulid;

use crate::{normalize, pii, pii::MaskingActions};

/// Lenient client payload. Recognised fields are promoted; everything else is
/// flattened into the structured `body` (auto-structured logging, Module 1.3).
#[derive(Debug, Default, Deserialize)]
pub struct IngestInput {
    pub timestamp: Option<Value>,
    pub service: Option<String>,
    pub level: Option<String>,
    pub message: Option<String>,
    pub environment: Option<String>,
    pub trace_id: Option<String>,
    pub span_id: Option<String>,
    /// Used only to derive the pseudonymous `user_linkage_key`; never stored raw.
    pub user_id: Option<String>,
    // Deployment + k8s enrichment (Module 04.3 / Gap 7). Explicit snake_case
    // fields; the OTLP path fills these from resource attributes. Any common
    // alternate spellings arriving in `extra` are also harvested in build_record.
    pub git_sha: Option<String>,
    pub deploy_id: Option<String>,
    pub pr_number: Option<String>,
    pub k8s_namespace: Option<String>,
    pub k8s_pod: Option<String>,
    pub k8s_node: Option<String>,
    /// OTel resource attributes (object); serialized to the `resource` column.
    pub resource: Option<Value>,
    #[serde(flatten)]
    pub extra: Map<String, Value>,
}

/// A normalised, PII-masked log record. Field names (incl. the `_retention_days`
/// rename) match the ClickHouse `logs` columns for JSONEachRow insertion.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LogRecord {
    pub id: String,
    pub timestamp: String,
    pub received_at: String,
    pub tenant_id: String,
    pub service: String,
    pub environment: String,
    pub level: String,
    pub message: String,
    pub body: String,
    pub resource: String,
    pub trace_id: String,
    pub span_id: String,
    pub git_sha: String,
    pub deploy_id: String,
    pub pr_number: String,
    pub k8s_namespace: String,
    pub k8s_pod: String,
    pub k8s_node: String,
    pub user_linkage_key: String,
    pub pii_detected: Vec<String>,
    pub ingested_by: String,
    #[serde(rename = "_retention_days")]
    pub retention_days: u16,
}

/// Build a normalised, PII-masked `LogRecord` from a client payload and the
/// resolved tenant context. Returns `(record, drop_record)`: when `drop_record`
/// is true the gate determined the record must not be stored.
pub fn build_record(
    input: IngestInput,
    tenant_id: &str,
    retention_days: u16,
    actions: &MaskingActions,
    ingested_by: &str,
) -> (LogRecord, bool) {
    let mut detected = BTreeSet::new();
    let mut drop_record = false;

    let timestamp = normalize::timestamp(input.timestamp.as_ref());
    let level = normalize::level(input.level.as_deref());

    let message = pii::gate_string(
        input.message.as_deref().unwrap_or(""),
        actions,
        &mut detected,
        &mut drop_record,
    );

    // Harvest deployment/k8s enrichment: explicit field wins, else pull a common
    // alternate spelling out of the flattened extras (so it isn't duplicated into
    // the body). Runs before the body is assembled from `extra`.
    let mut extra = input.extra;
    let git_sha = or_harvest(input.git_sha, &mut extra, &["git.sha", "vcs.revision", "commit", "commit_sha"]);
    let deploy_id = or_harvest(input.deploy_id, &mut extra, &["deploy.id", "deployment.id", "release"]);
    let pr_number = or_harvest(input.pr_number, &mut extra, &["pr.number", "pull_request", "pr"]);
    let k8s_namespace = or_harvest(input.k8s_namespace, &mut extra, &["k8s.namespace.name", "namespace"]);
    let k8s_pod = or_harvest(input.k8s_pod, &mut extra, &["k8s.pod.name", "pod"]);
    let k8s_node = or_harvest(input.k8s_node, &mut extra, &["k8s.node.name", "node"]);

    let resource = match &input.resource {
        Some(v) => v.to_string(),
        None => "{}".to_string(),
    };

    let mut body_val = Value::Object(extra);
    pii::gate_value(&mut body_val, actions, &mut detected, &mut drop_record);
    let body = serde_json::to_string(&body_val).unwrap_or_else(|_| "{}".to_string());

    let user_linkage_key = input
        .user_id
        .as_deref()
        .filter(|u| !u.is_empty())
        .map(|u| linkage_key(tenant_id, u))
        .unwrap_or_default();

    let record = LogRecord {
        id: Ulid::new().to_string(),
        timestamp,
        received_at: chrono::Utc::now().to_rfc3339_opts(chrono::SecondsFormat::Nanos, true),
        tenant_id: tenant_id.to_string(),
        service: input.service.unwrap_or_else(|| "unknown".to_string()),
        environment: input.environment.unwrap_or_default(),
        level,
        message,
        body,
        resource,
        trace_id: input.trace_id.unwrap_or_default(),
        span_id: input.span_id.unwrap_or_default(),
        git_sha,
        deploy_id,
        pr_number,
        k8s_namespace,
        k8s_pod,
        k8s_node,
        user_linkage_key,
        pii_detected: detected.into_iter().collect(),
        ingested_by: ingested_by.to_string(),
        retention_days,
    };
    (record, drop_record)
}

/// Enrichment resolution: an explicit non-empty value wins; otherwise pull the
/// first matching alternate key out of `extra` (removing it so it isn't also
/// duplicated into the stored body).
fn or_harvest(explicit: Option<String>, extra: &mut Map<String, Value>, keys: &[&str]) -> String {
    if let Some(v) = explicit {
        if !v.is_empty() {
            return v;
        }
    }
    for k in keys {
        if let Some(v) = extra.remove(*k) {
            match v {
                Value::String(s) if !s.is_empty() => return s,
                Value::String(_) | Value::Null => {}
                other => return other.to_string(),
            }
        }
    }
    String::new()
}

/// Pseudonymous, deterministic GDPR-erasure reference: sha256(tenant_id:user_id).
fn linkage_key(tenant_id: &str, user_id: &str) -> String {
    let mut h = Sha256::new();
    h.update(format!("{tenant_id}:{user_id}").as_bytes());
    hex::encode(h.finalize())
}
