package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	apimw "github.com/qeetgroup/qeet-logs/platform/api/middleware"
)

// webhookView is the API representation of a webhook endpoint. The signing
// secret is never returned (write-only) — only whether one is set.
type webhookView struct {
	ID          string    `json:"id"`
	URL         string    `json:"url"`
	Events      []string  `json:"events"`
	Active      bool      `json:"active"`
	Description string    `json:"description"`
	HasSecret   bool      `json:"has_secret"`
	CreatedAt   time.Time `json:"created_at"`
}

// CreateWebhook registers an outbound webhook endpoint (PRD Module 30.4).
// POST /v1/admin/webhooks {url, events?, secret?, description?}. logs:admin.
func CreateWebhook(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tenant := apimw.TenantID(ctx)
		var body struct {
			URL         string   `json:"url"`
			Events      []string `json:"events"`
			Secret      string   `json:"secret"`
			Description string   `json:"description"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		if body.URL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "url is required"})
			return
		}
		if body.Events == nil {
			body.Events = []string{}
		}
		var id string
		err := pool.QueryRow(ctx,
			`INSERT INTO webhook_endpoints (tenant_id, url, secret, events, description)
			 VALUES ($1::uuid, $2, $3, $4, $5) RETURNING id::text`,
			tenant, body.URL, body.Secret, body.Events, body.Description).Scan(&id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": id, "url": body.URL, "events": body.Events})
	}
}

// ListWebhooks returns the tenant's webhook endpoints (secrets redacted).
func ListWebhooks(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tenant := apimw.TenantID(ctx)
		rows, err := pool.Query(ctx,
			`SELECT id::text, url, events, active, description, secret <> '', created_at
			 FROM webhook_endpoints WHERE tenant_id = $1::uuid ORDER BY created_at DESC`, tenant)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()
		out := []webhookView{}
		for rows.Next() {
			var v webhookView
			if err := rows.Scan(&v.ID, &v.URL, &v.Events, &v.Active, &v.Description, &v.HasSecret, &v.CreatedAt); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			out = append(out, v)
		}
		writeJSON(w, http.StatusOK, map[string]any{"count": len(out), "webhooks": out})
	}
}

// DeleteWebhook removes a webhook endpoint. DELETE /v1/admin/webhooks/{id}.
func DeleteWebhook(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tenant := apimw.TenantID(ctx)
		id := chi.URLParam(r, "id")
		tag, err := pool.Exec(ctx,
			`DELETE FROM webhook_endpoints WHERE id = $1::uuid AND tenant_id = $2::uuid`, id, tenant)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if tag.RowsAffected() == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "webhook not found"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
