package handler

import (
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	apimw "github.com/qeetgroup/qeet-logs-server/platform/api/middleware"
)

type auditEntry struct {
	ID         int64      `json:"id"`
	TenantID   string     `json:"tenant_id"`
	Actor      *string    `json:"actor"`
	Action     string     `json:"action"`
	Resource   *string    `json:"resource"`
	ResourceID *string    `json:"resource_id"`
	Status     string     `json:"status"`
	IP         *string    `json:"ip"`
	UserAgent  *string    `json:"user_agent"`
	CreatedAt  time.Time  `json:"created_at"`
}

type auditResponse struct {
	Entries []auditEntry `json:"entries"`
	Total   int64        `json:"total"`
}

// ListAudit handles GET /v1/admin/audit.
// Supports optional ?actor=... and ?action=... query params for filtering.
func ListAudit(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		actor := r.URL.Query().Get("actor")
		action := r.URL.Query().Get("action")

		// Build a parameterised WHERE clause.
		args := []any{tenantID}
		where := "tenant_id = $1"
		if actor != "" {
			args = append(args, "%"+actor+"%")
			where += " AND actor ILIKE $" + itoa(len(args))
		}
		if action != "" {
			args = append(args, "%"+action+"%")
			where += " AND action ILIKE $" + itoa(len(args))
		}

		// Count
		var total int64
		pool.QueryRow(r.Context(), //nolint:errcheck
			"SELECT count(*) FROM audit_log WHERE "+where, args...,
		).Scan(&total)

		// Page (first 200)
		rows, err := pool.Query(r.Context(), `
			SELECT id, tenant_id, actor, action, resource, resource_id,
			       status, ip, user_agent, created_at
			FROM audit_log
			WHERE `+where+`
			ORDER BY created_at DESC
			LIMIT 200
		`, args...)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()

		var entries []auditEntry
		for rows.Next() {
			var e auditEntry
			if err := rows.Scan(
				&e.ID, &e.TenantID, &e.Actor, &e.Action,
				&e.Resource, &e.ResourceID, &e.Status,
				&e.IP, &e.UserAgent, &e.CreatedAt,
			); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			entries = append(entries, e)
		}
		if entries == nil {
			entries = []auditEntry{}
		}
		writeJSON(w, http.StatusOK, auditResponse{Entries: entries, Total: total})
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
