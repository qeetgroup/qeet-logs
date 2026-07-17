//! Legacy ingestion formats normalised to the canonical `IngestInput` at the
//! edge (PRD Module 01.2: "accepts legacy formats — syslog, plain JSON lines,
//! GELF — normalized to OTLP internally, so teams with existing
//! Fluent Bit/Vector/Filebeat deployments can point them at Qeet Logs without a
//! rewrite"). Plain JSON-lines is already handled by `/v1/ingest/batch`.

use serde::Deserialize;
use serde_json::{Map, Value};

use qeet_logs_core::IngestInput;

/// syslog severity (PRI % 8) → canonical level.
fn severity_level(sev: u8) -> &'static str {
    match sev {
        0..=2 => "fatal", // emerg/alert/crit
        3 => "error",
        4 => "warn",
        5 | 6 => "info", // notice/info
        _ => "debug",    // 7
    }
}

// ── GELF (Graylog Extended Log Format) ────────────────────────────────────────

#[derive(Deserialize)]
struct Gelf {
    #[serde(default)]
    host: Option<String>,
    #[serde(default)]
    short_message: Option<String>,
    #[serde(default)]
    full_message: Option<String>,
    #[serde(default)]
    timestamp: Option<Value>, // epoch seconds (may be float)
    #[serde(default)]
    level: Option<u8>, // syslog numeric severity
    #[serde(flatten)]
    extra: Map<String, Value>,
}

/// Parse a GELF body (one JSON object per line) into `IngestInput`s.
pub fn parse_gelf(body: &[u8]) -> Vec<IngestInput> {
    let text = String::from_utf8_lossy(body);
    let mut out = Vec::new();
    for line in text.lines() {
        let line = line.trim();
        if line.is_empty() {
            continue;
        }
        let g: Gelf = match serde_json::from_str(line) {
            Ok(g) => g,
            Err(_) => continue,
        };
        // Additional GELF fields are underscore-prefixed; strip it for storage.
        let mut extra = Map::new();
        for (k, v) in g.extra {
            let key = k.strip_prefix('_').unwrap_or(&k).to_string();
            if key == "id" {
                continue; // reserved in GELF
            }
            extra.insert(key, v);
        }
        out.push(IngestInput {
            timestamp: g.timestamp,
            service: g.host,
            level: g.level.map(|l| severity_level(l).to_string()),
            message: g.short_message.or(g.full_message),
            extra,
            ..Default::default()
        });
    }
    out
}

// ── syslog (RFC 5424, line-delimited) ─────────────────────────────────────────

/// Parse RFC 5424 syslog lines. A line that does not parse is preserved whole as
/// the message (fail-safe: never drop the data).
pub fn parse_syslog(body: &[u8]) -> Vec<IngestInput> {
    let text = String::from_utf8_lossy(body);
    let mut out = Vec::new();
    for line in text.lines() {
        let line = line.trim();
        if line.is_empty() {
            continue;
        }
        out.push(parse_syslog_line(line));
    }
    out
}

fn parse_syslog_line(line: &str) -> IngestInput {
    // <PRI>VERSION TIMESTAMP HOSTNAME APP-NAME PROCID MSGID [SD] MSG
    let raw_fallback = || IngestInput {
        message: Some(line.to_string()),
        level: Some("info".to_string()),
        ..Default::default()
    };

    let Some(rest) = line.strip_prefix('<') else {
        return raw_fallback();
    };
    let Some(gt) = rest.find('>') else {
        return raw_fallback();
    };
    let pri: u16 = match rest[..gt].parse() {
        Ok(p) => p,
        Err(_) => return raw_fallback(),
    };
    let severity = (pri % 8) as u8;
    let after_pri = &rest[gt + 1..];

    // Split the 6 leading space-delimited header fields, keeping the remainder
    // (structured data + message) intact.
    let mut it = after_pri.splitn(7, ' ');
    let _version = it.next();
    let timestamp = it.next().unwrap_or("-");
    let hostname = it.next().unwrap_or("-");
    let appname = it.next().unwrap_or("-");
    let _procid = it.next();
    let _msgid = it.next();
    let tail = it.next().unwrap_or("");

    // Drop a leading structured-data block ("[...]" or "-") from the message.
    let msg = strip_structured_data(tail);

    let mut extra = Map::new();
    if hostname != "-" {
        extra.insert("host".to_string(), Value::String(hostname.to_string()));
    }
    let service = if appname != "-" {
        Some(appname.to_string())
    } else if hostname != "-" {
        Some(hostname.to_string())
    } else {
        None
    };

    IngestInput {
        timestamp: (timestamp != "-").then(|| Value::String(timestamp.to_string())),
        service,
        level: Some(severity_level(severity).to_string()),
        message: Some(msg.to_string()),
        extra,
        ..Default::default()
    }
}

/// Strip a leading RFC 5424 structured-data element ("-" or one/more "[...]")
/// and return the human message that follows.
fn strip_structured_data(tail: &str) -> &str {
    let t = tail.trim_start();
    if let Some(rest) = t.strip_prefix('-') {
        return rest.trim_start();
    }
    if t.starts_with('[') {
        // Skip balanced bracket groups.
        let bytes = t.as_bytes();
        let mut i = 0;
        while i < bytes.len() && bytes[i] == b'[' {
            let mut depth = 0;
            while i < bytes.len() {
                match bytes[i] {
                    b'[' => depth += 1,
                    b']' => {
                        depth -= 1;
                        if depth == 0 {
                            i += 1;
                            break;
                        }
                    }
                    _ => {}
                }
                i += 1;
            }
            while i < bytes.len() && bytes[i] == b' ' {
                i += 1;
            }
        }
        return &t[i..];
    }
    t
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_gelf() {
        let body = br#"{"host":"web-1","short_message":"boom","level":3,"timestamp":1752142800,"_route":"/charge"}"#;
        let out = parse_gelf(body);
        assert_eq!(out.len(), 1);
        assert_eq!(out[0].service.as_deref(), Some("web-1"));
        assert_eq!(out[0].level.as_deref(), Some("error"));
        assert_eq!(out[0].message.as_deref(), Some("boom"));
        assert_eq!(out[0].extra.get("route").and_then(|v| v.as_str()), Some("/charge"));
    }

    #[test]
    fn parses_rfc5424_syslog() {
        let line = "<34>1 2003-10-11T22:14:15.003Z mymachine.example.com su 1234 ID47 - 'su root' failed";
        let out = parse_syslog(line.as_bytes());
        assert_eq!(out.len(), 1);
        assert_eq!(out[0].service.as_deref(), Some("su"));
        assert_eq!(out[0].level.as_deref(), Some("fatal")); // sev 34%8=2 crit
        assert_eq!(out[0].message.as_deref(), Some("'su root' failed"));
    }

    #[test]
    fn syslog_fallback_keeps_raw_line() {
        let out = parse_syslog(b"not a syslog line");
        assert_eq!(out.len(), 1);
        assert_eq!(out[0].message.as_deref(), Some("not a syslog line"));
    }
}
