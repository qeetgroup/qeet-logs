package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs/domains/billing"
	apimw "github.com/qeetgroup/qeet-logs/platform/api/middleware"
	"github.com/qeetgroup/qeet-logs/platform/clickhouse"
)

// Plans, quotas & overage — non-gated slice of PRD Module 33.4 / P2-G17.
//
// This file covers plan CONFIG (read/set) + usage-vs-plan overage COMPUTATION +
// invoice PREVIEW. It never charges anyone: turning a preview into a real
// invoice / payment is Module 33.5, delegated to Qeet Pay (external, gated) and
// intentionally NOT implemented here.
//
// All three routes mount under /v1/admin, which already requires the logs:admin
// scope, so tenant scoping comes from identity (apimw.TenantID), never input.

// validPlans is the CHECK-constrained plan set from migration 0017.
var validPlans = map[string]bool{"free": true, "pro": true, "enterprise": true}

// BillingGetPlan handles GET /v1/admin/plan — the caller's tenant billing plan.
// Returns the 'free' plan with zero allowances when no row has been set.
func BillingGetPlan(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		plan, err := billingLoadPlan(r.Context(), pool, tenantID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, plan)
	}
}

// BillingSetPlan handles PUT /v1/admin/plan — upsert the caller's plan +
// allowances + overage rates.
func BillingSetPlan(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())

		var body billing.Plan
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if body.Plan == "" {
			body.Plan = "free"
		}
		if !validPlans[body.Plan] {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "plan must be one of free|pro|enterprise"})
			return
		}
		if body.IncludedEvents < 0 || body.IncludedGB < 0 || body.OveragePerMillionEvents < 0 || body.OveragePerGB < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "allowances and rates must be non-negative"})
			return
		}

		_, err := pool.Exec(r.Context(), `
			INSERT INTO tenant_plans (
			    tenant_id, plan, included_events, included_gb,
			    overage_per_million_events, overage_per_gb, updated_at
			) VALUES ($1, $2, $3, $4, $5, $6, now())
			ON CONFLICT (tenant_id) DO UPDATE SET
			    plan                       = EXCLUDED.plan,
			    included_events            = EXCLUDED.included_events,
			    included_gb                = EXCLUDED.included_gb,
			    overage_per_million_events = EXCLUDED.overage_per_million_events,
			    overage_per_gb             = EXCLUDED.overage_per_gb,
			    updated_at                 = now()
		`, tenantID, body.Plan, body.IncludedEvents, body.IncludedGB,
			body.OveragePerMillionEvents, body.OveragePerGB)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, body)
	}
}

// BillingPreview handles GET /v1/admin/billing/preview — loads the tenant's plan
// + current calendar-month usage (same ClickHouse count/bytes query as
// QuotaUsage) and returns the computed overage InvoicePreview.
//
// NOTE: preview only. Actual invoicing / charging via Qeet Pay (Module 33.5) is
// an external gated integration and is deliberately NOT performed here.
func BillingPreview(ch *clickhouse.Client, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())

		plan, err := billingLoadPlan(r.Context(), pool, tenantID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}

		// Billing period = calendar month (identical to QuotaUsage).
		now := time.Now().UTC()
		periodStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
		periodEnd := periodStart.AddDate(0, 1, 0)

		events, bytes, err := billingMonthUsage(r.Context(), ch, tenantID, periodStart, periodEnd)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "usage query failed"})
			return
		}

		preview := billing.ComputeInvoicePreview(plan, events, bytes)

		writeJSON(w, http.StatusOK, map[string]any{
			"tenant_id":    tenantID,
			"period_start": periodStart,
			"period_end":   periodEnd,
			"plan":         plan,
			"usage_events": events,
			"usage_bytes":  bytes,
			"preview":      preview,
			"note":         "preview only — actual invoicing/charging via Qeet Pay (Module 33.5) is not performed here",
		})
	}
}

// billingLoadPlan reads the tenant's plan row, defaulting to the 'free' plan with
// zero allowances when none exists.
func billingLoadPlan(ctx context.Context, pool *pgxpool.Pool, tenantID string) (billing.Plan, error) {
	p := billing.Plan{Plan: "free"}
	err := pool.QueryRow(ctx, `
		SELECT plan, included_events, included_gb, overage_per_million_events, overage_per_gb
		FROM tenant_plans WHERE tenant_id = $1
	`, tenantID).Scan(&p.Plan, &p.IncludedEvents, &p.IncludedGB, &p.OveragePerMillionEvents, &p.OveragePerGB)
	if err != nil {
		// No row yet — 'free' plan with zero allowances is the documented default.
		return billing.Plan{Plan: "free"}, nil
	}
	return p, nil
}

// billingMonthUsage runs the same calendar-month count/bytes aggregate over the
// logs table as QuotaUsage, scoped to the identity-resolved tenant.
func billingMonthUsage(ctx context.Context, ch *clickhouse.Client, tenantID string, start, end time.Time) (int64, int64, error) {
	sql := fmt.Sprintf(
		`SELECT count() AS events, sum(length(body)) AS bytes
		 FROM logs
		 WHERE tenant_id = '%s'
		   AND timestamp >= toDateTime('%s')
		   AND timestamp < toDateTime('%s')`,
		tenantID,
		start.Format("2006-01-02 15:04:05"),
		end.Format("2006-01-02 15:04:05"),
	)
	rows, err := ch.Query(ctx, sql)
	if err != nil {
		return 0, 0, err
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
	return events, bytes, nil
}
