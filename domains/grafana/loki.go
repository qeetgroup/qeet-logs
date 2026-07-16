// Package grafana translates Grafana Loki read queries into LogQL++ so a
// Grafana "Loki" data source pointed at qeet-logs can browse the log store
// (PRD Module 22.4). It is *pure translation*: no ClickHouse, no HTTP, and no
// tenant handling. The LogQL++ it emits is compiled by domains/query, which
// always injects the authenticated tenant predicate (TAD §7.2) — this package
// must never see or trust a tenant id.
//
// Scope of the translation (a pragmatic subset, in the spirit of the PromQL
// surface the platform exposes for Grafana's Prometheus data source):
//
//   - stream selector:    {service="api", level!="debug"}  → WHERE service = 'api' AND level != 'debug'
//   - absolute ns window:  start/end nanoseconds            → WHERE time >= now()-Ns AND time <= now()-Ns
//   - direction:           backward|forward                 → ORDER BY timestamp DESC|ASC
//
// LogQL++ deliberately has no regex comparison and no absolute-epoch time
// literal (it supports only now()±duration — see domains/query). So regex label
// matchers (=~ / !~) and log-pipeline stages are rejected with a clear error,
// and the absolute [start,end] window is converted to whole-second offsets from
// a reference "now". The tiny (sub-second) skew between that reference and
// ClickHouse's own now() at execution time is immaterial for a log range query.
package grafana

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Labels are the log fields a Loki data source may browse and select on. They
// are fixed by the ClickHouse `logs` schema (clickhouse/migrations/0001_logs.sql),
// so the label *keys* are static; only their *values* require a DISTINCT query.
// Every entry here must be a real, selectable `logs` column (a LogQL++ field the
// query compiler accepts) — otherwise a matcher on it would fail to compile.
var Labels = []string{
	"service", "level", "environment", "trace_id", "span_id", "ingested_by",
}

var labelSet = func() map[string]bool {
	m := make(map[string]bool, len(Labels))
	for _, l := range Labels {
		m[l] = true
	}
	return m
}()

// IsLabel reports whether name is a browsable Loki label (case-insensitive).
func IsLabel(name string) bool { return labelSet[strings.ToLower(name)] }

// Options bounds the translated query (mirrors query.Options).
type Options struct {
	DefaultLimit int
	MaxLimit     int
}

// Matcher is one Loki stream-selector label matcher, e.g. {service="api"}.
type Matcher struct {
	Label string
	Op    string // "=", "!=", "=~", "!~" (only = and != are translatable)
	Value string
}

// Query is a parsed Loki /query_range request.
type Query struct {
	Selector string // raw LogQL stream selector, e.g. `{service="api"}`
	StartNs  int64  // range start, nanoseconds since epoch (Loki `start`); 0 => now-1h
	EndNs    int64  // range end, nanoseconds since epoch (Loki `end`); 0 => now
	Limit    int    // requested max entries (Loki `limit`); 0 => Options.DefaultLimit
	Forward  bool   // true => oldest-first (Loki direction=forward)
	NowNs    int64  // reference "now" for relative-time bounds; 0 => time.Now()
}

// Translated is the compiled LogQL++ plus the matchers used to build it.
type Translated struct {
	LogQLPP  string            // the LogQL++ SELECT statement (feed to query.Compile)
	Matchers []Matcher         // parsed label matchers
	LabelSet map[string]string // equality matchers as a base stream label set
}

// selectCols is the projection: enough to reconstruct a Loki stream (labels),
// its line (message), and the point time (timestamp). Every column is a real,
// selectable `logs` field the LogQL++ compiler accepts.
const selectCols = "timestamp, service, level, environment, message, trace_id, span_id"

// reMatcher matches one label matcher inside a stream selector.
var reMatcher = regexp.MustCompile(`([a-zA-Z_][a-zA-Z0-9_.]*)\s*(=~|!~|!=|=)\s*"((?:[^"\\]|\\.)*)"`)

// ParseSelector parses a Loki stream selector (`{a="b", c!="d"}`) into its
// matchers. Log-pipeline stages after the closing brace (line filters, parsers,
// label formatters, …) are rejected — the LogQL++ read path cannot express them.
func ParseSelector(sel string) ([]Matcher, error) {
	s := strings.TrimSpace(sel)
	if s == "" {
		return nil, fmt.Errorf("empty selector")
	}
	openIdx := strings.IndexByte(s, '{')
	closeIdx := strings.LastIndexByte(s, '}')
	if openIdx != 0 || closeIdx < 0 {
		return nil, fmt.Errorf(`selector must be a Loki stream selector like {service="api"}`)
	}
	if rest := strings.TrimSpace(s[closeIdx+1:]); rest != "" {
		return nil, fmt.Errorf("log-pipeline stages are not supported by the LogQL++ read path: %q", rest)
	}
	inner := s[openIdx+1 : closeIdx]

	var matchers []Matcher
	for _, mm := range reMatcher.FindAllStringSubmatch(inner, -1) {
		matchers = append(matchers, Matcher{Label: mm[1], Op: mm[2], Value: unescape(mm[3])})
	}
	// Everything the matcher regex did not consume must be only commas/space,
	// otherwise the selector is malformed and we would silently drop a filter.
	for _, r := range reMatcher.ReplaceAllString(inner, "") {
		if r != ',' && r != ' ' && r != '\t' && r != '\n' && r != '\r' {
			return nil, fmt.Errorf("could not parse stream selector %q", sel)
		}
	}
	return matchers, nil
}

// Translate turns a parsed Loki query into a LogQL++ statement string. The
// returned LogQL++ carries no tenant predicate — query.Compile injects it.
func Translate(q Query, opts Options) (*Translated, error) {
	matchers, err := ParseSelector(q.Selector)
	if err != nil {
		return nil, err
	}

	now := q.NowNs
	if now == 0 {
		now = time.Now().UnixNano()
	}
	end := q.EndNs
	if end == 0 {
		end = now
	}
	start := q.StartNs
	if start == 0 {
		start = end - int64(time.Hour)
	}
	if start > end {
		return nil, fmt.Errorf("start (%d) must not be after end (%d)", start, end)
	}

	var preds []string
	labels := map[string]string{}
	for _, m := range matchers {
		col := strings.ToLower(m.Label)
		if !IsLabel(col) {
			return nil, fmt.Errorf("unknown label %q (browsable labels: %s)", m.Label, strings.Join(Labels, ", "))
		}
		switch m.Op {
		case "=":
			preds = append(preds, fmt.Sprintf("%s = %s", col, logqlQuote(m.Value)))
			labels[col] = m.Value
		case "!=":
			preds = append(preds, fmt.Sprintf("%s != %s", col, logqlQuote(m.Value)))
		default:
			return nil, fmt.Errorf("matcher %s%s is not supported: the LogQL++ read path handles = and != only (regex =~/!~ has no columnar equivalent)", m.Label, m.Op)
		}
	}

	// Absolute ns window → now()±duration bounds (LogQL++ has no epoch literal).
	preds = append(preds, "time >= "+relTime(now, start, true))
	preds = append(preds, "time <= "+relTime(now, end, false))

	dir := "DESC"
	if q.Forward {
		dir = "ASC"
	}
	limit := q.Limit
	if limit <= 0 {
		limit = opts.DefaultLimit
	}
	if opts.MaxLimit > 0 && limit > opts.MaxLimit {
		limit = opts.MaxLimit
	}

	logql := fmt.Sprintf("SELECT %s FROM logs WHERE %s ORDER BY timestamp %s LIMIT %d",
		selectCols, strings.Join(preds, " AND "), dir, limit)

	return &Translated{LogQLPP: logql, Matchers: matchers, LabelSet: labels}, nil
}

// relTime renders a LogQL++ time expression for the absolute instant tsNs,
// relative to nowNs. roundUp widens the window at its older edge so the range
// stays inclusive after truncation to whole seconds (LogQL++ durations are
// integer seconds).
func relTime(nowNs, tsNs int64, roundUp bool) string {
	diff := nowNs - tsNs // nanoseconds in the past (positive)
	if diff <= 0 {
		return "now()"
	}
	sec := diff / int64(time.Second)
	if roundUp && diff%int64(time.Second) != 0 {
		sec++
	}
	if sec <= 0 {
		return "now()"
	}
	return fmt.Sprintf("now() - %ds", sec)
}

// logqlQuote renders s as a single-quoted LogQL++ string literal
// (backslash-escaped), matching the LogQL++ lexer's string grammar in
// domains/query/lexer.go.
func logqlQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return "'" + s + "'"
}

// unescape resolves backslash escapes in a Loki double-quoted matcher value.
func unescape(v string) string {
	if !strings.ContainsRune(v, '\\') {
		return v
	}
	var b strings.Builder
	b.Grow(len(v))
	for i := 0; i < len(v); i++ {
		if v[i] == '\\' && i+1 < len(v) {
			i++
		}
		b.WriteByte(v[i])
	}
	return b.String()
}
