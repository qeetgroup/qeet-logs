package handler

import (
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs/domains/timeline"
	apimw "github.com/qeetgroup/qeet-logs/platform/api/middleware"
	"github.com/qeetgroup/qeet-logs/platform/clickhouse"
)

// Timeline serves the Unified Investigation Timeline (PRD Module 09): one
// chronological cross-signal feed. ?trace_id=T gives the full story of a single
// request; otherwise ?service=&since=&include_info= scope a window feed.
func Timeline(ch *clickhouse.Client, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:read", "logs:query") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "requires logs:read or logs:query scope"})
			return
		}
		q := r.URL.Query()
		p := timeline.Params{
			TenantID:    apimw.TenantID(ctx),
			TraceID:     q.Get("trace_id"),
			Service:     q.Get("service"),
			IncludeInfo: q.Get("include_info") == "true",
		}
		if s := q.Get("since"); s != "" {
			if v, err := strconv.ParseInt(s, 10, 64); err == nil {
				p.SinceSeconds = v
			}
		}
		if s := q.Get("limit"); s != "" {
			if v, err := strconv.Atoi(s); err == nil {
				p.Limit = v
			}
		}
		events, err := timeline.Build(ctx, ch, p)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"count": len(events), "events": events})
	}
}
