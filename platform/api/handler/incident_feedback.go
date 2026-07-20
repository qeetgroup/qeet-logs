package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	apimw "github.com/qeetgroup/qeet-logs-server/platform/api/middleware"
)

// SubmitIncidentFeedback records an operator verdict on a resolved incident —
// "actionable" (true positive) or "noise" (false positive) — which feeds the
// alerter's continuous calibration of confidence (PRD Module 13.3).
// POST /v1/admin/incidents/{id}/feedback {verdict, note?}. Scope logs:admin.
func SubmitIncidentFeedback(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tenant := apimw.TenantID(ctx)
		incidentID := chi.URLParam(r, "id")

		var body struct {
			Verdict string `json:"verdict"`
			Note    string `json:"note"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		if body.Verdict != "actionable" && body.Verdict != "noise" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "verdict must be 'actionable' or 'noise'"})
			return
		}

		// Resolve the incident's fingerprint + service (and confirm it belongs to
		// the tenant) so the feedback is attributed to the right calibration key.
		var fingerprint, service string
		err := pool.QueryRow(ctx,
			`SELECT fingerprint, COALESCE(service, '') FROM incidents WHERE id = $1::uuid AND tenant_id = $2::uuid`,
			incidentID, tenant).Scan(&fingerprint, &service)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "incident not found"})
			return
		}

		var id string
		if err := pool.QueryRow(ctx,
			`INSERT INTO incident_feedback (tenant_id, incident_id, fingerprint, service, verdict, note)
			 VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6) RETURNING id::text`,
			tenant, incidentID, fingerprint, service, body.Verdict, body.Note).Scan(&id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": id, "incident_id": incidentID, "verdict": body.Verdict})
	}
}
