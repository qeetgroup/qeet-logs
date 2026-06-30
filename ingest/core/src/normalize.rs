//! Timestamp and log-level normalisation (PRD Module 1.3).

use chrono::{SecondsFormat, TimeZone, Utc};
use serde_json::Value;

/// Canonicalise a free-form level into trace|debug|info|warn|error|fatal.
pub fn level(input: Option<&str>) -> String {
    let raw = input.unwrap_or("").trim().to_ascii_lowercase();
    match raw.as_str() {
        "trace" => "trace",
        "debug" | "dbg" => "debug",
        "warn" | "warning" => "warn",
        "err" | "error" => "error",
        "fatal" | "critical" | "crit" | "panic" | "emergency" => "fatal",
        "" | "info" | "information" | "notice" | "log" => "info",
        _ => "info",
    }
    .to_string()
}

/// Normalise an event timestamp to an RFC3339 (UTC, nanosecond) string that
/// ClickHouse parses via best_effort. Accepts RFC3339/ISO-8601 strings and
/// epoch numbers (auto-detecting seconds/millis/micros/nanos); falls back to
/// now when absent or unparseable.
pub fn timestamp(input: Option<&Value>) -> String {
    match input {
        Some(Value::String(s)) => {
            let t = s.trim();
            // OTLP/HTTP encodes int64 timeUnixNano as a numeric string.
            if !t.is_empty() && t.chars().all(|c| c.is_ascii_digit()) {
                t.parse::<i64>().ok().and_then(epoch_to_rfc3339).unwrap_or_else(now)
            } else {
                parse_str(t).unwrap_or_else(now)
            }
        }
        Some(Value::Number(n)) => n.as_i64().and_then(epoch_to_rfc3339).unwrap_or_else(now),
        _ => now(),
    }
}

fn now() -> String {
    Utc::now().to_rfc3339_opts(SecondsFormat::Nanos, true)
}

fn parse_str(s: &str) -> Option<String> {
    if let Ok(dt) = chrono::DateTime::parse_from_rfc3339(s) {
        return Some(dt.with_timezone(&Utc).to_rfc3339_opts(SecondsFormat::Nanos, true));
    }
    // Common space-separated form "2006-01-02 15:04:05[.fff]".
    for fmt in ["%Y-%m-%d %H:%M:%S%.f", "%Y-%m-%d %H:%M:%S"] {
        if let Ok(ndt) = chrono::NaiveDateTime::parse_from_str(s, fmt) {
            return Some(Utc.from_utc_datetime(&ndt).to_rfc3339_opts(SecondsFormat::Nanos, true));
        }
    }
    None
}

fn epoch_to_rfc3339(n: i64) -> Option<String> {
    // Detect unit by digit count: 10=s, 13=ms, 16=us, 19=ns.
    let (secs, nanos) = match n.abs() {
        x if x < 1_000_000_000_000 => (n, 0),                       // seconds
        x if x < 1_000_000_000_000_000 => (n / 1_000, ((n % 1_000) * 1_000_000) as u32), // millis
        x if x < 1_000_000_000_000_000_000 => (n / 1_000_000, ((n % 1_000_000) * 1_000) as u32), // micros
        _ => (n / 1_000_000_000, (n % 1_000_000_000) as u32),       // nanos
    };
    Utc.timestamp_opt(secs, nanos)
        .single()
        .map(|dt| dt.to_rfc3339_opts(SecondsFormat::Nanos, true))
}
