package query

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// PromQL-compatible query surface over the `metrics` table (PRD Module 02.2 —
// "a PromQL-compatible query surface ... so any existing Prometheus/Grafana
// pipeline works with zero re-instrumentation"). This is a pragmatic subset,
// not the full PromQL engine:
//
//   - instant vector selector:      metric_name{label="v", l2=~"re", l3!="v"}
//   - aggregation over a selector:   sum|avg|min|max|count [by (labels)] (selector)
//   - per-second counter rate:       rate(selector[5m])
//
// Reserved labels `__name__`, `service`, `environment`/`env` map to their
// dedicated columns; every other label maps to the `attributes` Map. The tenant
// predicate is always injected from the authenticated identity, never the query.

// PromParams bounds a PromQL evaluation window. For an instant query, Step==0
// and the window is [Time-Lookback, Time]; for a range query Step>0 buckets
// [Start, End] at Step-second resolution.
type PromParams struct {
	StartUnix int64
	EndUnix   int64
	StepSec   int64 // 0 => instant
	Lookback  int64 // instant lookback seconds (default 300)
}

// PromCompiled is a compiled PromQL query.
type PromCompiled struct {
	SQL       string
	LabelCols []string // output columns to treat as series labels
	Metric    string   // __name__ to stamp on every series
	HasAttrs  bool     // whether an `attributes` Map column is in the projection
	Instant   bool
}

type promMatcher struct{ label, op, value string }

type promExpr struct {
	metric   string
	matchers []promMatcher
	agg      string // "", sum, avg, min, max, count
	by       []string
	rate     bool
	rangeSec int64
}

var (
	reAggPrefix = regexp.MustCompile(`^(sum|avg|min|max|count)\b`)
	reBy        = regexp.MustCompile(`(?i)\bby\s*\(([^)]*)\)`)
	reSelector  = regexp.MustCompile(`^([a-zA-Z_:][a-zA-Z0-9_:.]*)\s*(\{.*\})?$`)
	reRate      = regexp.MustCompile(`^rate\(\s*(.*?)\s*\[\s*([0-9]+)(ms|s|m|h|d)\s*\]\s*\)$`)
	reMatcher   = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_.]*)\s*(=~|!~|!=|=)\s*"((?:[^"\\]|\\.)*)"`)
)

// CompilePromQL parses a PromQL expression and compiles it to ClickHouse SQL.
func CompilePromQL(input, tenantID string, p PromParams) (*PromCompiled, error) {
	e, err := parsePromQL(strings.TrimSpace(input))
	if err != nil {
		return nil, err
	}
	return e.compile(tenantID, p)
}

func parsePromQL(s string) (*promExpr, error) {
	if s == "" {
		return nil, fmt.Errorf("empty query")
	}
	e := &promExpr{}

	// Optional aggregation wrapper: AGG [by (..)] ( inner ) [by (..)].
	if m := reAggPrefix.FindString(s); m != "" {
		e.agg = m
		rest := strings.TrimSpace(s[len(m):])
		if by := reBy.FindStringSubmatch(rest); by != nil {
			e.by = splitLabels(by[1])
			rest = strings.TrimSpace(reBy.ReplaceAllString(rest, ""))
		}
		inner, err := stripOuterParens(rest)
		if err != nil {
			return nil, fmt.Errorf("aggregation %s: %w", e.agg, err)
		}
		s = strings.TrimSpace(inner)
	}

	// Optional rate() wrapper.
	if rm := reRate.FindStringSubmatch(s); rm != nil {
		e.rate = true
		e.rangeSec = promDurationSeconds(rm[2], rm[3])
		s = strings.TrimSpace(rm[1])
	}

	// Selector: metric{matchers}.
	sm := reSelector.FindStringSubmatch(s)
	if sm == nil {
		return nil, fmt.Errorf("unsupported PromQL expression %q", s)
	}
	e.metric = sm[1]
	if sm[2] != "" {
		for _, mm := range reMatcher.FindAllStringSubmatch(sm[2], -1) {
			e.matchers = append(e.matchers, promMatcher{label: mm[1], op: mm[2], value: mm[3]})
		}
	}
	return e, nil
}

func (e *promExpr) compile(tenant string, p PromParams) (*PromCompiled, error) {
	instant := p.StepSec <= 0
	stepSec := p.StepSec
	var startUnix, endUnix int64
	if instant {
		endUnix = p.EndUnix
		lookback := p.Lookback
		if lookback <= 0 {
			lookback = 300
		}
		startUnix = endUnix - lookback
		stepSec = lookback // single bucket spanning the lookback
	} else {
		startUnix, endUnix = p.StartUnix, p.EndUnix
		if endUnix <= startUnix || stepSec <= 0 {
			return nil, fmt.Errorf("invalid range: need end>start and step>0")
		}
	}

	// WHERE: forced tenant guard + metric name + label matchers + time window.
	where := []string{
		fmt.Sprintf("tenant_id = %s", quote(tenant)),
		fmt.Sprintf("metric_name = %s", quote(e.metric)),
		fmt.Sprintf("timestamp >= fromUnixTimestamp(%d)", startUnix),
		fmt.Sprintf("timestamp <= fromUnixTimestamp(%d)", endUnix),
	}
	for _, m := range e.matchers {
		pred, err := matcherSQL(m)
		if err != nil {
			return nil, err
		}
		where = append(where, pred)
	}

	bucketExpr := fmt.Sprintf("toStartOfInterval(timestamp, INTERVAL %d SECOND)", stepSec)

	// Inner: resolve each distinct series (service+environment+labels) to ONE
	// value per bucket — the latest sample (gauge/counter instant value), or a
	// per-second delta for rate(). This is the PromQL instant-vector step that
	// must happen BEFORE any aggregation across series.
	var innerVal string
	if e.rate {
		secs := e.rangeSec
		if secs <= 0 {
			secs = stepSec
		}
		innerVal = fmt.Sprintf("(max(value) - min(value)) / %d", secs)
	} else {
		innerVal = "argMax(value, timestamp)"
	}
	inner := fmt.Sprintf(
		"SELECT service, environment, attributes, toUnixTimestamp(%s) AS bucket, %s AS v "+
			"FROM metrics WHERE %s GROUP BY service, environment, attributes, bucket",
		bucketExpr, innerVal, strings.Join(where, " AND "))

	// No aggregation: the per-series inner IS the result.
	if e.agg == "" {
		sql := fmt.Sprintf("SELECT service, environment, attributes, bucket, v AS value FROM (%s) ORDER BY bucket", inner)
		return &PromCompiled{
			SQL:       sql,
			LabelCols: []string{"service", "environment"},
			Metric:    e.metric,
			HasAttrs:  true,
			Instant:   instant,
		}, nil
	}

	// Outer: aggregate the instantaneous per-series values by the `by (...)`
	// labels (empty `by` collapses everything to one series).
	var selCols, groupBy, labelCols []string
	for _, lbl := range e.by {
		selCols = append(selCols, fmt.Sprintf("%s AS %s", labelExpr(lbl), quoteIdent(lbl)))
		groupBy = append(groupBy, quoteIdent(lbl))
		labelCols = append(labelCols, lbl)
	}
	aggFn := fmt.Sprintf("%s(v)", e.agg)
	if e.agg == "count" {
		aggFn = "toFloat64(count())"
	}
	selCols = append(selCols, "bucket", aggFn+" AS value")
	groupBy = append(groupBy, "bucket")
	sql := fmt.Sprintf("SELECT %s FROM (%s) GROUP BY %s ORDER BY bucket",
		strings.Join(selCols, ", "), inner, strings.Join(groupBy, ", "))

	return &PromCompiled{
		SQL:       sql,
		LabelCols: labelCols,
		Metric:    e.metric,
		HasAttrs:  false,
		Instant:   instant,
	}, nil
}

// labelExpr maps a PromQL label name to its ClickHouse column/attribute access.
func labelExpr(lbl string) string {
	switch strings.ToLower(lbl) {
	case "__name__":
		return "metric_name"
	case "service":
		return "service"
	case "environment", "env":
		return "environment"
	default:
		return fmt.Sprintf("attributes[%s]", quote(lbl))
	}
}

func matcherSQL(m promMatcher) (string, error) {
	lhs := labelExpr(m.label)
	val := strings.ReplaceAll(m.value, `\"`, `"`)
	switch m.op {
	case "=":
		return fmt.Sprintf("%s = %s", lhs, quote(val)), nil
	case "!=":
		return fmt.Sprintf("%s != %s", lhs, quote(val)), nil
	case "=~":
		return fmt.Sprintf("match(%s, %s)", lhs, quote("^(?:"+val+")$")), nil
	case "!~":
		return fmt.Sprintf("NOT match(%s, %s)", lhs, quote("^(?:"+val+")$")), nil
	}
	return "", fmt.Errorf("unsupported matcher op %q", m.op)
}

func stripOuterParens(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "(") || !strings.HasSuffix(s, ")") {
		return "", fmt.Errorf("expected parenthesised expression, got %q", s)
	}
	depth := 0
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && i != len(s)-1 {
				return "", fmt.Errorf("unbalanced parentheses in %q", s)
			}
		}
	}
	if depth != 0 {
		return "", fmt.Errorf("unbalanced parentheses in %q", s)
	}
	return s[1 : len(s)-1], nil
}

func splitLabels(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func promDurationSeconds(num, unit string) int64 {
	n, _ := strconv.ParseInt(num, 10, 64)
	switch unit {
	case "ms":
		if n < 1000 {
			return 1
		}
		return n / 1000
	case "s":
		return n
	case "m":
		return n * 60
	case "h":
		return n * 3600
	case "d":
		return n * 86400
	}
	return n
}

// quoteIdent guards a label used as a SQL identifier alias.
func quoteIdent(s string) string {
	var b strings.Builder
	b.WriteByte('`')
	for _, r := range s {
		if r == '`' {
			continue
		}
		b.WriteRune(r)
	}
	b.WriteByte('`')
	return b.String()
}
