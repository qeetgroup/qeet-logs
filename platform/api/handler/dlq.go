package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"

	apimw "github.com/qeetgroup/qeet-logs/platform/api/middleware"
)

type dlqEvent struct {
	ID         string     `json:"id"`
	TenantID   string     `json:"tenant_id"`
	Payload    any        `json:"payload"`
	ErrorMsg   string     `json:"error_msg"`
	Attempt    int        `json:"attempt"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	ReplayedAt *time.Time `json:"replayed_at"`
}

type dlqListResponse struct {
	Events []dlqEvent `json:"events"`
	Total  int64      `json:"total"`
}

// ListDLQ handles GET /v1/admin/dlq.
func ListDLQ(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		status := r.URL.Query().Get("status") // pending | replayed | dropped | "" = all

		args := []any{tenantID}
		where := "tenant_id = $1"
		if status != "" {
			args = append(args, status)
			where += " AND status = $2"
		}

		var total int64
		pool.QueryRow(r.Context(), //nolint:errcheck
			"SELECT count(*) FROM dlq_events WHERE "+where, args...,
		).Scan(&total)

		rows, err := pool.Query(r.Context(), `
			SELECT id, tenant_id, payload, error_msg, attempt, status, created_at, replayed_at
			FROM dlq_events WHERE `+where+`
			ORDER BY created_at DESC LIMIT 100
		`, args...)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		defer rows.Close()

		var events []dlqEvent
		for rows.Next() {
			var e dlqEvent
			var rawPayload []byte
			if err := rows.Scan(&e.ID, &e.TenantID, &rawPayload, &e.ErrorMsg,
				&e.Attempt, &e.Status, &e.CreatedAt, &e.ReplayedAt); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
			var p any
			_ = json.Unmarshal(rawPayload, &p)
			e.Payload = p
			events = append(events, e)
		}
		if events == nil {
			events = []dlqEvent{}
		}
		writeJSON(w, http.StatusOK, dlqListResponse{Events: events, Total: total})
	}
}

// ReplayDLQ handles POST /v1/admin/dlq/{id}/replay.
// Re-publishes the raw payload to the NATS ingest subject for another write attempt.
func ReplayDLQ(pool *pgxpool.Pool, nc *nats.Conn) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		id := chi.URLParam(r, "id")

		var rawPayload []byte
		err := pool.QueryRow(r.Context(),
			`SELECT payload FROM dlq_events WHERE id = $1 AND tenant_id = $2 AND status = 'pending'`,
			id, tenantID,
		).Scan(&rawPayload)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "DLQ event not found or not pending"})
			return
		}

		subject := "qeet-logs." + tenantID + ".logs"
		if err := nc.Publish(subject, rawPayload); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "NATS publish failed: " + err.Error()})
			return
		}

		now := time.Now().UTC()
		pool.Exec(r.Context(), //nolint:errcheck
			`UPDATE dlq_events SET status = 'replayed', replayed_at = $1, attempt = attempt + 1
			 WHERE id = $2`,
			now, id,
		)

		writeJSON(w, http.StatusOK, map[string]string{"status": "replayed", "id": id})
	}
}

// DropDLQ handles DELETE /v1/admin/dlq/{id} — marks an event as dropped (won't retry).
func DropDLQ(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := apimw.TenantID(r.Context())
		id := chi.URLParam(r, "id")
		ct, err := pool.Exec(r.Context(),
			`UPDATE dlq_events SET status = 'dropped' WHERE id = $1 AND tenant_id = $2`,
			id, tenantID,
		)
		if err != nil || ct.RowsAffected() == 0 {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "DLQ event not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "dropped", "id": id})
	}
}
