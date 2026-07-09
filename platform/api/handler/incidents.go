package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	apimw "github.com/qeetgroup/qeet-logs/platform/api/middleware"
)

// ListIncidents returns correlated incidents for the tenant (PRD Module 13.2),
// newest first. ?status=open|resolved filters; the full list includes the
// low-severity feed (incidents that were correlated but did not page).
func ListIncidents(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:read", "logs:query") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "requires logs:read or logs:query scope"})
			return
		}
		tenant := apimw.TenantID(ctx)
		sql := `SELECT id, title, service, severity, confidence, status, signal_count,
		               deploy_id, correlated_rules, paged, first_seen, last_seen, resolved_at
		        FROM incidents WHERE tenant_id = $1::uuid`
		args := []any{tenant}
		if s := r.URL.Query().Get("status"); s == "open" || s == "resolved" {
			sql += " AND status = $2"
			args = append(args, s)
		}
		sql += " ORDER BY last_seen DESC LIMIT 200"

		rows, err := pool.Query(ctx, sql, args...)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()

		type incident struct {
			ID              string          `json:"id"`
			Title           string          `json:"title"`
			Service         *string         `json:"service"`
			Severity        string          `json:"severity"`
			Confidence      float64         `json:"confidence"`
			Status          string          `json:"status"`
			SignalCount     int             `json:"signal_count"`
			DeployID        *string         `json:"deploy_id"`
			CorrelatedRules json.RawMessage `json:"correlated_rules"`
			Paged           bool            `json:"paged"`
			FirstSeen       time.Time       `json:"first_seen"`
			LastSeen        time.Time       `json:"last_seen"`
			ResolvedAt      *time.Time      `json:"resolved_at"`
		}
		out := []incident{}
		for rows.Next() {
			var i incident
			if err := rows.Scan(&i.ID, &i.Title, &i.Service, &i.Severity, &i.Confidence, &i.Status,
				&i.SignalCount, &i.DeployID, &i.CorrelatedRules, &i.Paged, &i.FirstSeen, &i.LastSeen, &i.ResolvedAt); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			out = append(out, i)
		}
		writeJSON(w, http.StatusOK, map[string]any{"count": len(out), "incidents": out})
	}
}
