package handler

import (
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs/domains/topology"
	apimw "github.com/qeetgroup/qeet-logs/platform/api/middleware"
	"github.com/qeetgroup/qeet-logs/platform/clickhouse"
)

// Topology returns the derived service dependency graph (PRD Module 10). With
// ?service=X it trims to X's neighbourhood and reports the blast radius (the
// upstream callers affected if X fails). ?since=<seconds> sets the window.
func Topology(ch *clickhouse.Client, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:read", "logs:query") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "requires logs:read or logs:query scope"})
			return
		}
		window := int64(3600)
		if s := r.URL.Query().Get("since"); s != "" {
			if v, err := strconv.ParseInt(s, 10, 64); err == nil && v > 0 {
				window = v
			}
		}
		g, err := topology.Derive(ctx, ch, apimw.TenantID(ctx), window)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		if svc := r.URL.Query().Get("service"); svc != "" {
			g.FocusBlastRadius(svc)
		}
		writeJSON(w, http.StatusOK, g)
	}
}
