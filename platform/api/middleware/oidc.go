package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v3"
	josejwt "github.com/go-jose/go-jose/v3/jwt"
	"github.com/jackc/pgx/v5/pgxpool"
)

// logsClaims holds the JWT claims issued by Qeet ID for qeet-logs console
// sessions. The tenant_id claim is a custom extension — Qeet ID sets it when
// the token is issued for a specific organisation.
type logsClaims struct {
	Sub      string `json:"sub"`
	Iss      string `json:"iss"`
	Exp      int64  `json:"exp"`
	TenantID string `json:"tenant_id"`
	// space-separated scope string ("logs:read logs:query")
	Scope string `json:"scope"`
}

// jwksEntry is a 5-minute in-process JWKS cache.
type jwksEntry struct {
	mu      sync.RWMutex
	keySet  *jose.JSONWebKeySet
	fetched time.Time
}

var globalJWKS = &jwksEntry{}

func (c *jwksEntry) get(ctx context.Context, issuer string) (*jose.JSONWebKeySet, error) {
	c.mu.RLock()
	if c.keySet != nil && time.Since(c.fetched) < 5*time.Minute {
		ks := c.keySet
		c.mu.RUnlock()
		return ks, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	url := strings.TrimRight(issuer, "/") + "/.well-known/jwks.json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build jwks request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch jwks: %w", err)
	}
	defer resp.Body.Close()

	var ks jose.JSONWebKeySet
	if err := json.NewDecoder(resp.Body).Decode(&ks); err != nil {
		return nil, fmt.Errorf("decode jwks: %w", err)
	}
	c.keySet = &ks
	c.fetched = time.Now()
	return &ks, nil
}

// OIDCAuth validates a Qeet ID Bearer JWT from the Authorization header and
// injects tenant + scopes into the request context (same ctxTenant/ctxScopes
// keys used by APIKeyAuth). Used on admin and console-facing routes.
//
// The JWT must have been issued by issuer and carry a tenant_id claim. The
// scope claim ("logs:read logs:query ...") is parsed into []string.
func OIDCAuth(issuer string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if raw == "" {
				writeError(w, http.StatusUnauthorized, "missing Authorization header")
				return
			}

			ks, err := globalJWKS.get(r.Context(), issuer)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "could not fetch JWKS")
				return
			}

			tok, err := josejwt.ParseSigned(raw)
			if err != nil {
				writeError(w, http.StatusUnauthorized, "malformed JWT")
				return
			}

			// Try each key in the set until one verifies.
			var claims logsClaims
			verified := false
			for _, k := range ks.Keys {
				if err := tok.Claims(k, &claims); err == nil {
					verified = true
					break
				}
			}
			if !verified {
				writeError(w, http.StatusUnauthorized, "JWT signature invalid")
				return
			}

			if time.Now().Unix() > claims.Exp {
				writeError(w, http.StatusUnauthorized, "JWT expired")
				return
			}
			if !strings.EqualFold(strings.TrimRight(claims.Iss, "/"),
				strings.TrimRight(issuer, "/")) {
				writeError(w, http.StatusUnauthorized, "JWT issuer mismatch")
				return
			}
			if claims.TenantID == "" {
				writeError(w, http.StatusUnauthorized, "JWT missing tenant_id claim")
				return
			}

			scopes := parseScope(claims.Scope)
			ctx := context.WithValue(r.Context(), ctxTenant, claims.TenantID)
			ctx = context.WithValue(ctx, ctxScopes, scopes)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// AnyAuth authenticates via API key (X-Qeet-Api-Key header) or OIDC Bearer
// JWT (Authorization header), whichever is present. Returns 401 when neither
// is supplied or valid. Used on admin routes that must be reachable from both
// the console (JWT) and CLI/SDK (API key with logs:admin).
func AnyAuth(pool *pgxpool.Pool, issuer string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Qeet-Api-Key") != "" {
				APIKeyAuth(pool)(next).ServeHTTP(w, r)
				return
			}
			if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
				OIDCAuth(issuer)(next).ServeHTTP(w, r)
				return
			}
			writeError(w, http.StatusUnauthorized, "missing X-Qeet-Api-Key or Authorization header")
		})
	}
}

func parseScope(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Fields(s)
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
