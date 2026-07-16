//go:build integration

package integration

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// logsProjection is the default column order the compiler emits for `SELECT *
// FROM logs` (domains/query.defaultProjection).
var logsProjection = []string{"id", "timestamp", "service", "level", "message", "trace_id", "span_id", "body"}

// TestQueryJSONRoundtrip runs a LogQL++ SELECT and asserts the {columns,count,rows}
// envelope: default projection columns, and count == len(rows).
func TestQueryJSONRoundtrip(t *testing.T) {
	c := apiClient(t)
	resp := c.get(qpath("/v1/query", "SELECT * FROM logs"))
	skipIfNoScope(t, resp)
	skipIfUpstreamDown(t, resp)
	requireStatus(t, resp, http.StatusOK)

	var qr queryResult
	resp.decode(t, &qr)
	if !equalStrings(qr.Columns, logsProjection) {
		t.Errorf("columns = %v, want %v", qr.Columns, logsProjection)
	}
	if qr.Count != len(qr.Rows) {
		t.Errorf("count = %d but len(rows) = %d", qr.Count, len(qr.Rows))
	}
}

// TestQueryCSVFormat asserts ?format=csv returns text/csv whose header row is
// exactly the compiled column order.
func TestQueryCSVFormat(t *testing.T) {
	c := apiClient(t)
	resp := c.get(qpath("/v1/query", "SELECT * FROM logs") + "&format=csv")
	skipIfNoScope(t, resp)
	skipIfUpstreamDown(t, resp)
	requireStatus(t, resp, http.StatusOK)

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/csv") {
		t.Errorf("Content-Type = %q, want text/csv", ct)
	}
	rec, err := csv.NewReader(strings.NewReader(string(resp.Body))).Read()
	if err != nil {
		t.Fatalf("read CSV header: %v\n  body: %s", err, truncate(resp.Body))
	}
	if !equalStrings(rec, logsProjection) {
		t.Errorf("CSV header = %v, want %v", rec, logsProjection)
	}
}

// TestQueryNDJSONFormat asserts ?format=ndjson returns application/x-ndjson where
// every non-empty line is a standalone JSON object.
func TestQueryNDJSONFormat(t *testing.T) {
	c := apiClient(t)
	resp := c.get(qpath("/v1/query", "SELECT * FROM logs LIMIT 10") + "&format=ndjson")
	skipIfNoScope(t, resp)
	skipIfUpstreamDown(t, resp)
	requireStatus(t, resp, http.StatusOK)

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/x-ndjson") {
		t.Errorf("Content-Type = %q, want application/x-ndjson", ct)
	}
	for i, line := range strings.Split(strings.TrimSpace(string(resp.Body)), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			t.Fatalf("NDJSON line %d is not a JSON object: %v\n  line: %s", i, err, line)
		}
	}
}

// TestQueryMissingParam asserts a request with no ?q= is a 400.
func TestQueryMissingParam(t *testing.T) {
	c := apiClient(t)
	resp := c.get("/v1/query")
	skipIfNoScope(t, resp)
	requireStatus(t, resp, http.StatusBadRequest)
}

// TestQueryTailRejectedOnSyncEndpoint asserts a TAIL statement is refused by the
// synchronous /v1/query endpoint (it belongs on the WebSocket).
func TestQueryTailRejectedOnSyncEndpoint(t *testing.T) {
	c := apiClient(t)
	resp := c.get(qpath("/v1/query", "TAIL FROM logs WHERE service = 'x'"))
	skipIfNoScope(t, resp)
	requireStatus(t, resp, http.StatusBadRequest)
}

// TestQueryUnknownTableRejected asserts the compiler rejects an unknown table
// (defence-in-depth against arbitrary table access) with a 400.
func TestQueryUnknownTableRejected(t *testing.T) {
	c := apiClient(t)
	resp := c.get(qpath("/v1/query", "SELECT * FROM secrets"))
	skipIfNoScope(t, resp)
	requireStatus(t, resp, http.StatusBadRequest)
}

// TestLiveTailWebSocketConnect asserts the live-tail WS upgrades successfully for
// a valid TAIL statement with a valid key, and closes cleanly. (It does not wait
// for records — a connect check by design; ingest may be idle.)
func TestLiveTailWebSocketConnect(t *testing.T) {
	c := apiClient(t)
	// Cheap read probe so we skip (not fail) if the key lacks logs:read/query.
	if probe := c.get(qpath("/v1/query", "SELECT * FROM logs LIMIT 1")); probe.Status == http.StatusForbidden {
		t.Skipf("key lacks logs:read/query; skipping WS tail: %q", probe.errorMessage())
	}

	wsBase := "ws" + strings.TrimPrefix(apiURL(), "http") // http->ws, https->wss
	u := wsBase + "/v1/query/tail?q=" + url.QueryEscape("TAIL FROM logs WHERE service = '__integration_probe__'")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, u, &websocket.DialOptions{
		HTTPHeader: http.Header{"X-Qeet-Api-Key": {c.key}},
	})
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("live-tail WS handshake failed (http status %d): %v", status, err)
	}
	_ = conn.Close(websocket.StatusNormalClosure, "test done")
}

// TestLiveTailRejectsNonTailStatement asserts the WS endpoint refuses a non-TAIL
// statement *before* upgrading — the handshake returns HTTP 400, not 101.
func TestLiveTailRejectsNonTailStatement(t *testing.T) {
	c := apiClient(t)

	wsBase := "ws" + strings.TrimPrefix(apiURL(), "http")
	u := wsBase + "/v1/query/tail?q=" + url.QueryEscape("SELECT * FROM logs")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, u, &websocket.DialOptions{
		HTTPHeader: http.Header{"X-Qeet-Api-Key": {c.key}},
	})
	if err == nil {
		_ = conn.Close(websocket.StatusNormalClosure, "")
		t.Fatal("WS upgraded for a non-TAIL statement; expected an HTTP 400 rejection")
	}
	if resp == nil {
		t.Fatalf("expected a non-101 HTTP response for a non-TAIL statement, got transport error: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("non-TAIL WS handshake status = %d, want 400", resp.StatusCode)
	}
}

// TestExportJSON asserts the export endpoint returns the query envelope AND marks
// the response as a file download (Content-Disposition: attachment).
func TestExportJSON(t *testing.T) {
	c := apiClient(t)
	resp := c.get(qpath("/v1/export", "SELECT * FROM logs LIMIT 5"))
	skipIfNoScope(t, resp)
	skipIfUpstreamDown(t, resp)
	requireStatus(t, resp, http.StatusOK)

	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("export Content-Disposition = %q, want it to contain \"attachment\"", cd)
	}
	var qr queryResult
	resp.decode(t, &qr)
	if qr.Count != len(qr.Rows) {
		t.Errorf("export count = %d but len(rows) = %d", qr.Count, len(qr.Rows))
	}
}

// TestExportNDJSON asserts the NDJSON export path streams an attachment.
func TestExportNDJSON(t *testing.T) {
	c := apiClient(t)
	resp := c.get(qpath("/v1/export", "SELECT * FROM logs LIMIT 5") + "&format=ndjson")
	skipIfNoScope(t, resp)
	skipIfUpstreamDown(t, resp)
	requireStatus(t, resp, http.StatusOK)
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Errorf("export Content-Disposition = %q, want it to contain \"attachment\"", cd)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
