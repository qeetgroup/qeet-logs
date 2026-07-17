//! qeet-logs ClickHouse writer.
//!
//! Consumes the `qeet-logs.>` JetStream work queue (logs / metrics / traces),
//! batches records (up to 100 / 2s), routes each by subject to its ClickHouse
//! table, and batch-inserts them (JSONEachRow, with an insert-deduplication
//! token per table for idempotency on redelivery). Log records are also
//! published to the Redis `tail.{tenant}.{service}` channel for live tail.
//! Messages are acked only when every table's insert in the batch succeeds;
//! otherwise they are left unacked and JetStream redelivers them.

use std::collections::HashMap;
use std::time::Duration;

use async_nats::jetstream::consumer::{pull::Config as PullConfig, AckPolicy};
use futures::StreamExt;
use sha2::{Digest, Sha256};

/// Per-target-table accumulation within one consumed batch.
#[derive(Default)]
struct Group {
    ndjson: String,
    ids: Vec<String>,
    /// Live-tail fan-out payloads (logs only): (tenant, service, payload).
    tail: Vec<(String, String, Vec<u8>)>,
}

/// Map a NATS subject `qeet-logs.{tenant}.{signal}` to its ClickHouse table.
fn signal_table(subject: &str) -> Option<&'static str> {
    match subject.rsplit('.').next() {
        Some("logs") => Some("logs"),
        Some("metrics") => Some("metrics"),
        Some("traces") => Some("traces"),
        _ => None,
    }
}

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
            subjects: vec!["qeet-logs.>".to_string()],
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

        // Route each message to its target table by subject, accumulating a
        // per-table NDJSON body, dedup ids, and (logs only) live-tail payloads.
        let mut groups: HashMap<&'static str, Group> = HashMap::new();
        for m in &msgs {
            let Some(table) = signal_table(&m.subject) else {
                tracing::warn!(subject = %m.subject, "skipping message on unknown subject");
                continue;
            };
            let v: serde_json::Value = match serde_json::from_slice(&m.payload) {
                Ok(v) => v,
                Err(e) => {
                    tracing::warn!(error = %e, "skipping unparseable record");
                    continue;
                }
            };
            let g = groups.entry(table).or_default();
            if let Some(id) = v.get("id").and_then(|x| x.as_str()) {
                g.ids.push(id.to_string());
            }
            g.ndjson.push_str(&String::from_utf8_lossy(&m.payload));
            g.ndjson.push('\n');
            if table == "logs" {
                let tenant = v.get("tenant_id").and_then(|x| x.as_str()).unwrap_or("").to_string();
                let service = v.get("service").and_then(|x| x.as_str()).unwrap_or("unknown").to_string();
                g.tail.push((tenant, service, m.payload.to_vec()));
            }
        }

        // Insert every table; ack the whole batch only if all succeed.
        let mut all_ok = true;
        let mut inserted = 0usize;
        for (table, g) in &mut groups {
            let token = dedup_token(&mut g.ids);
            match insert(&http, &ch_url, &ch_db, &ch_user, &ch_pass, table, &token, g.ndjson.clone()).await {
                Ok(()) => {
                    inserted += g.ids.len().max(1);
                    for (tenant, service, payload) in &g.tail {
                        let channel = format!("tail.{tenant}.{service}");
                        let _: Result<i64, _> = redis::cmd("PUBLISH")
                            .arg(&channel)
                            .arg(payload.as_slice())
                            .query_async(&mut redis)
                            .await;
                    }
                }
                Err(e) => {
                    all_ok = false;
                    tracing::error!(error = %e, table = %table, "insert failed; will retry batch");
                }
            }
        }

        if all_ok {
            for m in &msgs {
                if let Err(e) = m.ack().await {
                    tracing::warn!(error = %e, "ack failed");
                }
            }
            tracing::info!(count = inserted, tables = groups.len(), "inserted batch");
        }
        // else: leave messages unacked → JetStream redelivers the whole batch.
    }
}

#[allow(clippy::too_many_arguments)]
async fn insert(
    http: &reqwest::Client,
    ch_url: &str,
    db: &str,
    user: &str,
    pass: &str,
    table: &str,
    token: &str,
    ndjson: String,
) -> anyhow::Result<()> {
    let insert_query = format!("INSERT INTO {table} FORMAT JSONEachRow");
    let mut req = http
        .post(format!("{ch_url}/"))
        .query(&[
            ("database", db),
            ("query", insert_query.as_str()),
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
