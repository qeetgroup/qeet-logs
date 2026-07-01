package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	apimw "github.com/qeetgroup/qeet-logs/platform/api/middleware"
)

type retentionResponse struct {
	RetentionDays  int               `json:"retention_days"`
	MaskingActions map[string]string `json:"masking_actions"`
	UpdatedAt      time.Time         `json:"updated_at"`
}

// GetRetention handles GET /v1/admin/retention.
func GetRetention(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())

		var resp retentionResponse
		var rawActions []byte
		err := pool.QueryRow(r.Context(), `
			SELECT retention_days, masking_actions, updated_at
			FROM retention_config WHERE tenant_id = $1
		`, tenantID).Scan(&resp.RetentionDays, &rawActions, &resp.UpdatedAt)

		if err != nil {
			// No row yet — return defaults.
			resp = retentionResponse{
				RetentionDays:  7,
				MaskingActions: map[string]string{},
				UpdatedAt:      time.Now().UTC(),
			}
			writeJSON(w, http.StatusOK, resp)
			return
		}
		resp.MaskingActions = map[string]string{}
		if len(rawActions) > 0 {
			_ = json.Unmarshal(rawActions, &resp.MaskingActions)
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// UpdateRetention handles PUT /v1/admin/retention.
func UpdateRetention(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())

		var body struct {
			RetentionDays  int               `json:"retention_days"`
			MaskingActions map[string]string `json:"masking_actions"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
		if body.RetentionDays < 1 || body.RetentionDays > 3650 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "retention_days must be 1–3650"})
			return
		}
		if body.MaskingActions == nil {
			body.MaskingActions = map[string]string{}
		}
		actionsJSON, _ := json.Marshal(body.MaskingActions)

		_, err := pool.Exec(r.Context(), `
			INSERT INTO retention_config (tenant_id, retention_days, masking_actions, updated_at)
			VALUES ($1, $2, $3::jsonb, now())
			ON CONFLICT (tenant_id) DO UPDATE SET
			    retention_days  = EXCLUDED.retention_days,
			    masking_actions = EXCLUDED.masking_actions,
			    updated_at      = now()
		`, tenantID, body.RetentionDays, string(actionsJSON))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"retention_days":  body.RetentionDays,
			"masking_actions": body.MaskingActions,
		})
	}
}
