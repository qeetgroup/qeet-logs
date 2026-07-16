package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs/domains/notify"
	apimw "github.com/qeetgroup/qeet-logs/platform/api/middleware"
)

// Regional-language alert-delivery preference (PRD Module 27.5 / P2-G8). Sets the
// tenant's DEFAULT locale used when Qeet Logs triggers an alert notification
// through Qeet Notify (domains/notify). A per-recipient preference still wins at
// send time via notify.ResolveLocale; this is only the tenant-wide fallback.

type notifyLocaleResponse struct {
	DefaultLocale    string    `json:"default_locale"`
	SupportedLocales []string  `json:"supported_locales"`
	UpdatedAt        time.Time `json:"updated_at"`
}

// GetNotifyLocale handles GET /v1/admin/notify-locale — the tenant's default
// alert locale (platform default 'en' when no row exists) plus the set of
// locales Qeet Notify can render.
func GetNotifyLocale(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := apimw.TenantID(r.Context())
		var locale string
		var updated time.Time
		err := pool.QueryRow(r.Context(),
			`SELECT default_locale, updated_at FROM notify_prefs WHERE tenant_id = $1::uuid`, tenant).
			Scan(&locale, &updated)
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, notifyLocaleResponse{
				DefaultLocale: notify.DefaultLocale, SupportedLocales: notifySupportedList(), UpdatedAt: time.Now().UTC(),
			})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, notifyLocaleResponse{
			DefaultLocale: locale, SupportedLocales: notifySupportedList(), UpdatedAt: updated,
		})
	}
}

// SetNotifyLocale handles PUT /v1/admin/notify-locale. Rejects a locale Qeet
// Notify cannot render (422), so the stored default is always deliverable.
func SetNotifyLocale(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := apimw.TenantID(r.Context())
		var body struct {
			DefaultLocale string `json:"default_locale"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		if !notify.IsSupported(body.DefaultLocale) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{
				"error": "unsupported locale (not in the Qeet Notify catalogue)",
			})
			return
		}
		if _, err := pool.Exec(r.Context(),
			`INSERT INTO notify_prefs (tenant_id, default_locale, updated_at)
			 VALUES ($1::uuid, $2, now())
			 ON CONFLICT (tenant_id) DO UPDATE SET default_locale = EXCLUDED.default_locale, updated_at = now()`,
			tenant, body.DefaultLocale); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, notifyLocaleResponse{
			DefaultLocale: body.DefaultLocale, SupportedLocales: notifySupportedList(), UpdatedAt: time.Now().UTC(),
		})
	}
}

// notifySupportedList renders the supported-locale set as a stable slice for the
// API surface (map iteration order is not stable).
func notifySupportedList() []string {
	// Fixed, catalogue-ordered list mirroring notify.SupportedLocales.
	return []string{"en", "hi", "bn", "ta", "te", "mr", "gu", "kn", "ml", "pa", "or", "as", "ur"}
}
