// Package topology derives a service dependency graph (PRD Module 10) from the
// same columnar store as everything else — edges from cross-service parent/child
// trace spans, nodes from traces plus log-based service inference so the graph
// degrades gracefully in under-instrumented environments (Gap 11) rather than
// going blank where trace coverage is partial. Blast radius (who depends on a
// service) is the primary incident affordance, not a static architecture diagram.
package topology

import (
	"context"
	"fmt"
	"sort"

	"github.com/qeetgroup/qeet-logs-server/domains/query"
	"github.com/qeetgroup/qeet-logs-server/platform/clickhouse"
)

// Node is a service in the graph.
type Node struct {
	Service   string `json:"service"`
	Spans     uint64 `json:"spans"`
	Errors    uint64 `json:"errors"`
	LogCount  uint64 `json:"log_count"`
	HasTraces bool   `json:"has_traces"`
	HasLogs   bool   `json:"has_logs"`
	Coverage  string `json:"coverage"` // full | traces-only | logs-only (unknown topology)
}

// Edge is a directed caller→callee dependency derived from spans.
type Edge struct {
	Caller string  `json:"caller"`
	Callee string  `json:"callee"`
	Calls  uint64  `json:"calls"`
	Errors uint64  `json:"errors"`
	P95Ms  float64 `json:"p95_ms"`
}

// Graph is the derived topology over a time window.
type Graph struct {
	WindowSeconds int64  `json:"window_seconds"`
	Nodes         []Node `json:"nodes"`
	Edges         []Edge `json:"edges"`
	// Blast radius (present when a focus service is requested): the services
	// that call the focus service, transitively — i.e. who is affected if it fails.
	Focus       string   `json:"focus,omitempty"`
	BlastRadius []string `json:"blast_radius,omitempty"`
}

// Derive builds the full graph for a tenant over the trailing windowSeconds.
func Derive(ctx context.Context, ch *clickhouse.Client, tenant string, windowSeconds int64) (*Graph, error) {
	if windowSeconds <= 0 {
		windowSeconds = 3600
	}
	tq := query.QuoteLiteral(tenant)
	since := fmt.Sprintf("timestamp >= now() - INTERVAL %d SECOND", windowSeconds)

	// Edges: a child span whose parent lives in a different service is a
	// caller→callee dependency. Self-join within the same trace/tenant.
	edgeSQL := fmt.Sprintf(`SELECT p.service AS caller, c.service AS callee,
		count() AS calls,
		countIf(c.status_code = 'error') AS errors,
		quantile(0.95)(c.duration_ns) / 1e6 AS p95_ms
		FROM traces c
		INNER JOIN traces p ON c.tenant_id = p.tenant_id AND c.trace_id = p.trace_id AND c.parent_span_id = p.span_id
		WHERE c.tenant_id = %s AND c.%s AND p.service != c.service
		GROUP BY caller, callee`, tq, since)

	edgeRows, err := ch.Query(ctx, edgeSQL)
	if err != nil {
		return nil, fmt.Errorf("edge derivation: %w", err)
	}
	edges := make([]Edge, 0, len(edgeRows))
	for _, r := range edgeRows {
		edges = append(edges, Edge{
			Caller: str(r["caller"]), Callee: str(r["callee"]),
			Calls: u64(r["calls"]), Errors: u64(r["errors"]), P95Ms: f64(r["p95_ms"]),
		})
	}

	// Nodes from traces.
	nodes := map[string]*Node{}
	traceSQL := fmt.Sprintf(`SELECT service, count() AS spans, countIf(status_code='error') AS errors
		FROM traces WHERE tenant_id = %s AND %s GROUP BY service`, tq, since)
	traceRows, err := ch.Query(ctx, traceSQL)
	if err != nil {
		return nil, fmt.Errorf("trace nodes: %w", err)
	}
	for _, r := range traceRows {
		svc := str(r["service"])
		if svc == "" {
			continue
		}
		nodes[svc] = &Node{Service: svc, Spans: u64(r["spans"]), Errors: u64(r["errors"]), HasTraces: true}
	}

	// Log-based service inference: services that emit logs but may have no traces.
	logSQL := fmt.Sprintf(`SELECT service, count() AS log_count FROM logs
		WHERE tenant_id = %s AND %s GROUP BY service`, tq, since)
	logRows, err := ch.Query(ctx, logSQL)
	if err != nil {
		return nil, fmt.Errorf("log nodes: %w", err)
	}
	for _, r := range logRows {
		svc := str(r["service"])
		if svc == "" {
			continue
		}
		n := nodes[svc]
		if n == nil {
			n = &Node{Service: svc}
			nodes[svc] = n
		}
		n.LogCount = u64(r["log_count"])
		n.HasLogs = true
	}

	// Any service referenced only by an edge still becomes a node.
	for _, e := range edges {
		for _, svc := range []string{e.Caller, e.Callee} {
			if svc != "" && nodes[svc] == nil {
				nodes[svc] = &Node{Service: svc, HasTraces: true}
			}
		}
	}

	out := make([]Node, 0, len(nodes))
	for _, n := range nodes {
		n.Coverage = coverage(n)
		out = append(out, *n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Service < out[j].Service })

	return &Graph{WindowSeconds: windowSeconds, Nodes: out, Edges: edges}, nil
}

// FocusBlastRadius trims the graph to the focus service's connected neighbourhood
// and computes its blast radius — the transitive set of upstream callers that are
// affected if the focus service fails.
func (g *Graph) FocusBlastRadius(service string) {
	g.Focus = service
	// upstream[callee] = set of direct callers.
	callers := map[string][]string{}
	for _, e := range g.Edges {
		callers[e.Callee] = append(callers[e.Callee], e.Caller)
	}
	seen := map[string]bool{}
	var stack []string
	stack = append(stack, service)
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, up := range callers[cur] {
			if !seen[up] && up != service {
				seen[up] = true
				stack = append(stack, up)
			}
		}
	}
	radius := make([]string, 0, len(seen))
	for s := range seen {
		radius = append(radius, s)
	}
	sort.Strings(radius)
	g.BlastRadius = radius

	// Trim to the 1-hop neighbourhood for readability.
	keep := map[string]bool{service: true}
	for _, e := range g.Edges {
		if e.Caller == service || e.Callee == service {
			keep[e.Caller] = true
			keep[e.Callee] = true
		}
	}
	var edges []Edge
	for _, e := range g.Edges {
		if e.Caller == service || e.Callee == service {
			edges = append(edges, e)
		}
	}
	g.Edges = edges
	var nodes []Node
	for _, n := range g.Nodes {
		if keep[n.Service] {
			nodes = append(nodes, n)
		}
	}
	g.Nodes = nodes
}

// Neighbors returns the 1-hop services connected to service (as caller OR callee).
// Used by the alerting engine to merge topologically-proximate incidents.
func (g *Graph) Neighbors(service string) []string {
	seen := make(map[string]struct{})
	for _, e := range g.Edges {
		if e.Caller == service {
			seen[e.Callee] = struct{}{}
		} else if e.Callee == service {
			seen[e.Caller] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	return out
}

func coverage(n *Node) string {
	switch {
	case n.HasTraces && n.HasLogs:
		return "full"
	case n.HasTraces:
		return "traces-only"
	default:
		return "logs-only" // unknown topology — flagged, not omitted (Gap 11 edge case)
	}
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func u64(v any) uint64 {
	switch x := v.(type) {
	case float64:
		return uint64(x)
	case string:
		var n uint64
		fmt.Sscanf(x, "%d", &n)
		return n
	default:
		return 0
	}
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
