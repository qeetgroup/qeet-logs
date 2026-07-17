// Package anomaly is the Phase-1 "Tier 1" baseline anomaly scorer (PRD Module 14
// roadmap slice — non-predictive, feeds the alert-correlation engine). It flags
// a service whose current error rate is a statistical outlier vs its own rolling
// baseline, and deliberately WITHHOLDS a score for services without enough
// history rather than guessing (Module 14 edge case). Heavier model tiers
// (ONNX-based) are a Phase-2 follow-up; this is the always-on statistical floor.
package anomaly

import (
	"context"
	"fmt"
	"math"

	"github.com/qeetgroup/qeet-logs/platform/clickhouse"
)

// Anomaly is a scored deviation for one (tenant, service).
type Anomaly struct {
	TenantID string
	Service  string
	Score    float64 // [0,1]
	Current  float64
	Mean     float64
	Std      float64
}

// Score maps a current value against a rolling baseline to [0,1]. Only positive
// deviations (a spike above the mean) are anomalies; a drop or no-change scores
// 0. Std is floored at √mean (the Poisson approximation for count/rate data) so
// a spike out of a perfectly flat baseline is still detected rather than
// silently ignored. z=6 → 1.0.
func Score(current, mean, std float64) float64 {
	es := math.Max(std, math.Sqrt(math.Max(mean, 1)))
	z := (current - mean) / es
	if z <= 0 {
		return 0
	}
	return clamp01(z / 6.0)
}

// Sweep scores every (tenant, service) whose recent error rate is an outlier
// vs the prior `windows` windows. Services with < 4 baseline windows are
// withheld. Returns only anomalies scoring >= minScore.
func Sweep(ctx context.Context, ch *clickhouse.Client, windowSec, windows int, minScore float64) ([]Anomaly, error) {
	if windowSec <= 0 {
		windowSec = 300
	}
	if windows < 4 {
		windows = 12
	}

	// Current window: error count per (tenant, service).
	curSQL := fmt.Sprintf(`SELECT tenant_id, service, count() AS n FROM logs
		WHERE level IN ('error','fatal') AND timestamp > now() - INTERVAL %d SECOND
		GROUP BY tenant_id, service`, windowSec)
	curRows, err := ch.Query(ctx, curSQL)
	if err != nil {
		return nil, fmt.Errorf("anomaly current: %w", err)
	}
	current := map[string]float64{}
	for _, r := range curRows {
		current[key(str(r["tenant_id"]), str(r["service"]))] = toF64(r["n"])
	}

	// Baseline buckets per (tenant, service) over the prior windows.
	baseSQL := fmt.Sprintf(`SELECT tenant_id, service, toStartOfInterval(timestamp, INTERVAL %d SECOND) AS b, count() AS n
		FROM logs WHERE level IN ('error','fatal')
		  AND timestamp > now() - INTERVAL %d SECOND AND timestamp <= now() - INTERVAL %d SECOND
		GROUP BY tenant_id, service, b`, windowSec, (windows+1)*windowSec, windowSec)
	baseRows, err := ch.Query(ctx, baseSQL)
	if err != nil {
		return nil, fmt.Errorf("anomaly baseline: %w", err)
	}
	buckets := map[string][]float64{}
	for _, r := range baseRows {
		k := key(str(r["tenant_id"]), str(r["service"]))
		buckets[k] = append(buckets[k], toF64(r["n"]))
	}

	var out []Anomaly
	for k, cur := range current {
		bs := buckets[k]
		if len(bs) < 4 {
			continue // withhold: insufficient history
		}
		mean, std := meanStd(bs)
		score := Score(cur, mean, std)
		if score < minScore {
			continue
		}
		tid, svc := split(k)
		out = append(out, Anomaly{TenantID: tid, Service: svc, Score: score, Current: cur, Mean: mean, Std: std})
	}
	return out, nil
}

func meanStd(xs []float64) (float64, float64) {
	mean := 0.0
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))
	v := 0.0
	for _, x := range xs {
		v += (x - mean) * (x - mean)
	}
	v /= float64(len(xs))
	return mean, math.Sqrt(v)
}

func clamp01(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 1 {
		return 1
	}
	return x
}

func key(t, s string) string { return t + "\x00" + s }
func split(k string) (string, string) {
	for i := 0; i < len(k); i++ {
		if k[i] == 0 {
			return k[:i], k[i+1:]
		}
	}
	return k, ""
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
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
