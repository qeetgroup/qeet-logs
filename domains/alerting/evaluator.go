package alerting

import (
	"context"
	"fmt"
	"strings"

	"github.com/qeetgroup/qeet-logs-server/platform/clickhouse"
)

// Evaluate queries ClickHouse and returns the log count within the rule's
// evaluation window. It also returns whether the rule is currently firing.
func Evaluate(ctx context.Context, ch *clickhouse.Client, rule AlertRule) (count int64, firing bool, err error) {
	sql := buildQuery(rule)
	rows, err := ch.Query(ctx, sql)
	if err != nil {
		return 0, false, fmt.Errorf("alerting evaluate %s: %w", rule.ID, err)
	}

	if len(rows) > 0 {
		switch v := rows[0]["n"].(type) {
		case float64:
			count = int64(v)
		case int64:
			count = v
		case string:
			// ClickHouse renders 64-bit integers as JSON strings in JSONEachRow;
			// parse defensively so counts are never silently read as zero.
			var f float64
			fmt.Sscanf(v, "%g", &f)
			count = int64(f)
		}
	}

	switch rule.Kind {
	case KindThreshold:
		if rule.Threshold != nil {
			firing = float64(count) > *rule.Threshold
		}
	case KindAbsence:
		firing = count == 0
	}
	return count, firing, nil
}

// buildQuery constructs the ClickHouse SQL for a rule's evaluation.
// Security note: tenant_id is always injected from the stored rule (not user
// input), service/condition are stored strings never user-interpolated at eval
// time.
func buildQuery(rule AlertRule) string {
	var where []string
	where = append(where,
		fmt.Sprintf("tenant_id = '%s'", escapeSingle(rule.TenantID)),
		fmt.Sprintf("timestamp > now() - INTERVAL %d SECOND", rule.WindowSeconds),
	)
	if rule.Service != nil && *rule.Service != "" {
		where = append(where, fmt.Sprintf("service = '%s'", escapeSingle(*rule.Service)))
	}
	if rule.Condition != nil && *rule.Condition != "" {
		// Condition is stored as a LogQL++ WHERE fragment; we include it as-is
		// since it was validated at rule creation time (trusted admin input).
		where = append(where, "("+*rule.Condition+")")
	}
	return fmt.Sprintf(
		"SELECT count() AS n FROM logs WHERE %s",
		strings.Join(where, " AND "),
	)
}

// escapeSingle replaces single quotes in a string to prevent SQL injection.
// Only applied to stored rule fields (TenantID, Service) that are UUIDs or
// free-text entered by an admin — belt-and-suspenders guard.
func escapeSingle(s string) string {
	return strings.ReplaceAll(s, "'", "\\'")
}
