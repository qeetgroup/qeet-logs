package handler

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs/domains/query"
	apimw "github.com/qeetgroup/qeet-logs/platform/api/middleware"
	"github.com/qeetgroup/qeet-logs/platform/clickhouse"
)

var queryOpts = query.Options{DefaultLimit: 100, MaxLimit: 1000}

// Query executes a LogQL++ statement (?q=...) for the authenticated tenant and
// returns results as JSON (default), CSV, or NDJSON (?format=csv|ndjson).
func Query(ch *clickhouse.Client, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:read", "logs:query") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "requires logs:read or logs:query scope"})
			return
		}
		tenant := apimw.TenantID(ctx)
		q := r.URL.Query().Get("q")
		if q == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing query parameter q"})
			return
		}
		compiled, err := query.Compile(q, tenant, queryOpts)
		if errors.Is(err, query.ErrTail) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "use the live-tail endpoint /v1/query/tail for TAIL"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		runAndRespond(w, r, ch, pool, tenant, "query", q, compiled)
	}
}

// AuthEvents returns the tenant's typed auth-event stream (PRD Module 09).
// The stream is populated in Phase 2; until then this returns an empty set.
func AuthEvents(ch *clickhouse.Client, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:read", "logs:query") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "requires logs:read or logs:query scope"})
			return
		}
		tenant := apimw.TenantID(ctx)
		const q = "SELECT * FROM auth_events"
		compiled, err := query.Compile(q, tenant, queryOpts)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		runAndRespond(w, r, ch, pool, tenant, "auth-events", q, compiled)
	}
}

func runAndRespond(w http.ResponseWriter, r *http.Request, ch *clickhouse.Client, pool *pgxpool.Pool,
	tenant, action, q string, compiled *query.Compiled) {
	start := time.Now()
	rows, err := ch.Query(r.Context(), compiled.SQL)
	latency := time.Since(start).Milliseconds()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "query execution failed: " + err.Error()})
		return
	}
	writeAudit(r.Context(), pool, tenant, action, q, len(rows), int(latency))

	switch r.URL.Query().Get("format") {
	case "csv":
		writeCSV(w, compiled.Columns, rows)
	case "ndjson":
		writeNDJSON(w, rows)
	default:
		writeJSON(w, http.StatusOK, map[string]any{
			"columns": compiled.Columns,
			"count":   len(rows),
			"rows":    rows,
		})
	}
}

// writeAudit records the query in the tamper-evident audit log (PRD Module 13.3),
// best-effort — an audit write failure must not fail the query.
func writeAudit(ctx context.Context, pool *pgxpool.Pool, tenant, action, q string, count, latencyMs int) {
	_, _ = pool.Exec(ctx,
		`INSERT INTO audit_log (tenant_id, action, query_text, result_count, latency_ms)
		 VALUES ($1::uuid, $2, $3, $4, $5)`,
		tenant, action, q, count, latencyMs)
}

func writeCSV(w http.ResponseWriter, cols []string, rows []map[string]any) {
	w.Header().Set("Content-Type", "text/csv")
	cw := csv.NewWriter(w)
	_ = cw.Write(cols)
	rec := make([]string, len(cols))
	for _, row := range rows {
		for i, c := range cols {
			rec[i] = cell(row[c])
		}
		_ = cw.Write(rec)
	}
	cw.Flush()
}

func writeNDJSON(w http.ResponseWriter, rows []map[string]any) {
	w.Header().Set("Content-Type", "application/x-ndjson")
	enc := json.NewEncoder(w)
	for _, row := range rows {
		_ = enc.Encode(row)
	}
}

func cell(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}
