package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs/platform/api/middleware"
	"github.com/qeetgroup/qeet-logs/platform/clickhouse"
)

type erasureRequest struct {
	UserLinkageKey string `json:"user_linkage_key"`
	TimeFrom       string `json:"time_from"` // RFC3339, optional
	TimeTo         string `json:"time_to"`   // RFC3339, optional
}

type erasureReceipt struct {
	TablesAffected []string `json:"tables_affected"`
	Filters        []string `json:"filters"`
	SubmittedAt    string   `json:"submitted_at"`
	Note           string   `json:"note"`
}

// CreateErasure handles POST /v1/admin/erasure.
// Registers a DPDP/GDPR erasure request and submits async ClickHouse DELETE
// mutations. Mutations are non-blocking (ClickHouse processes them in the
// background); the request is marked "completed" once submitted.
func CreateErasure(pool *pgxpool.Pool, ch *clickhouse.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := middleware.TenantID(r.Context())

		var body erasureRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		if body.UserLinkageKey == "" && body.TimeFrom == "" && body.TimeTo == "" {
			http.Error(w, "at least one of user_linkage_key, time_from, or time_to is required", http.StatusBadRequest)
			return
		}

		// Build WHERE predicates for ClickHouse mutations.
		var preds []string
		preds = append(preds, fmt.Sprintf("tenant_id = '%s'", escapeCH(tenantID)))
		if body.UserLinkageKey != "" {
			// Match against the user_id key in the JSON extra_fields / attributes map.
			// logs and traces store it under extra_fields; metrics under attributes.
			preds = append(preds, fmt.Sprintf("JSONExtractString(extra_fields, 'user_id') = '%s'", escapeCH(body.UserLinkageKey)))
		}
		if body.TimeFrom != "" {
			if _, err := time.Parse(time.RFC3339, body.TimeFrom); err != nil {
				http.Error(w, "time_from must be RFC3339", http.StatusBadRequest)
				return
			}
			preds = append(preds, fmt.Sprintf("timestamp >= parseDateTime64BestEffort('%s')", escapeCH(body.TimeFrom)))
		}
		if body.TimeTo != "" {
			if _, err := time.Parse(time.RFC3339, body.TimeTo); err != nil {
				http.Error(w, "time_to must be RFC3339", http.StatusBadRequest)
				return
			}
			preds = append(preds, fmt.Sprintf("timestamp <= parseDateTime64BestEffort('%s')", escapeCH(body.TimeTo)))
		}

		where := strings.Join(preds, " AND ")

		// Register the request in Postgres before touching ClickHouse.
		ulk := body.UserLinkageKey
		if ulk == "" {
			ulk = "(time-range only)"
		}
		var reqID string
		err := pool.QueryRow(r.Context(),
			`INSERT INTO erasure_requests (tenant_id, user_linkage_key, status)
			 VALUES ($1, $2, 'pending') RETURNING id`,
			tenantID, ulk,
		).Scan(&reqID)
		if err != nil {
			http.Error(w, "db error: "+err.Error(), http.StatusInternalServerError)
			return
		}

		// Submit mutations for each signal table. metrics uses 'attributes' instead of extra_fields.
		tables := []string{"logs", "traces"}
		var metricPreds []string
		metricPreds = append(metricPreds, fmt.Sprintf("tenant_id = '%s'", escapeCH(tenantID)))
		if body.UserLinkageKey != "" {
			metricPreds = append(metricPreds,
				fmt.Sprintf("attributes['user_id'] = '%s'", escapeCH(body.UserLinkageKey)))
		}
		if body.TimeFrom != "" {
			metricPreds = append(metricPreds,
				fmt.Sprintf("timestamp >= parseDateTime64BestEffort('%s')", escapeCH(body.TimeFrom)))
		}
		if body.TimeTo != "" {
			metricPreds = append(metricPreds,
				fmt.Sprintf("timestamp <= parseDateTime64BestEffort('%s')", escapeCH(body.TimeTo)))
		}
		metricsWhere := strings.Join(metricPreds, " AND ")

		var mutErrs []string
		for _, tbl := range tables {
			sql := fmt.Sprintf("ALTER TABLE %s DELETE WHERE %s", tbl, where)
			if err := ch.Exec(r.Context(), sql); err != nil {
				mutErrs = append(mutErrs, fmt.Sprintf("%s: %s", tbl, err.Error()))
			}
		}
		if err := ch.Exec(r.Context(), fmt.Sprintf("ALTER TABLE metrics DELETE WHERE %s", metricsWhere)); err != nil {
			mutErrs = append(mutErrs, "metrics: "+err.Error())
		}

		status := "completed"
		var receiptNote string
		if len(mutErrs) > 0 {
			status = "failed"
			receiptNote = "mutation errors: " + strings.Join(mutErrs, "; ")
		} else {
			receiptNote = "ClickHouse mutations submitted; deletions are applied asynchronously"
		}

		receipt := erasureReceipt{
			TablesAffected: append(tables, "metrics"),
			Filters:        preds,
			SubmittedAt:    time.Now().UTC().Format(time.RFC3339),
			Note:           receiptNote,
		}
		receiptJSON, _ := json.Marshal(receipt)

		_, _ = pool.Exec(r.Context(),
			`UPDATE erasure_requests SET status=$1, completed_at=now(), receipt=$2 WHERE id=$3`,
			status, receiptJSON, reqID,
		)

		if status == "failed" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]any{
				"id":      reqID,
				"status":  status,
				"receipt": receipt,
			})
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		json.NewEncoder(w).Encode(map[string]any{
			"id":      reqID,
			"status":  status,
			"receipt": receipt,
		})
	}
}

// ListErasure handles GET /v1/admin/erasure.
func ListErasure(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := middleware.TenantID(r.Context())

		rows, err := pool.Query(r.Context(),
			`SELECT id, user_linkage_key, status, requested_at, completed_at, receipt
			 FROM erasure_requests
			 WHERE tenant_id = $1
			 ORDER BY requested_at DESC
			 LIMIT 100`,
			tenantID,
		)
		if err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		type row struct {
			ID             string     `json:"id"`
			UserLinkageKey string     `json:"user_linkage_key"`
			Status         string     `json:"status"`
			RequestedAt    time.Time  `json:"requested_at"`
			CompletedAt    *time.Time `json:"completed_at,omitempty"`
			Receipt        *json.RawMessage `json:"receipt,omitempty"`
		}
		var out []row
		for rows.Next() {
			var r2 row
			var receiptBytes []byte
			if err := rows.Scan(&r2.ID, &r2.UserLinkageKey, &r2.Status, &r2.RequestedAt, &r2.CompletedAt, &receiptBytes); err != nil {
				continue
			}
			if len(receiptBytes) > 0 {
				rm := json.RawMessage(receiptBytes)
				r2.Receipt = &rm
			}
			out = append(out, r2)
		}
		if out == nil {
			out = []row{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"requests": out})
	}
}

// escapeCH escapes a string for safe interpolation into a ClickHouse SQL literal.
// Only single-quotes and backslashes need escaping in CH string literals.
func escapeCH(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}
