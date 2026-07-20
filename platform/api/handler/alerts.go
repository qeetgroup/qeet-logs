package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	apimw "github.com/qeetgroup/qeet-logs-server/platform/api/middleware"
)

type alertRuleRow struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	Name          string    `json:"name"`
	Kind          string    `json:"kind"`
	Service       *string   `json:"service"`
	Condition     *string   `json:"condition"`
	Threshold     *float64  `json:"threshold"`
	WindowSeconds int       `json:"window_seconds"`
	Channels      any       `json:"channels"`
	Enabled       bool      `json:"enabled"`
	CreatedAt     time.Time `json:"created_at"`
}

// ListAlertRules handles GET /v1/admin/alert-rules.
func ListAlertRules(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		rows, err := pool.Query(r.Context(), `
			SELECT id, tenant_id, name, kind, service, condition, threshold,
			       window_seconds, channels, enabled, created_at
			FROM alert_rules
			WHERE tenant_id = $1
			ORDER BY created_at DESC
		`, tenantID)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()

		var results []alertRuleRow
		for rows.Next() {
			var rule alertRuleRow
			var rawCh []byte
			if err := rows.Scan(
				&rule.ID, &rule.TenantID, &rule.Name, &rule.Kind,
				&rule.Service, &rule.Condition, &rule.Threshold,
				&rule.WindowSeconds, &rawCh, &rule.Enabled, &rule.CreatedAt,
			); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			var ch any
			if len(rawCh) > 0 {
				_ = json.Unmarshal(rawCh, &ch)
			}
			rule.Channels = ch
			results = append(results, rule)
		}
		if results == nil {
			results = []alertRuleRow{}
		}
		writeJSON(w, http.StatusOK, results)
	}
}

// CreateAlertRule handles POST /v1/admin/alert-rules.
func CreateAlertRule(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())

		var body struct {
			Name          string   `json:"name"`
			Kind          string   `json:"kind"`
			Service       *string  `json:"service"`
			Condition     *string  `json:"condition"`
			Threshold     *float64 `json:"threshold"`
			WindowSeconds int      `json:"window_seconds"`
			Channels      any      `json:"channels"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if body.Name == "" || (body.Kind != "threshold" && body.Kind != "absence") {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name and kind (threshold|absence) required"})
			return
		}
		if body.WindowSeconds <= 0 {
			body.WindowSeconds = 300
		}
		chJSON, _ := json.Marshal(body.Channels)

		var rule alertRuleRow
		err := pool.QueryRow(r.Context(), `
			INSERT INTO alert_rules (tenant_id, name, kind, service, condition, threshold, window_seconds, channels)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8::jsonb)
			RETURNING id, tenant_id, name, kind, service, condition, threshold,
			          window_seconds, channels, enabled, created_at
		`, tenantID, body.Name, body.Kind, body.Service, body.Condition,
			body.Threshold, body.WindowSeconds, string(chJSON),
		).Scan(
			&rule.ID, &rule.TenantID, &rule.Name, &rule.Kind,
			&rule.Service, &rule.Condition, &rule.Threshold,
			&rule.WindowSeconds, new([]byte), &rule.Enabled, &rule.CreatedAt,
		)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		rule.Channels = body.Channels
		writeJSON(w, http.StatusCreated, rule)
	}
}

// DeleteAlertRule handles DELETE /v1/admin/alert-rules/{id}.
func DeleteAlertRule(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		id := chi.URLParam(r, "id")

		ct, err := pool.Exec(r.Context(),
			`DELETE FROM alert_rules WHERE id = $1 AND tenant_id = $2`,
			id, tenantID,
		)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if ct.RowsAffected() == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "rule not found"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
