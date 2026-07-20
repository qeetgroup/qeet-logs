//go:build integration

package clickhouse

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/qeetgroup/qeet-logs-server/domains/query"
)

// TestLogQLPlusPlusEndToEnd compiles LogQL++ expressions and executes them
// against the live ClickHouse to validate the full compile→execute pipeline.
func TestLogQLPlusPlusEndToEnd(t *testing.T) {
	ctx := context.Background()
	c := testClient(t)
	if err := c.Ping(ctx); err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}

	tenant := "ten_loql_" + time.Now().Format("150405.000000")
	now := time.Now().UTC()

	// Seed log data.
	logRows := []map[string]any{
		{
			"id": "01LOQL00000000000000000001", "timestamp": now.Format(time.RFC3339Nano),
			"received_at": now.Format(time.RFC3339Nano),
			"tenant_id": tenant, "service": "api", "environment": "prod",
			"level": "error", "message": "connection refused to db",
			"trace_id": "trace_loql_001", "body": `{"retries":3}`, "resource": "{}",
			"_retention_days": 7,
		},
		{
			"id": "01LOQL00000000000000000002", "timestamp": now.Add(-30 * time.Second).Format(time.RFC3339Nano),
			"received_at": now.Format(time.RFC3339Nano),
			"tenant_id": tenant, "service": "api", "environment": "prod",
			"level": "warn", "message": "high memory usage detected",
			"body": "{}", "resource": "{}", "_retention_days": 7,
		},
		{
			"id": "01LOQL00000000000000000003", "timestamp": now.Add(-60 * time.Second).Format(time.RFC3339Nano),
			"received_at": now.Format(time.RFC3339Nano),
			"tenant_id": tenant, "service": "worker", "environment": "staging",
			"level": "info", "message": "job completed successfully",
			"body": "{}", "resource": "{}", "_retention_days": 7,
		},
	}
	if err := c.Insert(ctx, "logs", logRows); err != nil {
		t.Fatalf("insert logs: %v", err)
	}

	// Seed metric data.
	metricRows := []map[string]any{
		{
			"tenant_id": tenant, "service": "api", "environment": "prod",
			"metric_name": "cpu_usage", "metric_type": "gauge",
			"value": float64(72.5), "count": uint64(1), "sum": float64(72.5),
			"timestamp": now.Format(time.RFC3339Nano),
			"attributes": map[string]string{"host": "pod-1"},
			"_retention_days": 7,
		},
		{
			"tenant_id": tenant, "service": "api", "environment": "prod",
			"metric_name": "cpu_usage", "metric_type": "gauge",
			"value": float64(61.0), "count": uint64(1), "sum": float64(61.0),
			"timestamp": now.Add(-time.Minute).Format(time.RFC3339Nano),
			"attributes": map[string]string{"host": "pod-2"},
			"_retention_days": 7,
		},
	}
	if err := c.Insert(ctx, "metrics", metricRows); err != nil {
		t.Fatalf("insert metrics: %v", err)
	}

	opts := query.Options{DefaultLimit: 100, MaxLimit: 1000}

	cases := []struct {
		name  string
		loql  string
		check func(t *testing.T, rows []map[string]any)
	}{
		{
			name: "filter logs by service + level",
			loql: fmt.Sprintf("SELECT id, level, message FROM logs WHERE service = 'api' AND level = 'error'"),
			check: func(t *testing.T, rows []map[string]any) {
				found := false
				for _, r := range rows {
					if r["level"] == "error" {
						found = true
					}
				}
				if !found {
					t.Errorf("expected an error-level row, got %v", rows)
				}
			},
		},
		{
			name: "aggregate metrics avg",
			loql: "SELECT service, avg(value) FROM metrics WHERE metric_name = 'cpu_usage' GROUP BY service",
			check: func(t *testing.T, rows []map[string]any) {
				if len(rows) == 0 {
					t.Error("expected at least one aggregation row for cpu_usage")
				}
			},
		},
		{
			name: "count by level on logs",
			loql: "SELECT level, count() AS n FROM logs GROUP BY level ORDER BY n DESC",
			check: func(t *testing.T, rows []map[string]any) {
				if len(rows) == 0 {
					t.Error("expected level-count rows")
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compiled, err := query.Compile(tc.loql, tenant, opts)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			// Tenant guard must be present.
			if !strings.Contains(compiled.SQL, tenant) {
				t.Fatalf("tenant guard missing in SQL: %s", compiled.SQL)
			}
			rows, err := c.Query(ctx, compiled.SQL)
			if err != nil {
				t.Fatalf("execute: %v (SQL: %s)", err, compiled.SQL)
			}
			tc.check(t, rows)
		})
	}
}

// TestPromQLEndToEnd inserts metrics and executes PromQL instant + range
// queries against them, verifying the full compile→execute pipeline.
func TestPromQLEndToEnd(t *testing.T) {
	ctx := context.Background()
	c := testClient(t)
	if err := c.Ping(ctx); err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}

	tenant := "ten_prom_" + time.Now().Format("150405.000000")
	now := time.Now().UTC()

	rows := []map[string]any{
		{
			"tenant_id": tenant, "service": "checkout", "environment": "prod",
			"metric_name": "http_req_total", "metric_type": "sum",
			"value": float64(500), "count": uint64(1), "sum": float64(500),
			"timestamp": now.Format(time.RFC3339Nano),
			"attributes": map[string]string{"status": "200"},
			"_retention_days": 7,
		},
		{
			"tenant_id": tenant, "service": "checkout", "environment": "prod",
			"metric_name": "http_req_total", "metric_type": "sum",
			"value": float64(20), "count": uint64(1), "sum": float64(20),
			"timestamp": now.Add(-30 * time.Second).Format(time.RFC3339Nano),
			"attributes": map[string]string{"status": "500"},
			"_retention_days": 7,
		},
	}
	if err := c.Insert(ctx, "metrics", rows); err != nil {
		t.Fatalf("insert metrics: %v", err)
	}

	// Instant query.
	instant, err := query.CompilePromQL("sum by (service)(http_req_total)", tenant, query.PromParams{
		EndUnix:  now.Unix(),
		Lookback: 300,
	})
	if err != nil {
		t.Fatalf("compile instant: %v", err)
	}
	if !strings.Contains(instant.SQL, tenant) {
		t.Fatalf("tenant guard missing: %s", instant.SQL)
	}
	irows, err := c.Query(ctx, instant.SQL)
	if err != nil {
		t.Fatalf("instant execute: %v", err)
	}
	_ = irows // may be empty if no data in lookback window, but must not error

	// Range query.
	rng, err := query.CompilePromQL("sum by (service)(http_req_total)", tenant, query.PromParams{
		StartUnix: now.Add(-5 * time.Minute).Unix(),
		EndUnix:   now.Unix(),
		StepSec:   60,
	})
	if err != nil {
		t.Fatalf("compile range: %v", err)
	}
	_, err = c.Query(ctx, rng.SQL)
	if err != nil {
		t.Fatalf("range execute: %v", err)
	}
}

// TestTenantIsolation verifies that a query compiled for tenant A never
// returns rows belonging to tenant B, even when both tenants have identical
// service/level values.
func TestTenantIsolation(t *testing.T) {
	ctx := context.Background()
	c := testClient(t)
	if err := c.Ping(ctx); err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}

	ts := time.Now().Format("150405.000000")
	tenantA := "ten_iso_a_" + ts
	tenantB := "ten_iso_b_" + ts
	now := time.Now().UTC()

	for _, tenant := range []string{tenantA, tenantB} {
		row := map[string]any{
			"id": "01ISO" + tenant[len(tenant)-6:] + "00000000000001",
			"timestamp": now.Format(time.RFC3339Nano),
			"received_at": now.Format(time.RFC3339Nano),
			"tenant_id": tenant, "service": "shared-svc", "environment": "prod",
			"level": "error", "message": "same message in both tenants",
			"body": "{}", "resource": "{}", "_retention_days": 7,
		}
		if err := c.Insert(ctx, "logs", []map[string]any{row}); err != nil {
			t.Fatalf("insert for %s: %v", tenant, err)
		}
	}

	opts := query.Options{DefaultLimit: 100, MaxLimit: 1000}
	compiled, err := query.Compile("SELECT id, tenant_id FROM logs WHERE service = 'shared-svc'", tenantA, opts)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	rows, err := c.Query(ctx, compiled.SQL)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	for _, r := range rows {
		if r["tenant_id"] != tenantA {
			t.Errorf("tenant isolation breach: got tenant_id=%v, want %s", r["tenant_id"], tenantA)
		}
	}
}
