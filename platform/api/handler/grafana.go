package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs-server/domains/grafana"
	"github.com/qeetgroup/qeet-logs-server/domains/query"
	apimw "github.com/qeetgroup/qeet-logs-server/platform/api/middleware"
	"github.com/qeetgroup/qeet-logs-server/platform/clickhouse"
)

// Grafana Loki-compatible read data source (PRD Module 22.4). Point a Grafana
// "Loki" data source at qeet-logs (base URL + X-Qeet-Api-Key) and it can browse
// the log store: /loki/api/v1/query_range, /labels, /label/{name}/values,
// /series, and /status/buildinfo. Every log query is translated to LogQL++ by
// domains/grafana and compiled by domains/query, which forces the authenticated
// tenant predicate (TAD §7.2). Scopes match the query API: logs:read | logs:query.

// grafanaTranslateOpts bounds the LogQL++ the translator emits; grafanaQueryOpts
// re-applies the same bounds when the LogQL++ is compiled to ClickHouse SQL.
var (
	grafanaTranslateOpts = grafana.Options{DefaultLimit: 1000, MaxLimit: 5000}
	grafanaQueryOpts     = query.Options{DefaultLimit: 1000, MaxLimit: 5000}
)

// GrafanaLokiQueryRange serves GET /loki/api/v1/query_range: it translates the
// Loki stream selector to LogQL++, runs it over `logs`, and returns Loki's
// `streams` result shape.
func GrafanaLokiQueryRange(ch *clickhouse.Client, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:read", "logs:query") {
			lokiError(w, http.StatusForbidden, "requires logs:read or logs:query scope")
			return
		}
		tenant := apimw.TenantID(ctx)
		expr := r.FormValue("query")
		if expr == "" {
			lokiError(w, http.StatusBadRequest, "missing query")
			return
		}
		now := time.Now().UnixNano()
		tr, err := grafana.Translate(grafana.Query{
			Selector: expr,
			StartNs:  lokiParseTimeNs(r.FormValue("start"), now-int64(time.Hour)),
			EndNs:    lokiParseTimeNs(r.FormValue("end"), now),
			Limit:    lokiParseInt(r.FormValue("limit")),
			Forward:  strings.EqualFold(r.FormValue("direction"), "forward"),
			NowNs:    now,
		}, grafanaTranslateOpts)
		if err != nil {
			lokiError(w, http.StatusBadRequest, err.Error())
			return
		}
		compiled, err := query.Compile(tr.LogQLPP, tenant, grafanaQueryOpts)
		if err != nil {
			lokiError(w, http.StatusBadRequest, err.Error())
			return
		}
		rows, err := ch.Query(ctx, compiled.SQL)
		if err != nil {
			lokiError(w, http.StatusBadGateway, "query execution failed: "+err.Error())
			return
		}
		writeAudit(ctx, pool, tenant, "loki_query_range", expr, len(rows), 0)
		lokiWriteStreams(w, rows)
	}
}

// GrafanaLokiLabels serves GET /loki/api/v1/labels: the browsable label keys.
// These are fixed by the `logs` schema, so the list is static.
func GrafanaLokiLabels(ch *clickhouse.Client, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !apimw.HasScope(r.Context(), "logs:read", "logs:query") {
			lokiError(w, http.StatusForbidden, "requires logs:read or logs:query scope")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "success", "data": grafana.Labels})
	}
}

// GrafanaLokiLabelValues serves GET /loki/api/v1/label/{name}/values: distinct
// values for one label via a tenant-scoped ClickHouse DISTINCT query.
func GrafanaLokiLabelValues(ch *clickhouse.Client, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:read", "logs:query") {
			lokiError(w, http.StatusForbidden, "requires logs:read or logs:query scope")
			return
		}
		name := strings.ToLower(chi.URLParam(r, "name"))
		if !grafana.IsLabel(name) {
			lokiError(w, http.StatusBadRequest, "unknown label "+name)
			return
		}
		now := time.Now().UnixNano()
		startNs := lokiParseTimeNs(r.FormValue("start"), now-int64(6*time.Hour))
		endNs := lokiParseTimeNs(r.FormValue("end"), now)
		sql := fmt.Sprintf(
			"SELECT DISTINCT %s AS v FROM logs WHERE tenant_id = %s AND %s != ''%s ORDER BY v LIMIT 1000",
			name, lokiQuoteLiteral(apimw.TenantID(ctx)), name, lokiTimeBoundsSQL(startNs, endNs))
		rows, err := ch.Query(ctx, sql)
		if err != nil {
			lokiError(w, http.StatusBadGateway, "query execution failed: "+err.Error())
			return
		}
		vals := make([]string, 0, len(rows))
		for _, row := range rows {
			if s := lokiString(row["v"]); s != "" {
				vals = append(vals, s)
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "success", "data": vals})
	}
}

// GrafanaLokiSeries serves GET /loki/api/v1/series: the distinct label-set
// combinations matching the given match[] stream selectors.
func GrafanaLokiSeries(ch *clickhouse.Client, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:read", "logs:query") {
			lokiError(w, http.StatusForbidden, "requires logs:read or logs:query scope")
			return
		}
		if err := r.ParseForm(); err != nil {
			lokiError(w, http.StatusBadRequest, "invalid form: "+err.Error())
			return
		}
		preds := []string{"tenant_id = " + lokiQuoteLiteral(apimw.TenantID(ctx))}
		for _, sel := range r.Form["match[]"] {
			matchers, err := grafana.ParseSelector(sel)
			if err != nil {
				lokiError(w, http.StatusBadRequest, err.Error())
				return
			}
			ms, err := lokiMatchersSQL(matchers)
			if err != nil {
				lokiError(w, http.StatusBadRequest, err.Error())
				return
			}
			preds = append(preds, ms...)
		}
		now := time.Now().UnixNano()
		startNs := lokiParseTimeNs(r.FormValue("start"), now-int64(6*time.Hour))
		endNs := lokiParseTimeNs(r.FormValue("end"), now)
		where := strings.Join(preds, " AND ") + lokiTimeBoundsSQL(startNs, endNs)
		sql := fmt.Sprintf(
			"SELECT DISTINCT service, level, environment FROM logs WHERE %s ORDER BY service, level, environment LIMIT 1000",
			where)
		rows, err := ch.Query(ctx, sql)
		if err != nil {
			lokiError(w, http.StatusBadGateway, "query execution failed: "+err.Error())
			return
		}
		series := make([]map[string]string, 0, len(rows))
		for _, row := range rows {
			s := map[string]string{}
			for _, k := range []string{"service", "level", "environment"} {
				if v := lokiString(row[k]); v != "" {
					s[k] = v
				}
			}
			series = append(series, s)
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "success", "data": series})
	}
}

// GrafanaLokiBuildInfo serves GET /loki/api/v1/status/buildinfo: static build
// info advertising a Loki-compatible version so Grafana's health check and
// feature detection succeed.
func GrafanaLokiBuildInfo() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"version":   "2.9.0",
			"revision":  "qeet-logs",
			"branch":    "main",
			"buildUser": "qeet-logs",
			"buildDate": "2026-01-01T00:00:00Z",
			"goVersion": runtime.Version(),
		})
	}
}

// --- helpers (all prefixed loki/grafana to avoid collisions in package handler) ---

// lokiWriteStreams groups rows into Loki streams keyed by their label set and
// writes the {"resultType":"streams", ...} response.
func lokiWriteStreams(w http.ResponseWriter, rows []map[string]any) {
	var order []string
	labels := map[string]map[string]string{}
	values := map[string][][]string{}
	for _, row := range rows {
		stream := lokiStreamLabels(row)
		key := lokiStreamKey(stream)
		if _, seen := labels[key]; !seen {
			labels[key] = stream
			order = append(order, key)
		}
		values[key] = append(values[key], []string{lokiTimestampNs(row["timestamp"]), lokiString(row["message"])})
	}
	result := make([]map[string]any, 0, len(order))
	for _, key := range order {
		result = append(result, map[string]any{"stream": labels[key], "values": values[key]})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   map[string]any{"resultType": "streams", "result": result},
	})
}

// lokiStreamLabels builds a Loki stream label set from a log row, dropping empty
// label columns.
func lokiStreamLabels(row map[string]any) map[string]string {
	out := map[string]string{}
	for _, k := range []string{"service", "level", "environment", "trace_id", "span_id"} {
		if v := lokiString(row[k]); v != "" {
			out[k] = v
		}
	}
	return out
}

// lokiStreamKey renders a stable identity for a label set.
func lokiStreamKey(m map[string]string) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(m[k])
		b.WriteByte(0)
	}
	return b.String()
}

// lokiMatchersSQL renders parsed Loki matchers as tenant-safe ClickHouse
// predicates over the `logs` columns.
func lokiMatchersSQL(matchers []grafana.Matcher) ([]string, error) {
	out := make([]string, 0, len(matchers))
	for _, m := range matchers {
		col := strings.ToLower(m.Label)
		if !grafana.IsLabel(col) {
			return nil, fmt.Errorf("unknown label %q", m.Label)
		}
		switch m.Op {
		case "=":
			out = append(out, fmt.Sprintf("%s = %s", col, lokiQuoteLiteral(m.Value)))
		case "!=":
			out = append(out, fmt.Sprintf("%s != %s", col, lokiQuoteLiteral(m.Value)))
		default:
			return nil, fmt.Errorf("unsupported matcher operator %q (use = or !=)", m.Op)
		}
	}
	return out, nil
}

// lokiTimeBoundsSQL renders the optional ClickHouse time-window predicate
// (leading " AND ") from an absolute [start,end] nanosecond range.
func lokiTimeBoundsSQL(startNs, endNs int64) string {
	var b strings.Builder
	if startNs > 0 {
		fmt.Fprintf(&b, " AND timestamp >= fromUnixTimestamp64Nano(%d)", startNs)
	}
	if endNs > 0 {
		fmt.Fprintf(&b, " AND timestamp <= fromUnixTimestamp64Nano(%d)", endNs)
	}
	return b.String()
}

// lokiTimestampNs converts a ClickHouse DateTime64(9,'UTC') JSON value (rendered
// as "2006-01-02 15:04:05.000000000") to a nanosecond-epoch string for Loki.
func lokiTimestampNs(v any) string {
	s := lokiString(v)
	if s == "" {
		return "0"
	}
	if t, err := time.ParseInLocation("2006-01-02 15:04:05.999999999", s, time.UTC); err == nil {
		return strconv.FormatInt(t.UnixNano(), 10)
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return strconv.FormatInt(t.UnixNano(), 10)
	}
	return s // already numeric, or an unrecognised shape — pass through
}

// lokiParseTimeNs parses a Loki time parameter (integer nanoseconds, float
// seconds, or RFC3339) into nanoseconds since epoch, falling back to def.
func lokiParseTimeNs(s string, def int64) int64 {
	if s == "" {
		return def
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n // Loki sends integer nanoseconds
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return int64(f * 1e9) // seconds with fraction
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t.UnixNano()
	}
	return def
}

func lokiParseInt(s string) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return 0
}

// lokiQuoteLiteral escapes and single-quotes a string as a ClickHouse literal
// (backslash style), for the small DISTINCT queries this file builds directly.
func lokiQuoteLiteral(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return "'" + s + "'"
}

// lokiString renders a decoded ClickHouse JSON value as a string.
func lokiString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

// lokiError writes a Loki-shaped error response.
func lokiError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{"status": "error", "error": msg})
}
