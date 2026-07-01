package qeetlogs

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// TailParams controls which service stream to follow.
type TailParams struct {
	// Service is the service name to tail. Required.
	Service string
	// Level filters by severity.
	Level string
}

// TailRecord is a single streamed log event from the live-tail endpoint.
type TailRecord struct {
	Timestamp time.Time `json:"timestamp"`
	Service   string    `json:"service"`
	Level     string    `json:"level"`
	Body      string    `json:"body"`
	TraceID   string    `json:"trace_id"`
}

// Tail connects to the live-tail WebSocket (served over HTTP SSE by the query
// API) and calls onRecord for every received event until ctx is cancelled.
func (c *Client) Tail(ctx context.Context, p TailParams, onRecord func(TailRecord) error) error {
	if p.Service == "" {
		return fmt.Errorf("qeetlogs: TailParams.Service is required")
	}
	q := url.Values{"service": {p.Service}}
	if p.Level != "" {
		q.Set("level", p.Level)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", c.cfg.BaseURL+"/v1/query/tail?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("build tail request: %w", err)
	}
	req.Header.Set("X-Qeet-Api-Key", c.cfg.APIKey)
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("tail connect: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tail: unexpected status %d", resp.StatusCode)
	}

	// Parse SSE stream: "data: <json>\n\n" frames.
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		raw := strings.TrimPrefix(line, "data: ")
		var rec TailRecord
		if err := json.Unmarshal([]byte(raw), &rec); err != nil {
			continue // skip malformed frames
		}
		if err := onRecord(rec); err != nil {
			return err
		}
	}
	return scanner.Err()
}
