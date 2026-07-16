// Package notify is the Qeet Logs → Qeet Notify integration for regional-
// language alert delivery (PRD Module 27.5 / P2-G8). Qeet Logs does NOT localise
// or fan out notifications itself; that is Qeet Notify's job (the sibling
// multi-channel product, which speaks 12+ Indian languages). This package is a
// thin, honest client over Qeet Notify's trigger API plus the pure locale-
// resolution logic that decides which language tag to send.
//
// It adds no new delivery infrastructure. When QEET_NOTIFY_URL / QEET_NOTIFY_API
// _KEY are unset (cfg.QeetNotifyURL/APIKey), Trigger is a no-op that returns
// ErrNotConfigured — never a fabricated success — so callers can degrade safely.
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ErrNotConfigured is returned by Trigger when the Qeet Notify URL or API key is
// empty. Delivery cannot happen without a running Qeet Notify; this is surfaced,
// not swallowed.
var ErrNotConfigured = errors.New("qeet notify not configured (QEET_NOTIFY_URL / QEET_NOTIFY_API_KEY unset)")

// DefaultLocale is the platform fallback when no supported locale is resolved.
const DefaultLocale = "en"

// SupportedLocales are the BCP-47 language tags Qeet Notify can render alert
// templates in (India-first, matching Qeet Notify's template catalogue). Kept as
// a set so IsSupported is O(1). Extend in lockstep with Qeet Notify.
var SupportedLocales = map[string]bool{
	"en": true, // English
	"hi": true, // Hindi
	"bn": true, // Bengali
	"ta": true, // Tamil
	"te": true, // Telugu
	"mr": true, // Marathi
	"gu": true, // Gujarati
	"kn": true, // Kannada
	"ml": true, // Malayalam
	"pa": true, // Punjabi
	"or": true, // Odia
	"as": true, // Assamese
	"ur": true, // Urdu
}

// IsSupported reports whether locale (case-insensitive, region-stripped) is a
// language Qeet Notify can render. "hi-IN" is treated as "hi".
func IsSupported(locale string) bool {
	return SupportedLocales[normalize(locale)]
}

// ResolveLocale picks the language tag for a notification via a deterministic
// fallback chain: a supported per-recipient preference wins; else a supported
// per-tenant default; else the platform DefaultLocale. Pure — no I/O.
func ResolveLocale(tenantDefault, recipientPref string) string {
	if l := normalize(recipientPref); SupportedLocales[l] {
		return l
	}
	if l := normalize(tenantDefault); SupportedLocales[l] {
		return l
	}
	return DefaultLocale
}

func normalize(locale string) string {
	l := strings.ToLower(strings.TrimSpace(locale))
	if i := strings.IndexAny(l, "-_"); i >= 0 {
		l = l[:i] // strip region/script subtag: hi-IN → hi
	}
	return l
}

// Client posts alert notifications to Qeet Notify. Construct with New; a zero
// Client (empty URL/APIKey) makes Trigger a no-op returning ErrNotConfigured.
type Client struct {
	url    string
	apiKey string
	http   *http.Client
}

// New builds a Client from the resolved Qeet Notify URL + API key
// (cfg.QeetNotifyURL / cfg.QeetNotifyAPIKey).
func New(url, apiKey string) *Client {
	return &Client{
		url:    strings.TrimRight(url, "/"),
		apiKey: apiKey,
		http:   &http.Client{Timeout: 10 * time.Second},
	}
}

// Configured reports whether the client can actually deliver.
func (c *Client) Configured() bool { return c.url != "" && c.apiKey != "" }

// Trigger sends one notification to Qeet Notify's in-app channel endpoint
// (mirroring the alerter's existing postNotify path) with a resolved locale tag,
// so Qeet Notify renders the template in the recipient's language. Returns
// ErrNotConfigured when the client is not configured — no network call is made.
func (c *Client) Trigger(ctx context.Context, template, recipient, locale string, variables map[string]any) error {
	if !c.Configured() {
		return ErrNotConfigured
	}
	msg := map[string]any{
		"recipient": recipient,
		"template":  template,
		"locale":    locale,
		"variables": variables,
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/v1/channels/in-app", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Qeet-Api-Key", c.apiKey)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("qeet notify non-2xx %d", resp.StatusCode)
	}
	return nil
}
