package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	apimw "github.com/qeetgroup/qeet-logs/platform/api/middleware"
)

// ShareDashboard mints (or returns) a stable share token for a dashboard (PRD
// Module 22.3), enabling seat-free viewing/embedding via GetSharedDashboard.
func ShareDashboard(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		tenant := apimw.TenantID(ctx)
		id := chi.URLParam(r, "id")

		var existing *string // nullable column
		err := pool.QueryRow(ctx,
			`SELECT share_token FROM dashboards WHERE id = $1::uuid AND tenant_id = $2::uuid`,
			id, tenant).Scan(&existing)
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "dashboard not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		// Reuse an existing token so the link is stable across calls.
		token := ""
		if existing != nil {
			token = *existing
		}
		if token == "" {
			token = "dsh_" + randToken()
			if _, err := pool.Exec(ctx,
				`UPDATE dashboards SET share_token = $1 WHERE id = $2::uuid AND tenant_id = $3::uuid`,
				token, id, tenant); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"token": token, "path": "/shared/dashboards/" + token})
	}
}

// GetSharedDashboard is the PUBLIC, unauthenticated read of a shared dashboard
// by token — no API key or seat required (Module 22.3). Returns only the
// dashboard name + panels, never tenant internals.
func GetSharedDashboard(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := chi.URLParam(r, "token")
		var (
			name   string
			panels json.RawMessage
		)
		err := pool.QueryRow(r.Context(),
			`SELECT name, panels FROM dashboards WHERE share_token = $1`, token).
			Scan(&name, &panels)
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"name": name, "panels": panels})
	}
}

func randToken() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
