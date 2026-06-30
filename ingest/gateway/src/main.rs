//! qeet-logs ingest gateway.
//!
//! Receives logs over HTTP (`/v1/ingest`, `/v1/ingest/batch`) and OTLP/HTTP JSON
//! (`/v1/logs`), resolves the tenant from its `X-Qeet-Api-Key`, enforces a
//! per-tenant Redis rate limit, normalises + runs the synchronous PII gate, then
//! publishes each record to NATS `qeet-logs.{tenant}.logs` for the writer.
//!
//! Listens on INGEST_PORT (default 8101) and also on 4318 (the OTLP/HTTP
//! convention). Protobuf OTLP is not yet supported (JSON only) in this milestone.

mod otlp;

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

use qeet_logs_core::{build_record, IngestInput, LogRecord, MaskingActions};

const AUTH_CACHE_TTL: Duration = Duration::from_secs(30);

#[derive(Clone)]
struct AppState {
    pg: Pool,
    redis: ConnectionManager,
    js: async_nats::jetstream::Context,
    rate_limit_per_min: u32,
    auth_cache: Arc<tokio::sync::RwLock<HashMap<String, (Auth, Instant)>>>,
}

#[derive(Clone)]
struct Auth {
    tenant_id: String,
    retention_days: u16,
    masking: MaskingActions,
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
        subjects: vec!["qeet-logs.*.logs".to_string()],
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
        auth_cache: Arc::new(tokio::sync::RwLock::new(HashMap::new())),
    };

    let app = Router::new()
        .route("/healthz", get(|| async { Json(json!({"status": "ok"})) }))
        .route("/readyz", get(readyz))
        .route("/v1/ingest", post(ingest_one))
        .route("/v1/ingest/batch", post(ingest_batch))
        .route("/v1/logs", post(ingest_otlp))
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
    let input: IngestInput = match serde_json::from_slice(&body) {
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
        let input: IngestInput = match serde_json::from_str(line) {
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
                    COALESCE(rc.masking_actions, '{}'::jsonb)::text AS masking \
             FROM api_keys k \
             JOIN tenants t ON t.id = k.tenant_id \
             LEFT JOIN retention_config rc ON rc.tenant_id = t.id \
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
    let auth = Auth {
        tenant_id: row.get::<_, String>("tenant_id"),
        retention_days,
        masking,
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

async fn publish(s: &AppState, rec: &LogRecord) -> anyhow::Result<()> {
    let subject = format!("qeet-logs.{}.logs", rec.tenant_id);
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
