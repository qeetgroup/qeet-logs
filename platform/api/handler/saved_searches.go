package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	apimw "github.com/qeetgroup/qeet-logs-server/platform/api/middleware"
)

type savedSearchRow struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Name      string    `json:"name"`
	QueryText string    `json:"query_text"`
	CreatedBy *string   `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
}

// ListSavedSearches handles GET /v1/admin/saved-searches.
func ListSavedSearches(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		rows, err := pool.Query(r.Context(), `
			SELECT id, tenant_id, name, query_text, created_by, created_at
			FROM saved_searches WHERE tenant_id = $1 ORDER BY created_at DESC
		`, tenantID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()
		var results []savedSearchRow
		for rows.Next() {
			var s savedSearchRow
			if err := rows.Scan(&s.ID, &s.TenantID, &s.Name, &s.QueryText, &s.CreatedBy, &s.CreatedAt); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			results = append(results, s)
		}
		if results == nil {
			results = []savedSearchRow{}
		}
		writeJSON(w, http.StatusOK, results)
	}
}

// CreateSavedSearch handles POST /v1/admin/saved-searches.
func CreateSavedSearch(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		var body struct {
			Name      string `json:"name"`
			QueryText string `json:"query_text"`
			CreatedBy string `json:"created_by"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if body.Name == "" || body.QueryText == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and query_text required"})
			return
		}
		var createdBy *string
		if body.CreatedBy != "" {
			createdBy = &body.CreatedBy
		}
		var s savedSearchRow
		err := pool.QueryRow(r.Context(), `
			INSERT INTO saved_searches (tenant_id, name, query_text, created_by)
			VALUES ($1, $2, $3, $4)
			RETURNING id, tenant_id, name, query_text, created_by, created_at
		`, tenantID, body.Name, body.QueryText, createdBy).
			Scan(&s.ID, &s.TenantID, &s.Name, &s.QueryText, &s.CreatedBy, &s.CreatedAt)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, s)
	}
}

// DeleteSavedSearch handles DELETE /v1/admin/saved-searches/{id}.
func DeleteSavedSearch(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		id := chi.URLParam(r, "id")
		ct, err := pool.Exec(r.Context(),
			`DELETE FROM saved_searches WHERE id = $1 AND tenant_id = $2`, id, tenantID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if ct.RowsAffected() == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "saved search not found"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
