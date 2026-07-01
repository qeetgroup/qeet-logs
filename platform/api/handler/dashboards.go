package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	apimw "github.com/qeetgroup/qeet-logs/platform/api/middleware"
)

type dashboardRow struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	Name      string    `json:"name"`
	Panels    any       `json:"panels"`
	CreatedBy *string   `json:"created_by"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func scanDashboard(row interface {
	Scan(...any) error
}) (dashboardRow, error) {
	var d dashboardRow
	var rawPanels []byte
	if err := row.Scan(&d.ID, &d.TenantID, &d.Name, &rawPanels, &d.CreatedBy, &d.CreatedAt, &d.UpdatedAt); err != nil {
		return d, err
	}
	var panels any = []any{}
	if len(rawPanels) > 0 {
		_ = json.Unmarshal(rawPanels, &panels)
	}
	d.Panels = panels
	return d, nil
}

// ListDashboards handles GET /v1/admin/dashboards.
func ListDashboards(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		rows, err := pool.Query(r.Context(), `
			SELECT id, tenant_id, name, panels, created_by, created_at, updated_at
			FROM dashboards WHERE tenant_id = $1 ORDER BY created_at DESC
		`, tenantID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()
		var results []dashboardRow
		for rows.Next() {
			d, err := scanDashboard(rows)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			results = append(results, d)
		}
		if results == nil {
			results = []dashboardRow{}
		}
		writeJSON(w, http.StatusOK, results)
	}
}

// GetDashboard handles GET /v1/admin/dashboards/{id}.
func GetDashboard(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		id := chi.URLParam(r, "id")
		row := pool.QueryRow(r.Context(), `
			SELECT id, tenant_id, name, panels, created_by, created_at, updated_at
			FROM dashboards WHERE id = $1 AND tenant_id = $2
		`, id, tenantID)
		d, err := scanDashboard(row)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "dashboard not found"})
			return
		}
		writeJSON(w, http.StatusOK, d)
	}
}

// CreateDashboard handles POST /v1/admin/dashboards.
func CreateDashboard(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		var body struct {
			Name      string `json:"name"`
			Panels    any    `json:"panels"`
			CreatedBy string `json:"created_by"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name required"})
			return
		}
		panelsJSON, _ := json.Marshal(body.Panels)
		if len(panelsJSON) == 0 || string(panelsJSON) == "null" {
			panelsJSON = []byte("[]")
		}
		var createdBy *string
		if body.CreatedBy != "" {
			createdBy = &body.CreatedBy
		}
		row := pool.QueryRow(r.Context(), `
			INSERT INTO dashboards (tenant_id, name, panels, created_by)
			VALUES ($1, $2, $3::jsonb, $4)
			RETURNING id, tenant_id, name, panels, created_by, created_at, updated_at
		`, tenantID, body.Name, string(panelsJSON), createdBy)
		d, err := scanDashboard(row)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, d)
	}
}

// UpdateDashboard handles PUT /v1/admin/dashboards/{id}.
func UpdateDashboard(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		id := chi.URLParam(r, "id")
		var body struct {
			Name   string `json:"name"`
			Panels any    `json:"panels"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		panelsJSON, _ := json.Marshal(body.Panels)
		if len(panelsJSON) == 0 || string(panelsJSON) == "null" {
			panelsJSON = []byte("[]")
		}
		row := pool.QueryRow(r.Context(), `
			UPDATE dashboards SET name = $1, panels = $2::jsonb, updated_at = now()
			WHERE id = $3 AND tenant_id = $4
			RETURNING id, tenant_id, name, panels, created_by, created_at, updated_at
		`, body.Name, string(panelsJSON), id, tenantID)
		d, err := scanDashboard(row)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "dashboard not found"})
			return
		}
		writeJSON(w, http.StatusOK, d)
	}
}

// DeleteDashboard handles DELETE /v1/admin/dashboards/{id}.
func DeleteDashboard(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		id := chi.URLParam(r, "id")
		ct, err := pool.Exec(r.Context(),
			`DELETE FROM dashboards WHERE id = $1 AND tenant_id = $2`, id, tenantID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if ct.RowsAffected() == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "dashboard not found"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
