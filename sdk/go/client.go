// Package qeetlogs provides the official Go SDK for the Qeet Logs API.
// It covers log ingest, structured querying, live tail, and admin operations.
//
// Quick start:
//
//	client, err := qeetlogs.New(qeetlogs.Config{
//	    APIKey:   os.Getenv("QEET_LOGS_API_KEY"),
//	    BaseURL:  "https://api.logs.qeet.in",
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close()
package qeetlogs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultBaseURL = "https://api.logs.qeet.in"

// Config holds connection settings for the Qeet Logs API.
type Config struct {
	// APIKey is the X-Qeet-Api-Key credential. Required.
	APIKey string
	// BaseURL overrides the default API endpoint. Optional.
	BaseURL string
	// HTTPClient is an optional custom HTTP transport. Defaults to a client
	// with 30-second timeout.
	HTTPClient *http.Client
}

// Client is the Qeet Logs API client. It is safe for concurrent use.
type Client struct {
	cfg  Config
	http *http.Client
}

// New creates and validates a new Client.
func New(cfg Config) (*Client, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("qeetlogs: APIKey is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultBaseURL
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{cfg: cfg, http: hc}, nil
}

// Close is a no-op for the HTTP client but follows the resource-cleanup convention.
func (c *Client) Close() {}

// do executes a request and decodes a JSON response body into v.
func (c *Client) do(ctx context.Context, method, path string, body, v any) error {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("X-Qeet-Api-Key", c.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return &APIError{Status: resp.StatusCode, Body: string(raw)}
	}

	if v != nil {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}

// APIError is returned for HTTP 4xx / 5xx responses.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("qeetlogs API error %d: %s", e.Status, e.Body)
}
