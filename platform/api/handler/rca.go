package handler

import (
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs/domains/rca"
	apimw "github.com/qeetgroup/qeet-logs/platform/api/middleware"
	"github.com/qeetgroup/qeet-logs/platform/clickhouse"
)

// RCA returns the structural root-cause candidate set for a service (PRD Module
// 11.1). ?service=X is required (or ?incident_id=… to resolve the service from
// an incident); ?since=<seconds> sets the window.
func RCA(ch *clickhouse.Client, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:read", "logs:query") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "requires logs:read or logs:query scope"})
			return
		}
		tenant := apimw.TenantID(ctx)
		service := r.URL.Query().Get("service")
		if service == "" {
			if inc := r.URL.Query().Get("incident_id"); inc != "" {
				_ = pool.QueryRow(ctx,
					`SELECT service FROM incidents WHERE id = $1::uuid AND tenant_id = $2::uuid`,
					inc, tenant).Scan(&service)
			}
		}
		if service == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "service or incident_id required"})
			return
		}
		window := int64(3600)
		if s := r.URL.Query().Get("since"); s != "" {
			if v, err := strconv.ParseInt(s, 10, 64); err == nil && v > 0 {
				window = v
			}
		}
		res, err := rca.RetrieveForService(ctx, ch, tenant, service, window)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, res)
	}
}
