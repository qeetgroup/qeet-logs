package handler

import (
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	apimw "github.com/qeetgroup/qeet-logs-server/platform/api/middleware"
	"github.com/qeetgroup/qeet-logs-server/platform/clickhouse"
)

type quotaUsage struct {
	TenantID   string    `json:"tenant_id"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	Events     int64     `json:"events"`
	BytesStored int64     `json:"bytes_stored"`
	RetentionDays int     `json:"retention_days"`
}

// QuotaUsage handles GET /v1/quota/usage.
// Returns the current billing-period (calendar month) ingest counts from
// ClickHouse. Requires logs:read scope.
func QuotaUsage(ch *clickhouse.Client, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		now := time.Now().UTC()
		// Billing period = calendar month.
		periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		periodEnd := periodStart.AddDate(0, 1, 0)

		sql := fmt.Sprintf(
			`SELECT count() AS events, sum(length(body)) AS bytes
			 FROM logs
			 WHERE tenant_id = '%s'
			   AND timestamp >= toDateTime('%s')
			   AND timestamp < toDateTime('%s')`,
			tenantID,
			periodStart.Format("2006-01-02 15:04:05"),
			periodEnd.Format("2006-01-02 15:04:05"),
		)
		rows, err := ch.Query(r.Context(), sql)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "quota query failed"})
			return
		}

		var events, bytes int64
		if len(rows) > 0 {
			if v, ok := rows[0]["events"].(float64); ok {
				events = int64(v)
			}
			if v, ok := rows[0]["bytes"].(float64); ok {
				bytes = int64(v)
			}
		}

		// Read retention_days from Postgres for context.
		var retentionDays int
		pool.QueryRow(r.Context(),
			`SELECT retention_days FROM retention_config WHERE tenant_id = $1`, tenantID,
		).Scan(&retentionDays) //nolint:errcheck
		if retentionDays == 0 {
			retentionDays = 7
		}

		writeJSON(w, http.StatusOK, quotaUsage{
			TenantID:      tenantID,
			PeriodStart:   periodStart,
			PeriodEnd:     periodEnd,
			Events:        events,
			BytesStored:   bytes,
			RetentionDays: retentionDays,
		})
	}
}
