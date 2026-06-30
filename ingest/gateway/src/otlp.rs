//! Minimal OTLP/HTTP JSON → `IngestInput` mapping (PRD Module 1.1).
//!
//! Parses the subset of the OTLP LogsData JSON we need: resource `service.name`,
//! and per-record time/severity/body/attributes/trace ids. Protobuf is handled
//! in a later milestone.

use serde::Deserialize;
use serde_json::{Map, Value};

use qeet_logs_core::IngestInput;

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct LogsData {
    #[serde(default)]
    resource_logs: Vec<ResourceLogs>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct ResourceLogs {
    #[serde(default)]
    resource: Option<Resource>,
    #[serde(default)]
    scope_logs: Vec<ScopeLogs>,
}

#[derive(Deserialize)]
struct Resource {
    #[serde(default)]
    attributes: Vec<KeyValue>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct ScopeLogs {
    #[serde(default)]
    log_records: Vec<LogRecordJson>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct LogRecordJson {
    #[serde(default)]
    time_unix_nano: Option<Value>,
    #[serde(default)]
    severity_text: Option<String>,
    #[serde(default)]
    body: Option<AnyValue>,
    #[serde(default)]
    attributes: Vec<KeyValue>,
    #[serde(default)]
    trace_id: Option<String>,
    #[serde(default)]
    span_id: Option<String>,
}

#[derive(Deserialize)]
struct KeyValue {
    key: String,
    #[serde(default)]
    value: Option<AnyValue>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct AnyValue {
    #[serde(default)]
    string_value: Option<String>,
    #[serde(default)]
    int_value: Option<Value>,
    #[serde(default)]
    double_value: Option<Value>,
    #[serde(default)]
    bool_value: Option<bool>,
}

impl AnyValue {
    fn to_json(&self) -> Value {
        if let Some(s) = &self.string_value {
            Value::String(s.clone())
        } else if let Some(b) = self.bool_value {
            Value::Bool(b)
        } else if let Some(n) = self.int_value.clone().or_else(|| self.double_value.clone()) {
            n
        } else {
            Value::Null
        }
    }
    fn as_string(&self) -> Option<String> {
        self.string_value.clone()
    }
}

/// Parse an OTLP/HTTP JSON logs payload into one `IngestInput` per log record.
pub fn parse_logs(body: &[u8]) -> serde_json::Result<Vec<IngestInput>> {
    let data: LogsData = serde_json::from_slice(body)?;
    let mut out = Vec::new();
    for rl in data.resource_logs {
        let service = rl
            .resource
            .as_ref()
            .and_then(|r| r.attributes.iter().find(|kv| kv.key == "service.name"))
            .and_then(|kv| kv.value.as_ref())
            .and_then(|v| v.as_string());

        for sl in rl.scope_logs {
            for lr in sl.log_records {
                let mut extra = Map::new();
                for kv in &lr.attributes {
                    if let Some(v) = &kv.value {
                        extra.insert(kv.key.clone(), v.to_json());
                    }
                }
                out.push(IngestInput {
                    timestamp: lr.time_unix_nano,
                    service: service.clone(),
                    level: lr.severity_text,
                    message: lr.body.as_ref().and_then(|b| b.as_string()),
                    environment: None,
                    trace_id: lr.trace_id,
                    span_id: lr.span_id,
                    user_id: None,
                    extra,
                });
            }
        }
    }
    Ok(out)
}
