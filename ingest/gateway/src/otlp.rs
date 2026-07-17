//! Minimal OTLP/HTTP JSON → `IngestInput` mapping (PRD Module 1.1).
//!
//! Parses the subset of the OTLP LogsData JSON we need: resource `service.name`,
//! and per-record time/severity/body/attributes/trace ids. Protobuf is handled
//! in a later milestone.

use serde::Deserialize;
use serde_json::{Map, Value};

use qeet_logs_core::{IngestInput, MetricInput, SpanInput};

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
    /// Flatten to a plain string for a ClickHouse `Map(String,String)` label.
    fn stringify(&self) -> String {
        match self.to_json() {
            Value::String(s) => s,
            Value::Null => String::new(),
            other => other.to_string(),
        }
    }
}

fn attrs_map(kvs: &[KeyValue]) -> std::collections::BTreeMap<String, String> {
    let mut m = std::collections::BTreeMap::new();
    for kv in kvs {
        if let Some(v) = &kv.value {
            m.insert(kv.key.clone(), v.stringify());
        }
    }
    m
}

fn resource_json(res: Option<&Resource>) -> String {
    let mut obj = Map::new();
    if let Some(r) = res {
        for kv in &r.attributes {
            if let Some(v) = &kv.value {
                obj.insert(kv.key.clone(), v.to_json());
            }
        }
    }
    if obj.is_empty() {
        "{}".to_string()
    } else {
        Value::Object(obj).to_string()
    }
}

fn resource_attr(res: Option<&Resource>, key: &str) -> Option<String> {
    res.and_then(|r| r.attributes.iter().find(|kv| kv.key == key))
        .and_then(|kv| kv.value.as_ref())
        .and_then(|v| v.as_string())
}

fn resource_value(res: Option<&Resource>) -> Option<serde_json::Value> {
    let s = resource_json(res);
    if s == "{}" {
        None
    } else {
        serde_json::from_str(&s).ok()
    }
}

/// Deployment + Kubernetes enrichment pulled from OTel resource attributes
/// (Module 04.3 / Gap 7), tolerant of the common semconv spellings.
struct Enrichment {
    git_sha: Option<String>,
    deploy_id: Option<String>,
    pr_number: Option<String>,
    k8s_namespace: Option<String>,
    k8s_pod: Option<String>,
    k8s_node: Option<String>,
}

fn enrichment(res: Option<&Resource>) -> Enrichment {
    let attr = |ks: &[&str]| ks.iter().find_map(|k| resource_attr(res, k));
    Enrichment {
        git_sha: attr(&["vcs.repository.ref.revision", "git.sha", "commit", "service.version"]),
        deploy_id: attr(&["deployment.id", "deploy.id"]),
        pr_number: attr(&["vcs.repository.change.id", "pr.number"]),
        k8s_namespace: attr(&["k8s.namespace.name"]),
        k8s_pod: attr(&["k8s.pod.name"]),
        k8s_node: attr(&["k8s.node.name"]),
    }
}

fn to_f64(v: Option<&Value>) -> f64 {
    match v {
        Some(Value::Number(n)) => n.as_f64().unwrap_or(0.0),
        Some(Value::String(s)) => s.parse().unwrap_or(0.0),
        _ => 0.0,
    }
}

fn to_u64(v: Option<&Value>) -> u64 {
    match v {
        Some(Value::Number(n)) => n.as_u64().or_else(|| n.as_f64().map(|f| f as u64)).unwrap_or(0),
        Some(Value::String(s)) => s.parse().unwrap_or(0),
        _ => 0,
    }
}

fn to_i32(v: Option<&Value>) -> i32 {
    match v {
        Some(Value::Number(n)) => n.as_i64().unwrap_or(0) as i32,
        Some(Value::String(s)) => s.parse().unwrap_or(0),
        _ => 0,
    }
}

/// OTLP `aggregationTemporality` (1=delta, 2=cumulative) → our string form.
fn temporality(v: Option<&Value>) -> String {
    match to_i32(v) {
        1 => "delta",
        2 => "cumulative",
        _ => "",
    }
    .to_string()
}

/// Parse an OTLP/HTTP JSON logs payload into one `IngestInput` per log record.
pub fn parse_logs(body: &[u8]) -> serde_json::Result<Vec<IngestInput>> {
    let data: LogsData = serde_json::from_slice(body)?;
    let mut out = Vec::new();
    for rl in data.resource_logs {
        let res = rl.resource.as_ref();
        let service = resource_attr(res, "service.name");
        let environment = resource_attr(res, "deployment.environment");
        let en = enrichment(res);
        let resource_val = resource_value(res);

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
                    environment: environment.clone(),
                    trace_id: lr.trace_id,
                    span_id: lr.span_id,
                    git_sha: en.git_sha.clone(),
                    deploy_id: en.deploy_id.clone(),
                    pr_number: en.pr_number.clone(),
                    k8s_namespace: en.k8s_namespace.clone(),
                    k8s_pod: en.k8s_pod.clone(),
                    k8s_node: en.k8s_node.clone(),
                    resource: resource_val.clone(),
                    extra,
                    ..Default::default()
                });
            }
        }
    }
    Ok(out)
}

// ── Metrics (PRD Module 02) ───────────────────────────────────────────────────

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct MetricsData {
    #[serde(default)]
    resource_metrics: Vec<ResourceMetrics>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct ResourceMetrics {
    #[serde(default)]
    resource: Option<Resource>,
    #[serde(default)]
    scope_metrics: Vec<ScopeMetrics>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct ScopeMetrics {
    #[serde(default)]
    metrics: Vec<MetricJson>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct MetricJson {
    #[serde(default)]
    name: String,
    #[serde(default)]
    unit: String,
    #[serde(default)]
    description: String,
    #[serde(default)]
    gauge: Option<Points>,
    #[serde(default)]
    sum: Option<SumPoints>,
    #[serde(default)]
    histogram: Option<HistPoints>,
    #[serde(default)]
    exponential_histogram: Option<ExpHistPoints>,
    #[serde(default)]
    summary: Option<Points>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct Points {
    #[serde(default)]
    data_points: Vec<DataPoint>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct SumPoints {
    #[serde(default)]
    data_points: Vec<DataPoint>,
    #[serde(default)]
    is_monotonic: bool,
    #[serde(default)]
    aggregation_temporality: Option<Value>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct HistPoints {
    #[serde(default)]
    data_points: Vec<DataPoint>,
    #[serde(default)]
    aggregation_temporality: Option<Value>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct ExpHistPoints {
    #[serde(default)]
    data_points: Vec<DataPoint>,
    #[serde(default)]
    aggregation_temporality: Option<Value>,
}

/// One data point across all metric kinds — every field optional so a single
/// lenient struct covers Number / Histogram / ExponentialHistogram / Summary.
#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct DataPoint {
    #[serde(default)]
    time_unix_nano: Option<Value>,
    #[serde(default)]
    start_time_unix_nano: Option<Value>,
    #[serde(default)]
    as_double: Option<Value>,
    #[serde(default)]
    as_int: Option<Value>,
    #[serde(default)]
    count: Option<Value>,
    #[serde(default)]
    sum: Option<Value>,
    #[serde(default)]
    min: Option<Value>,
    #[serde(default)]
    max: Option<Value>,
    #[serde(default)]
    bucket_counts: Vec<Value>,
    #[serde(default)]
    explicit_bounds: Vec<Value>,
    #[serde(default)]
    scale: Option<Value>,
    #[serde(default)]
    zero_count: Option<Value>,
    #[serde(default)]
    positive: Option<Buckets>,
    #[serde(default)]
    negative: Option<Buckets>,
    #[serde(default)]
    attributes: Vec<KeyValue>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct Buckets {
    #[serde(default)]
    offset: Option<Value>,
    #[serde(default)]
    bucket_counts: Vec<Value>,
}

/// Parse an OTLP/HTTP JSON metrics payload into one `MetricInput` per data point.
pub fn parse_metrics(body: &[u8]) -> serde_json::Result<Vec<MetricInput>> {
    let data: MetricsData = serde_json::from_slice(body)?;
    let mut out = Vec::new();
    for rm in &data.resource_metrics {
        let res = rm.resource.as_ref();
        let service = resource_attr(res, "service.name");
        let environment = resource_attr(res, "deployment.environment");
        let resource = resource_json(res);

        for sm in &rm.scope_metrics {
            for m in &sm.metrics {
                let base = |dp: &DataPoint, ty: &str| MetricInput {
                    timestamp: dp.time_unix_nano.clone(),
                    start_timestamp: dp.start_time_unix_nano.clone(),
                    service: service.clone(),
                    environment: environment.clone(),
                    metric_name: m.name.clone(),
                    metric_type: ty.to_string(),
                    unit: m.unit.clone(),
                    description: m.description.clone(),
                    attributes: attrs_map(&dp.attributes),
                    resource: resource.clone(),
                    ..Default::default()
                };

                if let Some(g) = &m.gauge {
                    for dp in &g.data_points {
                        let mut mi = base(dp, "gauge");
                        mi.value = value_of(dp);
                        out.push(mi);
                    }
                }
                if let Some(s) = &m.sum {
                    for dp in &s.data_points {
                        let mut mi = base(dp, "sum");
                        mi.value = value_of(dp);
                        mi.is_monotonic = s.is_monotonic;
                        mi.temporality = temporality(s.aggregation_temporality.as_ref());
                        out.push(mi);
                    }
                }
                if let Some(h) = &m.histogram {
                    for dp in &h.data_points {
                        let mut mi = base(dp, "histogram");
                        mi.temporality = temporality(h.aggregation_temporality.as_ref());
                        mi.count = to_u64(dp.count.as_ref());
                        mi.sum = to_f64(dp.sum.as_ref());
                        mi.min = to_f64(dp.min.as_ref());
                        mi.max = to_f64(dp.max.as_ref());
                        mi.bucket_counts = dp.bucket_counts.iter().map(|v| to_u64(Some(v))).collect();
                        mi.explicit_bounds = dp.explicit_bounds.iter().map(|v| to_f64(Some(v))).collect();
                        out.push(mi);
                    }
                }
                if let Some(e) = &m.exponential_histogram {
                    for dp in &e.data_points {
                        let mut mi = base(dp, "exp_histogram");
                        mi.temporality = temporality(e.aggregation_temporality.as_ref());
                        mi.count = to_u64(dp.count.as_ref());
                        mi.sum = to_f64(dp.sum.as_ref());
                        mi.min = to_f64(dp.min.as_ref());
                        mi.max = to_f64(dp.max.as_ref());
                        mi.scale = to_i32(dp.scale.as_ref());
                        mi.zero_count = to_u64(dp.zero_count.as_ref());
                        if let Some(p) = &dp.positive {
                            mi.positive_offset = to_i32(p.offset.as_ref());
                            mi.positive_buckets = p.bucket_counts.iter().map(|v| to_u64(Some(v))).collect();
                        }
                        if let Some(n) = &dp.negative {
                            mi.negative_offset = to_i32(n.offset.as_ref());
                            mi.negative_buckets = n.bucket_counts.iter().map(|v| to_u64(Some(v))).collect();
                        }
                        out.push(mi);
                    }
                }
                if let Some(sum) = &m.summary {
                    for dp in &sum.data_points {
                        let mut mi = base(dp, "summary");
                        mi.count = to_u64(dp.count.as_ref());
                        mi.sum = to_f64(dp.sum.as_ref());
                        out.push(mi);
                    }
                }
            }
        }
    }
    Ok(out)
}

/// Scalar value of a Number data point: `asDouble` preferred, else `asInt`.
fn value_of(dp: &DataPoint) -> f64 {
    if dp.as_double.is_some() {
        to_f64(dp.as_double.as_ref())
    } else {
        to_f64(dp.as_int.as_ref())
    }
}

// ── Traces (PRD Module 03) ────────────────────────────────────────────────────

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct TracesData {
    #[serde(default)]
    resource_spans: Vec<ResourceSpans>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct ResourceSpans {
    #[serde(default)]
    resource: Option<Resource>,
    #[serde(default)]
    scope_spans: Vec<ScopeSpans>,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct ScopeSpans {
    #[serde(default)]
    scope: Option<Scope>,
    #[serde(default)]
    spans: Vec<SpanJson>,
}

#[derive(Deserialize)]
struct Scope {
    #[serde(default)]
    name: String,
    #[serde(default)]
    version: String,
}

#[derive(Deserialize)]
#[serde(rename_all = "camelCase")]
struct SpanJson {
    #[serde(default)]
    trace_id: String,
    #[serde(default)]
    span_id: String,
    #[serde(default)]
    parent_span_id: String,
    #[serde(default)]
    name: String,
    #[serde(default)]
    kind: Option<Value>,
    #[serde(default)]
    start_time_unix_nano: Option<Value>,
    #[serde(default)]
    end_time_unix_nano: Option<Value>,
    #[serde(default)]
    attributes: Vec<KeyValue>,
    #[serde(default)]
    status: Option<SpanStatus>,
    #[serde(default)]
    trace_state: String,
}

#[derive(Deserialize)]
struct SpanStatus {
    #[serde(default)]
    code: Option<Value>,
    #[serde(default)]
    message: String,
}

/// Parse an OTLP/HTTP JSON traces payload into one `SpanInput` per span.
pub fn parse_traces(body: &[u8]) -> serde_json::Result<Vec<SpanInput>> {
    let data: TracesData = serde_json::from_slice(body)?;
    let mut out = Vec::new();
    for rs in &data.resource_spans {
        let res = rs.resource.as_ref();
        let service = resource_attr(res, "service.name");
        let environment = resource_attr(res, "deployment.environment");
        let resource = resource_json(res);
        let en = enrichment(res);

        for ss in &rs.scope_spans {
            let (scope_name, scope_version) = ss
                .scope
                .as_ref()
                .map(|s| (s.name.clone(), s.version.clone()))
                .unwrap_or_default();

            for sp in &ss.spans {
                let start = to_u64(sp.start_time_unix_nano.as_ref());
                let end = to_u64(sp.end_time_unix_nano.as_ref());
                out.push(SpanInput {
                    start_time: sp.start_time_unix_nano.clone(),
                    end_time: sp.end_time_unix_nano.clone(),
                    duration_ns: end.saturating_sub(start),
                    service: service.clone(),
                    environment: environment.clone(),
                    trace_id: sp.trace_id.clone(),
                    span_id: sp.span_id.clone(),
                    parent_span_id: sp.parent_span_id.clone(),
                    name: sp.name.clone(),
                    kind: span_kind(sp.kind.as_ref()),
                    status_code: status_code(sp.status.as_ref()),
                    status_message: sp.status.as_ref().map(|s| s.message.clone()).unwrap_or_default(),
                    attributes: attrs_json(&sp.attributes),
                    resource: resource.clone(),
                    scope_name: scope_name.clone(),
                    scope_version: scope_version.clone(),
                    trace_state: sp.trace_state.clone(),
                    git_sha: en.git_sha.clone().unwrap_or_default(),
                    deploy_id: en.deploy_id.clone().unwrap_or_default(),
                    pr_number: en.pr_number.clone().unwrap_or_default(),
                    k8s_namespace: en.k8s_namespace.clone().unwrap_or_default(),
                    k8s_pod: en.k8s_pod.clone().unwrap_or_default(),
                    k8s_node: en.k8s_node.clone().unwrap_or_default(),
                });
            }
        }
    }
    Ok(out)
}

/// OTLP `SpanKind` int → string.
fn span_kind(v: Option<&Value>) -> String {
    match to_i32(v) {
        2 => "server",
        3 => "client",
        4 => "producer",
        5 => "consumer",
        _ => "internal", // 0 UNSPECIFIED / 1 INTERNAL
    }
    .to_string()
}

/// OTLP `Status.code` int (0=UNSET, 1=OK, 2=ERROR) → string.
fn status_code(s: Option<&SpanStatus>) -> String {
    match s.and_then(|s| to_i32_opt(s.code.as_ref())) {
        Some(1) => "ok",
        Some(2) => "error",
        _ => "unset",
    }
    .to_string()
}

fn to_i32_opt(v: Option<&Value>) -> Option<i32> {
    v.map(|_| to_i32(v))
}

fn attrs_json(kvs: &[KeyValue]) -> String {
    let mut obj = Map::new();
    for kv in kvs {
        if let Some(v) = &kv.value {
            obj.insert(kv.key.clone(), v.to_json());
        }
    }
    if obj.is_empty() {
        "{}".to_string()
    } else {
        Value::Object(obj).to_string()
    }
}
