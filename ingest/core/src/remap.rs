//! A small VRL-inspired remap language (PRD Module 04.2) applied in-flight to
//! every event before storage. Deliberately a bounded subset — no loops, no
//! recursion, a hard statement cap — so it is inherently sandboxable and cheap
//! (one tenant's transform can't degrade another's ingestion). Evaluation is
//! **fail-open**: a statement that errors is skipped and recorded, never
//! dropping the event (Module 04 edge case).
//!
//! Grammar (one statement per line; `#` comments):
//!   set  PATH = EXPR       # assign
//!   del  PATH              # delete a field
//!   redact PATH            # replace value with "[REDACTED]" if present
//!   rename PATH PATH       # move a field
//!
//! EXPR: "string" | number | true|false | PATH | FUNC(EXPR, ...)
//! FUNC: to_string | to_int | to_float | upcase | downcase | coalesce | concat
//! PATH: .a.b.c  (roots into the event object; `set` creates intermediates)

use serde_json::{Map, Value};

const MAX_STATEMENTS: usize = 200;

#[derive(Debug, Clone)]
pub struct Program {
    stmts: Vec<Stmt>,
}

#[derive(Debug, Clone)]
enum Stmt {
    Set(Vec<String>, Expr),
    Del(Vec<String>),
    Redact(Vec<String>),
    Rename(Vec<String>, Vec<String>),
}

#[derive(Debug, Clone)]
enum Expr {
    Str(String),
    Num(f64),
    Bool(bool),
    Path(Vec<String>),
    Call(String, Vec<Expr>),
}

/// Outcome of applying a program: number of statements applied and any
/// per-statement errors (surfaced, never fatal).
#[derive(Debug, Default)]
pub struct ApplyReport {
    pub applied: usize,
    pub errors: Vec<String>,
}

impl Program {
    /// Parse a remap program. Empty/whitespace-only input is a valid no-op.
    pub fn parse(src: &str) -> Result<Program, String> {
        let mut stmts = Vec::new();
        for (i, raw) in src.lines().enumerate() {
            let line = strip_comment(raw).trim();
            if line.is_empty() {
                continue;
            }
            if stmts.len() >= MAX_STATEMENTS {
                return Err(format!("program exceeds {MAX_STATEMENTS} statements"));
            }
            let stmt = parse_stmt(line).map_err(|e| format!("line {}: {e}", i + 1))?;
            stmts.push(stmt);
        }
        Ok(Program { stmts })
    }

    pub fn is_empty(&self) -> bool {
        self.stmts.is_empty()
    }

    /// Apply the program to an event object in place. Non-object events are
    /// wrapped so paths still resolve.
    pub fn apply(&self, event: &mut Value) -> ApplyReport {
        if !event.is_object() {
            *event = Value::Object(Map::new());
        }
        let mut report = ApplyReport::default();
        for stmt in &self.stmts {
            match self.exec(stmt, event) {
                Ok(()) => report.applied += 1,
                Err(e) => report.errors.push(e),
            }
        }
        report
    }

    fn exec(&self, stmt: &Stmt, event: &mut Value) -> Result<(), String> {
        match stmt {
            Stmt::Set(path, expr) => {
                let v = eval(expr, event)?;
                set_path(event, path, v);
                Ok(())
            }
            Stmt::Del(path) => {
                del_path(event, path);
                Ok(())
            }
            Stmt::Redact(path) => {
                if get_path(event, path).is_some() {
                    set_path(event, path, Value::String("[REDACTED]".to_string()));
                }
                Ok(())
            }
            Stmt::Rename(src, dst) => {
                if let Some(v) = get_path(event, src).cloned() {
                    del_path(event, src);
                    set_path(event, dst, v);
                }
                Ok(())
            }
        }
    }
}

fn strip_comment(line: &str) -> &str {
    // A '#' outside of a string literal starts a comment.
    let bytes = line.as_bytes();
    let mut in_str = false;
    let mut i = 0;
    while i < bytes.len() {
        match bytes[i] {
            b'"' => in_str = !in_str,
            b'\\' if in_str => i += 1,
            b'#' if !in_str => return &line[..i],
            _ => {}
        }
        i += 1;
    }
    line
}

fn parse_stmt(line: &str) -> Result<Stmt, String> {
    let (kw, rest) = split_first_word(line);
    match kw {
        "set" => {
            let (lhs, rhs) = rest.split_once('=').ok_or("set requires `PATH = EXPR`")?;
            Ok(Stmt::Set(parse_path(lhs.trim())?, parse_expr(rhs.trim())?))
        }
        "del" => Ok(Stmt::Del(parse_path(rest.trim())?)),
        "redact" => Ok(Stmt::Redact(parse_path(rest.trim())?)),
        "rename" => {
            let (a, b) = rest.trim().split_once(char::is_whitespace).ok_or("rename requires two paths")?;
            Ok(Stmt::Rename(parse_path(a.trim())?, parse_path(b.trim())?))
        }
        other => Err(format!("unknown statement `{other}`")),
    }
}

fn split_first_word(s: &str) -> (&str, &str) {
    match s.find(char::is_whitespace) {
        Some(i) => (&s[..i], s[i..].trim_start()),
        None => (s, ""),
    }
}

fn parse_path(s: &str) -> Result<Vec<String>, String> {
    let s = s.strip_prefix('.').ok_or_else(|| format!("path must start with `.`: {s}"))?;
    if s.is_empty() {
        return Err("empty path".to_string());
    }
    let parts: Vec<String> = s.split('.').map(|p| p.to_string()).collect();
    if parts.iter().any(|p| p.is_empty()) {
        return Err(format!("invalid path `.{s}`"));
    }
    Ok(parts)
}

fn parse_expr(s: &str) -> Result<Expr, String> {
    let s = s.trim();
    if s.is_empty() {
        return Err("empty expression".to_string());
    }
    // String literal.
    if let Some(rest) = s.strip_prefix('"') {
        let inner = rest.strip_suffix('"').ok_or("unterminated string")?;
        return Ok(Expr::Str(unescape(inner)));
    }
    // Function call NAME(...).
    if let Some(open) = s.find('(') {
        if s.ends_with(')') && is_ident(&s[..open]) {
            let name = s[..open].to_string();
            let args_src = &s[open + 1..s.len() - 1];
            let mut args = Vec::new();
            for a in split_args(args_src) {
                if !a.trim().is_empty() {
                    args.push(parse_expr(&a)?);
                }
            }
            return Ok(Expr::Call(name, args));
        }
    }
    // Path.
    if s.starts_with('.') {
        return Ok(Expr::Path(parse_path(s)?));
    }
    // Bool / number.
    match s {
        "true" => return Ok(Expr::Bool(true)),
        "false" => return Ok(Expr::Bool(false)),
        _ => {}
    }
    if let Ok(n) = s.parse::<f64>() {
        return Ok(Expr::Num(n));
    }
    Err(format!("unrecognised expression `{s}`"))
}

/// Split top-level comma-separated function args (respecting nested parens and strings).
fn split_args(s: &str) -> Vec<String> {
    let mut out = Vec::new();
    let (mut depth, mut in_str, mut start) = (0i32, false, 0usize);
    let bytes = s.as_bytes();
    let mut i = 0;
    while i < bytes.len() {
        match bytes[i] {
            b'"' => in_str = !in_str,
            b'\\' if in_str => i += 1,
            b'(' if !in_str => depth += 1,
            b')' if !in_str => depth -= 1,
            b',' if !in_str && depth == 0 => {
                out.push(s[start..i].to_string());
                start = i + 1;
            }
            _ => {}
        }
        i += 1;
    }
    out.push(s[start..].to_string());
    out
}

fn eval(expr: &Expr, event: &Value) -> Result<Value, String> {
    match expr {
        Expr::Str(s) => Ok(Value::String(s.clone())),
        Expr::Num(n) => Ok(json_num(*n)),
        Expr::Bool(b) => Ok(Value::Bool(*b)),
        Expr::Path(p) => Ok(get_path(event, p).cloned().unwrap_or(Value::Null)),
        Expr::Call(name, args) => eval_call(name, args, event),
    }
}

fn eval_call(name: &str, args: &[Expr], event: &Value) -> Result<Value, String> {
    let a = |i: usize| -> Result<Value, String> {
        eval(args.get(i).ok_or_else(|| format!("{name}: missing argument {}", i + 1))?, event)
    };
    match name {
        "to_string" => Ok(Value::String(to_str(&a(0)?))),
        "upcase" => Ok(Value::String(to_str(&a(0)?).to_uppercase())),
        "downcase" => Ok(Value::String(to_str(&a(0)?).to_lowercase())),
        "to_int" => {
            let n = to_num(&a(0)?).ok_or("to_int: not a number")?;
            Ok(json_num(n.trunc()))
        }
        "to_float" => {
            let n = to_num(&a(0)?).ok_or("to_float: not a number")?;
            Ok(json_num(n))
        }
        "concat" => {
            let mut s = String::new();
            for i in 0..args.len() {
                s.push_str(&to_str(&a(i)?));
            }
            Ok(Value::String(s))
        }
        "coalesce" => {
            for i in 0..args.len() {
                let v = a(i)?;
                if !v.is_null() && v != Value::String(String::new()) {
                    return Ok(v);
                }
            }
            Ok(Value::Null)
        }
        other => Err(format!("unknown function `{other}`")),
    }
}

// ── JSON path helpers ─────────────────────────────────────────────────────────

fn get_path<'a>(v: &'a Value, path: &[String]) -> Option<&'a Value> {
    let mut cur = v;
    for key in path {
        cur = cur.as_object()?.get(key)?;
    }
    Some(cur)
}

fn set_path(v: &mut Value, path: &[String], val: Value) {
    let mut cur = v;
    for key in &path[..path.len() - 1] {
        if !cur.is_object() {
            *cur = Value::Object(Map::new());
        }
        cur = cur.as_object_mut().unwrap().entry(key.clone()).or_insert(Value::Object(Map::new()));
    }
    if !cur.is_object() {
        *cur = Value::Object(Map::new());
    }
    cur.as_object_mut().unwrap().insert(path[path.len() - 1].clone(), val);
}

fn del_path(v: &mut Value, path: &[String]) {
    let mut cur = v;
    for key in &path[..path.len() - 1] {
        match cur.as_object_mut().and_then(|o| o.get_mut(key)) {
            Some(next) => cur = next,
            None => return,
        }
    }
    if let Some(obj) = cur.as_object_mut() {
        obj.remove(&path[path.len() - 1]);
    }
}

fn to_str(v: &Value) -> String {
    match v {
        Value::String(s) => s.clone(),
        Value::Null => String::new(),
        other => other.to_string(),
    }
}

fn to_num(v: &Value) -> Option<f64> {
    match v {
        Value::Number(n) => n.as_f64(),
        Value::String(s) => s.parse().ok(),
        Value::Bool(b) => Some(if *b { 1.0 } else { 0.0 }),
        _ => None,
    }
}

fn json_num(n: f64) -> Value {
    if n.fract() == 0.0 && n.abs() < 9.007e15 {
        Value::Number((n as i64).into())
    } else {
        serde_json::Number::from_f64(n).map(Value::Number).unwrap_or(Value::Null)
    }
}

fn is_ident(s: &str) -> bool {
    !s.is_empty() && s.chars().all(|c| c.is_ascii_alphanumeric() || c == '_')
}

fn unescape(s: &str) -> String {
    let mut out = String::with_capacity(s.len());
    let mut chars = s.chars();
    while let Some(c) = chars.next() {
        if c == '\\' {
            match chars.next() {
                Some('n') => out.push('\n'),
                Some('t') => out.push('\t'),
                Some('"') => out.push('"'),
                Some('\\') => out.push('\\'),
                Some(other) => {
                    out.push('\\');
                    out.push(other);
                }
                None => out.push('\\'),
            }
        } else {
            out.push(c);
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn run(prog: &str, mut ev: Value) -> Value {
        let p = Program::parse(prog).unwrap();
        p.apply(&mut ev);
        ev
    }

    #[test]
    fn set_del_redact_rename() {
        let out = run(
            "set .env = \"prod\"\ndel .secret\nredact .password\nrename .msg .message",
            json!({"secret": "x", "password": "hunter2", "msg": "hi"}),
        );
        assert_eq!(out["env"], "prod");
        assert!(out.get("secret").is_none());
        assert_eq!(out["password"], "[REDACTED]");
        assert_eq!(out["message"], "hi");
        assert!(out.get("msg").is_none());
    }

    #[test]
    fn functions_and_nested_paths() {
        let out = run(
            "set .level = upcase(.level)\nset .k8s.pod = \"web-1\"\nset .n = to_int(\"42\")\nset .who = coalesce(.missing, .user)",
            json!({"level": "warn", "user": "alice"}),
        );
        assert_eq!(out["level"], "WARN");
        assert_eq!(out["k8s"]["pod"], "web-1");
        assert_eq!(out["n"], 42);
        assert_eq!(out["who"], "alice");
    }

    #[test]
    fn fail_open_on_bad_statement_is_a_parse_error_not_a_drop() {
        // A parse error is caught at load time; a runtime error is skipped.
        let p = Program::parse("set .x = unknown_fn(.a)").unwrap();
        let mut ev = json!({"a": 1});
        let report = p.apply(&mut ev);
        assert_eq!(report.errors.len(), 1);
        assert_eq!(ev["a"], 1); // event preserved
    }

    #[test]
    fn comments_and_blank_lines_ignored() {
        let p = Program::parse("# a comment\n\nset .a = \"b\" # trailing\n").unwrap();
        assert_eq!(p.stmts.len(), 1);
        let mut ev = json!({});
        p.apply(&mut ev);
        assert_eq!(ev["a"], "b");
    }
}
