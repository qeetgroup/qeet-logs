//! qeet-logs ingest gateway.
//!
//! Receives logs over HTTP (`/v1/ingest`, `/v1/ingest/batch`) and OTLP/HTTP JSON
//! (`/v1/logs`), metrics over OTLP/HTTP JSON (`/v1/metrics`) and the Prometheus
//! `remote_write` protocol (`/api/v1/write`), resolves the tenant from its
//! `X-Qeet-Api-Key`, enforces a per-tenant Redis rate limit, normalises + runs
//! the synchronous PII gate on logs, then publishes each record to NATS
//! `qeet-logs.{tenant}.{logs|metrics}` for the writer.
//!
//! Listens on INGEST_PORT (default 8101) and also on 4318 (the OTLP/HTTP
//! convention). Protobuf OTLP is not yet supported (JSON only) in this milestone.

mod legacy;
mod otlp;
mod prom;

use std::collections::HashMap;
use std::sync::Arc;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use axum::{
    body::Bytes,
    extract::State,
    http::{HeaderMap, StatusCode},
    response::{IntoResponse, Response},
    routing::{get, post},
    Json, Router,
};
use deadpool_postgres::{Manager, ManagerConfig, Pool, RecyclingMethod};
use redis::aio::ConnectionManager;
use serde_json::json;
use sha2::{Digest, Sha256};
use std::str::FromStr;
use tokio_postgres::NoTls;

use qeet_logs_core::{
    build_metric, build_record, build_span, IngestInput, LogRecord, MaskingActions, MetricRecord,
    Program, SpanRecord,
};

const AUTH_CACHE_TTL: Duration = Duration::from_secs(30);

#[derive(Clone)]
struct AppState {
    pg: Pool,
    redis: ConnectionManager,
    js: async_nats::jetstream::Context,
    rate_limit_per_min: u32,
    /// Tail-based trace sampling: fraction of non-error/non-slow traces to keep
    /// (1.0 = keep everything, the default). Error + slow spans are always kept.
    trace_sample_rate: f64,
    /// Spans at/above this duration (ns) are always kept ("slow outlier").
    trace_slow_ns: u64,
    auth_cache: Arc<tokio::sync::RwLock<HashMap<String, (Auth, Instant)>>>,
}

#[derive(Clone)]
struct Auth {
    tenant_id: String,
    retention_days: u16,
    masking: MaskingActions,
    /// Compiled per-tenant remap program (PRD Module 04.2); None when unset or
    /// disabled. A program that fails to parse is treated as None (fail-open).
    transform: Option<Arc<Program>>,
}

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(tracing_subscriber::EnvFilter::from_default_env().add_directive("info".parse()?))
        .init();

    let database_url = env("DATABASE_URL", "postgres://qeet-logs:qeet-logs@localhost:5434/qeet-logs");
    let redis_url = env("REDIS_URL", "redis://localhost:6380");
    let nats_url = env("NATS_URL", "nats://localhost:4223");
    let port: u16 = env("INGEST_PORT", "8101").parse().unwrap_or(8101);
    let rate_limit_per_min: u32 = env("INGEST_RATE_LIMIT_PER_MIN", "1000").parse().unwrap_or(1000);
    let trace_sample_rate: f64 = env("TRACE_SAMPLE_RATE", "1.0").parse::<f64>().unwrap_or(1.0).clamp(0.0, 1.0);
    let trace_slow_ns: u64 = env("TRACE_SLOW_MS", "1000")
        .parse::<u64>()
        .unwrap_or(1000)
        .saturating_mul(1_000_000);

    // Postgres pool (tenant / api-key / retention lookups).
    let pg_config = tokio_postgres::Config::from_str(&database_url)?;
    let mgr = Manager::from_config(pg_config, NoTls, ManagerConfig { recycling_method: RecyclingMethod::Fast });
    let pg = Pool::builder(mgr).max_size(8).build()?;

    // Redis (rate limiting).
    let redis = ConnectionManager::new(redis::Client::open(redis_url)?).await?;

    // NATS JetStream (ingestion bus) — ensure the QEET_LOGS stream exists.
    let nc = async_nats::connect(&nats_url).await?;
    let js = async_nats::jetstream::new(nc);
    js.get_or_create_stream(async_nats::jetstream::stream::Config {
        name: "QEET_LOGS".to_string(),
        // `>` captures every signal subject: qeet-logs.{tenant}.{logs|metrics|traces}.
        subjects: vec!["qeet-logs.>".to_string()],
        retention: async_nats::jetstream::stream::RetentionPolicy::WorkQueue,
        max_age: Duration::from_secs(24 * 3600),
        ..Default::default()
    })
    .await?;

    let state = AppState {
        pg,
        redis,
        js,
        rate_limit_per_min,
        trace_sample_rate,
        trace_slow_ns,
        auth_cache: Arc::new(tokio::sync::RwLock::new(HashMap::new())),
    };

    let app = Router::new()
        .route("/healthz", get(|| async { Json(json!({"status": "ok"})) }))
        .route("/readyz", get(readyz))
        .route("/v1/ingest", post(ingest_one))
        .route("/v1/ingest/batch", post(ingest_batch))
        .route("/v1/ingest/gelf", post(ingest_gelf))
        .route("/v1/ingest/syslog", post(ingest_syslog))
        .route("/v1/logs", post(ingest_otlp))
        .route("/v1/metrics", post(ingest_metrics))
        .route("/v1/traces", post(ingest_traces))
        .route("/api/v1/write", post(prom_write))
        .with_state(state);

    // Serve on the primary ingest port and the OTLP/HTTP convention port (4318).
    let primary = tokio::net::TcpListener::bind(("0.0.0.0", port)).await?;
    let otlp_port = tokio::net::TcpListener::bind(("0.0.0.0", 4318)).await?;
    tracing::info!(port, otlp_port = 4318, "qeet-logs ingest gateway starting");

    let app2 = app.clone();
    let s1 = async move { axum::serve(primary, app).await };
    let s2 = async move { axum::serve(otlp_port, app2).await };
    tokio::try_join!(s1, s2)?;
    Ok(())
}

// ── Handlers ────────────────────────────────────────────────────────────────

async fn readyz(State(s): State<AppState>) -> Response {
    let mut redis = s.redis.clone();
    let redis_ok = redis::cmd("PING").query_async::<String>(&mut redis).await.is_ok();
    let pg_ok = s.pg.get().await.is_ok();
    if redis_ok && pg_ok {
        (StatusCode::OK, Json(json!({"status": "ready"}))).into_response()
    } else {
        (StatusCode::SERVICE_UNAVAILABLE,
         Json(json!({"status": "degraded", "redis": redis_ok, "postgres": pg_ok}))).into_response()
    }
}

async fn ingest_one(State(s): State<AppState>, headers: HeaderMap, body: Bytes) -> Response {
    let auth = match authorize(&s, &headers).await {
        Ok(a) => a,
        Err(r) => return r,
    };
    if let Err(r) = rate_limit(&s, &auth.tenant_id).await {
        return r;
    }
    let input = match parse_and_transform(&body, &auth) {
        Ok(v) => v,
        Err(e) => return err(StatusCode::BAD_REQUEST, &format!("invalid JSON: {e}")),
    };
    let (rec, dropped) = build_record(input, &auth.tenant_id, auth.retention_days, &auth.masking, "http");
    if dropped {
        return (StatusCode::ACCEPTED, Json(json!({"accepted": 0, "dropped": 1}))).into_response();
    }
    match publish(&s, &rec).await {
        Ok(()) => (StatusCode::ACCEPTED, Json(json!({"accepted": 1, "dropped": 0}))).into_response(),
        Err(e) => err(StatusCode::BAD_GATEWAY, &format!("publish failed: {e}")),
    }
}

async fn ingest_batch(State(s): State<AppState>, headers: HeaderMap, body: Bytes) -> Response {
    let auth = match authorize(&s, &headers).await {
        Ok(a) => a,
        Err(r) => return r,
    };
    if let Err(r) = rate_limit(&s, &auth.tenant_id).await {
        return r;
    }
    let text = String::from_utf8_lossy(&body);
    let (mut accepted, mut dropped, mut errors) = (0u64, 0u64, 0u64);
    for line in text.lines() {
        let line = line.trim();
        if line.is_empty() {
            continue;
        }
        let input = match parse_and_transform(line.as_bytes(), &auth) {
            Ok(v) => v,
            Err(_) => {
                errors += 1;
                continue;
            }
        };
        let (rec, drop) = build_record(input, &auth.tenant_id, auth.retention_days, &auth.masking, "http");
        if drop {
            dropped += 1;
            continue;
        }
        match publish(&s, &rec).await {
            Ok(()) => accepted += 1,
            Err(_) => errors += 1,
        }
    }
    (StatusCode::ACCEPTED, Json(json!({"accepted": accepted, "dropped": dropped, "errors": errors}))).into_response()
}

async fn ingest_otlp(State(s): State<AppState>, headers: HeaderMap, body: Bytes) -> Response {
    let auth = match authorize(&s, &headers).await {
        Ok(a) => a,
        Err(r) => return r,
    };
    if let Err(r) = rate_limit(&s, &auth.tenant_id).await {
        return r;
    }
    let ct = headers
        .get(axum::http::header::CONTENT_TYPE)
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");
    if ct.contains("protobuf") {
        return err(StatusCode::UNSUPPORTED_MEDIA_TYPE, "protobuf OTLP not yet supported (send application/json)");
    }
    let inputs = match otlp::parse_logs(&body) {
        Ok(v) => v,
        Err(e) => return err(StatusCode::BAD_REQUEST, &format!("invalid OTLP JSON: {e}")),
    };
    let (mut accepted, mut dropped, mut errors) = (0u64, 0u64, 0u64);
    for input in inputs {
        let (rec, drop) = build_record(input, &auth.tenant_id, auth.retention_days, &auth.masking, "otlp");
        if drop {
            dropped += 1;
            continue;
        }
        match publish(&s, &rec).await {
            Ok(()) => accepted += 1,
            Err(_) => errors += 1,
        }
    }
    (StatusCode::ACCEPTED, Json(json!({"accepted": accepted, "dropped": dropped, "errors": errors}))).into_response()
}

async fn ingest_gelf(State(s): State<AppState>, headers: HeaderMap, body: Bytes) -> Response {
    let auth = match authorize(&s, &headers).await {
        Ok(a) => a,
        Err(r) => return r,
    };
    if let Err(r) = rate_limit(&s, &auth.tenant_id).await {
        return r;
    }
    ingest_log_inputs(&s, &auth, legacy::parse_gelf(&body), "gelf").await
}

async fn ingest_syslog(State(s): State<AppState>, headers: HeaderMap, body: Bytes) -> Response {
    let auth = match authorize(&s, &headers).await {
        Ok(a) => a,
        Err(r) => return r,
    };
    if let Err(r) = rate_limit(&s, &auth.tenant_id).await {
        return r;
    }
    ingest_log_inputs(&s, &auth, legacy::parse_syslog(&body), "syslog").await
}

/// Shared log-record path: build (normalise + PII gate) then publish each input.
async fn ingest_log_inputs(
    s: &AppState,
    auth: &Auth,
    inputs: Vec<IngestInput>,
    source: &str,
) -> Response {
    let (mut accepted, mut dropped, mut errors) = (0u64, 0u64, 0u64);
    for input in inputs {
        let (rec, drop) = build_record(input, &auth.tenant_id, auth.retention_days, &auth.masking, source);
        if drop {
            dropped += 1;
            continue;
        }
        match publish(s, &rec).await {
            Ok(()) => accepted += 1,
            Err(_) => errors += 1,
        }
    }
    (StatusCode::ACCEPTED, Json(json!({"accepted": accepted, "dropped": dropped, "errors": errors}))).into_response()
}

async fn ingest_metrics(State(s): State<AppState>, headers: HeaderMap, body: Bytes) -> Response {
    let auth = match authorize(&s, &headers).await {
        Ok(a) => a,
        Err(r) => return r,
    };
    if let Err(r) = rate_limit(&s, &auth.tenant_id).await {
        return r;
    }
    let ct = headers
        .get(axum::http::header::CONTENT_TYPE)
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");
    if ct.contains("protobuf") {
        return err(StatusCode::UNSUPPORTED_MEDIA_TYPE, "protobuf OTLP not yet supported (send application/json)");
    }
    let inputs = match otlp::parse_metrics(&body) {
        Ok(v) => v,
        Err(e) => return err(StatusCode::BAD_REQUEST, &format!("invalid OTLP metrics JSON: {e}")),
    };
    ingest_metric_inputs(&s, &auth, inputs).await
}

/// Prometheus `remote_write`: Snappy-compressed protobuf `WriteRequest`.
async fn prom_write(State(s): State<AppState>, headers: HeaderMap, body: Bytes) -> Response {
    let auth = match authorize(&s, &headers).await {
        Ok(a) => a,
        Err(r) => return r,
    };
    if let Err(r) = rate_limit(&s, &auth.tenant_id).await {
        return r;
    }
    let inputs = match prom::parse_remote_write(&body) {
        Ok(v) => v,
        Err(e) => return err(StatusCode::BAD_REQUEST, &format!("invalid remote_write body: {e}")),
    };
    ingest_metric_inputs(&s, &auth, inputs).await
}

async fn ingest_metric_inputs(
    s: &AppState,
    auth: &Auth,
    inputs: Vec<qeet_logs_core::MetricInput>,
) -> Response {
    let (mut accepted, mut errors) = (0u64, 0u64);
    for input in inputs {
        let Some(rec) = build_metric(input, &auth.tenant_id, auth.retention_days) else {
            errors += 1;
            continue;
        };
        match publish_metric(s, &rec).await {
            Ok(()) => accepted += 1,
            Err(_) => errors += 1,
        }
    }
    (StatusCode::ACCEPTED, Json(json!({"accepted": accepted, "errors": errors}))).into_response()
}

async fn ingest_traces(State(s): State<AppState>, headers: HeaderMap, body: Bytes) -> Response {
    let auth = match authorize(&s, &headers).await {
        Ok(a) => a,
        Err(r) => return r,
    };
    if let Err(r) = rate_limit(&s, &auth.tenant_id).await {
        return r;
    }
    let ct = headers
        .get(axum::http::header::CONTENT_TYPE)
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");
    if ct.contains("protobuf") {
        return err(StatusCode::UNSUPPORTED_MEDIA_TYPE, "protobuf OTLP not yet supported (send application/json)");
    }
    let inputs = match otlp::parse_traces(&body) {
        Ok(v) => v,
        Err(e) => return err(StatusCode::BAD_REQUEST, &format!("invalid OTLP traces JSON: {e}")),
    };
    let (mut accepted, mut sampled_out, mut errors) = (0u64, 0u64, 0u64);
    for input in inputs {
        let Some(rec) = build_span(input, &auth.tenant_id, auth.retention_days) else {
            errors += 1;
            continue;
        };
        if !keep_span(&rec, s.trace_sample_rate, s.trace_slow_ns) {
            sampled_out += 1;
            continue;
        }
        match publish_span(&s, &rec).await {
            Ok(()) => accepted += 1,
            Err(_) => errors += 1,
        }
    }
    (StatusCode::ACCEPTED, Json(json!({"accepted": accepted, "sampled_out": sampled_out, "errors": errors}))).into_response()
}

/// Tail-based-style sampling decision. Error and slow-outlier spans are always
/// kept; everything else is kept iff a deterministic hash of the `trace_id`
/// falls under the sample rate — so the decision is consistent for every span
/// of the same trace (Module 03.3: a trace is never partially sampled).
fn keep_span(rec: &SpanRecord, rate: f64, slow_ns: u64) -> bool {
    if rate >= 1.0 || rec.is_error() || rec.duration_ns >= slow_ns {
        return true;
    }
    if rate <= 0.0 {
        return false;
    }
    let h = sha256_hex(&rec.trace_id);
    // First 8 hex chars → u32 → [0,1).
    let bucket = u32::from_str_radix(&h[..8], 16).unwrap_or(0);
    (bucket as f64) / (u32::MAX as f64) < rate
}

// ── Auth, rate limit, publish ─────────────────────────────────────────────────

async fn authorize(s: &AppState, headers: &HeaderMap) -> Result<Auth, Response> {
    let key = headers
        .get("X-Qeet-Api-Key")
        .and_then(|v| v.to_str().ok())
        .unwrap_or("");
    if key.is_empty() {
        return Err(err(StatusCode::UNAUTHORIZED, "missing X-Qeet-Api-Key"));
    }
    let hash = sha256_hex(key);

    if let Some((auth, at)) = s.auth_cache.read().await.get(&hash).cloned() {
        if at.elapsed() < AUTH_CACHE_TTL {
            return Ok(auth);
        }
    }

    let client = s
        .pg
        .get()
        .await
        .map_err(|e| err(StatusCode::INTERNAL_SERVER_ERROR, &format!("db: {e}")))?;
    let row = client
        .query_opt(
            "SELECT t.id::text AS tenant_id, k.scopes AS scopes, \
                    COALESCE(rc.retention_days, t.retention_days) AS retention_days, \
                    COALESCE(rc.masking_actions, '{}'::jsonb)::text AS masking, \
                    COALESCE(tr.program, '') AS transform \
             FROM api_keys k \
             JOIN tenants t ON t.id = k.tenant_id \
             LEFT JOIN retention_config rc ON rc.tenant_id = t.id \
             LEFT JOIN transforms tr ON tr.tenant_id = t.id AND tr.enabled \
             WHERE k.key_hash = $1 AND k.revoked_at IS NULL \
               AND (k.expires_at IS NULL OR k.expires_at > now()) \
             LIMIT 1",
            &[&hash],
        )
        .await
        .map_err(|e| err(StatusCode::INTERNAL_SERVER_ERROR, &format!("db query: {e}")))?;

    let row = row.ok_or_else(|| err(StatusCode::UNAUTHORIZED, "invalid api key"))?;
    let scopes: Vec<String> = row.get("scopes");
    if !scopes.iter().any(|sc| sc == "logs:ingest") {
        return Err(err(StatusCode::FORBIDDEN, "api key lacks logs:ingest scope"));
    }
    let retention_days = row.get::<_, i32>("retention_days").clamp(1, u16::MAX as i32) as u16;
    let masking: MaskingActions =
        serde_json::from_str(&row.get::<_, String>("masking")).unwrap_or_default();
    // Compile the per-tenant remap program (fail-open: a bad program is ignored
    // so it can never block ingestion — Module 04.2 sandbox discipline).
    let transform = {
        let src: String = row.get("transform");
        if src.trim().is_empty() {
            None
        } else {
            match Program::parse(&src) {
                Ok(p) if !p.is_empty() => Some(Arc::new(p)),
                Ok(_) => None,
                Err(e) => {
                    tracing::warn!(error = %e, "ignoring invalid tenant transform program");
                    None
                }
            }
        }
    };
    let auth = Auth {
        tenant_id: row.get::<_, String>("tenant_id"),
        retention_days,
        masking,
        transform,
    };

    s.auth_cache.write().await.insert(hash, (auth.clone(), Instant::now()));
    Ok(auth)
}

async fn rate_limit(s: &AppState, tenant: &str) -> Result<(), Response> {
    let now = SystemTime::now().duration_since(UNIX_EPOCH).unwrap().as_secs();
    let key = format!("ratelimit:ingest:{tenant}:{}", now / 60);
    let mut redis = s.redis.clone();
    let count: u32 = match redis::cmd("INCR").arg(&key).query_async(&mut redis).await {
        Ok(c) => c,
        Err(_) => return Ok(()), // fail-open on rate-limiter outage
    };
    if count == 1 {
        let _: Result<(), _> = redis::cmd("EXPIRE").arg(&key).arg(60).query_async(&mut redis).await;
    }
    if count > s.rate_limit_per_min {
        let retry_after = 60 - (now % 60);
        let mut resp = err(StatusCode::TOO_MANY_REQUESTS, "ingestion rate limit exceeded");
        resp.headers_mut()
            .insert("Retry-After", retry_after.to_string().parse().unwrap());
        return Err(resp);
    }
    Ok(())
}

/// Parse a JSON log payload and apply the tenant's remap program (Module 04.2)
/// before deserialising into the canonical `IngestInput`. Remap is applied to
/// the raw event object so it can touch any field, including nested body keys.
fn parse_and_transform(body: &[u8], auth: &Auth) -> Result<IngestInput, serde_json::Error> {
    let mut v: serde_json::Value = serde_json::from_slice(body)?;
    if let Some(prog) = &auth.transform {
        prog.apply(&mut v);
    }
    serde_json::from_value(v)
}

async fn publish(s: &AppState, rec: &LogRecord) -> anyhow::Result<()> {
    let subject = format!("qeet-logs.{}.logs", rec.tenant_id);
    let payload = serde_json::to_vec(rec)?;
    let ack = s.js.publish(subject, payload.into()).await?;
    ack.await?;
    Ok(())
}

async fn publish_metric(s: &AppState, rec: &MetricRecord) -> anyhow::Result<()> {
    let subject = format!("qeet-logs.{}.metrics", rec.tenant_id);
    let payload = serde_json::to_vec(rec)?;
    let ack = s.js.publish(subject, payload.into()).await?;
    ack.await?;
    Ok(())
}

async fn publish_span(s: &AppState, rec: &SpanRecord) -> anyhow::Result<()> {
    let subject = format!("qeet-logs.{}.traces", rec.tenant_id);
    let payload = serde_json::to_vec(rec)?;
    let ack = s.js.publish(subject, payload.into()).await?;
    ack.await?;
    Ok(())
}

// ── Helpers ───────────────────────────────────────────────────────────────────

fn env(key: &str, default: &str) -> String {
    std::env::var(key).unwrap_or_else(|_| default.to_string())
}

fn sha256_hex(s: &str) -> String {
    let mut h = Sha256::new();
    h.update(s.as_bytes());
    hex::encode(h.finalize())
}

fn err(code: StatusCode, msg: &str) -> Response {
    (code, Json(json!({"error": msg}))).into_response()
}
