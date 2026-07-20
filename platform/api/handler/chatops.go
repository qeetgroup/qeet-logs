package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/qeetgroup/qeet-logs-server/domains/chatops"
	apimw "github.com/qeetgroup/qeet-logs-server/platform/api/middleware"
)

// chatopsClient delivers the one-way ChatOps payload to a Slack/Teams
// incoming-webhook URL. Short timeout — a slow ChatOps endpoint must never hang
// the admin request that triggered the test.
var chatopsClient = &http.Client{Timeout: 5 * time.Second}

// chatopsTestRequest is the POST /v1/admin/chatops/test body. provider and
// webhook_url are required; the remaining fields populate the sample event that
// gets formatted and delivered (url and kind are optional extras).
type chatopsTestRequest struct {
	Provider   string `json:"provider"`    // "slack" | "teams"
	WebhookURL string `json:"webhook_url"` // the incoming-webhook URL to POST to
	Title      string `json:"title"`
	Service    string `json:"service"`
	Severity   string `json:"severity"`
	Message    string `json:"message"`
	URL        string `json:"url"`  // optional deep link
	Kind       string `json:"kind"` // optional event kind
}

// ChatOpsTest handles POST /v1/admin/chatops/test — the non-gated slice of PRD
// Module 19 (ChatOps). It formats a sample notification for the requested
// provider and POSTs it to the tenant's Slack/Teams incoming-webhook URL, then
// reports the upstream status. This is ONE-WAY delivery only: no OAuth app, no
// bot token, no slash-command handling. Delivery is best-effort — a failure to
// reach the webhook is reported as 502, not a server error.
//
// Requires logs:admin (also enforced by the /v1/admin route group; re-checked
// here so the handler is safe wherever it is mounted).
func ChatOpsTest() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:admin") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "requires logs:admin scope"})
			return
		}

		var body chatopsTestRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}
		if body.WebhookURL == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "webhook_url is required"})
			return
		}

		ev := chatops.Event{
			Title:    body.Title,
			Service:  body.Service,
			Severity: body.Severity,
			Message:  body.Message,
			URL:      body.URL,
			Kind:     body.Kind,
		}

		var payload []byte
		switch body.Provider {
		case "slack":
			payload = chatops.FormatSlack(ev)
		case "teams":
			payload = chatops.FormatTeams(ev)
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provider must be slack or teams"})
			return
		}

		status, err := chatopsDeliver(ctx, body.WebhookURL, payload)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{
				"error":    "delivery failed: " + err.Error(),
				"provider": body.Provider,
			})
			return
		}

		delivered := status/100 == 2
		code := http.StatusOK
		if !delivered {
			code = http.StatusBadGateway
		}
		writeJSON(w, code, map[string]any{
			"provider":        body.Provider,
			"delivered":       delivered,
			"upstream_status": status,
		})
	}
}

// chatopsDeliver POSTs the formatted payload to the incoming-webhook URL and
// returns the upstream HTTP status code (best-effort, short timeout).
func chatopsDeliver(ctx context.Context, url string, payload []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "qeet-logs-chatops/1")
	resp, err := chatopsClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}
