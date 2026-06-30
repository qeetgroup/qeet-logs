// Package middleware holds HTTP middleware for the query API.
//
// M3 ships API-key authentication (X-Qeet-Api-Key → tenant + scopes). Qeet ID
// OIDC for the console and the full RBAC/scoped-key management land in M5.
package middleware

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs/platform/database"
	"github.com/qeetgroup/qeet-logs/platform/security"
)

type ctxKey int

const (
	ctxTenant ctxKey = iota
	ctxScopes
)

// APIKeyAuth resolves X-Qeet-Api-Key to a tenant + scopes and injects them into
// the request context. Returns 401 when the key is missing or invalid.
func APIKeyAuth(pool *pgxpool.Pool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := r.Header.Get("X-Qeet-Api-Key")
			if key == "" {
				writeError(w, http.StatusUnauthorized, "missing X-Qeet-Api-Key")
				return
			}
			ak, ok, err := database.LookupAPIKey(r.Context(), pool, security.HashAPIKey(key))
			if err != nil {
				writeError(w, http.StatusInternalServerError, "auth lookup failed")
				return
			}
			if !ok {
				writeError(w, http.StatusUnauthorized, "invalid api key")
				return
			}
			ctx := context.WithValue(r.Context(), ctxTenant, ak.TenantID)
			ctx = context.WithValue(ctx, ctxScopes, ak.Scopes)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// TenantID returns the authenticated tenant for the request, or "".
func TenantID(ctx context.Context) string {
	if v, ok := ctx.Value(ctxTenant).(string); ok {
		return v
	}
	return ""
}

// Scopes returns the granted scopes for the request.
func Scopes(ctx context.Context) []string {
	if v, ok := ctx.Value(ctxScopes).([]string); ok {
		return v
	}
	return nil
}

// HasScope reports whether the request was granted any of the given scopes.
func HasScope(ctx context.Context, want ...string) bool {
	have := Scopes(ctx)
	for _, w := range want {
		for _, s := range have {
			if s == w {
				return true
			}
		}
	}
	return false
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg}) //nolint:errcheck
}
