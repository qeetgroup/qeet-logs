package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs-server/domains/ttfiq"
	apimw "github.com/qeetgroup/qeet-logs-server/platform/api/middleware"
)

// ttfiqResponse is the JSON summary returned by GET /v1/analytics/ttfiq.
type ttfiqResponse struct {
	TenantID           string     `json:"tenant_id"`
	OnboardedAt        *time.Time `json:"onboarded_at"`
	FirstQueryAt       *time.Time `json:"first_query_at"`
	TTFIQSeconds       *float64   `json:"ttfiq_seconds"`
	TTFIQHuman         string     `json:"ttfiq_human,omitempty"`
	CohortCount        int        `json:"cohort_count"`
	MedianTTFIQSeconds *float64   `json:"median_ttfiq_seconds,omitempty"`
	Status             string     `json:"status"`
	Assumptions        []string   `json:"assumptions"`
}

// TTFIQ handles GET /v1/analytics/ttfiq — a best-effort "Time To First
// Independent Query" analytic (PRD 7.5): the elapsed time between the tenant's
// onboarding (earliest api_keys.created_at) and its first query audit event,
// plus a median across API-key cohorts when more than one key exists. The pure
// computation and its assumptions live in domains/ttfiq.
//
// Scope: logs:read.
func TTFIQ(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:read") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "requires logs:read scope"})
			return
		}
		tenant := apimw.TenantID(ctx)

		// Onboarding cohorts: every API key's creation time (revoked keys
		// included — they still mark a historical onboarding moment).
		keyTimes, err := ttfiqScanTimes(ctx, pool,
			`SELECT created_at FROM api_keys WHERE tenant_id = $1 ORDER BY created_at`, tenant)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read api keys"})
			return
		}
		// Query events: audit rows that represent a user-run query/export.
		queryTimes, err := ttfiqScanTimes(ctx, pool,
			`SELECT created_at FROM audit_log
			 WHERE tenant_id = $1 AND action IN ('query', 'export', 'auth-events')
			 ORDER BY created_at`, tenant)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to read audit log"})
			return
		}

		s := ttfiq.Compute(keyTimes, queryTimes)

		resp := ttfiqResponse{
			TenantID:           tenant,
			OnboardedAt:        s.OnboardedAt,
			FirstQueryAt:       s.FirstQueryAt,
			TTFIQSeconds:       s.TTFIQSeconds,
			CohortCount:        s.CohortCount,
			MedianTTFIQSeconds: s.MedianTTFIQSeconds,
			Status:             ttfiqStatus(s),
			Assumptions: []string{
				"onboarding = earliest api_keys.created_at for the tenant (revoked keys included)",
				"query event = audit_log row with action in (query, export, auth-events)",
				"each api key is an onboarding cohort; median reported only when more than one key exists",
				"a query event predating onboarding is treated as an anomaly and omitted from the overall TTFIQ",
			},
		}
		if s.TTFIQSeconds != nil {
			resp.TTFIQHuman = time.Duration(*s.TTFIQSeconds * float64(time.Second)).String()
		}

		writeJSON(w, http.StatusOK, resp)
	}
}

func ttfiqStatus(s ttfiq.Summary) string {
	switch {
	case s.OnboardedAt == nil:
		return "no onboarding record (no api keys for tenant)"
	case s.FirstQueryAt == nil:
		return "onboarded but no query activity recorded yet"
	case s.TTFIQSeconds == nil:
		return "first query event predates onboarding (anomaly); ttfiq omitted"
	default:
		return "ok"
	}
}

// ttfiqScanTimes runs a single-column timestamp query for the tenant.
func ttfiqScanTimes(ctx context.Context, pool *pgxpool.Pool, sql, tenant string) ([]time.Time, error) {
	rows, err := pool.Query(ctx, sql, tenant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []time.Time
	for rows.Next() {
		var t time.Time
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
