package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Per-tenant request rate limiting (product-readiness; CLAUDE.md lists rate
// limits as a Redis responsibility). A fixed-window counter in Redis, keyed by
// the authenticated tenant, caps requests per window across all query-API
// replicas (shared Redis = shared budget, unlike an in-process limiter).
//
// Placement: AFTER APIKeyAuth (so the tenant is resolved). Unauthenticated
// requests (no tenant in context) are passed through — they were already
// rejected by auth. Redis errors FAIL OPEN: a limiter outage must not take the
// API down, so the request is allowed and only the rate cap is temporarily lost.

// RateLimitConfig configures the fixed-window limiter.
type RateLimitConfig struct {
	Requests int           // max requests per window per tenant
	Window   time.Duration // window length
}

// DefaultRateLimit is a sane baseline: 600 requests/minute/tenant (10 rps
// sustained) — generous for dashboards + programmatic access, restrictive
// enough to blunt a runaway client. Override via env in cmd/query.
var DefaultRateLimit = RateLimitConfig{Requests: 600, Window: time.Minute}

// RateLimit returns a middleware enforcing cfg per tenant using rdb. It sets the
// standard X-RateLimit-Limit / X-RateLimit-Remaining / X-RateLimit-Reset headers
// and, on breach, 429 with Retry-After.
func RateLimit(rdb *redis.Client, cfg RateLimitConfig) func(http.Handler) http.Handler {
	if cfg.Requests <= 0 {
		cfg = DefaultRateLimit
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tenant := TenantID(r.Context())
			if tenant == "" || rdb == nil {
				next.ServeHTTP(w, r) // unauthenticated or no limiter — pass through
				return
			}

			ctx := r.Context()
			now := time.Now()
			windowStart := now.Truncate(cfg.Window)
			key := "rl:" + tenant + ":" + strconv.FormatInt(windowStart.Unix(), 10)

			count, err := rdb.Incr(ctx, key).Result()
			if err != nil {
				next.ServeHTTP(w, r) // FAIL OPEN on Redis error
				return
			}
			if count == 1 {
				// First hit in this window — set the TTL so the key self-expires.
				_ = rdb.Expire(ctx, key, cfg.Window).Err()
			}

			remaining := cfg.Requests - int(count)
			if remaining < 0 {
				remaining = 0
			}
			reset := windowStart.Add(cfg.Window)
			h := w.Header()
			h.Set("X-RateLimit-Limit", strconv.Itoa(cfg.Requests))
			h.Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			h.Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))

			if int(count) > cfg.Requests {
				h.Set("Retry-After", strconv.Itoa(int(time.Until(reset).Seconds())+1))
				writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
