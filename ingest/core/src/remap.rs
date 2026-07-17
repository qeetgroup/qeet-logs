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
    // Conditional: if COND inner_stmt (single-line, no loops).
    If(Cond, Box<Stmt>),
    // Route hint: sets the target signal table for this event.
    Route(String),
}

#[derive(Debug, Clone)]
enum Cond {
    Eq(Vec<String>, Expr),
    Ne(Vec<String>, Expr),
    Lt(Vec<String>, Expr),
    Gt(Vec<String>, Expr),
    Exists(Vec<String>),
    Absent(Vec<String>),
}

#[derive(Debug, Clone)]
enum Expr {
    Str(String),
    Num(f64),
    Bool(bool),
    Path(Vec<String>),
    Call(String, Vec<Expr>),
}

/// Outcome of applying a program: number of statements applied, any
/// per-statement errors (surfaced, never fatal), and an optional routing hint.
#[derive(Debug, Default)]
pub struct ApplyReport {
    pub applied: usize,
    pub errors: Vec<String>,
    /// Set by a `route` statement to redirect this event to a different table.
    /// None = use the default table for the endpoint that received the event.
    pub route: Option<String>,
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
            match self.exec_top(stmt, event) {
                Ok(maybe_route) => {
                    if let Some(r) = maybe_route {
                        report.route = Some(r);
                    }
                    report.applied += 1;
                }
                Err(e) => report.errors.push(e),
            }
        }
        report
    }

    // exec_top handles the statement types that have special return values
    // (If, Route) before delegating to exec for the leaf statement types.
    fn exec_top(&self, stmt: &Stmt, event: &mut Value) -> Result<Option<String>, String> {
        match stmt {
            Stmt::If(cond, inner) => {
                if eval_cond(cond, event) {
                    self.exec_top(inner, event)
                } else {
                    Ok(None)
                }
            }
            Stmt::Route(table) => {
                const VALID: &[&str] = &["logs", "metrics", "traces", "auth_events", "change_events"];
                if !VALID.contains(&table.as_str()) {
                    return Err(format!("route: unknown table `{table}`"));
                }
                Ok(Some(table.clone()))
            }
            other => {
                self.exec(other, event)?;
                Ok(None)
            }
        }
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
            // If and Route are handled in exec_top and should never reach here.
            Stmt::If(_, _) | Stmt::Route(_) => unreachable!(),
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
        "if" => {
            // Grammar: if PATH OP [EXPR] STMT
            //   OP: == | != | < | > | exists | absent
            // The body STMT starts at the next statement keyword after the condition.
            let (cond, body_src) = parse_if_cond(rest.trim())?;
            let inner = parse_stmt(body_src.trim())?;
            Ok(Stmt::If(cond, Box::new(inner)))
        }
        "route" => {
            // Grammar: route "table_name"
            let tbl = rest.trim();
            if let Some(inner) = tbl.strip_prefix('"').and_then(|s| s.strip_suffix('"')) {
                Ok(Stmt::Route(inner.to_string()))
            } else {
                Err(format!("route requires a quoted table name, got `{tbl}`"))
            }
        }
        other => Err(format!("unknown statement `{other}`")),
    }
}

/// Parse an `if` condition from the rest of the line and return (Cond, body_src).
/// Body_src is the remainder of the line after the condition (the nested stmt).
fn parse_if_cond(s: &str) -> Result<(Cond, &str), String> {
    // Read the path (first whitespace-separated token).
    let (path_tok, after_path) = split_first_word(s);
    let path = parse_path(path_tok)?;

    let (op, after_op) = split_first_word(after_path);
    match op {
        "exists" => return Ok((Cond::Exists(path), after_op)),
        "absent" => return Ok((Cond::Absent(path), after_op)),
        _ => {}
    }

    // Value-bearing operators: ==  !=  <  >
    let (val_tok, body) = if after_op.starts_with('"') {
        // Quoted string: scan to closing '"', respecting '\"'.
        let bytes = after_op.as_bytes();
        let mut i = 1;
        while i < bytes.len() {
            if bytes[i] == b'\\' {
                i += 2;
            } else if bytes[i] == b'"' {
                i += 1;
                break;
            } else {
                i += 1;
            }
        }
        let (tok, rest) = after_op.split_at(i);
        (tok, rest.trim_start())
    } else {
        split_first_word(after_op)
    };

    let expr = parse_expr(val_tok)?;
    let cond = match op {
        "==" => Cond::Eq(path, expr),
        "!=" => Cond::Ne(path, expr),
        "<"  => Cond::Lt(path, expr),
        ">"  => Cond::Gt(path, expr),
        other => return Err(format!("unknown condition operator `{other}`")),
    };
    Ok((cond, body))
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

fn eval_cond(cond: &Cond, event: &Value) -> bool {
    match cond {
        Cond::Eq(path, expr) => {
            let lhs = get_path(event, path).cloned().unwrap_or(Value::Null);
            let rhs = eval(expr, event).unwrap_or(Value::Null);
            lhs == rhs
        }
        Cond::Ne(path, expr) => {
            let lhs = get_path(event, path).cloned().unwrap_or(Value::Null);
            let rhs = eval(expr, event).unwrap_or(Value::Null);
            lhs != rhs
        }
        Cond::Lt(path, expr) => {
            let l = get_path(event, path).and_then(to_num);
            let r = eval(expr, event).ok().as_ref().and_then(to_num);
            matches!((l, r), (Some(l), Some(r)) if l < r)
        }
        Cond::Gt(path, expr) => {
            let l = get_path(event, path).and_then(to_num);
            let r = eval(expr, event).ok().as_ref().and_then(to_num);
            matches!((l, r), (Some(l), Some(r)) if l > r)
        }
        Cond::Exists(path) => get_path(event, path).is_some(),
        Cond::Absent(path) => get_path(event, path).is_none(),
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

    #[test]
    fn if_eq_fires_when_true() {
        let out = run(
            "if .level == \"error\" set .alert = true",
            json!({"level": "error"}),
        );
        assert_eq!(out["alert"], true);
    }

    #[test]
    fn if_eq_skips_when_false() {
        let out = run(
            "if .level == \"error\" set .alert = true",
            json!({"level": "info"}),
        );
        assert!(out.get("alert").is_none());
    }

    #[test]
    fn if_ne_fires_when_not_equal() {
        let out = run(
            "if .env != \"prod\" set .debug = true",
            json!({"env": "staging"}),
        );
        assert_eq!(out["debug"], true);
    }

    #[test]
    fn if_exists_and_absent() {
        let out = run(
            "if .trace_id exists set .traced = true\nif .missing absent set .no_trace = true",
            json!({"trace_id": "abc"}),
        );
        assert_eq!(out["traced"], true);
        assert_eq!(out["no_trace"], true);
    }

    #[test]
    fn if_gt_lt_numeric() {
        let out = run(
            "if .count > 100 set .big = true\nif .count < 10 set .small = true",
            json!({"count": 200}),
        );
        assert_eq!(out["big"], true);
        assert!(out.get("small").is_none());
    }

    #[test]
    fn route_sets_hint() {
        let p = Program::parse("route \"auth_events\"").unwrap();
        let mut ev = json!({"event_type": "login"});
        let report = p.apply(&mut ev);
        assert_eq!(report.route.as_deref(), Some("auth_events"));
    }

    #[test]
    fn route_unknown_table_is_skipped_not_fatal() {
        let p = Program::parse("route \"unknown_table\"").unwrap();
        let mut ev = json!({});
        let report = p.apply(&mut ev);
        assert_eq!(report.errors.len(), 1);
        assert!(report.route.is_none());
    }

    #[test]
    fn if_route_conditional_routing() {
        let p = Program::parse("if .event_type exists route \"auth_events\"").unwrap();
        let mut ev = json!({"event_type": "login"});
        let report = p.apply(&mut ev);
        assert_eq!(report.route.as_deref(), Some("auth_events"));

        let mut ev2 = json!({"level": "info"});
        let report2 = p.apply(&mut ev2);
        assert!(report2.route.is_none());
    }
}
