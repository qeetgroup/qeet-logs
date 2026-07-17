package middleware

import "net/http"

// Security hardening middleware for product-readiness (SOC2 / OWASP secure-
// headers baseline). These are cheap, pure, dependency-free wrappers applied
// globally in cmd/query. They harden the API responses without affecting the
// JSON contract.

// DefaultMaxBodyBytes caps request bodies at 4 MiB. The query API is GET-heavy;
// the POST surfaces (changes, alert rules, copilot, webhooks) carry small JSON.
// A global cap bounds memory + protects against oversized-payload abuse. The
// Slack slash-command handler applies its own tighter 1 MiB limit on top.
const DefaultMaxBodyBytes int64 = 4 << 20

// SecureHeaders sets a conservative security-header baseline appropriate for a
// JSON API (no HTML is served from these hosts). HSTS is emitted only when the
// request arrived over TLS (directly or via a terminating proxy that sets
// X-Forwarded-Proto=https), so local plaintext dev is unaffected.
func SecureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		// JSON API: lock down anything a mistakenly-rendered response could do.
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		h.Set("Cross-Origin-Resource-Policy", "same-origin")
		if isHTTPS(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// MaxBodyBytes returns a middleware that caps request body size at n bytes via
// http.MaxBytesReader; a handler reading past the limit gets an error and the
// server responds 413. n <= 0 disables the cap.
func MaxBodyBytes(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if n > 0 && r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, n)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isHTTPS(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	return r.Header.Get("X-Forwarded-Proto") == "https"
}
