// Package deploy is the Phase-2 Deployment Intelligence layer (PRD Module 15).
// It turns the raw change-event stream (Module 15.1, shipped in G8) into ranked
// "most-likely-cause" scoring for a service window: each recent deploy / flag /
// config / rollback is scored by recency, change-type weight, and — the key
// Phase-2 signal — the measured error-rate delta before vs after the change
// (15.3 deploy/flag health correlation). The top deploy culprit that degraded
// health yields a one-click rollback suggestion to the prior deploy (15.4).
//
// Pure Go over ClickHouse `change_events` + `logs`; no ONNX/LLM. This is the #1
// structural signal the Phase-2 RCA learned ranker (Module 11.2) consumes, so it
// ships first — every downstream AI step gets a better input on day one.
package deploy

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/qeetgroup/qeet-logs/domains/query"
	"github.com/qeetgroup/qeet-logs/platform/clickhouse"
)

// changeTypeWeight ranks how likely each change kind is to cause a regression.
var changeTypeWeight = map[string]float64{
	"deploy":   1.00,
	"rollback": 0.95,
	"config":   0.75,
	"flag":     0.65,
}

// HealthDelta is the error-rate comparison across a change boundary (15.3).
type HealthDelta struct {
	BeforeTotal  int64   `json:"before_total"`
	BeforeErrors int64   `json:"before_errors"`
	AfterTotal   int64   `json:"after_total"`
	AfterErrors  int64   `json:"after_errors"`
	BeforeRate   float64 `json:"before_rate"`
	AfterRate    float64 `json:"after_rate"`
	Delta        float64 `json:"delta"` // after_rate - before_rate
	Degraded     bool    `json:"degraded"`
}

// Rollback is a one-click rollback suggestion for a degraded deploy (15.4).
type Rollback struct {
	Suggested      bool   `json:"suggested"`
	TargetGitSHA   string `json:"target_git_sha,omitempty"`
	TargetDeployID string `json:"target_deploy_id,omitempty"`
	Reason         string `json:"reason,omitempty"`
}

// Culprit is one scored change event.
type Culprit struct {
	ID         string       `json:"id"`
	Kind       string       `json:"kind"`
	Title      string       `json:"title"`
	Service    string       `json:"service"`
	GitSHA     string       `json:"git_sha,omitempty"`
	DeployID   string       `json:"deploy_id,omitempty"`
	PRNumber   string       `json:"pr_number,omitempty"`
	FlagKey    string       `json:"flag_key,omitempty"`
	AgeSeconds int64        `json:"age_seconds"`
	Timestamp  string       `json:"timestamp"`
	Score      float64      `json:"score"` // culprit likelihood [0,1]
	Reason     string       `json:"reason"`
	Health     *HealthDelta `json:"health,omitempty"`
	Rollback   *Rollback    `json:"rollback,omitempty"`
}

// CulpritResult is the ranked change set for a service window.
type CulpritResult struct {
	Service       string    `json:"service"`
	WindowSeconds int64     `json:"window_seconds"`
	Culprits      []Culprit `json:"culprits"`
}

const maxCulprits = 15

// RankCulprits scores recent change events for a service over the trailing
// window, highest-likelihood first (Module 15.2).
func RankCulprits(ctx context.Context, ch *clickhouse.Client, tenant, service string, windowSeconds int64) (*CulpritResult, error) {
	if windowSeconds <= 0 {
		windowSeconds = 3600
	}
	tq := query.QuoteLiteral(tenant)
	sq := query.QuoteLiteral(service)

	sql := fmt.Sprintf(`SELECT id, toString(timestamp) AS ts, kind, title, git_sha, deploy_id, pr_number, flag_key,
		dateDiff('second', timestamp, now()) AS age
		FROM change_events WHERE tenant_id = %s AND service = %s
		  AND timestamp > now() - INTERVAL %d SECOND
		ORDER BY timestamp DESC LIMIT %d`, tq, sq, windowSeconds, maxCulprits)
	rows, err := ch.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("deploy changes: %w", err)
	}

	// Half-window for the health comparison: bounded so a very recent change
	// still gets a meaningful (if short) "after" window.
	hw := windowSeconds / 4
	if hw < 300 {
		hw = 300
	}

	culprits := make([]Culprit, 0, len(rows))
	for _, r := range rows {
		age := int64(f64(r["age"]))
		if age < 0 {
			age = 0
		}
		kind := str(r["kind"])
		c := Culprit{
			ID:         str(r["id"]),
			Kind:       kind,
			Title:      str(r["title"]),
			Service:    service,
			GitSHA:     str(r["git_sha"]),
			DeployID:   str(r["deploy_id"]),
			PRNumber:   str(r["pr_number"]),
			FlagKey:    str(r["flag_key"]),
			AgeSeconds: age,
			Timestamp:  str(r["ts"]),
		}

		health, herr := deployHealth(ctx, ch, tq, sq, age, hw)
		if herr == nil {
			c.Health = health
		}
		c.Score, c.Reason = scoreCulprit(kind, age, windowSeconds, health)
		culprits = append(culprits, c)
	}

	sort.SliceStable(culprits, func(i, j int) bool { return culprits[i].Score > culprits[j].Score })

	// Rollback suggestion for the top culprit if it's a degraded deploy (15.4).
	if len(culprits) > 0 {
		top := &culprits[0]
		if top.Kind == "deploy" && top.Health != nil && top.Health.Degraded {
			if rb := priorDeployRollback(ctx, ch, tq, sq, top.AgeSeconds); rb != nil {
				top.Rollback = rb
			}
		}
	}

	return &CulpritResult{Service: service, WindowSeconds: windowSeconds, Culprits: culprits}, nil
}

// scoreCulprit combines recency, change-type weight, and post-change error-rate
// degradation into a [0,1] likelihood, with a human-readable reason.
func scoreCulprit(kind string, age, window int64, h *HealthDelta) (float64, string) {
	recency := math.Exp(-float64(age) / float64(window))
	weight, ok := changeTypeWeight[kind]
	if !ok {
		weight = 0.70
	}
	structural := recency * weight

	if h == nil || h.AfterTotal == 0 {
		// No telemetry to correlate — structural signal only.
		return round2(structural), fmt.Sprintf("%s ~%ds ago (no post-change telemetry to correlate)", kindLabel(kind), age)
	}
	// A 20%+ error-rate rise saturates the health signal.
	healthScore := clamp(h.Delta*5, 0, 1)
	score := 0.55*structural + 0.45*healthScore
	if h.Degraded {
		return round2(score), fmt.Sprintf("%s ~%ds ago; error rate rose %.1f%%→%.1f%% after the change",
			kindLabel(kind), age, h.BeforeRate*100, h.AfterRate*100)
	}
	return round2(score), fmt.Sprintf("%s ~%ds ago; error rate stable (%.1f%%→%.1f%%)",
		kindLabel(kind), age, h.BeforeRate*100, h.AfterRate*100)
}

// deployHealth compares the error rate in the window before vs after a change,
// using the change's age (seconds before now) and a half-window hw.
func deployHealth(ctx context.Context, ch *clickhouse.Client, tq, sq string, age, hw int64) (*HealthDelta, error) {
	afterEnd := age - hw // seconds before now; clamp at 0 (== now)
	if afterEnd < 0 {
		afterEnd = 0
	}
	sql := fmt.Sprintf(`SELECT
		countIf(timestamp BETWEEN now() - INTERVAL %d SECOND AND now() - INTERVAL %d SECOND) AS before_total,
		countIf(level IN ('error','fatal') AND timestamp BETWEEN now() - INTERVAL %d SECOND AND now() - INTERVAL %d SECOND) AS before_err,
		countIf(timestamp BETWEEN now() - INTERVAL %d SECOND AND now() - INTERVAL %d SECOND) AS after_total,
		countIf(level IN ('error','fatal') AND timestamp BETWEEN now() - INTERVAL %d SECOND AND now() - INTERVAL %d SECOND) AS after_err
		FROM logs
		WHERE tenant_id = %s AND service = %s
		  AND timestamp BETWEEN now() - INTERVAL %d SECOND AND now() - INTERVAL %d SECOND`,
		age+hw, age, // before window
		age+hw, age,
		age, afterEnd, // after window
		age, afterEnd,
		tq, sq,
		age+hw, afterEnd) // outer scan bound
	rows, err := ch.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("no health row")
	}
	r := rows[0]
	return newHealthDelta(
		int64(f64(r["before_total"])), int64(f64(r["before_err"])),
		int64(f64(r["after_total"])), int64(f64(r["after_err"])),
	), nil
}

// newHealthDelta computes before/after error rates and the degradation verdict
// from raw counts (pure; unit-tested independently of ClickHouse).
func newHealthDelta(beforeTotal, beforeErr, afterTotal, afterErr int64) *HealthDelta {
	h := &HealthDelta{
		BeforeTotal:  beforeTotal,
		BeforeErrors: beforeErr,
		AfterTotal:   afterTotal,
		AfterErrors:  afterErr,
	}
	if h.BeforeTotal > 0 {
		h.BeforeRate = float64(h.BeforeErrors) / float64(h.BeforeTotal)
	}
	if h.AfterTotal > 0 {
		h.AfterRate = float64(h.AfterErrors) / float64(h.AfterTotal)
	}
	h.Delta = round4(h.AfterRate - h.BeforeRate)
	h.BeforeRate = round4(h.BeforeRate)
	h.AfterRate = round4(h.AfterRate)
	// Degraded = a meaningful absolute rise in error rate after the change.
	h.Degraded = h.AfterTotal > 0 && h.Delta > 0.02
	return h
}

// priorDeployRollback finds the deploy immediately preceding the culprit on the
// same service and suggests rolling back to it.
func priorDeployRollback(ctx context.Context, ch *clickhouse.Client, tq, sq string, culpritAge int64) *Rollback {
	sql := fmt.Sprintf(`SELECT git_sha, deploy_id FROM change_events
		WHERE tenant_id = %s AND service = %s AND kind = 'deploy'
		  AND timestamp < now() - INTERVAL %d SECOND
		ORDER BY timestamp DESC LIMIT 1`, tq, sq, culpritAge)
	rows, err := ch.Query(ctx, sql)
	if err != nil || len(rows) == 0 {
		return nil
	}
	git, dep := str(rows[0]["git_sha"]), str(rows[0]["deploy_id"])
	if git == "" && dep == "" {
		return nil
	}
	return &Rollback{
		Suggested:      true,
		TargetGitSHA:   git,
		TargetDeployID: dep,
		Reason:         "error rate degraded after this deploy; roll back to the preceding deploy",
	}
}

func kindLabel(kind string) string {
	if kind == "" {
		return "change"
	}
	return kind + " change"
}

func clamp(x, lo, hi float64) float64 { return math.Max(lo, math.Min(hi, x)) }
func round2(x float64) float64        { return math.Round(x*100) / 100 }
func round4(x float64) float64        { return math.Round(x*10000) / 10000 }

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func f64(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int64:
		return float64(x)
	case string:
		var n float64
		fmt.Sscanf(x, "%g", &n)
		return n
	default:
		return 0
	}
}
