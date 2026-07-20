package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs-server/domains/warroom"
	apimw "github.com/qeetgroup/qeet-logs-server/platform/api/middleware"
)

// DeclareIncidentWarRoom opens (or returns the existing open) war-room session
// for an incident (PRD Module 18.1). POST /v1/admin/incidents/{id}/declare.
func DeclareIncidentWarRoom(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tenant := apimw.TenantID(ctx)
		incidentID := chi.URLParam(r, "id")
		var body struct {
			Commander string `json:"commander"`
			Summary   string `json:"summary"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)

		// Confirm the incident belongs to the tenant.
		var exists bool
		_ = pool.QueryRow(ctx, `SELECT true FROM incidents WHERE id = $1::uuid AND tenant_id = $2::uuid`,
			incidentID, tenant).Scan(&exists)
		if !exists {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "incident not found"})
			return
		}
		var id, status string
		var openedAt time.Time
		err := pool.QueryRow(ctx, `
			INSERT INTO incident_sessions (tenant_id, incident_id, commander, summary)
			VALUES ($1::uuid, $2::uuid, $3, $4)
			ON CONFLICT (incident_id) WHERE status = 'open'
			DO UPDATE SET commander = COALESCE(NULLIF(EXCLUDED.commander, ''), incident_sessions.commander),
			              summary   = COALESCE(NULLIF(EXCLUDED.summary, ''),   incident_sessions.summary)
			RETURNING id::text, status, opened_at`,
			tenant, incidentID, body.Commander, body.Summary).Scan(&id, &status, &openedAt)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"id": id, "incident_id": incidentID, "status": status, "opened_at": openedAt,
		})
	}
}

// GetIncidentWarRoom returns the open session + timeline + roles for an incident.
// GET /v1/admin/incidents/{id}/session.
func GetIncidentWarRoom(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tenant := apimw.TenantID(ctx)
		incidentID := chi.URLParam(r, "id")

		var sessionID, commander, summary, status string
		var openedAt time.Time
		err := pool.QueryRow(ctx,
			`SELECT id::text, commander, summary, status, opened_at FROM incident_sessions
			 WHERE incident_id = $1::uuid AND tenant_id = $2::uuid
			 ORDER BY opened_at DESC LIMIT 1`, incidentID, tenant).Scan(&sessionID, &commander, &summary, &status, &openedAt)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "no war room for this incident"})
			return
		}
		entries := warroomEntries(ctx, pool, tenant, sessionID)
		roles := warroomRoles(ctx, pool, tenant, sessionID)
		writeJSON(w, http.StatusOK, map[string]any{
			"id": sessionID, "incident_id": incidentID, "commander": commander,
			"summary": summary, "status": status, "opened_at": openedAt,
			"roles": roles, "entries": entries,
		})
	}
}

// AddWarRoomEntry appends a timeline entry. POST /v1/admin/sessions/{id}/entries.
func AddWarRoomEntry(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tenant := apimw.TenantID(ctx)
		sessionID := chi.URLParam(r, "id")
		var body struct {
			Kind   string `json:"kind"`
			Author string `json:"author"`
			Body   string `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		if body.Kind == "" {
			body.Kind = "note"
		}
		var id string
		err := pool.QueryRow(ctx,
			`INSERT INTO incident_session_entries (tenant_id, session_id, kind, author, body)
			 SELECT $1::uuid, $2::uuid, $3, $4, $5
			 WHERE EXISTS (SELECT 1 FROM incident_sessions WHERE id = $2::uuid AND tenant_id = $1::uuid)
			 RETURNING id::text`,
			tenant, sessionID, body.Kind, body.Author, body.Body).Scan(&id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": id})
	}
}

// AssignWarRoomRole assigns/updates an incident command role.
// POST /v1/admin/sessions/{id}/roles.
func AssignWarRoomRole(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tenant := apimw.TenantID(ctx)
		sessionID := chi.URLParam(r, "id")
		var body struct {
			Role     string `json:"role"`
			Assignee string `json:"assignee"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Role == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role is required"})
			return
		}
		tag, err := pool.Exec(ctx,
			`INSERT INTO incident_session_roles (tenant_id, session_id, role, assignee)
			 SELECT $1::uuid, $2::uuid, $3, $4
			 WHERE EXISTS (SELECT 1 FROM incident_sessions WHERE id = $2::uuid AND tenant_id = $1::uuid)
			 ON CONFLICT (tenant_id, session_id, role) DO UPDATE SET assignee = EXCLUDED.assignee`,
			tenant, sessionID, body.Role, body.Assignee)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if tag.RowsAffected() == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"role": body.Role, "assignee": body.Assignee})
	}
}

// HandoffWarRoom assembles the post-incident handoff and resolves the session.
// POST /v1/admin/sessions/{id}/handoff.
func HandoffWarRoom(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tenant := apimw.TenantID(ctx)
		sessionID := chi.URLParam(r, "id")

		var s warroom.Session
		err := pool.QueryRow(ctx,
			`SELECT id::text, incident_id::text, commander, summary, opened_at FROM incident_sessions
			 WHERE id = $1::uuid AND tenant_id = $2::uuid`, sessionID, tenant).
			Scan(&s.ID, &s.IncidentID, &s.Commander, &s.Summary, &s.OpenedAt)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}
		roles := warroomRoles(ctx, pool, tenant, sessionID)
		entries := warroomEntries(ctx, pool, tenant, sessionID)

		handoff := warroom.BuildHandoff(s, roles, entries, time.Now().UTC())
		if _, err := pool.Exec(ctx,
			`UPDATE incident_sessions SET status = 'resolved', closed_at = now(), summary = $3
			 WHERE id = $1::uuid AND tenant_id = $2::uuid AND status = 'open'`,
			sessionID, tenant, handoff.Summary); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, handoff)
	}
}

func warroomEntries(ctx context.Context, pool *pgxpool.Pool, tenant, sessionID string) []warroom.Entry {
	rows, err := pool.Query(ctx,
		`SELECT kind, author, body, created_at FROM incident_session_entries
		 WHERE tenant_id = $1::uuid AND session_id = $2::uuid ORDER BY created_at`, tenant, sessionID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []warroom.Entry
	for rows.Next() {
		var e warroom.Entry
		if err := rows.Scan(&e.Kind, &e.Author, &e.Body, &e.CreatedAt); err == nil {
			out = append(out, e)
		}
	}
	return out
}

func warroomRoles(ctx context.Context, pool *pgxpool.Pool, tenant, sessionID string) []warroom.Role {
	rows, err := pool.Query(ctx,
		`SELECT role, assignee FROM incident_session_roles
		 WHERE tenant_id = $1::uuid AND session_id = $2::uuid ORDER BY role`, tenant, sessionID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []warroom.Role
	for rows.Next() {
		var rr warroom.Role
		if err := rows.Scan(&rr.Role, &rr.Assignee); err == nil {
			out = append(out, rr)
		}
	}
	return out
}
