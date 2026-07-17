package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// roundTripFunc adapts a function to an http.RoundTripper for injection in tests.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// toolsCallReq builds a tools/call request for name with raw JSON arguments.
func toolsCallReq(name, args string) *Request {
	params := `{"name":"` + name + `","arguments":` + args + `}`
	return &Request{
		JSONRPC: jsonrpcVersion,
		ID:      json.RawMessage(`1`),
		Method:  "tools/call",
		Params:  json.RawMessage(params),
	}
}

// resultText extracts the first text content block from a tool result.
func resultText(t *testing.T, resp *Response) (string, bool) {
	t.Helper()
	if resp == nil || resp.Error != nil {
		t.Fatalf("unexpected error response: %+v", resp)
	}
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("result wrong type %T", resp.Result)
	}
	content := result["content"].([]map[string]any)
	if len(content) == 0 {
		t.Fatal("empty content")
	}
	text := content[0]["text"].(string)
	isErr, _ := result["isError"].(bool)
	return text, isErr
}

// TestToolsCallQuery dispatches qeet_logs_query against a real httptest server,
// asserting the path, query param, auth header, and returned content.
func TestToolsCallQuery(t *testing.T) {
	const wantQ = `{service="api"} |= "error"`
	body := `{"columns":["ts","message"],"rows":[]}`

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/query" {
			t.Errorf("path = %q, want /v1/query", r.URL.Path)
		}
		if got := r.URL.Query().Get("q"); got != wantQ {
			t.Errorf("q = %q, want %q", got, wantQ)
		}
		if got := r.Header.Get("X-Qeet-Api-Key"); got != "test-key" {
			t.Errorf("X-Qeet-Api-Key = %q, want test-key", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}))
	defer ts.Close()

	srv := newServer(newQueryClient(ts.URL, "test-key"))
	argsJSON, _ := json.Marshal(map[string]string{"q": wantQ})
	resp := srv.handle(context.Background(), toolsCallReq("qeet_logs_query", string(argsJSON)))

	text, isErr := resultText(t, resp)
	if isErr {
		t.Fatalf("unexpected isError result: %s", text)
	}
	if text != body {
		t.Errorf("content text = %q, want %q", text, body)
	}
}

// TestToolsCallDeployCulprits verifies a multi-arg tool forwards service + since.
func TestToolsCallDeployCulprits(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/deploy/culprits" {
			t.Errorf("path = %q, want /v1/deploy/culprits", r.URL.Path)
		}
		if got := r.URL.Query().Get("service"); got != "checkout" {
			t.Errorf("service = %q, want checkout", got)
		}
		if got := r.URL.Query().Get("since"); got != "1h" {
			t.Errorf("since = %q, want 1h", got)
		}
		_, _ = io.WriteString(w, `[]`)
	}))
	defer ts.Close()

	srv := newServer(newQueryClient(ts.URL, "k"))
	resp := srv.handle(context.Background(), toolsCallReq("qeet_logs_deploy_culprits", `{"service":"checkout","since":"1h"}`))
	if text, isErr := resultText(t, resp); isErr {
		t.Fatalf("unexpected error content: %s", text)
	}
}

// TestToolsCallHTTPError exercises the injected round-tripper path and asserts a
// 5xx becomes an isError tool result rather than a JSON-RPC error.
func TestToolsCallHTTPError(t *testing.T) {
	c := newQueryClient("http://qeet-logs.invalid", "k")
	c.http = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusInternalServerError,
			Body:       io.NopCloser(strings.NewReader(`{"error":"boom"}`)),
			Header:     make(http.Header),
		}, nil
	})}

	srv := newServer(c)
	resp := srv.handle(context.Background(), toolsCallReq("qeet_logs_query", `{"q":"x"}`))
	text, isErr := resultText(t, resp)
	if !isErr {
		t.Fatalf("expected isError result, got %s", text)
	}
	if !strings.Contains(text, "500") || !strings.Contains(text, "boom") {
		t.Errorf("error text = %q, want it to mention status 500 and body", text)
	}
}

// TestToolsCallMissingRequiredArg surfaces a missing required arg as isError.
func TestToolsCallMissingRequiredArg(t *testing.T) {
	srv := newServer(newQueryClient("", ""))
	resp := srv.handle(context.Background(), toolsCallReq("qeet_logs_query", `{}`))
	text, isErr := resultText(t, resp)
	if !isErr {
		t.Fatalf("expected isError result, got %s", text)
	}
	if !strings.Contains(text, "q") {
		t.Errorf("error text = %q, want mention of missing arg q", text)
	}
}

// TestToolsCallUnknownTool returns a JSON-RPC invalid-params error.
func TestToolsCallUnknownTool(t *testing.T) {
	srv := newServer(newQueryClient("", ""))
	resp := srv.handle(context.Background(), toolsCallReq("qeet_logs_nope", `{}`))
	if resp == nil || resp.Error == nil {
		t.Fatalf("expected error response, got %+v", resp)
	}
	if resp.Error.Code != codeInvalidParams {
		t.Errorf("error code = %d, want %d", resp.Error.Code, codeInvalidParams)
	}
}
