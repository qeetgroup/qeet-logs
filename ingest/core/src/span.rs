//! The canonical span record (PRD Module 03/05) and the build pipeline that
//! turns a parsed OTLP span into a row for the ClickHouse `traces` table.
//!
//! FLAT span rows (Slack SpanEvent model) — one span == one row, sharing
//! `trace_id` as the join key to logs and metrics (Module 03.4). Field names
//! (incl. the `_retention_days` rename) match the ClickHouse `traces` columns.

use serde::{Deserialize, Serialize};
use serde_json::Value;
use ulid::Ulid;

use crate::normalize;

/// A lenient, parsed span before it is finalised into a `SpanRecord`.
#[derive(Debug, Default)]
pub struct SpanInput {
    pub start_time: Option<Value>, // unix nanos
    pub end_time: Option<Value>,   // unix nanos
    pub duration_ns: u64,
    pub service: Option<String>,
    pub environment: Option<String>,
    pub trace_id: String,
    pub span_id: String,
    pub parent_span_id: String,
    pub name: String,
    pub kind: String,       // internal|server|client|producer|consumer
    pub status_code: String, // unset|ok|error
    pub status_message: String,
    pub attributes: String, // JSON object string
    pub resource: String,   // JSON object string
    pub scope_name: String,
    pub scope_version: String,
    pub trace_state: String,
    pub git_sha: String,
    pub deploy_id: String,
    pub pr_number: String,
    pub k8s_namespace: String,
    pub k8s_pod: String,
    pub k8s_node: String,
}

/// A finalised span row matching the ClickHouse `traces` columns.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SpanRecord {
    pub id: String,
    pub timestamp: String, // == start_time; partition/order/TTL key
    pub received_at: String,
    pub tenant_id: String,
    pub service: String,
    pub environment: String,
    pub trace_id: String,
    pub span_id: String,
    pub parent_span_id: String,
    pub name: String,
    pub kind: String,
    pub start_time: String,
    pub end_time: String,
    pub duration_ns: u64,
    pub status_code: String,
    pub status_message: String,
    pub attributes: String,
    pub resource: String,
    pub scope_name: String,
    pub scope_version: String,
    pub trace_state: String,
    pub git_sha: String,
    pub deploy_id: String,
    pub pr_number: String,
    pub k8s_namespace: String,
    pub k8s_pod: String,
    pub k8s_node: String,
    #[serde(rename = "_retention_days")]
    pub retention_days: u16,
}

impl SpanRecord {
    /// True when this span is an error (feeds tail-based sampling: keep 100%).
    pub fn is_error(&self) -> bool {
        self.status_code == "error"
    }
}

/// Finalise a `SpanInput` into a `SpanRecord`. Returns `None` for a span with
/// no trace/span id (unusable). A missing/broken parent ref is preserved as-is
/// so the trace degrades to a partial timeline rather than being dropped
/// (Module 03 edge case).
pub fn build_span(input: SpanInput, tenant_id: &str, retention_days: u16) -> Option<SpanRecord> {
    if input.trace_id.is_empty() || input.span_id.is_empty() {
        return None;
    }
    let start = normalize::timestamp(input.start_time.as_ref());
    let end = match input.end_time.as_ref() {
        Some(v) => normalize::timestamp(Some(v)),
        None => start.clone(),
    };
    Some(SpanRecord {
        id: Ulid::new().to_string(),
        timestamp: start.clone(),
        received_at: chrono::Utc::now().to_rfc3339_opts(chrono::SecondsFormat::Nanos, true),
        tenant_id: tenant_id.to_string(),
        service: input
            .service
            .filter(|s| !s.is_empty())
            .unwrap_or_else(|| "unknown".to_string()),
        environment: input.environment.unwrap_or_default(),
        trace_id: input.trace_id,
        span_id: input.span_id,
        parent_span_id: input.parent_span_id,
        name: input.name,
        kind: if input.kind.is_empty() {
            "internal".to_string()
        } else {
            input.kind
        },
        start_time: start,
        end_time: end,
        duration_ns: input.duration_ns,
        status_code: if input.status_code.is_empty() {
            "unset".to_string()
        } else {
            input.status_code
        },
        status_message: input.status_message,
        attributes: if input.attributes.is_empty() {
            "{}".to_string()
        } else {
            input.attributes
        },
        resource: if input.resource.is_empty() {
            "{}".to_string()
        } else {
            input.resource
        },
        scope_name: input.scope_name,
        scope_version: input.scope_version,
        trace_state: input.trace_state,
        git_sha: input.git_sha,
        deploy_id: input.deploy_id,
        pr_number: input.pr_number,
        k8s_namespace: input.k8s_namespace,
        k8s_pod: input.k8s_pod,
        k8s_node: input.k8s_node,
        retention_days,
    })
}
