package handler

import (
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs-server/domains/changesource"
	apimw "github.com/qeetgroup/qeet-logs-server/platform/api/middleware"
	"github.com/qeetgroup/qeet-logs-server/platform/clickhouse"
)

// ChangeConnector ingests a provider webhook (GitHub / GitLab / LaunchDarkly),
// translates it into the normalized change-event contract, and stores it in
// change_events — the same substrate the Deployment Intelligence layer (Module
// 15) and the timeline/RCA read (PRD 30.4 inbound / 31.3 CI-CD / 31.4 flags).
// POST /v1/changes/{provider}. Requires logs:ingest.
func ChangeConnector(ch *clickhouse.Client, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:ingest") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "requires logs:ingest scope"})
			return
		}
		provider := chi.URLParam(r, "provider")
		body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MiB cap
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "read body: " + err.Error()})
			return
		}
		// GitHub carries the event type in a header; other providers self-describe.
		eventType := r.Header.Get("X-GitHub-Event")

		events, err := changesource.Parse(provider, eventType, body)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if len(events) == 0 {
			// A recognised but non-actionable payload (e.g. a failed workflow run):
			// acknowledge without recording, so the provider doesn't retry.
			writeJSON(w, http.StatusOK, map[string]any{"accepted": 0, "provider": provider})
			return
		}

		tenant := apimw.TenantID(ctx)
		now := time.Now().UTC().Format(time.RFC3339Nano)
		rows := make([]map[string]any, 0, len(events))
		ids := make([]string, 0, len(events))
		for _, e := range events {
			id := newEventID()
			ids = append(ids, id)
			rows = append(rows, map[string]any{
				"id":          id,
				"timestamp":   now,
				"tenant_id":   tenant,
				"service":     e.Service,
				"environment": e.Environment,
				"kind":        e.Kind,
				"title":       e.Title,
				"git_sha":     e.GitSHA,
				"deploy_id":   e.DeployID,
				"pr_number":   e.PRNumber,
				"flag_key":    e.FlagKey,
				"config_diff": e.ConfigDiff,
				"author":      e.Author,
				"metadata":    "{}",
			})
		}
		if err := ch.Insert(ctx, "change_events", rows); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "insert failed: " + err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"accepted": len(rows), "provider": provider, "ids": ids})
	}
}
