//! Synchronous, pre-storage PII gate (PRD Module 10).
//!
//! Regex fast-path detectors run over the message and every string leaf of the
//! structured body BEFORE the record leaves the gateway, so PII never reaches
//! ClickHouse. Per-tenant actions decide what happens to each detected type.
//! (Phase 2 adds an ML slow-path for ambiguous content.)

use std::cell::Cell;
use std::collections::{BTreeSet, HashMap};

use once_cell::sync::Lazy;
use regex::Regex;
use serde_json::Value;
use sha2::{Digest, Sha256};

/// Per-tenant map of PII type (email|ipv4|ipv6|jwt|card|phone) -> action
/// (mask|hash|drop_field|drop_record). Missing types default to `mask`.
pub type MaskingActions = HashMap<String, String>;

#[derive(Clone, Copy)]
enum Action {
    Mask,
    Hash,
    DropField,
    DropRecord,
}

fn action_for(actions: &MaskingActions, kind: &str) -> Action {
    match actions.get(kind).map(String::as_str) {
        Some("hash") => Action::Hash,
        Some("drop_field") => Action::DropField,
        Some("drop_record") => Action::DropRecord,
        // Privacy-first default: anything detected is masked unless told otherwise.
        _ => Action::Mask,
    }
}

static EMAIL_RE: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}").unwrap());
static JWT_RE: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"eyJ[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+").unwrap());
static IPV4_RE: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"\b(?:\d{1,3}\.){3}\d{1,3}\b").unwrap());
static IPV6_RE: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"\b(?:[A-Fa-f0-9]{1,4}:){2,7}[A-Fa-f0-9]{1,4}\b").unwrap());
static CARD_RE: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"\b(?:\d[ \-]?){13,19}\b").unwrap());
// Conservative phone matcher (requires an international prefix) to limit false positives.
static PHONE_RE: Lazy<Regex> =
    Lazy::new(|| Regex::new(r"\+\d{1,3}[\s\-]?\d{6,12}\b").unwrap());

/// Run the gate over a plain string, masking detected PII in place. Records the
/// detected types into `detected` and sets `drop_record` if any matched type's
/// action is drop_record.
pub fn gate_string(
    s: &str,
    actions: &MaskingActions,
    detected: &mut BTreeSet<String>,
    drop_record: &mut bool,
) -> String {
    let mut out = s.to_string();
    out = mask(&out, "email", &EMAIL_RE, actions, detected, drop_record, None);
    out = mask(&out, "jwt", &JWT_RE, actions, detected, drop_record, None);
    out = mask(&out, "ipv4", &IPV4_RE, actions, detected, drop_record, None);
    out = mask(&out, "ipv6", &IPV6_RE, actions, detected, drop_record, None);
    out = mask(&out, "card", &CARD_RE, actions, detected, drop_record, Some(luhn_ok));
    out = mask(&out, "phone", &PHONE_RE, actions, detected, drop_record, None);
    out
}

/// Recursively run the gate over every string leaf of a JSON value (the body).
pub fn gate_value(
    v: &mut Value,
    actions: &MaskingActions,
    detected: &mut BTreeSet<String>,
    drop_record: &mut bool,
) {
    match v {
        Value::String(s) => *s = gate_string(s, actions, detected, drop_record),
        Value::Array(a) => a
            .iter_mut()
            .for_each(|e| gate_value(e, actions, detected, drop_record)),
        Value::Object(m) => m
            .values_mut()
            .for_each(|e| gate_value(e, actions, detected, drop_record)),
        _ => {}
    }
}

#[allow(clippy::too_many_arguments)]
fn mask(
    text: &str,
    kind: &str,
    re: &Regex,
    actions: &MaskingActions,
    detected: &mut BTreeSet<String>,
    drop_record: &mut bool,
    validate: Option<fn(&str) -> bool>,
) -> String {
    let action = action_for(actions, kind);
    let hit = Cell::new(false);
    let replaced = re.replace_all(text, |caps: &regex::Captures| {
        let matched = &caps[0];
        if let Some(v) = validate {
            if !v(matched) {
                return matched.to_string(); // not a real match (e.g. failed Luhn)
            }
        }
        hit.set(true);
        match action {
            Action::Mask => format!("[redacted:{kind}]"),
            Action::Hash => format!("{kind}:{}", &sha256_hex(matched)[..12]),
            // drop_field/drop_record both remove the sensitive substring here;
            // drop_record additionally flags the whole record below.
            Action::DropField | Action::DropRecord => String::new(),
        }
    });
    if hit.get() {
        detected.insert(kind.to_string());
        if matches!(action, Action::DropRecord) {
            *drop_record = true;
        }
    }
    replaced.into_owned()
}

fn sha256_hex(s: &str) -> String {
    let mut h = Sha256::new();
    h.update(s.as_bytes());
    hex::encode(h.finalize())
}

/// Luhn checksum over the digits in a candidate card number.
fn luhn_ok(s: &str) -> bool {
    let digits: Vec<u32> = s.chars().filter_map(|c| c.to_digit(10)).collect();
    if digits.len() < 13 || digits.len() > 19 {
        return false;
    }
    let mut sum = 0u32;
    for (i, d) in digits.iter().rev().enumerate() {
        let mut d = *d;
        if i % 2 == 1 {
            d *= 2;
            if d > 9 {
                d -= 9;
            }
        }
        sum += d;
    }
    sum.is_multiple_of(10)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn run(s: &str) -> (String, Vec<String>, bool) {
        let actions = MaskingActions::new();
        let mut detected = BTreeSet::new();
        let mut drop = false;
        let out = gate_string(s, &actions, &mut detected, &mut drop);
        (out, detected.into_iter().collect(), drop)
    }

    #[test]
    fn masks_email_by_default() {
        let (out, types, _) = run("user alice@example.com logged in");
        assert!(out.contains("[redacted:email]"), "got {out}");
        assert!(types.contains(&"email".to_string()));
    }

    #[test]
    fn masks_valid_card_but_not_random_number() {
        let (out, types, _) = run("card 4242424242424242 charged");
        assert!(out.contains("[redacted:card]"), "got {out}");
        assert!(types.contains(&"card".to_string()));

        let (out2, types2, _) = run("order 1234567890123 placed");
        assert!(!out2.contains("[redacted:card]"), "got {out2}");
        assert!(!types2.contains(&"card".to_string()));
    }

    #[test]
    fn hash_action_replaces_with_hash() {
        let mut actions = MaskingActions::new();
        actions.insert("email".into(), "hash".into());
        let mut detected = BTreeSet::new();
        let mut drop = false;
        let out = gate_string("contact bob@acme.io", &actions, &mut detected, &mut drop);
        assert!(out.contains("email:"), "got {out}");
        assert!(!out.contains("bob@acme.io"));
    }

    #[test]
    fn drop_record_flags() {
        let mut actions = MaskingActions::new();
        actions.insert("email".into(), "drop_record".into());
        let mut detected = BTreeSet::new();
        let mut drop = false;
        gate_string("leak eve@x.com", &actions, &mut detected, &mut drop);
        assert!(drop);
    }
}
