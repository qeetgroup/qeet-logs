package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// defaultBaseURL is the local dev address of the qeet-logs query API.
const defaultBaseURL = "http://localhost:8100"

// queryClient talks to the qeet-logs query API over HTTP. It mirrors the stdlib
// net/http style of the Go SDK (github.com/qeetgroup/qeet-logs-server/sdk/go): a plain
// *http.Client with a 30s timeout, authenticated via the X-Qeet-Api-Key header.
type queryClient struct {
	baseURL string
	apiKey  string
	http    *http.Client
}

// newQueryClient builds a client. An empty baseURL falls back to defaultBaseURL.
func newQueryClient(baseURL, apiKey string) *queryClient {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &queryClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		http:    &http.Client{Timeout: 30 * time.Second},
	}
}

// get issues an authenticated GET to path (optionally with query params) and
// returns the raw JSON response body. A 4xx/5xx status yields an error whose
// message includes the response body, so callers can surface it to the agent.
func (c *queryClient) get(ctx context.Context, path string, params url.Values) (json.RawMessage, error) {
	u := c.baseURL + path
	if len(params) > 0 {
		u += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Qeet-Api-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("qeet-logs API error %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.RawMessage(body), nil
}
