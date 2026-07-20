package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	apimw "github.com/qeetgroup/qeet-logs-server/platform/api/middleware"
)

// GetTransform returns the tenant's in-flight remap program (PRD Module 04.2).
func GetTransform(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := apimw.TenantID(r.Context())
		var (
			program string
			version int
			enabled bool
		)
		err := pool.QueryRow(r.Context(),
			`SELECT program, version, enabled FROM transforms WHERE tenant_id = $1::uuid`, tenant).
			Scan(&program, &version, &enabled)
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, map[string]any{"program": "", "version": 0, "enabled": false})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"program": program, "version": version, "enabled": enabled})
	}
}

// UpsertTransform sets the tenant's remap program, bumping the version so the
// gateway picks up the change atomically on its next auth-cache refresh.
func UpsertTransform(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := apimw.TenantID(r.Context())
		var body struct {
			Program string `json:"program"`
			Enabled *bool  `json:"enabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		enabled := true
		if body.Enabled != nil {
			enabled = *body.Enabled
		}
		var version int
		err := pool.QueryRow(r.Context(),
			`INSERT INTO transforms (tenant_id, program, enabled)
			 VALUES ($1::uuid, $2, $3)
			 ON CONFLICT (tenant_id) DO UPDATE
			   SET program = EXCLUDED.program,
			       enabled = EXCLUDED.enabled,
			       version = transforms.version + 1,
			       updated_at = now()
			 RETURNING version`,
			tenant, body.Program, enabled).Scan(&version)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": version, "enabled": enabled})
	}
}
