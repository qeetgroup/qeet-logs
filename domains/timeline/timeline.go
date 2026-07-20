// Package timeline builds the Unified Investigation Timeline (PRD Module 09):
// ONE chronological feed interleaving logs, trace spans, and deploy/change
// events that share a trace or incident window — the primary investigation
// surface, replacing the swivel-chair between separate log/trace/deploy tools
// (Gap 9). Everything is one query over the shared columnar store.
package timeline

import (
	"context"
	"fmt"
	"sort"

	"github.com/qeetgroup/qeet-logs-server/domains/query"
	"github.com/qeetgroup/qeet-logs-server/platform/clickhouse"
)

// Event is one row in the unified feed.
type Event struct {
	Type      string         `json:"type"` // log | span | deploy
	Timestamp string         `json:"timestamp"`
	Service   string         `json:"service"`
	Severity  string         `json:"severity"` // level / status_code / kind
	Title     string         `json:"title"`
	TraceID   string         `json:"trace_id,omitempty"`
	SpanID    string         `json:"span_id,omitempty"`
	Fields    map[string]any `json:"fields,omitempty"`
}

// Params scopes the timeline. TraceID (when set) yields the full cross-signal
// story of one request; otherwise it's a service/time window feed.
type Params struct {
	TenantID     string
	TraceID      string
	Service      string
	SinceSeconds int64
	Limit        int
	// IncludeInfo pulls in info/debug logs too (default: warn+ only, to keep the
	// incident feed readable — Gap 9 "unreadable wall of noise").
	IncludeInfo bool
}

// Build assembles the merged, time-sorted timeline.
func Build(ctx context.Context, ch *clickhouse.Client, p Params) ([]Event, error) {
	if p.SinceSeconds <= 0 {
		p.SinceSeconds = 3600
	}
	if p.Limit <= 0 || p.Limit > 1000 {
		p.Limit = 200
	}
	tq := query.QuoteLiteral(p.TenantID)
	var whereScope string
	if p.TraceID != "" {
		whereScope = "trace_id = " + query.QuoteLiteral(p.TraceID)
	} else {
		whereScope = fmt.Sprintf("timestamp >= now() - INTERVAL %d SECOND", p.SinceSeconds)
		if p.Service != "" {
			whereScope += " AND service = " + query.QuoteLiteral(p.Service)
		}
	}

	var events []Event

	// Logs.
	logLevel := "level IN ('warn','error','fatal')"
	if p.IncludeInfo || p.TraceID != "" {
		logLevel = "1" // everything for a specific trace, or when explicitly asked
	}
	logSQL := fmt.Sprintf(`SELECT toString(timestamp) AS ts, service, level, message, trace_id, span_id
		FROM logs WHERE tenant_id = %s AND %s AND (%s) ORDER BY timestamp DESC LIMIT %d`,
		tq, whereScope, logLevel, p.Limit)
	logRows, err := ch.Query(ctx, logSQL)
	if err != nil {
		return nil, fmt.Errorf("timeline logs: %w", err)
	}
	for _, r := range logRows {
		events = append(events, Event{
			Type: "log", Timestamp: str(r["ts"]), Service: str(r["service"]),
			Severity: str(r["level"]), Title: str(r["message"]),
			TraceID: str(r["trace_id"]), SpanID: str(r["span_id"]),
		})
	}

	// Spans.
	spanSQL := fmt.Sprintf(`SELECT toString(timestamp) AS ts, service, name, kind, status_code, duration_ns, trace_id, span_id
		FROM traces WHERE tenant_id = %s AND %s ORDER BY timestamp DESC LIMIT %d`,
		tq, whereScope, p.Limit)
	spanRows, err := ch.Query(ctx, spanSQL)
	if err != nil {
		return nil, fmt.Errorf("timeline spans: %w", err)
	}
	for _, r := range spanRows {
		events = append(events, Event{
			Type: "span", Timestamp: str(r["ts"]), Service: str(r["service"]),
			Severity: str(r["status_code"]), Title: str(r["name"]),
			TraceID: str(r["trace_id"]), SpanID: str(r["span_id"]),
			Fields: map[string]any{"kind": r["kind"], "duration_ns": r["duration_ns"]},
		})
	}

	// Deploy/change events (window-scoped; skipped when focusing a single trace).
	if p.TraceID == "" {
		changeSQL := fmt.Sprintf(`SELECT toString(timestamp) AS ts, service, kind, title, git_sha, deploy_id, pr_number, author
			FROM change_events WHERE tenant_id = %s AND %s ORDER BY timestamp DESC LIMIT %d`,
			tq, whereScope, p.Limit)
		changeRows, err := ch.Query(ctx, changeSQL)
		if err != nil {
			return nil, fmt.Errorf("timeline changes: %w", err)
		}
		for _, r := range changeRows {
			events = append(events, Event{
				Type: "deploy", Timestamp: str(r["ts"]), Service: str(r["service"]),
				Severity: str(r["kind"]), Title: str(r["title"]),
				Fields: map[string]any{
					"git_sha": r["git_sha"], "deploy_id": r["deploy_id"],
					"pr_number": r["pr_number"], "author": r["author"],
				},
			})
		}
	}

	// One chronological feed (ascending — request/incident flow reads top-down).
	sort.SliceStable(events, func(i, j int) bool { return events[i].Timestamp < events[j].Timestamp })
	if len(events) > p.Limit {
		events = events[len(events)-p.Limit:]
	}
	return events, nil
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
