//! Request-hardening for the ingest gateway (product-readiness; the Rust
//! counterpart to the Go query plane's `platform/api/middleware/secure.go` +
//! `ratelimit.go`). Three concerns live here:
//!
//!   1. **Security headers** — a conservative header baseline for a JSON API
//!      surface (no HTML is served), matching the Go side exactly.
//!   2. **Body-size cap config** — the ceiling wired into axum's
//!      `DefaultBodyLimit` layer in `main`, which answers 413 on excess.
//!   3. **Per-tenant rate-limit math** — the pure fixed-window decision helpers
//!      used by the (already Redis-backed) limiter in `main`, factored out so
//!      the window key / breach / remaining / reset arithmetic is unit-tested.
//!
//! Header + rate-limit *values* mirror the Go baseline so both planes present an
//! identical contract to clients and scanners.

use axum::{
    extract::Request,
    http::{header, HeaderMap, HeaderName, HeaderValue},
    middleware::Next,
    response::Response,
};

/// Default request-body ceiling: 4 MiB, mirroring the Go plane's
/// `DefaultMaxBodyBytes`. Bounds memory and blunts oversized-payload abuse;
/// individual NDJSON batch lines are tiny, so this is generous headroom.
/// Override with `INGEST_MAX_BODY_BYTES`.
pub const DEFAULT_MAX_BODY_BYTES: usize = 4 << 20;

/// The per-window rate-limit reset/window granularity, in seconds. The gateway
/// uses a fixed one-minute window (matching the Go plane's default).
const WINDOW_SECS: u64 = 60;

// ── Security headers ──────────────────────────────────────────────────────────

/// Axum middleware applying the security-header baseline to every response.
/// HSTS is emitted only when the request arrived over TLS (directly or via a
/// terminating proxy that set `X-Forwarded-Proto: https`), so plaintext local
/// dev is unaffected.
pub async fn security_headers(req: Request, next: Next) -> Response {
    let https = is_https(req.headers());
    let mut resp = next.run(req).await;
    apply_security_headers(resp.headers_mut(), https);
    resp
}

/// Set the conservative security-header baseline for a JSON API (matches the Go
/// `SecureHeaders` middleware). Pure over the header map so it is unit-testable.
pub fn apply_security_headers(h: &mut HeaderMap, https: bool) {
    h.insert(header::X_CONTENT_TYPE_OPTIONS, HeaderValue::from_static("nosniff"));
    h.insert(header::X_FRAME_OPTIONS, HeaderValue::from_static("DENY"));
    h.insert(header::REFERRER_POLICY, HeaderValue::from_static("no-referrer"));
    // JSON API: lock down anything a mistakenly-rendered response could do.
    h.insert(
        header::CONTENT_SECURITY_POLICY,
        HeaderValue::from_static("default-src 'none'; frame-ancestors 'none'"),
    );
    h.insert(
        HeaderName::from_static("cross-origin-resource-policy"),
        HeaderValue::from_static("same-origin"),
    );
    if https {
        h.insert(
            header::STRICT_TRANSPORT_SECURITY,
            HeaderValue::from_static("max-age=31536000; includeSubDomains"),
        );
    }
}

/// Whether the request should be treated as TLS-terminated (direct TLS is
/// handled by the proxy in front of ingest; here we only see the forwarded
/// hint).
pub fn is_https(headers: &HeaderMap) -> bool {
    headers
        .get("x-forwarded-proto")
        .and_then(|v| v.to_str().ok())
        .map(|v| v.eq_ignore_ascii_case("https"))
        .unwrap_or(false)
}

// ── Body-size cap ──────────────────────────────────────────────────────────────

/// Resolve the body-size cap from an optional env value, falling back to
/// [`DEFAULT_MAX_BODY_BYTES`] for missing / malformed / zero values.
pub fn max_body_bytes(env_val: Option<&str>) -> usize {
    match env_val.and_then(|v| v.trim().parse::<usize>().ok()) {
        Some(n) if n > 0 => n,
        _ => DEFAULT_MAX_BODY_BYTES,
    }
}

// ── Per-tenant rate-limit math ─────────────────────────────────────────────────
//
// The limiter itself lives in `main::rate_limit` and is backed by the SHARED
// Redis already used by ingest (INCR + EXPIRE on a per-tenant, per-window key) —
// so the budget is enforced across all gateway replicas, exactly like the Go
// plane's fixed-window limiter. Only the arithmetic is factored out here.

/// Redis key for a tenant's fixed one-minute window containing `unix_secs`.
pub fn ratelimit_window_key(tenant: &str, unix_secs: u64) -> String {
    format!("ratelimit:ingest:{tenant}:{}", unix_secs / WINDOW_SECS)
}

/// Whether an observed request `count` in the current window breaches `limit`.
pub fn ratelimit_exceeded(count: u32, limit: u32) -> bool {
    count > limit
}

/// Remaining budget in the window (saturating at 0), for `X-RateLimit-Remaining`.
pub fn ratelimit_remaining(count: u32, limit: u32) -> u32 {
    limit.saturating_sub(count)
}

/// Seconds until the current window rolls over, for `Retry-After`.
pub fn ratelimit_retry_after(unix_secs: u64) -> u64 {
    WINDOW_SECS - (unix_secs % WINDOW_SECS)
}

/// Unix timestamp at which the current window resets, for `X-RateLimit-Reset`.
pub fn ratelimit_reset(unix_secs: u64) -> u64 {
    (unix_secs / WINDOW_SECS + 1) * WINDOW_SECS
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::{body::Body, extract::DefaultBodyLimit, http::Request as HttpRequest, http::StatusCode, routing::post, Router};
    use tower::ServiceExt;

    #[test]
    fn max_body_bytes_defaults_and_overrides() {
        assert_eq!(max_body_bytes(None), DEFAULT_MAX_BODY_BYTES);
        assert_eq!(max_body_bytes(Some("")), DEFAULT_MAX_BODY_BYTES);
        assert_eq!(max_body_bytes(Some("nope")), DEFAULT_MAX_BODY_BYTES);
        assert_eq!(max_body_bytes(Some("0")), DEFAULT_MAX_BODY_BYTES);
        assert_eq!(max_body_bytes(Some(" 2048 ")), 2048);
        assert_eq!(max_body_bytes(Some("8388608")), 8_388_608);
    }

    #[test]
    fn is_https_reads_forwarded_proto() {
        let mut h = HeaderMap::new();
        assert!(!is_https(&h));
        h.insert("x-forwarded-proto", HeaderValue::from_static("http"));
        assert!(!is_https(&h));
        h.insert("x-forwarded-proto", HeaderValue::from_static("https"));
        assert!(is_https(&h));
        h.insert("x-forwarded-proto", HeaderValue::from_static("HTTPS"));
        assert!(is_https(&h));
    }

    #[test]
    fn security_headers_baseline() {
        let mut h = HeaderMap::new();
        apply_security_headers(&mut h, false);
        assert_eq!(h.get(header::X_CONTENT_TYPE_OPTIONS).unwrap(), "nosniff");
        assert_eq!(h.get(header::X_FRAME_OPTIONS).unwrap(), "DENY");
        assert_eq!(h.get(header::REFERRER_POLICY).unwrap(), "no-referrer");
        assert!(h.get(header::CONTENT_SECURITY_POLICY).is_some());
        assert!(h.get("cross-origin-resource-policy").is_some());
        // No HSTS on plaintext.
        assert!(h.get(header::STRICT_TRANSPORT_SECURITY).is_none());
    }

    #[test]
    fn security_headers_hsts_only_on_tls() {
        let mut h = HeaderMap::new();
        apply_security_headers(&mut h, true);
        assert!(h.get(header::STRICT_TRANSPORT_SECURITY).is_some());
    }

    #[test]
    fn ratelimit_window_key_buckets_by_minute() {
        assert_eq!(ratelimit_window_key("t1", 0), "ratelimit:ingest:t1:0");
        assert_eq!(ratelimit_window_key("t1", 59), "ratelimit:ingest:t1:0");
        assert_eq!(ratelimit_window_key("t1", 60), "ratelimit:ingest:t1:1");
        assert_eq!(ratelimit_window_key("t1", 125), "ratelimit:ingest:t1:2");
    }

    #[test]
    fn ratelimit_decision_math() {
        assert!(!ratelimit_exceeded(1000, 1000));
        assert!(ratelimit_exceeded(1001, 1000));
        assert_eq!(ratelimit_remaining(3, 1000), 997);
        assert_eq!(ratelimit_remaining(1200, 1000), 0);
        // 125s in → 5s into the window → 55s left, resets at 180.
        assert_eq!(ratelimit_retry_after(125), 55);
        assert_eq!(ratelimit_reset(125), 180);
    }

    // Exercises the exact axum layer wired in `main`: an over-limit body is
    // rejected with 413 before the handler runs; an under-limit body passes.
    #[tokio::test]
    async fn body_limit_layer_rejects_oversized() {
        fn app(limit: usize) -> Router {
            Router::new()
                .route("/x", post(|_: axum::body::Bytes| async { "ok" }))
                .layer(DefaultBodyLimit::max(limit))
        }

        let over = app(8)
            .oneshot(HttpRequest::post("/x").body(Body::from(vec![0u8; 64])).unwrap())
            .await
            .unwrap();
        assert_eq!(over.status(), StatusCode::PAYLOAD_TOO_LARGE);

        let under = app(64)
            .oneshot(HttpRequest::post("/x").body(Body::from(vec![0u8; 8])).unwrap())
            .await
            .unwrap();
        assert_eq!(under.status(), StatusCode::OK);
    }

    // Exercises the security-headers middleware end-to-end through the router.
    #[tokio::test]
    async fn security_headers_layer_applies() {
        let app = Router::new()
            .route("/x", post(|| async { "ok" }))
            .layer(axum::middleware::from_fn(security_headers));

        let resp = app
            .oneshot(
                HttpRequest::post("/x")
                    .header("x-forwarded-proto", "https")
                    .body(Body::empty())
                    .unwrap(),
            )
            .await
            .unwrap();

        assert_eq!(resp.headers().get(header::X_CONTENT_TYPE_OPTIONS).unwrap(), "nosniff");
        assert_eq!(resp.headers().get(header::X_FRAME_OPTIONS).unwrap(), "DENY");
        assert!(resp.headers().get(header::STRICT_TRANSPORT_SECURITY).is_some());
    }
}
