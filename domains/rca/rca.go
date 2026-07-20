// Package rca is the Phase-1 RCA structural retrieval layer (PRD Module 11.1),
// modelled on Meta's retrieve-then-rank architecture: a heuristic retriever
// narrows candidates using STRUCTURAL signals — deploy proximity (Module 15),
// dependency-graph proximity (Module 10), and correlated errors — before any
// generative step. It ranks by structure and shows the evidence (queries/counts)
// behind every candidate (Dash0 Agent0 "show your work" transparency). The
// confidence-gated learned ranker (11.2) is deliberately Phase 2 — this layer
// ships first so every signal already feeds it.
package rca

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/qeetgroup/qeet-logs-server/domains/query"
	"github.com/qeetgroup/qeet-logs-server/domains/topology"
	"github.com/qeetgroup/qeet-logs-server/platform/clickhouse"
)

// Candidate is one structurally-retrieved potential root cause.
type Candidate struct {
	Type     string         `json:"type"` // deploy | dependency
	Subject  string         `json:"subject"`
	Score    float64        `json:"score"` // structural rank score [0,1]
	Reason   string         `json:"reason"`
	Evidence map[string]any `json:"evidence"`
}

// Result is the ranked candidate set for a service.
type Result struct {
	Service       string      `json:"service"`
	WindowSeconds int64       `json:"window_seconds"`
	Candidates    []Candidate `json:"candidates"`
}

// RetrieveForService narrows and ranks root-cause candidates for a service over
// the trailing window.
func RetrieveForService(ctx context.Context, ch *clickhouse.Client, tenant, service string, windowSeconds int64) (*Result, error) {
	if windowSeconds <= 0 {
		windowSeconds = 3600
	}
	tq := query.QuoteLiteral(tenant)
	sq := query.QuoteLiteral(service)
	var cands []Candidate

	// 1) Deploy proximity — the strongest structural signal. A change on this
	//    service inside the window is a prime suspect, scored by recency.
	deploySQL := fmt.Sprintf(`SELECT toString(timestamp) AS ts, title, kind, git_sha, deploy_id, pr_number,
		dateDiff('second', timestamp, now()) AS age
		FROM change_events WHERE tenant_id = %s AND service = %s
		  AND timestamp > now() - INTERVAL %d SECOND
		ORDER BY timestamp DESC LIMIT 10`, tq, sq, windowSeconds)
	deployRows, err := ch.Query(ctx, deploySQL)
	if err != nil {
		return nil, fmt.Errorf("rca deploys: %w", err)
	}
	for _, r := range deployRows {
		age := f64(r["age"])
		// recency decay: a change right before now scores ~0.9, older decays.
		score := 0.9 * math.Exp(-age/float64(windowSeconds))
		cands = append(cands, Candidate{
			Type:    "deploy",
			Subject: fmt.Sprintf("%v %v", r["kind"], r["title"]),
			Score:   round2(score),
			Reason:  fmt.Sprintf("%s change on %s ~%.0fs before now", str(r["kind"]), service, age),
			Evidence: map[string]any{
				"git_sha": r["git_sha"], "deploy_id": r["deploy_id"],
				"pr_number": r["pr_number"], "timestamp": r["ts"],
			},
		})
	}

	// 2) Dependency-graph proximity — downstream services this one calls that are
	//    themselves erroring are likely upstream causes of its failure.
	g, err := topology.Derive(ctx, ch, tenant, windowSeconds)
	if err != nil {
		return nil, fmt.Errorf("rca topology: %w", err)
	}
	for _, e := range g.Edges {
		if e.Caller != service || e.Errors == 0 {
			continue
		}
		errRate := 0.0
		if e.Calls > 0 {
			errRate = float64(e.Errors) / float64(e.Calls)
		}
		score := 0.4 + 0.5*math.Min(errRate, 1)
		cands = append(cands, Candidate{
			Type:    "dependency",
			Subject: e.Callee,
			Score:   round2(score),
			Reason:  fmt.Sprintf("downstream dependency %s erroring (%d/%d calls)", e.Callee, e.Errors, e.Calls),
			Evidence: map[string]any{
				"calls": e.Calls, "errors": e.Errors, "p95_ms": e.P95Ms,
			},
		})
	}

	sort.SliceStable(cands, func(i, j int) bool { return cands[i].Score > cands[j].Score })
	if len(cands) > 10 {
		cands = cands[:10]
	}
	return &Result{Service: service, WindowSeconds: windowSeconds, Candidates: cands}, nil
}

func round2(x float64) float64 { return math.Round(x*100) / 100 }

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
	case string:
		var n float64
		fmt.Sscanf(x, "%g", &n)
		return n
	default:
		return 0
	}
}
