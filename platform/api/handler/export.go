package handler

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs-server/domains/query"
	apimw "github.com/qeetgroup/qeet-logs-server/platform/api/middleware"
	"github.com/qeetgroup/qeet-logs-server/platform/clickhouse"
)

// exportOpts permits far larger result sets than the interactive query path,
// because export is meant to return the full result set for programmatic use
// (PRD 8.5), not a single screen's worth. The LogQL++ compiler still applies a
// bounded LIMIT, so this caps a runaway export rather than being unbounded.
var exportOpts = query.Options{DefaultLimit: 100_000, MaxLimit: 1_000_000}

// Export handles GET /v1/export?q=<LogQL++>&format=csv|ndjson|json.
//
// It compiles and runs the query for the authenticated tenant (tenant predicate
// injected from identity, never user input) and streams the full result set as
// a downloadable file via Content-Disposition: attachment. Reuses the CSV/
// NDJSON writers from query.go. (PRD 8.5 — Export & Programmatic Access.)
//
// Scope: logs:export OR logs:read.
func Export(ch *clickhouse.Client, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:export", "logs:read") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "requires logs:export or logs:read scope"})
			return
		}
		tenant := apimw.TenantID(ctx)
		q := r.URL.Query().Get("q")
		if q == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing query parameter q"})
			return
		}
		format := exportFormat(r.URL.Query().Get("format"))

		compiled, err := query.Compile(q, tenant, exportOpts)
		if errors.Is(err, query.ErrTail) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "use the live-tail endpoint /v1/query/tail for TAIL"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}

		start := time.Now()
		rows, err := ch.Query(ctx, compiled.SQL)
		latencyMs := int(time.Since(start).Milliseconds())
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "query execution failed: " + err.Error()})
			return
		}

		// Record the export in the tamper-evident audit log (best-effort — an
		// audit-write failure must not fail the export). Mirrors query.go.
		writeAudit(ctx, pool, tenant, "export", q, len(rows), latencyMs)

		// Mark the response as a file download. Set before the body writers set
		// their Content-Type, so the header is committed with the 200 response.
		w.Header().Set("Content-Disposition", "attachment; filename="+exportFilename(format))

		switch format {
		case "csv":
			writeCSV(w, compiled.Columns, rows)
		case "ndjson":
			writeNDJSON(w, rows)
		default: // json
			writeJSON(w, http.StatusOK, map[string]any{
				"columns": compiled.Columns,
				"count":   len(rows),
				"rows":    rows,
			})
		}
	}
}

// exportFormat normalises the ?format= value; unknown/blank defaults to json.
func exportFormat(v string) string {
	switch v {
	case "csv":
		return "csv"
	case "ndjson":
		return "ndjson"
	default:
		return "json"
	}
}

// exportFilename builds a timestamped download filename for the given format.
func exportFilename(format string) string {
	return fmt.Sprintf("qeet-logs-export-%s.%s", time.Now().UTC().Format("20060102T150405Z"), format)
}
