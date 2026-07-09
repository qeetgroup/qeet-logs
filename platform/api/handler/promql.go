package handler

import (
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs/domains/query"
	apimw "github.com/qeetgroup/qeet-logs/platform/api/middleware"
	"github.com/qeetgroup/qeet-logs/platform/clickhouse"
)

// PromInstantQuery serves the Prometheus HTTP API instant query
// (`/api/v1/query`) over the metrics store, so a Grafana Prometheus data source
// pointed at qeet-logs works unchanged (PRD Module 02.2).
func PromInstantQuery(ch *clickhouse.Client, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:read", "logs:query") {
			promErr(w, "requires logs:read or logs:query scope")
			return
		}
		expr := r.FormValue("query")
		if expr == "" {
			promErr(w, "missing query")
			return
		}
		end := parseUnixTime(r.FormValue("time"), time.Now())
		compiled, err := query.CompilePromQL(expr, apimw.TenantID(ctx), query.PromParams{
			EndUnix:  end,
			StepSec:  0,
			Lookback: 300,
		})
		if err != nil {
			promErr(w, err.Error())
			return
		}
		rows, err := ch.Query(ctx, compiled.SQL)
		if err != nil {
			promErr(w, "query execution failed: "+err.Error())
			return
		}
		writeAudit(ctx, pool, apimw.TenantID(ctx), "promql", expr, len(rows), 0)
		writeVector(w, compiled, rows)
	}
}

// PromRangeQuery serves the Prometheus HTTP API range query (`/api/v1/query_range`).
func PromRangeQuery(ch *clickhouse.Client, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:read", "logs:query") {
			promErr(w, "requires logs:read or logs:query scope")
			return
		}
		expr := r.FormValue("query")
		if expr == "" {
			promErr(w, "missing query")
			return
		}
		start := parseUnixTime(r.FormValue("start"), time.Now().Add(-time.Hour))
		end := parseUnixTime(r.FormValue("end"), time.Now())
		step := parseStep(r.FormValue("step"))
		compiled, err := query.CompilePromQL(expr, apimw.TenantID(ctx), query.PromParams{
			StartUnix: start,
			EndUnix:   end,
			StepSec:   step,
		})
		if err != nil {
			promErr(w, err.Error())
			return
		}
		rows, err := ch.Query(ctx, compiled.SQL)
		if err != nil {
			promErr(w, "query execution failed: "+err.Error())
			return
		}
		writeAudit(ctx, pool, apimw.TenantID(ctx), "promql_range", expr, len(rows), 0)
		writeMatrix(w, compiled, rows)
	}
}

// labelsOf builds the Prometheus label set for a result row.
func labelsOf(c *query.PromCompiled, row map[string]any) map[string]string {
	labels := map[string]string{"__name__": c.Metric}
	for _, col := range c.LabelCols {
		if s, ok := row[col].(string); ok && s != "" {
			labels[col] = s
		}
	}
	if c.HasAttrs {
		if attrs, ok := row["attributes"].(map[string]any); ok {
			for k, v := range attrs {
				if s, ok := v.(string); ok && s != "" {
					labels[k] = s
				}
			}
		}
	}
	return labels
}

func seriesKey(labels map[string]string) string {
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b []byte
	for _, k := range keys {
		b = append(b, k...)
		b = append(b, '=')
		b = append(b, labels[k]...)
		b = append(b, ';')
	}
	return string(b)
}

func writeVector(w http.ResponseWriter, c *query.PromCompiled, rows []map[string]any) {
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		v, ok := toFloat(row["value"])
		if !ok {
			continue
		}
		ts := toFloatDefault(row["bucket"], float64(time.Now().Unix()))
		result = append(result, map[string]any{
			"metric": labelsOf(c, row),
			"value":  []any{ts, strconv.FormatFloat(v, 'f', -1, 64)},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   map[string]any{"resultType": "vector", "result": result},
	})
}

func writeMatrix(w http.ResponseWriter, c *query.PromCompiled, rows []map[string]any) {
	// Group rows into series by their label set, accumulating [ts, value] points.
	order := []string{}
	series := map[string]map[string]string{}
	points := map[string][][]any{}
	for _, row := range rows {
		v, ok := toFloat(row["value"])
		if !ok {
			continue
		}
		labels := labelsOf(c, row)
		key := seriesKey(labels)
		if _, seen := series[key]; !seen {
			series[key] = labels
			order = append(order, key)
		}
		ts := toFloatDefault(row["bucket"], 0)
		points[key] = append(points[key], []any{ts, strconv.FormatFloat(v, 'f', -1, 64)})
	}
	result := make([]map[string]any, 0, len(order))
	for _, key := range order {
		result = append(result, map[string]any{
			"metric": series[key],
			"values": points[key],
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "success",
		"data":   map[string]any{"resultType": "matrix", "result": result},
	})
}

func promErr(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadRequest, map[string]any{
		"status":    "error",
		"errorType": "bad_data",
		"error":     msg,
	})
}

func parseUnixTime(s string, def time.Time) int64 {
	if s == "" {
		return def.Unix()
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return int64(f)
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.Unix()
	}
	return def.Unix()
}

func parseStep(s string) int64 {
	if s == "" {
		return 60
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		if f < 1 {
			return 1
		}
		return int64(f)
	}
	// Prometheus step duration like "30s", "5m", "1h".
	if d, err := time.ParseDuration(s); err == nil {
		if sec := int64(d.Seconds()); sec >= 1 {
			return sec
		}
	}
	return 60
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return 0, false
		}
		return x, true
	case string:
		f, err := strconv.ParseFloat(x, 64)
		return f, err == nil
	default:
		return 0, false
	}
}

func toFloatDefault(v any, def float64) float64 {
	if f, ok := toFloat(v); ok {
		return f
	}
	return def
}
