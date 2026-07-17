//go:build integration

// Package integration holds black-box integration tests for the qeet-logs query
// API. They exercise a *running* server (default http://localhost:8100) over
// real HTTP + WebSocket — they do NOT import the server packages — so they are
// gated behind the `integration` build tag and are never compiled by the default
// `go test ./...`.
//
// Environment:
//
//	QEET_LOGS_API_URL          query API base URL          (default http://localhost:8100)
//	QEET_LOGS_API_KEY          primary key — should carry logs:admin + logs:read/query + logs:export
//	QEET_LOGS_API_KEY_READONLY optional key WITHOUT logs:admin (auth scope test; auto-minted otherwise)
//	QEET_LOGS_API_KEY_B        optional SECOND tenant's admin key (cross-tenant isolation test)
//	QEET_LOGS_INGEST_URL       Rust ingest gateway base URL (default http://localhost:8101)
//
// Every test self-skips (t.Skip) when the API is unreachable or the required key
// is absent, so the suite is safe to invoke without infra.
//
// Run:  go test -tags=integration ./test/integration/...
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const (
	defaultAPIURL    = "http://localhost:8100"
	defaultIngestURL = "http://localhost:8101"
)

// ── environment ──────────────────────────────────────────────────────────────

func apiURL() string {
	if v := os.Getenv("QEET_LOGS_API_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultAPIURL
}

func ingestURL() string {
	if v := os.Getenv("QEET_LOGS_INGEST_URL"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return defaultIngestURL
}

func primaryKey() string  { return os.Getenv("QEET_LOGS_API_KEY") }
func readOnlyKey() string { return os.Getenv("QEET_LOGS_API_KEY_READONLY") }
func tenantBKey() string  { return os.Getenv("QEET_LOGS_API_KEY_B") }

// ── typed HTTP client ────────────────────────────────────────────────────────

// client is a small typed HTTP client bound to a base URL + API key. It reads
// the full response body once so callers can assert on both status and shape.
type client struct {
	t       *testing.T
	baseURL string
	key     string
	http    *http.Client
}

func newClient(t *testing.T, baseURL, key string) *client {
	return &client{
		t:       t,
		baseURL: strings.TrimRight(baseURL, "/"),
		key:     key,
		http:    &http.Client{Timeout: 20 * time.Second},
	}
}

// apiClient returns a client for the query API keyed by QEET_LOGS_API_KEY. It
// skips the test when the key is unset or the API is unreachable.
func apiClient(t *testing.T) *client {
	t.Helper()
	key := primaryKey()
	if key == "" {
		t.Skip("QEET_LOGS_API_KEY not set; skipping integration test")
	}
	c := newClient(t, apiURL(), key)
	c.skipIfUnreachable()
	return c
}

// publicClient returns a client for the API's unauthenticated probes. It skips
// only when the API is unreachable (no key required).
func publicClient(t *testing.T) *client {
	t.Helper()
	c := newClient(t, apiURL(), primaryKey())
	c.skipIfUnreachable()
	return c
}

// skipIfUnreachable pings /healthz and skips the test when the API cannot be
// reached, so the suite stays green when infra is absent.
func (c *client) skipIfUnreachable() {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/healthz", nil)
	if err != nil {
		c.t.Skipf("cannot build health probe for %s: %v", c.baseURL, err)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Skipf("query API unreachable at %s (%v); start it with `make dev`", c.baseURL, err)
	}
	_ = resp.Body.Close()
}

// response captures a completed HTTP response.
type response struct {
	Status int
	Header http.Header
	Body   []byte
}

func (r *response) decode(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.Body, v); err != nil {
		t.Fatalf("decode JSON (status %d): %v\n  body: %s", r.Status, err, truncate(r.Body))
	}
}

// errorMessage extracts the {"error": "..."} field the API returns on failure.
func (r *response) errorMessage() string {
	var e struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(r.Body, &e)
	return e.Error
}

// queryResult is the {columns,count,rows} envelope the query/export API returns.
type queryResult struct {
	Columns []string         `json:"columns"`
	Count   int              `json:"count"`
	Rows    []map[string]any `json:"rows"`
}

// newAPIKey mirrors database.NewAPIKey, which has NO json tags — so encoding/json
// emits the exported Go field names verbatim ("ID", "Key", "Scopes", …).
type newAPIKey struct {
	ID     string   `json:"ID"`
	Key    string   `json:"Key"`
	Scopes []string `json:"Scopes"`
}

func (c *client) do(method, path string, body any) *response {
	c.t.Helper()
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("marshal body for %s %s: %v", method, path, err)
		}
		rdr = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		c.t.Fatalf("build request %s %s: %v", method, path, err)
	}
	if c.key != "" {
		req.Header.Set("X-Qeet-Api-Key", c.key)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("read body %s %s: %v", method, path, err)
	}
	return &response{Status: resp.StatusCode, Header: resp.Header.Clone(), Body: raw}
}

func (c *client) get(path string) *response          { return c.do(http.MethodGet, path, nil) }
func (c *client) post(path string, b any) *response  { return c.do(http.MethodPost, path, b) }
func (c *client) put(path string, b any) *response   { return c.do(http.MethodPut, path, b) }
func (c *client) patch(path string, b any) *response { return c.do(http.MethodPatch, path, b) }
func (c *client) delete(path string) *response       { return c.do(http.MethodDelete, path, nil) }

// ── assertions / guards ──────────────────────────────────────────────────────

// requireStatus fails unless resp.Status is one of want.
func requireStatus(t *testing.T, resp *response, want ...int) {
	t.Helper()
	for _, w := range want {
		if resp.Status == w {
			return
		}
	}
	t.Fatalf("unexpected status %d (want %v)\n  error: %q\n  body: %s",
		resp.Status, want, resp.errorMessage(), truncate(resp.Body))
}

// skipIfNoScope skips when the caller's key lacks the scope a route needs (403).
func skipIfNoScope(t *testing.T, resp *response) {
	t.Helper()
	if resp.Status == http.StatusForbidden {
		t.Skipf("API key lacks the scope this route requires (403: %q)", resp.errorMessage())
	}
}

// skipIfUpstreamDown skips when a read hit ClickHouse and it was unavailable /
// unmigrated (502) — that is an infra-setup condition, not an API-code failure.
func skipIfUpstreamDown(t *testing.T, resp *response) {
	t.Helper()
	if resp.Status == http.StatusBadGateway {
		t.Skipf("upstream query execution unavailable (ClickHouse not migrated/reachable? run `make ch-migrate`): %q",
			resp.errorMessage())
	}
}

// requireAdmin probes an admin route and skips the whole test when the primary
// key cannot act as logs:admin.
func requireAdmin(t *testing.T, c *client) {
	t.Helper()
	resp := c.get("/v1/admin/api-keys")
	switch resp.Status {
	case http.StatusOK:
		return
	case http.StatusForbidden, http.StatusUnauthorized:
		t.Skipf("QEET_LOGS_API_KEY cannot act as logs:admin (status %d: %q); skipping admin test",
			resp.Status, resp.errorMessage())
	default:
		t.Fatalf("probing /v1/admin/api-keys failed: status %d, body %s", resp.Status, truncate(resp.Body))
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

// qpath builds a /v1/query (or any) path with a URL-escaped ?q= LogQL++ value.
func qpath(base, q string) string {
	return base + "?q=" + url.QueryEscape(q)
}

// mintScopedKey uses the admin client to create a key with exactly the given
// scopes, returning its raw key + id. Skips when the primary key is not admin.
func mintScopedKey(t *testing.T, admin *client, scopes []string) (rawKey, id string) {
	t.Helper()
	resp := admin.post("/v1/admin/api-keys", map[string]any{
		"name":   "integration-scoped-" + uniq(),
		"scopes": scopes,
	})
	if resp.Status == http.StatusForbidden || resp.Status == http.StatusUnauthorized {
		t.Skipf("cannot mint scoped key (primary key not logs:admin, status %d)", resp.Status)
	}
	requireStatus(t, resp, http.StatusCreated)
	var nk newAPIKey
	resp.decode(t, &nk)
	if nk.Key == "" || nk.ID == "" {
		t.Fatalf("minted key missing Key/ID: %s", truncate(resp.Body))
	}
	return nk.Key, nk.ID
}

// revokeKey best-effort deletes a minted key (used in defer for cleanup).
func revokeKey(c *client, id string) {
	if id == "" {
		return
	}
	_ = c.delete("/v1/admin/api-keys/" + id)
}

var uniqCounter atomic.Int64

// uniq returns a short unique-per-run token for naming created resources.
func uniq() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), uniqCounter.Add(1))
}

func truncate(b []byte) string {
	const max = 512
	if len(b) > max {
		return string(b[:max]) + "…"
	}
	return string(b)
}
