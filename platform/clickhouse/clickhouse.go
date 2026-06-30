// Package clickhouse is a thin client over the ClickHouse HTTP interface.
//
// Phase-0 (M0) ships only Ping (readiness) and Exec (used by M1 to apply DDL
// and by later milestones for inserts/queries). The high-throughput batch
// writer lives in the Rust ingest service, which talks the native protocol;
// this Go client handles DDL and the read/query path.
package clickhouse

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL  string
	database string
	user     string
	password string
	http     *http.Client
}

// New constructs a ClickHouse HTTP client. baseURL is e.g. http://localhost:8123.
func New(baseURL, database, user, password string) *Client {
	return &Client{
		baseURL:  strings.TrimRight(baseURL, "/"),
		database: database,
		user:     user,
		password: password,
		http:     &http.Client{Timeout: 30 * time.Second},
	}
}

// Ping verifies ClickHouse is reachable (used by the /readyz probe).
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/ping", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("clickhouse ping: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("clickhouse ping: status %d", resp.StatusCode)
	}
	return nil
}

// Exec runs a statement (DDL or insert) against the configured database and
// returns an error if ClickHouse responds non-2xx.
func (c *Client) Exec(ctx context.Context, query string) error {
	q := url.Values{}
	q.Set("database", c.database)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/?"+q.Encode(), strings.NewReader(query))
	if err != nil {
		return err
	}
	c.auth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("clickhouse exec: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("clickhouse exec: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// Query runs a read-only statement and decodes the result rows from
// JSONEachRow. Callers must NOT include a FORMAT clause — it is appended.
// This is the Go read path used by the LogQL++ query API (M3); the
// high-throughput write path lives in the Rust writer (M2).
func (c *Client) Query(ctx context.Context, sql string) ([]map[string]any, error) {
	q := url.Values{}
	q.Set("database", c.database)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/?"+q.Encode(), strings.NewReader(sql+"\nFORMAT JSONEachRow"))
	if err != nil {
		return nil, err
	}
	c.auth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("clickhouse query: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("clickhouse query: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var rows []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(string(body)), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			return nil, fmt.Errorf("decode row: %w", err)
		}
		rows = append(rows, m)
	}
	return rows, nil
}

// Insert writes rows to a table via JSONEachRow. Map keys must match column
// names; timestamps may be RFC3339 strings (best_effort parsing is enabled).
// Convenience helper for the seed tool and tests — production ingest uses the
// Rust writer over the native protocol.
func (c *Client) Insert(ctx context.Context, table string, rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	for _, r := range rows {
		if err := enc.Encode(r); err != nil {
			return fmt.Errorf("encode row: %w", err)
		}
	}
	q := url.Values{}
	q.Set("database", c.database)
	q.Set("query", "INSERT INTO "+table+" FORMAT JSONEachRow")
	q.Set("date_time_input_format", "best_effort")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/?"+q.Encode(), &buf)
	if err != nil {
		return err
	}
	c.auth(req)
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("clickhouse insert: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("clickhouse insert: status %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

func (c *Client) auth(req *http.Request) {
	if c.user != "" {
		req.Header.Set("X-ClickHouse-User", c.user)
	}
	if c.password != "" {
		req.Header.Set("X-ClickHouse-Key", c.password)
	}
}
