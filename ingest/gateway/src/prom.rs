//! Prometheus `remote_write` receiver (PRD Module 02.2).
//!
//! The wire format is a Snappy-block-compressed protobuf `prometheus.WriteRequest`.
//! We decode it into `MetricInput`s (every sample is a gauge point — remote_write
//! carries no metric type). This lets any existing Prometheus server, Grafana
//! Agent/Alloy, or PrometheusRule pipeline ship metrics with zero re-instrumentation.
//!
//! The protobuf messages are declared inline with `prost` derives so we need no
//! `.proto`/`protoc` build step for the small, stable remote_write schema.

use std::collections::BTreeMap;

use prost::Message;
use serde_json::Value;

use qeet_logs_core::MetricInput;

#[derive(Clone, PartialEq, Message)]
pub struct WriteRequest {
    #[prost(message, repeated, tag = "1")]
    pub timeseries: Vec<TimeSeries>,
}

#[derive(Clone, PartialEq, Message)]
pub struct TimeSeries {
    #[prost(message, repeated, tag = "1")]
    pub labels: Vec<Label>,
    #[prost(message, repeated, tag = "2")]
    pub samples: Vec<Sample>,
}

#[derive(Clone, PartialEq, Message)]
pub struct Label {
    #[prost(string, tag = "1")]
    pub name: String,
    #[prost(string, tag = "2")]
    pub value: String,
}

#[derive(Clone, PartialEq, Message)]
pub struct Sample {
    #[prost(double, tag = "1")]
    pub value: f64,
    /// Sample time, milliseconds since epoch.
    #[prost(int64, tag = "2")]
    pub timestamp: i64,
}

/// Decode a Snappy-compressed remote_write body into `MetricInput`s.
pub fn parse_remote_write(body: &[u8]) -> anyhow::Result<Vec<MetricInput>> {
    let raw = snap::raw::Decoder::new()
        .decompress_vec(body)
        .map_err(|e| anyhow::anyhow!("snappy decompress: {e}"))?;
    let req = WriteRequest::decode(&*raw).map_err(|e| anyhow::anyhow!("protobuf decode: {e}"))?;

    let mut out = Vec::new();
    for ts in &req.timeseries {
        // Split labels: __name__ is the metric name; job/service map to service;
        // deployment.environment/env map to environment; the rest are attributes.
        let mut metric_name = String::new();
        let mut service: Option<String> = None;
        let mut job: Option<String> = None;
        let mut environment: Option<String> = None;
        let mut attributes: BTreeMap<String, String> = BTreeMap::new();
        for l in &ts.labels {
            match l.name.as_str() {
                "__name__" => metric_name = l.value.clone(),
                "service" | "service_name" => service = Some(l.value.clone()),
                "job" => job = Some(l.value.clone()),
                "environment" | "env" => environment = Some(l.value.clone()),
                _ => {
                    attributes.insert(l.name.clone(), l.value.clone());
                }
            }
        }
        if metric_name.is_empty() {
            continue;
        }
        let service = service.or(job);

        for s in &ts.samples {
            out.push(MetricInput {
                // remote_write timestamps are milliseconds.
                timestamp: Some(Value::Number(s.timestamp.into())),
                service: service.clone(),
                environment: environment.clone(),
                metric_name: metric_name.clone(),
                metric_type: "gauge".to_string(),
                value: s.value,
                attributes: attributes.clone(),
                ..Default::default()
            });
        }
    }
    Ok(out)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn round_trips_a_remote_write_request() {
        let req = WriteRequest {
            timeseries: vec![TimeSeries {
                labels: vec![
                    Label { name: "__name__".into(), value: "http_requests_total".into() },
                    Label { name: "job".into(), value: "payments".into() },
                    Label { name: "env".into(), value: "prod".into() },
                    Label { name: "code".into(), value: "200".into() },
                ],
                samples: vec![Sample { value: 42.0, timestamp: 1_752_142_800_000 }],
            }],
        };
        let encoded = req.encode_to_vec();
        let compressed = snap::raw::Encoder::new().compress_vec(&encoded).unwrap();

        let inputs = parse_remote_write(&compressed).unwrap();
        assert_eq!(inputs.len(), 1);
        let mi = &inputs[0];
        assert_eq!(mi.metric_name, "http_requests_total");
        assert_eq!(mi.service.as_deref(), Some("payments"));
        assert_eq!(mi.environment.as_deref(), Some("prod"));
        assert_eq!(mi.metric_type, "gauge");
        assert_eq!(mi.value, 42.0);
        assert_eq!(mi.attributes.get("code").map(String::as_str), Some("200"));
        assert!(!mi.attributes.contains_key("__name__"));
    }
}
