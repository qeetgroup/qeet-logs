package handler

import (
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	apimw "github.com/qeetgroup/qeet-logs/platform/api/middleware"
)

// RCAFeedback records an operator label on a retrieved RCA candidate — whether
// it was actually the root cause of an incident (PRD Module 11.2). These labels
// are the training corpus for the DEFERRED learned-to-rank model: the shipped
// ranker (domains/rca/ranker.go) is a transparent feature-weighted linear model,
// and the trained model (11.2 GA) stays gated until enough labels accrue here to
// train on. This endpoint is how that corpus is collected.
//
// POST /v1/admin/rca/feedback
//
//	{incident_id?, candidate_subject, candidate_type?, was_root_cause, note?}
//
// Scope logs:admin (enforced by the /v1/admin route group). tenant_id is taken
// from the authenticated identity, never the body.
func RCAFeedback(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tenant := apimw.TenantID(ctx)

		var body rcaFeedbackRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		if body.CandidateSubject == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "candidate_subject is required"})
			return
		}
		if body.WasRootCause == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "was_root_cause is required"})
			return
		}

		// incident_id is optional; a label may be attached to a candidate outside a
		// tracked incident. Empty -> NULL (nil arg on a ::uuid placeholder).
		var incidentArg any
		if body.IncidentID != "" {
			incidentArg = body.IncidentID
		}

		var id string
		if err := pool.QueryRow(ctx,
			`INSERT INTO rca_feedback
			   (tenant_id, incident_id, candidate_subject, candidate_type, was_root_cause, note)
			 VALUES ($1::uuid, $2::uuid, $3, $4, $5, $6)
			 RETURNING id::text`,
			tenant, incidentArg, body.CandidateSubject, body.CandidateType, *body.WasRootCause, body.Note,
		).Scan(&id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":             id,
			"was_root_cause": *body.WasRootCause,
		})
	}
}

// rcaFeedbackRequest is the JSON body for POST /v1/admin/rca/feedback.
// was_root_cause is a *bool so a missing field is rejected rather than silently
// defaulting to false.
type rcaFeedbackRequest struct {
	IncidentID       string `json:"incident_id"`
	CandidateSubject string `json:"candidate_subject"`
	CandidateType    string `json:"candidate_type"`
	WasRootCause     *bool  `json:"was_root_cause"`
	Note             string `json:"note"`
}
