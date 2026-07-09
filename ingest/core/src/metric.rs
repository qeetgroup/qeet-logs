//! The canonical metric record (PRD Module 02/05) and the build pipeline that
//! turns a parsed OTLP/Prometheus data point into a row for the ClickHouse
//! `metrics` table.
//!
//! One `MetricRecord` == one OTLP data point. Scalar points (gauge / sum) carry
//! `value`; histogram / exponential-histogram points carry the count/sum/bucket
//! columns. Field names (incl. the `_retention_days` rename) match the
//! ClickHouse `metrics` columns for JSONEachRow insertion.

use std::collections::BTreeMap;

use serde::{Deserialize, Serialize};
use serde_json::Value;
use ulid::Ulid;

use crate::normalize;

const EPOCH_ZERO: &str = "1970-01-01T00:00:00.000000000Z";

/// A lenient, source-agnostic data point (from OTLP JSON or Prometheus
/// remote_write) before it is finalised into a `MetricRecord`.
#[derive(Debug, Default)]
pub struct MetricInput {
    pub timestamp: Option<Value>,
    pub start_timestamp: Option<Value>,
    pub service: Option<String>,
    pub environment: Option<String>,
    pub metric_name: String,
    pub metric_type: String, // gauge|sum|histogram|exp_histogram|summary
    pub unit: String,
    pub description: String,
    pub temporality: String, // delta|cumulative
    pub is_monotonic: bool,
    pub value: f64,
    pub attributes: BTreeMap<String, String>,
    pub resource: String, // JSON object of OTel resource attributes
    // Histogram.
    pub count: u64,
    pub sum: f64,
    pub min: f64,
    pub max: f64,
    pub bucket_counts: Vec<u64>,
    pub explicit_bounds: Vec<f64>,
    // Exponential histogram.
    pub scale: i32,
    pub zero_count: u64,
    pub positive_offset: i32,
    pub positive_buckets: Vec<u64>,
    pub negative_offset: i32,
    pub negative_buckets: Vec<u64>,
}

/// A finalised metric row matching the ClickHouse `metrics` columns.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct MetricRecord {
    pub id: String,
    pub timestamp: String,
    pub start_timestamp: String,
    pub received_at: String,
    pub tenant_id: String,
    pub service: String,
    pub environment: String,
    pub metric_name: String,
    pub metric_type: String,
    pub unit: String,
    pub description: String,
    pub temporality: String,
    pub is_monotonic: u8,
    pub value: f64,
    pub attributes: BTreeMap<String, String>,
    pub resource: String,
    pub count: u64,
    pub sum: f64,
    pub min: f64,
    pub max: f64,
    pub bucket_counts: Vec<u64>,
    pub explicit_bounds: Vec<f64>,
    pub scale: i32,
    pub zero_count: u64,
    pub positive_offset: i32,
    pub positive_buckets: Vec<u64>,
    pub negative_offset: i32,
    pub negative_buckets: Vec<u64>,
    #[serde(rename = "_retention_days")]
    pub retention_days: u16,
}

/// Finalise a `MetricInput` into a `MetricRecord`. Returns `None` for an
/// unusable point (empty metric name). Non-finite float values (Prometheus
/// staleness markers, NaN/Inf) are coerced to 0 so the JSONEachRow body stays
/// valid for a non-Nullable ClickHouse column.
pub fn build_metric(
    input: MetricInput,
    tenant_id: &str,
    retention_days: u16,
) -> Option<MetricRecord> {
    if input.metric_name.is_empty() {
        return None;
    }
    let timestamp = normalize::timestamp(input.timestamp.as_ref());
    let start_timestamp = match input.start_timestamp.as_ref() {
        Some(v) => normalize::timestamp(Some(v)),
        None => EPOCH_ZERO.to_string(),
    };
    Some(MetricRecord {
        id: Ulid::new().to_string(),
        timestamp,
        start_timestamp,
        received_at: chrono::Utc::now().to_rfc3339_opts(chrono::SecondsFormat::Nanos, true),
        tenant_id: tenant_id.to_string(),
        service: input
            .service
            .filter(|s| !s.is_empty())
            .unwrap_or_else(|| "unknown".to_string()),
        environment: input.environment.unwrap_or_default(),
        metric_name: input.metric_name,
        metric_type: if input.metric_type.is_empty() {
            "gauge".to_string()
        } else {
            input.metric_type
        },
        unit: input.unit,
        description: input.description,
        temporality: input.temporality,
        is_monotonic: input.is_monotonic as u8,
        value: finite(input.value),
        attributes: input.attributes,
        resource: if input.resource.is_empty() {
            "{}".to_string()
        } else {
            input.resource
        },
        count: input.count,
        sum: finite(input.sum),
        min: finite(input.min),
        max: finite(input.max),
        bucket_counts: input.bucket_counts,
        explicit_bounds: input.explicit_bounds.into_iter().map(finite).collect(),
        scale: input.scale,
        zero_count: input.zero_count,
        positive_offset: input.positive_offset,
        positive_buckets: input.positive_buckets,
        negative_offset: input.negative_offset,
        negative_buckets: input.negative_buckets,
        retention_days,
    })
}

#[inline]
fn finite(x: f64) -> f64 {
    if x.is_finite() {
        x
    } else {
        0.0
    }
}
