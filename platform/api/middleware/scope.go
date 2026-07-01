package middleware

import "net/http"

// RequireScope returns 403 unless the authenticated caller holds at least one
// of the given logs:* scopes. Must be placed after APIKeyAuth or OIDCAuth.
//
// Defined scopes: logs:ingest  logs:read  logs:query  logs:export
//                 logs:admin   logs:platform
//
// logs:platform grants cross-tenant visibility (QEET operators only).
func RequireScope(want ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !HasScope(r.Context(), want...) {
				writeError(w, http.StatusForbidden, "insufficient scope")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
