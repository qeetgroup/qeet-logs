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

    let mut body_val = Value::Object(input.extra);
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
        resource: "{}".to_string(),
        trace_id: input.trace_id.unwrap_or_default(),
        span_id: input.span_id.unwrap_or_default(),
        user_linkage_key,
        pii_detected: detected.into_iter().collect(),
        ingested_by: ingested_by.to_string(),
        retention_days,
    };
    (record, drop_record)
}

/// Pseudonymous, deterministic GDPR-erasure reference: sha256(tenant_id:user_id).
fn linkage_key(tenant_id: &str, user_id: &str) -> String {
    let mut h = Sha256::new();
    h.update(format!("{tenant_id}:{user_id}").as_bytes());
    hex::encode(h.finalize())
}
