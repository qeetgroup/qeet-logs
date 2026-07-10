//! Inline prost structs for OTLP/protobuf (`application/x-protobuf`) decoding.
//!
//! We declare only the fields we actually consume; prost silently skips unknown
//! tags on decode, so partial structs work correctly against any compliant encoder.
//! Tags are taken from the stable opentelemetry-proto v1 spec.
//!
//! Pattern mirrors `prom.rs` (Prometheus inline structs) — zero build-script /
//! proto-file vendoring required.

use std::collections::BTreeMap;

use prost::Message;
use serde_json::{Map, Value};

use qeet_logs_core::{IngestInput, MetricInput, SpanInput};

// ── Common proto types ────────────────────────────────────────────────────

#[derive(Clone, PartialEq, Message)]
pub struct KeyValue {
    #[prost(string, tag = "1")]
    pub key: String,
    #[prost(message, optional, tag = "2")]
    pub value: Option<AnyValue>,
}

#[derive(Clone, PartialEq, Message)]
pub struct AnyValue {
    #[prost(oneof = "any_value::Value", tags = "1, 2, 3, 4, 5, 6, 7")]
    pub value: Option<any_value::Value>,
}

pub mod any_value {
    #[derive(Clone, PartialEq, ::prost::Oneof)]
    pub enum Value {
        #[prost(string, tag = "1")]
        StringValue(String),
        #[prost(bool, tag = "2")]
        BoolValue(bool),
        #[prost(int64, tag = "3")]
        IntValue(i64),
        #[prost(double, tag = "4")]
        DoubleValue(f64),
        #[prost(message, tag = "5")]
        ArrayValue(Box<super::ArrayValue>),
        #[prost(message, tag = "6")]
        KvlistValue(Box<super::KeyValueList>),
        #[prost(bytes = "vec", tag = "7")]
        BytesValue(Vec<u8>),
    }
}

#[derive(Clone, PartialEq, Message)]
pub struct ArrayValue {
    #[prost(message, repeated, tag = "1")]
    pub values: Vec<AnyValue>,
}

#[derive(Clone, PartialEq, Message)]
pub struct KeyValueList {
    #[prost(message, repeated, tag = "1")]
    pub values: Vec<KeyValue>,
}

#[derive(Clone, PartialEq, Message)]
pub struct Resource {
    #[prost(message, repeated, tag = "1")]
    pub attributes: Vec<KeyValue>,
}

#[derive(Clone, PartialEq, Message)]
pub struct InstrumentationScope {
    #[prost(string, tag = "1")]
    pub name: String,
    #[prost(string, tag = "2")]
    pub version: String,
}

// ── Logs proto types ──────────────────────────────────────────────────────

#[derive(Clone, PartialEq, Message)]
pub struct ExportLogsServiceRequest {
    #[prost(message, repeated, tag = "1")]
    pub resource_logs: Vec<ResourceLogs>,
}

#[derive(Clone, PartialEq, Message)]
pub struct ResourceLogs {
    #[prost(message, optional, tag = "1")]
    pub resource: Option<Resource>,
    #[prost(message, repeated, tag = "2")]
    pub scope_logs: Vec<ScopeLogs>,
}

#[derive(Clone, PartialEq, Message)]
pub struct ScopeLogs {
    #[prost(message, optional, tag = "1")]
    pub scope: Option<InstrumentationScope>,
    #[prost(message, repeated, tag = "2")]
    pub log_records: Vec<LogRecord>,
}

#[derive(Clone, PartialEq, Message)]
pub struct LogRecord {
    #[prost(fixed64, tag = "1")]
    pub time_unix_nano: u64,
    #[prost(int32, tag = "2")]
    pub severity_number: i32,
    #[prost(string, tag = "3")]
    pub severity_text: String,
    #[prost(message, optional, tag = "5")]
    pub body: Option<AnyValue>,
    #[prost(message, repeated, tag = "6")]
    pub attributes: Vec<KeyValue>,
    #[prost(bytes = "vec", tag = "9")]
    pub trace_id: Vec<u8>,
    #[prost(bytes = "vec", tag = "10")]
    pub span_id: Vec<u8>,
    #[prost(fixed64, tag = "11")]
    pub observed_time_unix_nano: u64,
}

// ── Metrics proto types ───────────────────────────────────────────────────

#[derive(Clone, PartialEq, Message)]
pub struct ExportMetricsServiceRequest {
    #[prost(message, repeated, tag = "1")]
    pub resource_metrics: Vec<ResourceMetrics>,
}

#[derive(Clone, PartialEq, Message)]
pub struct ResourceMetrics {
    #[prost(message, optional, tag = "1")]
    pub resource: Option<Resource>,
    #[prost(message, repeated, tag = "2")]
    pub scope_metrics: Vec<ScopeMetrics>,
}

#[derive(Clone, PartialEq, Message)]
pub struct ScopeMetrics {
    #[prost(message, optional, tag = "1")]
    pub scope: Option<InstrumentationScope>,
    #[prost(message, repeated, tag = "2")]
    pub metrics: Vec<Metric>,
}

#[derive(Clone, PartialEq, Message)]
pub struct Metric {
    #[prost(string, tag = "1")]
    pub name: String,
    #[prost(string, tag = "2")]
    pub description: String,
    #[prost(string, tag = "3")]
    pub unit: String,
    #[prost(oneof = "metric::Data", tags = "5, 7, 9, 10, 11")]
    pub data: Option<metric::Data>,
}

pub mod metric {
    #[derive(Clone, PartialEq, ::prost::Oneof)]
    pub enum Data {
        #[prost(message, tag = "5")]
        Gauge(Box<super::Gauge>),
        #[prost(message, tag = "7")]
        Sum(Box<super::Sum>),
        #[prost(message, tag = "9")]
        Histogram(Box<super::Histogram>),
        #[prost(message, tag = "10")]
        ExponentialHistogram(Box<super::ExponentialHistogram>),
        #[prost(message, tag = "11")]
        Summary(Box<super::Summary>),
    }
}

#[derive(Clone, PartialEq, Message)]
pub struct Gauge {
    #[prost(message, repeated, tag = "1")]
    pub data_points: Vec<NumberDataPoint>,
}

#[derive(Clone, PartialEq, Message)]
pub struct Sum {
    #[prost(message, repeated, tag = "1")]
    pub data_points: Vec<NumberDataPoint>,
    #[prost(int32, tag = "2")]
    pub aggregation_temporality: i32,
    #[prost(bool, tag = "3")]
    pub is_monotonic: bool,
}

#[derive(Clone, PartialEq, Message)]
pub struct Histogram {
    #[prost(message, repeated, tag = "1")]
    pub data_points: Vec<HistogramDataPoint>,
    #[prost(int32, tag = "2")]
    pub aggregation_temporality: i32,
}

#[derive(Clone, PartialEq, Message)]
pub struct ExponentialHistogram {
    #[prost(message, repeated, tag = "1")]
    pub data_points: Vec<ExpHistogramDataPoint>,
    #[prost(int32, tag = "2")]
    pub aggregation_temporality: i32,
}

#[derive(Clone, PartialEq, Message)]
pub struct Summary {
    #[prost(message, repeated, tag = "1")]
    pub data_points: Vec<SummaryDataPoint>,
}

// NumberDataPoint: value is oneof as_double(4) | as_int(6).
// We model them as two optional fields — prost decodes whichever tag is present.
#[derive(Clone, PartialEq, Message)]
pub struct NumberDataPoint {
    #[prost(message, repeated, tag = "7")]
    pub attributes: Vec<KeyValue>,
    #[prost(fixed64, tag = "2")]
    pub start_time_unix_nano: u64,
    #[prost(fixed64, tag = "3")]
    pub time_unix_nano: u64,
    #[prost(double, optional, tag = "4")]
    pub as_double: Option<f64>,
    #[prost(sfixed64, optional, tag = "6")]
    pub as_int: Option<i64>,
}

impl NumberDataPoint {
    pub fn value(&self) -> f64 {
        self.as_double.unwrap_or_else(|| self.as_int.unwrap_or(0) as f64)
    }
}

#[derive(Clone, PartialEq, Message)]
pub struct HistogramDataPoint {
    #[prost(message, repeated, tag = "9")]
    pub attributes: Vec<KeyValue>,
    #[prost(fixed64, tag = "2")]
    pub start_time_unix_nano: u64,
    #[prost(fixed64, tag = "3")]
    pub time_unix_nano: u64,
    #[prost(uint64, tag = "4")]
    pub count: u64,
    #[prost(double, optional, tag = "5")]
    pub sum: Option<f64>,
    #[prost(uint64, repeated, tag = "6")]
    pub bucket_counts: Vec<u64>,
    #[prost(double, repeated, tag = "7")]
    pub explicit_bounds: Vec<f64>,
    #[prost(double, optional, tag = "11")]
    pub min: Option<f64>,
    #[prost(double, optional, tag = "12")]
    pub max: Option<f64>,
}

#[derive(Clone, PartialEq, Message)]
pub struct Buckets {
    #[prost(sint32, tag = "1")]
    pub offset: i32,
    #[prost(uint64, repeated, tag = "2")]
    pub bucket_counts: Vec<u64>,
}

#[derive(Clone, PartialEq, Message)]
pub struct ExpHistogramDataPoint {
    #[prost(message, repeated, tag = "1")]
    pub attributes: Vec<KeyValue>,
    #[prost(fixed64, tag = "2")]
    pub start_time_unix_nano: u64,
    #[prost(fixed64, tag = "3")]
    pub time_unix_nano: u64,
    #[prost(uint64, tag = "4")]
    pub count: u64,
    #[prost(double, optional, tag = "5")]
    pub sum: Option<f64>,
    #[prost(sint32, tag = "6")]
    pub scale: i32,
    #[prost(uint64, tag = "7")]
    pub zero_count: u64,
    #[prost(message, optional, tag = "8")]
    pub positive: Option<Buckets>,
    #[prost(message, optional, tag = "9")]
    pub negative: Option<Buckets>,
    #[prost(double, optional, tag = "11")]
    pub min: Option<f64>,
    #[prost(double, optional, tag = "12")]
    pub max: Option<f64>,
}

#[derive(Clone, PartialEq, Message)]
pub struct SummaryDataPoint {
    #[prost(message, repeated, tag = "7")]
    pub attributes: Vec<KeyValue>,
    #[prost(fixed64, tag = "2")]
    pub start_time_unix_nano: u64,
    #[prost(fixed64, tag = "3")]
    pub time_unix_nano: u64,
    #[prost(uint64, tag = "4")]
    pub count: u64,
    #[prost(double, tag = "5")]
    pub sum: f64,
}

// ── Traces proto types ────────────────────────────────────────────────────

#[derive(Clone, PartialEq, Message)]
pub struct ExportTraceServiceRequest {
    #[prost(message, repeated, tag = "1")]
    pub resource_spans: Vec<ResourceSpans>,
}

#[derive(Clone, PartialEq, Message)]
pub struct ResourceSpans {
    #[prost(message, optional, tag = "1")]
    pub resource: Option<Resource>,
    #[prost(message, repeated, tag = "2")]
    pub scope_spans: Vec<ScopeSpans>,
}

#[derive(Clone, PartialEq, Message)]
pub struct ScopeSpans {
    #[prost(message, optional, tag = "1")]
    pub scope: Option<InstrumentationScope>,
    #[prost(message, repeated, tag = "2")]
    pub spans: Vec<Span>,
}

#[derive(Clone, PartialEq, Message)]
pub struct Span {
    #[prost(bytes = "vec", tag = "1")]
    pub trace_id: Vec<u8>,
    #[prost(bytes = "vec", tag = "2")]
    pub span_id: Vec<u8>,
    #[prost(string, tag = "3")]
    pub trace_state: String,
    #[prost(bytes = "vec", tag = "4")]
    pub parent_span_id: Vec<u8>,
    #[prost(string, tag = "5")]
    pub name: String,
    #[prost(int32, tag = "6")]
    pub kind: i32,
    #[prost(fixed64, tag = "7")]
    pub start_time_unix_nano: u64,
    #[prost(fixed64, tag = "8")]
    pub end_time_unix_nano: u64,
    #[prost(message, repeated, tag = "9")]
    pub attributes: Vec<KeyValue>,
    #[prost(message, optional, tag = "15")]
    pub status: Option<SpanStatus>,
}

#[derive(Clone, PartialEq, Message)]
pub struct SpanStatus {
    #[prost(string, tag = "2")]
    pub message: String,
    #[prost(int32, tag = "3")]
    pub code: i32,
}

// ── Helpers ───────────────────────────────────────────────────────────────

fn any_to_json(v: &AnyValue) -> Value {
    match &v.value {
        Some(any_value::Value::StringValue(s)) => Value::String(s.clone()),
        Some(any_value::Value::BoolValue(b)) => Value::Bool(*b),
        Some(any_value::Value::IntValue(i)) => Value::Number((*i).into()),
        Some(any_value::Value::DoubleValue(d)) => {
            serde_json::Number::from_f64(*d).map(Value::Number).unwrap_or(Value::Null)
        }
        Some(any_value::Value::BytesValue(b)) => Value::String(hex::encode(b)),
        Some(any_value::Value::ArrayValue(arr)) => {
            Value::Array(arr.values.iter().map(any_to_json).collect())
        }
        Some(any_value::Value::KvlistValue(kv)) => {
            let mut m = Map::new();
            for entry in &kv.values {
                m.insert(
                    entry.key.clone(),
                    entry.value.as_ref().map(any_to_json).unwrap_or(Value::Null),
                );
            }
            Value::Object(m)
        }
        None => Value::Null,
    }
}

/// Flatten OTLP attributes to BTreeMap<String, String> (for MetricInput.attributes).
fn attrs_to_btree(attrs: &[KeyValue]) -> BTreeMap<String, String> {
    attrs
        .iter()
        .map(|kv| {
            let v = kv.value.as_ref().map(any_to_json).unwrap_or(Value::Null);
            let s = match &v {
                Value::String(s) => s.clone(),
                Value::Null => String::new(),
                other => other.to_string(),
            };
            (kv.key.clone(), s)
        })
        .collect()
}

/// Flatten OTLP attributes to serde_json Map (for IngestInput.extra).
fn attrs_to_map(attrs: &[KeyValue]) -> Map<String, Value> {
    attrs
        .iter()
        .map(|kv| {
            let v = kv.value.as_ref().map(any_to_json).unwrap_or(Value::Null);
            (kv.key.clone(), v)
        })
        .collect()
}

/// Serialize resource attributes to a JSON string (for MetricInput.resource / SpanInput.resource).
fn attrs_to_json_string(attrs: &[KeyValue]) -> String {
    let m: Map<String, Value> = attrs_to_map(attrs);
    serde_json::to_string(&m).unwrap_or_else(|_| "{}".to_string())
}

fn resource_attr(attrs: &[KeyValue], key: &str) -> String {
    for kv in attrs {
        if kv.key == key {
            if let Some(AnyValue { value: Some(any_value::Value::StringValue(s)) }) = &kv.value {
                return s.clone();
            }
        }
    }
    String::new()
}

fn bytes_to_hex(b: &[u8]) -> String {
    if b.is_empty() { String::new() } else { hex::encode(b) }
}

fn nano_to_rfc3339(ns: u64) -> String {
    let secs = (ns / 1_000_000_000) as i64;
    let nanos = (ns % 1_000_000_000) as u32;
    chrono::DateTime::from_timestamp(secs, nanos)
        .unwrap_or_default()
        .to_rfc3339()
}

fn severity_to_level(number: i32, text: &str) -> String {
    if !text.is_empty() {
        return text.to_lowercase();
    }
    // SeverityNumber 1-4=trace, 5-8=debug, 9-12=info, 13-16=warn, 17-20=error, 21-24=fatal
    match number {
        1..=4 => "trace",
        5..=8 => "debug",
        9..=12 => "info",
        13..=16 => "warn",
        17..=20 => "error",
        21..=24 => "fatal",
        _ => "info",
    }
    .to_string()
}

fn temporality_str(t: i32) -> String {
    match t {
        1 => "delta",
        _ => "cumulative",
    }
    .to_string()
}

fn span_kind_str(k: i32) -> String {
    match k {
        1 => "internal",
        2 => "server",
        3 => "client",
        4 => "producer",
        5 => "consumer",
        _ => "unspecified",
    }
    .to_string()
}

fn status_code_str(c: i32) -> String {
    match c {
        1 => "ok",
        2 => "error",
        _ => "unset",
    }
    .to_string()
}

// ── Public parse functions ────────────────────────────────────────────────

/// Parse an OTLP/protobuf ExportLogsServiceRequest body into IngestInputs.
pub fn parse_logs_proto(body: &[u8]) -> anyhow::Result<Vec<IngestInput>> {
    let req = ExportLogsServiceRequest::decode(body)
        .map_err(|e| anyhow::anyhow!("protobuf decode (logs): {e}"))?;
    let mut out = Vec::new();
    for rl in &req.resource_logs {
        let res_attrs = rl.resource.as_ref().map(|r| r.attributes.as_slice()).unwrap_or(&[]);
        let service = resource_attr(res_attrs, "service.name");
        let environment = resource_attr(res_attrs, "deployment.environment");
        let git_sha = resource_attr(res_attrs, "vcs.repository.ref.revision");
        let deploy_id = resource_attr(res_attrs, "deployment.id");
        let k8s_namespace = resource_attr(res_attrs, "k8s.namespace.name");
        let k8s_pod = resource_attr(res_attrs, "k8s.pod.name");
        let k8s_node = resource_attr(res_attrs, "k8s.node.name");
        let resource_json = serde_json::to_value(attrs_to_map(res_attrs)).ok();

        for sl in &rl.scope_logs {
            for lr in &sl.log_records {
                let ts_ns = if lr.time_unix_nano != 0 {
                    lr.time_unix_nano
                } else {
                    lr.observed_time_unix_nano
                };
                let timestamp = Value::String(nano_to_rfc3339(ts_ns));
                let level = severity_to_level(lr.severity_number, &lr.severity_text);
                let message = lr.body.as_ref().map(|b| match &b.value {
                    Some(any_value::Value::StringValue(s)) => s.clone(),
                    Some(v) => any_to_json(&AnyValue { value: Some(v.clone()) }).to_string(),
                    None => String::new(),
                });
                let mut extra = attrs_to_map(&lr.attributes);
                if let Some(scope) = &sl.scope {
                    if !scope.name.is_empty() {
                        extra.insert("instrumentation_scope".to_string(), Value::String(scope.name.clone()));
                    }
                }
                out.push(IngestInput {
                    timestamp: Some(timestamp),
                    service: opt_str(service.clone()),
                    environment: opt_str(environment.clone()),
                    level: Some(level),
                    message,
                    trace_id: Some(bytes_to_hex(&lr.trace_id)),
                    span_id: Some(bytes_to_hex(&lr.span_id)),
                    git_sha: opt_str(git_sha.clone()),
                    deploy_id: opt_str(deploy_id.clone()),
                    k8s_namespace: opt_str(k8s_namespace.clone()),
                    k8s_pod: opt_str(k8s_pod.clone()),
                    k8s_node: opt_str(k8s_node.clone()),
                    resource: resource_json.clone(),
                    extra,
                    ..Default::default()
                });
            }
        }
    }
    Ok(out)
}

/// Parse an OTLP/protobuf ExportMetricsServiceRequest body into MetricInputs.
pub fn parse_metrics_proto(body: &[u8]) -> anyhow::Result<Vec<MetricInput>> {
    let req = ExportMetricsServiceRequest::decode(body)
        .map_err(|e| anyhow::anyhow!("protobuf decode (metrics): {e}"))?;
    let mut out = Vec::new();
    for rm in &req.resource_metrics {
        let res_attrs = rm.resource.as_ref().map(|r| r.attributes.as_slice()).unwrap_or(&[]);
        let service = resource_attr(res_attrs, "service.name");
        let environment = resource_attr(res_attrs, "deployment.environment");
        let resource_str = attrs_to_json_string(res_attrs);

        for sm in &rm.scope_metrics {
            for metric in &sm.metrics {
                match &metric.data {
                    Some(metric::Data::Gauge(g)) => {
                        for dp in &g.data_points {
                            out.push(MetricInput {
                                metric_name: metric.name.clone(),
                                description: metric.description.clone(),
                                unit: metric.unit.clone(),
                                metric_type: "gauge".to_string(),
                                temporality: "cumulative".to_string(),
                                is_monotonic: false,
                                service: opt_str(service.clone()),
                                environment: opt_str(environment.clone()),
                                timestamp: Some(Value::String(nano_to_rfc3339(dp.time_unix_nano))),
                                start_timestamp: Some(Value::String(nano_to_rfc3339(dp.start_time_unix_nano))),
                                value: dp.value(),
                                attributes: attrs_to_btree(&dp.attributes),
                                resource: resource_str.clone(),
                                ..Default::default()
                            });
                        }
                    }
                    Some(metric::Data::Sum(s)) => {
                        for dp in &s.data_points {
                            out.push(MetricInput {
                                metric_name: metric.name.clone(),
                                description: metric.description.clone(),
                                unit: metric.unit.clone(),
                                metric_type: "sum".to_string(),
                                temporality: temporality_str(s.aggregation_temporality),
                                is_monotonic: s.is_monotonic,
                                service: opt_str(service.clone()),
                                environment: opt_str(environment.clone()),
                                timestamp: Some(Value::String(nano_to_rfc3339(dp.time_unix_nano))),
                                start_timestamp: Some(Value::String(nano_to_rfc3339(dp.start_time_unix_nano))),
                                value: dp.value(),
                                attributes: attrs_to_btree(&dp.attributes),
                                resource: resource_str.clone(),
                                ..Default::default()
                            });
                        }
                    }
                    Some(metric::Data::Histogram(h)) => {
                        for dp in &h.data_points {
                            out.push(MetricInput {
                                metric_name: metric.name.clone(),
                                description: metric.description.clone(),
                                unit: metric.unit.clone(),
                                metric_type: "histogram".to_string(),
                                temporality: temporality_str(h.aggregation_temporality),
                                service: opt_str(service.clone()),
                                environment: opt_str(environment.clone()),
                                timestamp: Some(Value::String(nano_to_rfc3339(dp.time_unix_nano))),
                                start_timestamp: Some(Value::String(nano_to_rfc3339(dp.start_time_unix_nano))),
                                value: dp.sum.unwrap_or(0.0),
                                count: dp.count,
                                sum: dp.sum.unwrap_or(0.0),
                                min: dp.min.unwrap_or(0.0),
                                max: dp.max.unwrap_or(0.0),
                                bucket_counts: dp.bucket_counts.clone(),
                                explicit_bounds: dp.explicit_bounds.clone(),
                                attributes: attrs_to_btree(&dp.attributes),
                                resource: resource_str.clone(),
                                ..Default::default()
                            });
                        }
                    }
                    Some(metric::Data::ExponentialHistogram(eh)) => {
                        for dp in &eh.data_points {
                            out.push(MetricInput {
                                metric_name: metric.name.clone(),
                                description: metric.description.clone(),
                                unit: metric.unit.clone(),
                                metric_type: "exp_histogram".to_string(),
                                temporality: temporality_str(eh.aggregation_temporality),
                                service: opt_str(service.clone()),
                                environment: opt_str(environment.clone()),
                                timestamp: Some(Value::String(nano_to_rfc3339(dp.time_unix_nano))),
                                start_timestamp: Some(Value::String(nano_to_rfc3339(dp.start_time_unix_nano))),
                                value: dp.sum.unwrap_or(0.0),
                                count: dp.count,
                                sum: dp.sum.unwrap_or(0.0),
                                min: dp.min.unwrap_or(0.0),
                                max: dp.max.unwrap_or(0.0),
                                scale: dp.scale,
                                zero_count: dp.zero_count,
                                positive_offset: dp.positive.as_ref().map(|b| b.offset).unwrap_or(0),
                                positive_buckets: dp.positive.as_ref().map(|b| b.bucket_counts.clone()).unwrap_or_default(),
                                negative_offset: dp.negative.as_ref().map(|b| b.offset).unwrap_or(0),
                                negative_buckets: dp.negative.as_ref().map(|b| b.bucket_counts.clone()).unwrap_or_default(),
                                attributes: attrs_to_btree(&dp.attributes),
                                resource: resource_str.clone(),
                                ..Default::default()
                            });
                        }
                    }
                    Some(metric::Data::Summary(s)) => {
                        for dp in &s.data_points {
                            out.push(MetricInput {
                                metric_name: metric.name.clone(),
                                description: metric.description.clone(),
                                unit: metric.unit.clone(),
                                metric_type: "summary".to_string(),
                                temporality: "cumulative".to_string(),
                                service: opt_str(service.clone()),
                                environment: opt_str(environment.clone()),
                                timestamp: Some(Value::String(nano_to_rfc3339(dp.time_unix_nano))),
                                start_timestamp: Some(Value::String(nano_to_rfc3339(dp.start_time_unix_nano))),
                                value: dp.sum,
                                count: dp.count,
                                sum: dp.sum,
                                attributes: attrs_to_btree(&dp.attributes),
                                resource: resource_str.clone(),
                                ..Default::default()
                            });
                        }
                    }
                    None => {}
                }
            }
        }
    }
    Ok(out)
}

/// Parse an OTLP/protobuf ExportTraceServiceRequest body into SpanInputs.
pub fn parse_traces_proto(body: &[u8]) -> anyhow::Result<Vec<SpanInput>> {
    let req = ExportTraceServiceRequest::decode(body)
        .map_err(|e| anyhow::anyhow!("protobuf decode (traces): {e}"))?;
    let mut out = Vec::new();
    for rs in &req.resource_spans {
        let res_attrs = rs.resource.as_ref().map(|r| r.attributes.as_slice()).unwrap_or(&[]);
        let service = resource_attr(res_attrs, "service.name");
        let environment = resource_attr(res_attrs, "deployment.environment");
        let git_sha = resource_attr(res_attrs, "vcs.repository.ref.revision");
        let deploy_id = resource_attr(res_attrs, "deployment.id");
        let k8s_namespace = resource_attr(res_attrs, "k8s.namespace.name");
        let k8s_pod = resource_attr(res_attrs, "k8s.pod.name");
        let k8s_node = resource_attr(res_attrs, "k8s.node.name");
        let resource_str = attrs_to_json_string(res_attrs);

        for ss in &rs.scope_spans {
            let scope_name = ss.scope.as_ref().map(|s| s.name.as_str()).unwrap_or("").to_string();
            let scope_version = ss.scope.as_ref().map(|s| s.version.as_str()).unwrap_or("").to_string();
            for span in &ss.spans {
                let duration_ns = span.end_time_unix_nano.saturating_sub(span.start_time_unix_nano);
                let status_code = span.status.as_ref().map(|s| status_code_str(s.code)).unwrap_or_default();
                let status_message = span.status.as_ref().map(|s| s.message.clone()).unwrap_or_default();
                let attrs_str = serde_json::to_string(&attrs_to_map(&span.attributes)).unwrap_or_else(|_| "{}".to_string());

                out.push(SpanInput {
                    trace_id: bytes_to_hex(&span.trace_id),
                    span_id: bytes_to_hex(&span.span_id),
                    parent_span_id: bytes_to_hex(&span.parent_span_id),
                    trace_state: span.trace_state.clone(),
                    name: span.name.clone(),
                    kind: span_kind_str(span.kind),
                    start_time: Some(Value::String(nano_to_rfc3339(span.start_time_unix_nano))),
                    end_time: Some(Value::String(nano_to_rfc3339(span.end_time_unix_nano))),
                    duration_ns,
                    status_code,
                    status_message,
                    attributes: attrs_str,
                    resource: resource_str.clone(),
                    scope_name: scope_name.clone(),
                    scope_version: scope_version.clone(),
                    service: opt_str(service.clone()),
                    environment: opt_str(environment.clone()),
                    git_sha: git_sha.clone(),
                    deploy_id: deploy_id.clone(),
                    k8s_namespace: k8s_namespace.clone(),
                    k8s_pod: k8s_pod.clone(),
                    k8s_node: k8s_node.clone(),
                    ..Default::default()
                });
            }
        }
    }
    Ok(out)
}

fn opt_str(s: String) -> Option<String> {
    if s.is_empty() { None } else { Some(s) }
}
