//! Prometheus self-metrics for the ingest gateway (product-readiness / PRD
//! Module 02 self-observability — the Rust ingest counterpart to the Go query
//! plane's `platform/observability/metrics.go`).
//!
//! Exposes RED-style metrics (Rate, Errors, Duration) plus ingest-volume
//! counters so operators can scrape request throughput, latency, error ratios
//! and how many records/bytes each route accepted:
//!
//!   * `ingest_requests_total{method,route,status}`   — RED rate + errors
//!   * `ingest_request_duration_seconds{method,route}` — RED duration histogram
//!   * `ingest_requests_in_flight`                     — concurrency gauge
//!   * `ingest_records_total{outcome}`                 — accepted/dropped/errored/sampled_out records
//!   * `ingest_bytes_received_total{route}`            — request bytes ingested
//!
//! Like the Go side, the `route` label is the matched axum route PATTERN
//! (`MatchedPath`, e.g. `/v1/ingest`), never the raw URI, so high-cardinality
//! segments can never blow up the series count; requests that never matched a
//! route collapse to `unmatched`.

use std::time::Instant;

use axum::{
    extract::{MatchedPath, Request},
    http::header,
    middleware::Next,
    response::{IntoResponse, Response},
};
use metrics::{counter, gauge, histogram};
use metrics_exporter_prometheus::{PrometheusBuilder, PrometheusHandle};

/// Latency buckets, seconds. Mirrors the Go query plane's histogram: sub-ms
/// health probes up to multi-second slow batches / NATS back-pressure.
const LATENCY_BUCKETS: &[f64] = &[
    0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0,
];

/// Install the global Prometheus recorder and return a handle used to render the
/// `/metrics` scrape body. Called once from `main`; panics if a recorder is
/// already installed (a programming error) or the bucket set is invalid.
pub fn install_recorder() -> PrometheusHandle {
    PrometheusBuilder::new()
        .set_buckets(LATENCY_BUCKETS)
        .expect("valid latency buckets")
        .install_recorder()
        .expect("install prometheus recorder")
}

/// Render the current metrics as a Prometheus text-exposition response, for the
/// `/metrics` route.
pub fn render(handle: &PrometheusHandle) -> Response {
    (
        [(header::CONTENT_TYPE, "text/plain; version=0.0.4; charset=utf-8")],
        handle.render(),
    )
        .into_response()
}

/// Axum middleware recording RED metrics for a request. Wired via
/// `Router::route_layer` (NOT `layer`) so it runs *after* routing — that is the
/// only placement where `MatchedPath` is populated, giving low-cardinality route
/// labels. As a consequence it observes matched routes only; requests rejected
/// before routing (e.g. an oversized body → 413) or that match no route (404)
/// are not counted, which is the documented axum trade-off for this pattern.
pub async fn track_metrics(req: Request, next: Next) -> Response {
    let method = req.method().as_str().to_owned();
    let route = route_label(req.extensions().get::<MatchedPath>().map(|m| m.as_str()));
    let bytes = parse_content_length(
        req.headers()
            .get(header::CONTENT_LENGTH)
            .and_then(|v| v.to_str().ok()),
    );

    // Best-effort byte accounting from the declared Content-Length. Chunked
    // uploads without the header contribute 0; the record counters below track
    // the authoritative accepted/dropped totals regardless.
    if bytes > 0 {
        counter!("ingest_bytes_received_total", "route" => route.clone()).increment(bytes);
    }

    // RAII guard so the gauge is decremented even if the inner service unwinds
    // (mirrors the Go handler's `defer ...Dec()`).
    let _in_flight = InFlightGuard::new();
    let start = Instant::now();
    let resp = next.run(req).await;
    let elapsed = start.elapsed().as_secs_f64();

    let status = resp.status().as_u16().to_string();
    counter!(
        "ingest_requests_total",
        "method" => method.clone(),
        "route" => route.clone(),
        "status" => status,
    )
    .increment(1);
    histogram!(
        "ingest_request_duration_seconds",
        "method" => method,
        "route" => route,
    )
    .record(elapsed);

    resp
}

/// Record a batch outcome tally (`accepted`/`dropped`/`errors`) against
/// `ingest_records_total{outcome}`. Zero-valued outcomes are skipped so a series
/// only appears once it has actually happened.
pub fn record_batch(accepted: u64, dropped: u64, errors: u64) {
    inc_records("accepted", accepted);
    inc_records("dropped", dropped);
    inc_records("errors", errors);
}

/// Increment `ingest_records_total{outcome}` by `n` (no-op when `n == 0`).
pub fn inc_records(outcome: &'static str, n: u64) {
    if n > 0 {
        counter!("ingest_records_total", "outcome" => outcome).increment(n);
    }
}

/// Holds the in-flight gauge up for one request; decrements on drop (including
/// unwind), so a panicking handler cannot leak the concurrency count.
struct InFlightGuard(metrics::Gauge);

impl InFlightGuard {
    fn new() -> Self {
        let g = gauge!("ingest_requests_in_flight");
        g.increment(1.0);
        Self(g)
    }
}

impl Drop for InFlightGuard {
    fn drop(&mut self) {
        self.0.decrement(1.0);
    }
}

// ── Pure label helpers (unit-tested) ──────────────────────────────────────────

/// Map an optional matched route pattern to the metric `route` label, collapsing
/// unmatched requests to a single low-cardinality `unmatched` series.
fn route_label(matched: Option<&str>) -> String {
    matched.unwrap_or("unmatched").to_owned()
}

/// Parse a `Content-Length` header value into a byte count; missing, malformed,
/// or negative values count as 0.
fn parse_content_length(value: Option<&str>) -> u64 {
    value
        .and_then(|v| v.trim().parse::<u64>().ok())
        .unwrap_or(0)
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::{body::Body, http::Request as HttpRequest, routing::post, Router};
    use tower::ServiceExt;

    #[test]
    fn route_label_uses_matched_pattern() {
        assert_eq!(route_label(Some("/v1/ingest")), "/v1/ingest");
        assert_eq!(route_label(Some("/api/v1/write")), "/api/v1/write");
    }

    #[test]
    fn route_label_collapses_unmatched() {
        assert_eq!(route_label(None), "unmatched");
    }

    #[test]
    fn parse_content_length_handles_edge_cases() {
        assert_eq!(parse_content_length(None), 0);
        assert_eq!(parse_content_length(Some("")), 0);
        assert_eq!(parse_content_length(Some("abc")), 0);
        assert_eq!(parse_content_length(Some("-5")), 0);
        assert_eq!(parse_content_length(Some("0")), 0);
        assert_eq!(parse_content_length(Some(" 4096 ")), 4096);
        assert_eq!(parse_content_length(Some("1048576")), 1_048_576);
    }

    // Confirms the middleware sees the matched route PATTERN (not the raw URI)
    // when applied as a router layer — the labeling contract the metrics rely on.
    #[tokio::test]
    async fn matched_path_is_visible_to_layer() {
        async fn echo_route(req: Request, next: Next) -> Response {
            let label = route_label(req.extensions().get::<MatchedPath>().map(|m| m.as_str()));
            let mut resp = next.run(req).await;
            resp.headers_mut()
                .insert("x-test-route", label.parse().unwrap());
            resp
        }

        // axum 0.7 path-param syntax is `:id` (0.8 switched to `{id}`).
        let app = Router::new()
            .route("/v1/ingest/:id", post(|| async { "ok" }))
            .route_layer(axum::middleware::from_fn(echo_route));

        let resp = app
            .oneshot(
                HttpRequest::post("/v1/ingest/abc123")
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();

        assert_eq!(
            resp.headers().get("x-test-route").unwrap(),
            "/v1/ingest/:id"
        );
    }
}
