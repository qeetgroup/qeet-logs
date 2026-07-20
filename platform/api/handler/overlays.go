package handler

import (
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs-server/domains/query"
	apimw "github.com/qeetgroup/qeet-logs-server/platform/api/middleware"
	"github.com/qeetgroup/qeet-logs-server/platform/clickhouse"
)

// Overlays returns chart-annotation markers for correlation-aware panels (PRD
// Module 22.2): deploy/change markers (from change_events) and incident windows
// (from Postgres) over a time window, optionally scoped by ?service=.
func Overlays(ch *clickhouse.Client, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:read", "logs:query") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "requires logs:read or logs:query scope"})
			return
		}
		tenant := apimw.TenantID(ctx)
		window := int64(3600)
		if s := r.URL.Query().Get("since"); s != "" {
			if v, err := strconv.ParseInt(s, 10, 64); err == nil && v > 0 {
				window = v
			}
		}
		service := r.URL.Query().Get("service")

		// Deploy/change markers from ClickHouse. Tenant guard injected from
		// identity (never user input); service value is safely quoted.
		deploySQL := "SELECT toString(timestamp) AS ts, kind, title, service, git_sha, deploy_id FROM change_events WHERE tenant_id = " +
			query.QuoteLiteral(tenant) + " AND timestamp > now() - INTERVAL " + strconv.FormatInt(window, 10) + " SECOND"
		if service != "" {
			deploySQL += " AND service = " + query.QuoteLiteral(service)
		}
		deploySQL += " ORDER BY timestamp DESC LIMIT 200"
		deployRows, err := ch.Query(ctx, deploySQL)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}

		// Incident windows from Postgres.
		incSQL := `SELECT title, service, severity, first_seen, last_seen, status
		           FROM incidents WHERE tenant_id = $1::uuid AND last_seen > now() - make_interval(secs => $2)`
		args := []any{tenant, window}
		if service != "" {
			incSQL += " AND service = $3"
			args = append(args, service)
		}
		incSQL += " ORDER BY last_seen DESC LIMIT 200"
		incRows, err := pool.Query(ctx, incSQL, args...)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer incRows.Close()
		incidents := []map[string]any{}
		for incRows.Next() {
			var title, severity, status string
			var svc *string
			var firstSeen, lastSeen any
			if err := incRows.Scan(&title, &svc, &severity, &firstSeen, &lastSeen, &status); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			incidents = append(incidents, map[string]any{
				"title": title, "service": svc, "severity": severity,
				"start": firstSeen, "end": lastSeen, "status": status,
			})
		}

		writeJSON(w, http.StatusOK, map[string]any{
			"deploys":   deployRows,
			"incidents": incidents,
		})
	}
}
