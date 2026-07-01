package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

var deliveryClient = &http.Client{Timeout: 10 * time.Second}

// Deliver sends a notification payload to all channels configured on the rule.
// Errors are logged and skipped — one bad channel must not block the rest.
func Deliver(ctx context.Context, rule AlertRule, payload Payload, notifyURL, notifyKey string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}

	var errs []error
	for _, ch := range rule.Channels {
		switch ch.Type {
		case "webhook":
			if err := postWebhook(ctx, ch.Target, body); err != nil {
				errs = append(errs, fmt.Errorf("webhook %s: %w", ch.Target, err))
			}
		case "qeet_notify":
			if notifyURL != "" && notifyKey != "" {
				if err := postNotify(ctx, notifyURL, notifyKey, ch.Target, payload, body); err != nil {
					errs = append(errs, fmt.Errorf("qeet_notify: %w", err))
				}
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("delivery errors: %v", errs)
	}
	return nil
}

func postWebhook(ctx context.Context, target string, body []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "qeet-logs-alerter/1.0")
	resp, err := deliveryClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("non-2xx response %d", resp.StatusCode)
	}
	return nil
}

// postNotify sends an alert notification via the Qeet Notify API
// (POST /v1/channels/in-app or /v1/channels/webhook depending on configuration).
func postNotify(ctx context.Context, notifyURL, notifyKey, recipient string, payload Payload, raw []byte) error {
	msg := map[string]any{
		"recipient": recipient,
		"template":  "alert-fired",
		"variables": map[string]any{
			"alert_name": payload.AlertName,
			"firing":     payload.Firing,
			"count":      payload.Count,
			"message":    payload.Message,
		},
		"raw_payload": json.RawMessage(raw),
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, notifyURL+"/v1/channels/in-app", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Qeet-Api-Key", notifyKey)
	resp, err := deliveryClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("qeet_notify non-2xx %d", resp.StatusCode)
	}
	return nil
}
