//go:build integration

package clickhouse

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

// testClient builds a client from CLICKHOUSE_URL (default local dev) targeting
// the qeet_logs database. Requires `make infra-up` + `make ch-migrate`.
func testClient(t *testing.T) *Client {
	t.Helper()
	url := os.Getenv("CLICKHOUSE_URL")
	if url == "" {
		url = "http://localhost:8123"
	}
	return New(url, "qeet_logs", "default", "")
}

func TestInsertAndQuery(t *testing.T) {
	ctx := context.Background()
	c := testClient(t)
	if err := c.Ping(ctx); err != nil {
		t.Skipf("clickhouse unavailable: %v", err)
	}

	// Unique tenant per run so the assertion is independent of persisted data.
	tenant := "ten_test_" + time.Now().Format("150405.000000")
	now := time.Now().UTC()

	rows := []map[string]any{
		{
			"id": "01TEST00000000000000000001", "timestamp": now.Format(time.RFC3339Nano),
			"tenant_id": tenant, "service": "checkout", "level": "error",
			"message": "payment failed for order 9931", "_retention_days": 7,
		},
		{
			"id": "01TEST00000000000000000002", "timestamp": now.Format(time.RFC3339Nano),
			"tenant_id": tenant, "service": "checkout", "level": "info",
			"message": "checkout started", "trace_id": "abc123", "_retention_days": 7,
		},
		{
			"id": "01TEST00000000000000000003", "timestamp": now.Format(time.RFC3339Nano),
			"tenant_id": tenant, "service": "auth-api", "level": "warn",
			"message": "token nearing expiry", "_retention_days": 30,
		},
	}
	if err := c.Insert(ctx, "logs", rows); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Count is immediately queryable (synchronous single-node insert).
	out, err := c.Query(ctx, fmt.Sprintf(
		"SELECT count() AS c FROM logs WHERE tenant_id = '%s'", tenant))
	if err != nil {
		t.Fatalf("query count: %v", err)
	}
	if len(out) != 1 || fmt.Sprint(out[0]["c"]) != "3" {
		t.Fatalf("expected count 3, got %v", out)
	}

	// Full-text SEARCH path: token bloom-filter index over message.
	hits, err := c.Query(ctx, fmt.Sprintf(
		"SELECT service, level FROM logs WHERE tenant_id = '%s' AND hasToken(message, 'payment')", tenant))
	if err != nil {
		t.Fatalf("query search: %v", err)
	}
	if len(hits) != 1 || hits[0]["service"] != "checkout" || hits[0]["level"] != "error" {
		t.Fatalf("expected 1 checkout/error hit, got %v", hits)
	}
}
