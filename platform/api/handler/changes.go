package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs-server/domains/query"
	apimw "github.com/qeetgroup/qeet-logs-server/platform/api/middleware"
	"github.com/qeetgroup/qeet-logs-server/platform/clickhouse"
)

// newEventID returns a random 128-bit hex id for a change event.
func newEventID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// CreateChange ingests a deploy/flag/config change event (PRD Module 15.1). Any
// CI/CD, feature-flag, or config tool can POST this simple contract; the event
// is stored in the same columnar store as logs/metrics/traces for timeline and
// RCA change-proximity correlation.
func CreateChange(ch *clickhouse.Client, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:ingest") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "requires logs:ingest scope"})
			return
		}
		var body struct {
			Service     string          `json:"service"`
			Environment string          `json:"environment"`
			Kind        string          `json:"kind"`
			Title       string          `json:"title"`
			GitSHA      string          `json:"git_sha"`
			DeployID    string          `json:"deploy_id"`
			PRNumber    string          `json:"pr_number"`
			FlagKey     string          `json:"flag_key"`
			ConfigDiff  string          `json:"config_diff"`
			Author      string          `json:"author"`
			Timestamp   *time.Time      `json:"timestamp"`
			Metadata    json.RawMessage `json:"metadata"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		kind := body.Kind
		if kind == "" {
			kind = "deploy"
		}
		ts := time.Now().UTC()
		if body.Timestamp != nil {
			ts = body.Timestamp.UTC()
		}
		metadata := "{}"
		if len(body.Metadata) > 0 {
			metadata = string(body.Metadata)
		}
		id := newEventID()
		row := map[string]any{
			"id":          id,
			"timestamp":   ts.Format(time.RFC3339Nano),
			"tenant_id":   apimw.TenantID(ctx),
			"service":     body.Service,
			"environment": body.Environment,
			"kind":        kind,
			"title":       body.Title,
			"git_sha":     body.GitSHA,
			"deploy_id":   body.DeployID,
			"pr_number":   body.PRNumber,
			"flag_key":    body.FlagKey,
			"config_diff": body.ConfigDiff,
			"author":      body.Author,
			"metadata":    metadata,
		}
		if err := ch.Insert(ctx, "change_events", []map[string]any{row}); err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "insert failed: " + err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": id, "kind": kind, "timestamp": ts})
	}
}

// ListChanges returns recent change events, optionally filtered by ?service=.
// Uses the LogQL++ compiler so the tenant predicate is injected from identity.
func ListChanges(ch *clickhouse.Client, pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:read", "logs:query") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "requires logs:read or logs:query scope"})
			return
		}
		tenant := apimw.TenantID(ctx)
		q := "SELECT * FROM change_events"
		if svc := r.URL.Query().Get("service"); svc != "" {
			q += " WHERE service = " + query.QuoteLiteral(svc)
		}
		q += " ORDER BY timestamp DESC LIMIT 200"
		compiled, err := query.Compile(q, tenant, queryOpts)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		runAndRespond(w, r, ch, pool, tenant, "changes", q, compiled)
	}
}
