package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	apimw "github.com/qeetgroup/qeet-logs-server/platform/api/middleware"
	"github.com/qeetgroup/qeet-logs-server/platform/database"
)

// createAPIKeyRequest is the JSON body for POST /v1/admin/api-keys.
type createAPIKeyRequest struct {
	Name      string     `json:"name"`
	Scopes    []string   `json:"scopes"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// CreateAPIKey handles POST /v1/admin/api-keys.
// Requires logs:admin scope. Returns the raw key once; never retrievable again.
func CreateAPIKey(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())

		var req createAPIKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		if req.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
			return
		}
		if len(req.Scopes) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "at least one scope is required"})
			return
		}
		if !validScopes(req.Scopes) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid scope(s); allowed: logs:ingest logs:read logs:query logs:export logs:admin logs:platform"})
			return
		}

		nk, err := database.CreateAPIKey(r.Context(), pool, tenantID, req.Name, req.Scopes, req.ExpiresAt)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create api key"})
			return
		}

		writeJSON(w, http.StatusCreated, nk)
	}
}

// ListAPIKeys handles GET /v1/admin/api-keys.
// Requires logs:admin scope. Returns all active keys for the tenant.
func ListAPIKeys(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())

		keys, err := database.ListAPIKeys(r.Context(), pool, tenantID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list api keys"})
			return
		}

		if keys == nil {
			keys = []database.APIKeyRow{}
		}
		writeJSON(w, http.StatusOK, keys)
	}
}

// RevokeAPIKey handles DELETE /v1/admin/api-keys/{id}.
// Requires logs:admin scope. Soft-deletes the key (sets revoked_at).
func RevokeAPIKey(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		keyID := chi.URLParam(r, "id")

		ok, err := database.RevokeAPIKey(r.Context(), pool, tenantID, keyID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to revoke api key"})
			return
		}
		if !ok {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "api key not found"})
			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

var allowedScopes = map[string]bool{
	"logs:ingest":   true,
	"logs:read":     true,
	"logs:query":    true,
	"logs:export":   true,
	"logs:admin":    true,
	"logs:platform": true,
}

func validScopes(scopes []string) bool {
	for _, s := range scopes {
		if !allowedScopes[s] {
			return false
		}
	}
	return true
}

