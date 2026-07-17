// Package webhook is the Phase-2 outbound webhook dispatcher (PRD Module 30.4).
// A tenant registers endpoint URLs subscribed to event types; on an alert or
// incident state change the dispatcher POSTs a JSON payload, signed with the
// endpoint's HMAC-SHA256 secret so receivers can verify authenticity. Delivery
// is best-effort with a small retry budget — a slow or down receiver never
// blocks or fails the alerter cycle that triggered it.
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

const maxAttempts = 3

// Endpoint is a registered outbound webhook target.
type Endpoint struct {
	ID     string
	URL    string
	Secret string
}

// Sign returns the hex-encoded HMAC-SHA256 of body under secret. Empty secret
// yields an empty signature (the endpoint opted out of signing).
func Sign(secret string, body []byte) string {
	if secret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// Dispatch loads the tenant's active endpoints subscribed to event and POSTs the
// signed payload to each. Safe to call in a goroutine with a background context;
// errors are swallowed (best-effort) — callers that need delivery guarantees use
// the DLQ, not this path.
func Dispatch(ctx context.Context, pool *pgxpool.Pool, tenant, event string, payload any) {
	eps, err := loadEndpoints(ctx, pool, tenant, event)
	if err != nil || len(eps) == 0 {
		return
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return
	}
	for _, ep := range eps {
		_ = deliver(ctx, ep, event, body)
	}
}

// loadEndpoints returns active endpoints for the tenant that subscribe to event
// (an endpoint with no explicit events subscribes to all).
func loadEndpoints(ctx context.Context, pool *pgxpool.Pool, tenant, event string) ([]Endpoint, error) {
	rows, err := pool.Query(ctx,
		`SELECT id::text, url, secret FROM webhook_endpoints
		 WHERE tenant_id = $1::uuid AND active
		   AND (cardinality(events) = 0 OR $2 = ANY(events))`,
		tenant, event)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Endpoint
	for rows.Next() {
		var e Endpoint
		if err := rows.Scan(&e.ID, &e.URL, &e.Secret); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// deliver POSTs body to one endpoint, retrying on network/5xx up to maxAttempts.
func deliver(ctx context.Context, ep Endpoint, event string, body []byte) error {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * 200 * time.Millisecond):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.URL, bytes.NewReader(body))
		if err != nil {
			return err // malformed URL: no point retrying
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "qeet-logs-webhooks/1")
		req.Header.Set("X-Qeet-Event", event)
		req.Header.Set("X-Qeet-Webhook-Id", ep.ID)
		if sig := Sign(ep.Secret, body); sig != "" {
			req.Header.Set("X-Qeet-Signature", "sha256="+sig)
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue // network error: retry
		}
		resp.Body.Close()
		if resp.StatusCode < 300 {
			return nil
		}
		lastErr = fmt.Errorf("webhook %s returned %d", ep.URL, resp.StatusCode)
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			return lastErr // client error: don't retry
		}
	}
	return lastErr
}
