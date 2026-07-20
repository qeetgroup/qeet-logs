package alerting

import (
	"context"
	"fmt"
	"math"

	"github.com/qeetgroup/qeet-logs-server/platform/clickhouse"
)

// ComputeBaseline returns the rolling mean/std of per-window log counts over the
// prior `windows` windows (excluding the current one), for adaptive detection
// (PRD Module 13.4). Returns nil when there is too little history to be
// trustworthy — the engine then falls back to the static threshold (cold start).
func ComputeBaseline(ctx context.Context, ch *clickhouse.Client, rule AlertRule, windows int) (*Baseline, error) {
	if rule.Kind != KindThreshold || windows < 4 {
		return nil, nil
	}
	w := rule.WindowSeconds
	where := []string{
		fmt.Sprintf("tenant_id = '%s'", escapeSingle(rule.TenantID)),
		fmt.Sprintf("timestamp > now() - INTERVAL %d SECOND", (windows+1)*w),
		fmt.Sprintf("timestamp <= now() - INTERVAL %d SECOND", w),
	}
	if rule.Service != nil && *rule.Service != "" {
		where = append(where, fmt.Sprintf("service = '%s'", escapeSingle(*rule.Service)))
	}
	if rule.Condition != nil && *rule.Condition != "" {
		where = append(where, "("+*rule.Condition+")")
	}
	sql := fmt.Sprintf(
		`SELECT toStartOfInterval(timestamp, INTERVAL %d SECOND) AS b, count() AS n
		 FROM logs WHERE %s GROUP BY b ORDER BY b`,
		w, joinAnd(where))

	rows, err := ch.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("baseline: %w", err)
	}
	if len(rows) < 4 {
		return nil, nil // insufficient history
	}
	counts := make([]float64, 0, len(rows))
	for _, r := range rows {
		counts = append(counts, toF64(r["n"]))
	}
	mean := 0.0
	for _, c := range counts {
		mean += c
	}
	mean /= float64(len(counts))
	variance := 0.0
	for _, c := range counts {
		variance += (c - mean) * (c - mean)
	}
	variance /= float64(len(counts))
	return &Baseline{Mean: mean, Std: math.Sqrt(variance), Windows: len(counts)}, nil
}

func joinAnd(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += " AND "
		}
		out += p
	}
	return out
}

func toF64(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		var n float64
		fmt.Sscanf(x, "%g", &n)
		return n
	default:
		return 0
	}
}
