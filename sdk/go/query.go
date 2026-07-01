package qeetlogs

import (
	"context"
	"net/url"
	"time"
)

// QueryParams controls the log query endpoint.
type QueryParams struct {
	// Q is a LogQL++ query expression, e.g. `service="api" | level="error"`.
	Q string
	// From / To define the time range (RFC 3339 / Unix seconds).
	From time.Time
	To   time.Time
	// Limit caps the number of returned records (default 100, max 1000).
	Limit int
	// Services filters by service name (comma-separated, server handles split).
	Services string
	// Level filters by severity level.
	Level string
}

// LogEntry is a single record returned by the query API.
type LogEntry struct {
	ID         string         `json:"id"`
	TenantID   string         `json:"tenant_id"`
	Timestamp  time.Time      `json:"timestamp"`
	Service    string         `json:"service"`
	Level      string         `json:"level"`
	TraceID    string         `json:"trace_id"`
	Body       string         `json:"body"`
	Attributes map[string]any `json:"attributes"`
}

// QueryResponse wraps the list of records and pagination metadata.
type QueryResponse struct {
	Records []LogEntry `json:"records"`
	Total   int64      `json:"total"`
	HasMore bool       `json:"has_more"`
}

// Query searches stored log records with LogQL++ and time-range filtering.
func (c *Client) Query(ctx context.Context, p QueryParams) (*QueryResponse, error) {
	q := url.Values{}
	if p.Q != "" {
		q.Set("q", p.Q)
	}
	if !p.From.IsZero() {
		q.Set("from", p.From.UTC().Format(time.RFC3339))
	}
	if !p.To.IsZero() {
		q.Set("to", p.To.UTC().Format(time.RFC3339))
	}
	if p.Limit > 0 {
		q.Set("limit", itoa(p.Limit))
	}
	if p.Services != "" {
		q.Set("services", p.Services)
	}
	if p.Level != "" {
		q.Set("level", p.Level)
	}

	var resp QueryResponse
	if err := c.do(ctx, "GET", "/v1/query?"+q.Encode(), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [20]byte{}
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
