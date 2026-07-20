package handler

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs-server/domains/query"
	"github.com/qeetgroup/qeet-logs-server/domains/retention"
	apimw "github.com/qeetgroup/qeet-logs-server/platform/api/middleware"
	"github.com/qeetgroup/qeet-logs-server/platform/clickhouse"
)

// per-row byte estimates for signals stored as typed columns rather than a raw
// body (documented approximations; logs are measured exactly from the payload).
const (
	metricRowBytesEstimate = 200
	traceRowBytesEstimate  = 400
	costObserveDays        = 7
)

// RetentionCost returns a cost-transparent breakdown of the tenant's steady-state
// storage cost per signal at the current retention window, plus what-if previews
// for alternative windows (PRD Module 6.4). GET /v1/admin/retention/cost.
func RetentionCost(ch *clickhouse.Client, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tenant := apimw.TenantID(ctx)
		tq := query.QuoteLiteral(tenant)

		rate := 0.10 // USD / GB-month; transparent + overridable
		if v, err := strconv.ParseFloat(os.Getenv("QEET_LOGS_COST_PER_GB_MONTH"), 64); err == nil && v >= 0 {
			rate = v
		}

		retDays := retention.DefaultDays
		_ = pool.QueryRow(ctx, `SELECT retention_days FROM retention_config WHERE tenant_id = $1`, tenant).Scan(&retDays)
		if retDays <= 0 {
			retDays = retention.DefaultDays
		}

		daily := map[string]int64{}
		// Logs: measured bytes (message + body) over the observation window.
		logsBytes := chScalar(ctx, ch, fmt.Sprintf(
			`SELECT toInt64(sum(length(message) + length(body))) FROM logs
			 WHERE tenant_id = %s AND timestamp > now() - INTERVAL %d DAY`, tq, costObserveDays))
		daily["logs"] = logsBytes / costObserveDays
		// Metrics / traces: row count × a documented per-row estimate.
		metricRows := chScalar(ctx, ch, fmt.Sprintf(
			`SELECT toInt64(count()) FROM metrics WHERE tenant_id = %s AND timestamp > now() - INTERVAL %d DAY`, tq, costObserveDays))
		daily["metrics"] = metricRows * metricRowBytesEstimate / costObserveDays
		traceRows := chScalar(ctx, ch, fmt.Sprintf(
			`SELECT toInt64(count()) FROM traces WHERE tenant_id = %s AND timestamp > now() - INTERVAL %d DAY`, tq, costObserveDays))
		daily["traces"] = traceRows * traceRowBytesEstimate / costObserveDays

		estimate := retention.EstimateCost(daily, retDays, rate)
		whatIf := retention.WhatIfRetention(daily, []int{7, 14, 30, 90, 365}, rate)

		writeJSON(w, http.StatusOK, map[string]any{
			"estimate":             estimate,
			"what_if":              whatIf,
			"observed_window_days": costObserveDays,
			"basis": "steady-state retained bytes = observed daily ingest × retention days; " +
				"logs measured from payload, metrics/traces estimated per-row",
		})
	}
}

// chScalar runs a single-scalar ClickHouse query and returns it as int64 (0 on
// error/NULL) — a small convenience for the cost aggregates.
func chScalar(ctx context.Context, ch *clickhouse.Client, sql string) int64 {
	rows, err := ch.Query(ctx, sql)
	if err != nil || len(rows) == 0 {
		return 0
	}
	for _, v := range rows[0] {
		switch x := v.(type) {
		case float64:
			return int64(x)
		case int64:
			return x
		case string:
			n, _ := strconv.ParseInt(x, 10, 64)
			return n
		}
	}
	return 0
}
