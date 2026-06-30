package query

import (
	"errors"
	"fmt"
	"strings"
)

// ErrTail is returned when a TAIL statement reaches Compile — TAIL is served by
// the live-tail WebSocket endpoint (M4), not the query SQL path.
var ErrTail = errors.New("TAIL is served by the live-tail endpoint, not /v1/query")

// Options bounds the compiled query (per-tier limits, PRD Module 4.7).
type Options struct {
	DefaultLimit int
	MaxLimit     int
}

// Compiled is the result of compiling a LogQL++ statement.
type Compiled struct {
	SQL     string
	Columns []string // output column order (for CSV export)
	Kind    Kind
}

type compiler struct {
	tenant string
	table  string
}

const levelArr = "['trace','debug','info','warn','error','fatal']"

var logsColumns = set("id", "timestamp", "received_at", "tenant_id", "service", "environment",
	"level", "message", "trace_id", "span_id", "user_linkage_key", "ingested_by",
	"_retention_days", "body", "resource", "pii_detected")

var authColumns = set("id", "timestamp", "received_at", "tenant_id", "event_type", "auth_method",
	"user_id", "session_id", "error_code", "attempt_number", "mfa_required", "mfa_passed",
	"ip_country", "ip_asn", "device_new", "risk_score", "user_linkage_key", "_retention_days")

// Compile parses and compiles a LogQL++ statement to ClickHouse SQL, always
// injecting the authenticated tenant predicate (never trusting user input).
func Compile(input, tenantID string, opts Options) (*Compiled, error) {
	stmt, err := Parse(input)
	if err != nil {
		return nil, err
	}
	if stmt.Table != "logs" && stmt.Table != "auth_events" {
		return nil, fmt.Errorf("unknown table %q (use logs or auth_events)", stmt.Table)
	}
	c := &compiler{tenant: tenantID, table: stmt.Table}
	switch stmt.Kind {
	case KindTail:
		return nil, ErrTail
	case KindSearch:
		return c.compileSearch(stmt, opts)
	default:
		return c.compileSelect(stmt, opts)
	}
}

func (c *compiler) compileSearch(s *Stmt, opts Options) (*Compiled, error) {
	where, err := c.whereSQL(s)
	if err != nil {
		return nil, err
	}
	cols := defaultProjection(c.table)
	sql := fmt.Sprintf("SELECT %s FROM %s WHERE %s ORDER BY timestamp DESC LIMIT %d",
		strings.Join(cols, ", "), c.table, where, limit(s, opts))
	return &Compiled{SQL: sql, Columns: cols, Kind: KindSearch}, nil
}

func (c *compiler) compileSelect(s *Stmt, opts Options) (*Compiled, error) {
	var exprs, cols []string
	for _, it := range s.Select {
		if it.Star && it.Agg == "" { // bare "*" (count(*) is handled below)
			for _, col := range defaultProjection(c.table) {
				exprs = append(exprs, col)
				cols = append(cols, col)
			}
			continue
		}
		e, alias, err := c.selectItem(it)
		if err != nil {
			return nil, err
		}
		exprs = append(exprs, e+" AS "+alias)
		cols = append(cols, alias)
	}
	if len(exprs) == 0 {
		return nil, fmt.Errorf("empty SELECT list")
	}

	where, err := c.whereSQL(s)
	if err != nil {
		return nil, err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "SELECT %s FROM %s WHERE %s", strings.Join(exprs, ", "), c.table, where)

	if len(s.GroupBy) > 0 {
		var gb []string
		for _, f := range s.GroupBy {
			expr, _, err := c.mapField(f)
			if err != nil {
				return nil, err
			}
			gb = append(gb, expr)
		}
		fmt.Fprintf(&b, " GROUP BY %s", strings.Join(gb, ", "))
	}
	if s.OrderBy != nil {
		expr, _, err := c.mapField(s.OrderBy.Field)
		if err != nil {
			return nil, err
		}
		dir := "ASC"
		if s.OrderBy.Desc {
			dir = "DESC"
		}
		fmt.Fprintf(&b, " ORDER BY %s %s", expr, dir)
	}
	fmt.Fprintf(&b, " LIMIT %d", limit(s, opts))
	return &Compiled{SQL: b.String(), Columns: cols, Kind: KindSelect}, nil
}

func (c *compiler) selectItem(it SelectItem) (expr, alias string, err error) {
	if it.Agg != "" {
		if it.Star {
			if it.Agg != "count" {
				return "", "", fmt.Errorf("%s(*) is not allowed; only count(*)", it.Agg)
			}
			return "count()", aliasOr(it.Alias, "count"), nil
		}
		mapped, _, err := c.mapField(it.Field)
		if err != nil {
			return "", "", err
		}
		return fmt.Sprintf("%s(%s)", it.Agg, mapped), aliasOr(it.Alias, it.Agg+"_"+last(it.Field)), nil
	}
	mapped, _, err := c.mapField(it.Field)
	if err != nil {
		return "", "", err
	}
	return mapped, aliasOr(it.Alias, last(it.Field)), nil
}

// whereSQL builds the full WHERE clause: the forced tenant guard, the compiled
// user predicate, and (for SEARCH) the token-bloom-filter probes.
func (c *compiler) whereSQL(s *Stmt) (string, error) {
	parts := []string{fmt.Sprintf("tenant_id = %s", quote(c.tenant))}
	if s.Where != nil {
		w, err := c.expr(s.Where)
		if err != nil {
			return "", err
		}
		parts = append(parts, "("+w+")")
	}
	if s.Kind == KindSearch {
		if sp := searchPredicate(s.Search); sp != "" {
			parts = append(parts, "("+sp+")")
		}
	}
	return strings.Join(parts, " AND "), nil
}

func (c *compiler) expr(e Expr) (string, error) {
	switch v := e.(type) {
	case Paren:
		inner, err := c.expr(v.Inner)
		if err != nil {
			return "", err
		}
		return "(" + inner + ")", nil
	case Binary:
		l, err := c.expr(v.Left)
		if err != nil {
			return "", err
		}
		r, err := c.expr(v.Right)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("(%s %s %s)", l, v.Op, r), nil
	case Comparison:
		return c.comparison(v)
	default:
		return "", fmt.Errorf("unsupported expression")
	}
}

func (c *compiler) comparison(cmp Comparison) (string, error) {
	// tenant / tenant_id filters are forced to the authenticated tenant — user
	// input is never trusted (TAD §7.2).
	if len(cmp.Field.Parts) == 1 {
		switch strings.ToLower(cmp.Field.Parts[0]) {
		case "tenant", "tenant_id":
			return fmt.Sprintf("tenant_id = %s", quote(c.tenant)), nil
		}
	}

	lhs, isLevel, err := c.mapField(cmp.Field)
	if err != nil {
		return "", err
	}

	// Severity-aware ordering for `level <|>|<=|>= 'warn'`.
	if isLevel && cmp.Value.Kind == ValString && isOrdering(cmp.Op) {
		return fmt.Sprintf("indexOf(%s, %s) %s indexOf(%s, %s)",
			levelArr, lhs, cmp.Op, levelArr, quote(cmp.Value.Str)), nil
	}

	rhs, err := compileValue(cmp.Value)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %s %s", lhs, cmp.Op, rhs), nil
}

// mapField validates and maps a field to a ClickHouse expression. Returns
// isLevel when the field is the canonical `level` column.
func (c *compiler) mapField(f Field) (expr string, isLevel bool, err error) {
	if len(f.Parts) == 1 {
		name := strings.ToLower(f.Parts[0])
		switch name {
		case "time":
			return "timestamp", false, nil
		case "tenant":
			return "tenant_id", false, nil
		}
		if c.columns()[name] {
			return name, name == "level", nil
		}
		return "", false, fmt.Errorf("unknown field %q", f.Parts[0])
	}

	head := strings.ToLower(f.Parts[0])
	switch head {
	case "body", "resource":
		if c.table != "logs" {
			return "", false, fmt.Errorf("%s.* is only available on logs", head)
		}
		keys := make([]string, 0, len(f.Parts)-1)
		for _, k := range f.Parts[1:] {
			keys = append(keys, quote(k))
		}
		return fmt.Sprintf("JSONExtractString(%s, %s)", head, strings.Join(keys, ", ")), false, nil
	case "auth":
		if c.table != "auth_events" {
			return "", false, fmt.Errorf("auth.* is only available on auth_events")
		}
		if len(f.Parts) != 2 {
			return "", false, fmt.Errorf("auth field must be auth.<column>")
		}
		col := strings.ToLower(f.Parts[1])
		if !authColumns[col] {
			return "", false, fmt.Errorf("unknown auth field %q", f.Parts[1])
		}
		return col, false, nil
	}
	return "", false, fmt.Errorf("unknown field %q", strings.Join(f.Parts, "."))
}

func (c *compiler) columns() map[string]bool {
	if c.table == "auth_events" {
		return authColumns
	}
	return logsColumns
}

func compileValue(v Value) (string, error) {
	switch v.Kind {
	case ValString:
		return quote(v.Str), nil
	case ValNumber:
		return v.Num, nil // lexer guarantees digits only
	case ValTime:
		if v.TimeSeconds == 0 {
			return "now()", nil
		}
		sign := "+"
		if v.TimeSub {
			sign = "-"
		}
		return fmt.Sprintf("now() %s INTERVAL %d SECOND", sign, v.TimeSeconds), nil
	}
	return "", fmt.Errorf("invalid value")
}

// searchPredicate compiles a SEARCH phrase into token-bloom-filter probes (one
// per alphanumeric token, AND-ed) so the tokenbf_v1 index accelerates it.
func searchPredicate(phrase string) string {
	var toks []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			toks = append(toks, cur.String())
			cur.Reset()
		}
	}
	for _, r := range phrase {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	if len(toks) == 0 {
		return ""
	}
	var probes []string
	for _, t := range toks {
		probes = append(probes, fmt.Sprintf("hasTokenCaseInsensitive(message, %s)", quote(t)))
	}
	return strings.Join(probes, " AND ")
}

func defaultProjection(table string) []string {
	if table == "auth_events" {
		return []string{"id", "timestamp", "event_type", "auth_method", "user_id", "ip_country", "risk_score"}
	}
	return []string{"id", "timestamp", "service", "level", "message", "trace_id", "span_id", "body"}
}

func limit(s *Stmt, opts Options) int {
	l := s.Limit
	if l <= 0 {
		l = opts.DefaultLimit
	}
	if opts.MaxLimit > 0 && l > opts.MaxLimit {
		l = opts.MaxLimit
	}
	return l
}

// quote escapes and single-quotes a string literal for ClickHouse (backslash style).
func quote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return "'" + s + "'"
}

func isOrdering(op string) bool {
	switch op {
	case "<", ">", "<=", ">=":
		return true
	}
	return false
}

func aliasOr(alias, def string) string {
	if alias != "" {
		return alias
	}
	return def
}

func last(f Field) string { return f.Parts[len(f.Parts)-1] }

func set(items ...string) map[string]bool {
	m := make(map[string]bool, len(items))
	for _, it := range items {
		m[it] = true
	}
	return m
}
