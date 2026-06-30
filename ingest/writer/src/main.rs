//! qeet-logs ClickHouse writer.
//!
//! Consumes the `qeet-logs.{tenant}.logs` JetStream work queue, batches records
//! (up to 100 / 2s), batch-inserts them into ClickHouse (JSONEachRow, with an
//! insert-deduplication token for idempotency on redelivery), publishes each
//! record to the Redis `tail.{tenant}.{service}` channel for live tail, then
//! acks. On insert failure messages are left unacked and redelivered.

use std::time::Duration;

use async_nats::jetstream::consumer::{pull::Config as PullConfig, AckPolicy};
use futures::StreamExt;
use sha2::{Digest, Sha256};

const BATCH_MAX: usize = 100;
const BATCH_WAIT: Duration = Duration::from_secs(2);

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(tracing_subscriber::EnvFilter::from_default_env().add_directive("info".parse()?))
        .init();

    let nats_url = env("NATS_URL", "nats://localhost:4223");
    let redis_url = env("REDIS_URL", "redis://localhost:6380");
    let ch_url = env("CLICKHOUSE_URL", "http://localhost:8123");
    let ch_db = env("CLICKHOUSE_DATABASE", "qeet_logs");
    let ch_user = env("CLICKHOUSE_USER", "default");
    let ch_pass = env("CLICKHOUSE_PASSWORD", "");

    let mut redis =
        redis::aio::ConnectionManager::new(redis::Client::open(redis_url)?).await?;
    let http = reqwest::Client::new();

    let nc = async_nats::connect(&nats_url).await?;
    let js = async_nats::jetstream::new(nc);
    let stream = js
        .get_or_create_stream(async_nats::jetstream::stream::Config {
            name: "QEET_LOGS".to_string(),
            subjects: vec!["qeet-logs.*.logs".to_string()],
            retention: async_nats::jetstream::stream::RetentionPolicy::WorkQueue,
            max_age: Duration::from_secs(24 * 3600),
            ..Default::default()
        })
        .await?;
    let consumer = stream
        .get_or_create_consumer(
            "qeet-logs-writer",
            PullConfig {
                durable_name: Some("qeet-logs-writer".to_string()),
                ack_policy: AckPolicy::Explicit,
                ..Default::default()
            },
        )
        .await?;

    tracing::info!(%ch_url, %ch_db, "qeet-logs writer started");

    loop {
        let mut batch = consumer
            .batch()
            .max_messages(BATCH_MAX)
            .expires(BATCH_WAIT)
            .messages()
            .await?;

        let mut msgs = Vec::new();
        while let Some(msg) = batch.next().await {
            match msg {
                Ok(m) => msgs.push(m),
                Err(e) => tracing::warn!(error = %e, "batch message error"),
            }
        }
        if msgs.is_empty() {
            continue;
        }

        // Build the NDJSON insert body and collect (tenant, service, payload)
        // for live-tail fan-out + the dedup token from record ids.
        let mut ndjson = String::new();
        let mut tail: Vec<(String, String, Vec<u8>)> = Vec::new();
        let mut ids: Vec<String> = Vec::new();
        for m in &msgs {
            let v: serde_json::Value = match serde_json::from_slice(&m.payload) {
                Ok(v) => v,
                Err(e) => {
                    tracing::warn!(error = %e, "skipping unparseable record");
                    continue;
                }
            };
            let tenant = v.get("tenant_id").and_then(|x| x.as_str()).unwrap_or("").to_string();
            let service = v.get("service").and_then(|x| x.as_str()).unwrap_or("unknown").to_string();
            if let Some(id) = v.get("id").and_then(|x| x.as_str()) {
                ids.push(id.to_string());
            }
            ndjson.push_str(&String::from_utf8_lossy(&m.payload));
            ndjson.push('\n');
            tail.push((tenant, service, m.payload.to_vec()));
        }

        let token = dedup_token(&mut ids);
        match insert(&http, &ch_url, &ch_db, &ch_user, &ch_pass, &token, ndjson).await {
            Ok(()) => {
                for (tenant, service, payload) in &tail {
                    let channel = format!("tail.{tenant}.{service}");
                    let _: Result<i64, _> = redis::cmd("PUBLISH")
                        .arg(&channel)
                        .arg(payload.as_slice())
                        .query_async(&mut redis)
                        .await;
                }
                for m in &msgs {
                    if let Err(e) = m.ack().await {
                        tracing::warn!(error = %e, "ack failed");
                    }
                }
                tracing::info!(count = msgs.len(), "inserted batch");
            }
            Err(e) => {
                // Leave messages unacked → JetStream redelivers them.
                tracing::error!(error = %e, count = msgs.len(), "insert failed; will retry");
            }
        }
    }
}

async fn insert(
    http: &reqwest::Client,
    ch_url: &str,
    db: &str,
    user: &str,
    pass: &str,
    token: &str,
    ndjson: String,
) -> anyhow::Result<()> {
    let mut req = http
        .post(format!("{ch_url}/"))
        .query(&[
            ("database", db),
            ("query", "INSERT INTO logs FORMAT JSONEachRow"),
            ("date_time_input_format", "best_effort"),
            ("insert_deduplication_token", token),
        ])
        .body(ndjson);
    if !user.is_empty() {
        req = req.header("X-ClickHouse-User", user);
    }
    if !pass.is_empty() {
        req = req.header("X-ClickHouse-Key", pass);
    }
    let resp = req.send().await?;
    if !resp.status().is_success() {
        let status = resp.status();
        let body = resp.text().await.unwrap_or_default();
        anyhow::bail!("clickhouse insert {}: {}", status, body.trim());
    }
    Ok(())
}

/// Deterministic dedup token over the batch's record ids (order-independent).
fn dedup_token(ids: &mut [String]) -> String {
    ids.sort();
    let mut h = Sha256::new();
    for id in ids.iter() {
        h.update(id.as_bytes());
        h.update(b",");
    }
    hex::encode(h.finalize())
}

fn env(key: &str, default: &str) -> String {
    std::env::var(key).unwrap_or_else(|_| default.to_string())
}
