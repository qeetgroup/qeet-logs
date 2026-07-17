package main

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func newTestServer() *server {
	return newServer(newQueryClient("", ""))
}

func TestInitialize(t *testing.T) {
	srv := newTestServer()
	req := &Request{
		JSONRPC: jsonrpcVersion,
		ID:      json.RawMessage(`1`),
		Method:  "initialize",
		Params:  json.RawMessage(`{"protocolVersion":"2025-06-18"}`),
	}
	resp := srv.handle(context.Background(), req)
	if resp == nil || resp.Error != nil {
		t.Fatalf("initialize returned error: %+v", resp)
	}
	result := resp.Result.(map[string]any)
	if result["protocolVersion"] != "2025-06-18" {
		t.Errorf("protocolVersion = %v, want echoed 2025-06-18", result["protocolVersion"])
	}
	caps := result["capabilities"].(map[string]any)
	if _, ok := caps["tools"]; !ok {
		t.Error("capabilities.tools missing")
	}
	info := result["serverInfo"].(map[string]any)
	if info["name"] != serverName {
		t.Errorf("serverInfo.name = %v, want %s", info["name"], serverName)
	}
}

func TestInitializeDefaultVersion(t *testing.T) {
	srv := newTestServer()
	req := &Request{JSONRPC: jsonrpcVersion, ID: json.RawMessage(`1`), Method: "initialize"}
	resp := srv.handle(context.Background(), req)
	result := resp.Result.(map[string]any)
	if result["protocolVersion"] != protocolVersion {
		t.Errorf("protocolVersion = %v, want default %s", result["protocolVersion"], protocolVersion)
	}
}

func TestToolsListShape(t *testing.T) {
	srv := newTestServer()
	req := &Request{JSONRPC: jsonrpcVersion, ID: json.RawMessage(`2`), Method: "tools/list"}
	resp := srv.handle(context.Background(), req)
	if resp == nil || resp.Error != nil {
		t.Fatalf("tools/list returned error: %+v", resp)
	}
	result := resp.Result.(map[string]any)
	tools, ok := result["tools"].([]map[string]any)
	if !ok {
		t.Fatalf("tools has wrong type %T", result["tools"])
	}

	want := map[string]bool{
		"qeet_logs_query":           false,
		"qeet_logs_incidents":       false,
		"qeet_logs_rca":             false,
		"qeet_logs_topology":        false,
		"qeet_logs_deploy_culprits": false,
	}
	if len(tools) != len(want) {
		t.Fatalf("advertised %d tools, want %d", len(tools), len(want))
	}
	for _, tool := range tools {
		name, _ := tool["name"].(string)
		if _, expected := want[name]; !expected {
			t.Errorf("unexpected tool %q", name)
			continue
		}
		want[name] = true
		if desc, _ := tool["description"].(string); desc == "" {
			t.Errorf("tool %q has empty description", name)
		}
		schema, ok := tool["inputSchema"].(map[string]any)
		if !ok {
			t.Errorf("tool %q inputSchema wrong type %T", name, tool["inputSchema"])
			continue
		}
		if schema["type"] != "object" {
			t.Errorf("tool %q inputSchema.type = %v, want object", name, schema["type"])
		}
		if _, ok := schema["properties"].(map[string]any); !ok {
			t.Errorf("tool %q inputSchema missing properties", name)
		}
	}
	for name, seen := range want {
		if !seen {
			t.Errorf("tool %q not advertised", name)
		}
	}

	// The advertised schema must marshal to valid JSON.
	if _, err := json.Marshal(result); err != nil {
		t.Fatalf("tools/list result not JSON-serializable: %v", err)
	}
}

func TestUnknownMethod(t *testing.T) {
	srv := newTestServer()
	req := &Request{JSONRPC: jsonrpcVersion, ID: json.RawMessage(`9`), Method: "does/not/exist"}
	resp := srv.handle(context.Background(), req)
	if resp == nil || resp.Error == nil {
		t.Fatalf("expected error response, got %+v", resp)
	}
	if resp.Error.Code != codeMethodNotFound {
		t.Errorf("error code = %d, want %d", resp.Error.Code, codeMethodNotFound)
	}
}

func TestNotificationsInitializedNoResponse(t *testing.T) {
	srv := newTestServer()
	req := &Request{JSONRPC: jsonrpcVersion, Method: "notifications/initialized"}
	if resp := srv.handle(context.Background(), req); resp != nil {
		t.Errorf("notification should produce no response, got %+v", resp)
	}
}

func TestUnknownNotificationNoResponse(t *testing.T) {
	srv := newTestServer()
	// No id → notification. Unknown notifications must not be answered.
	req := &Request{JSONRPC: jsonrpcVersion, Method: "notifications/cancelled"}
	if resp := srv.handle(context.Background(), req); resp != nil {
		t.Errorf("unknown notification should produce no response, got %+v", resp)
	}
}

func TestRunLoop(t *testing.T) {
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}` + "\n" +
			`{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"tools/list"}` + "\n",
	)
	var out bytes.Buffer
	srv := newTestServer()
	if err := srv.run(in, &out); err != nil {
		t.Fatalf("run: %v", err)
	}

	var ids []float64
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var resp map[string]any
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			t.Fatalf("response line not valid JSON (%q): %v", line, err)
		}
		ids = append(ids, resp["id"].(float64))
	}
	// The notification in the middle must produce no output line.
	if len(ids) != 2 {
		t.Fatalf("got %d responses, want 2 (notification must be silent)", len(ids))
	}
	if ids[0] != 1 || ids[1] != 2 {
		t.Errorf("response ids = %v, want [1 2]", ids)
	}
}

func TestHandleLineParseError(t *testing.T) {
	srv := newTestServer()
	resp := srv.handleLine(context.Background(), []byte(`{not json`))
	if resp == nil || resp.Error == nil {
		t.Fatalf("expected parse error response, got %+v", resp)
	}
	if resp.Error.Code != codeParseError {
		t.Errorf("error code = %d, want %d", resp.Error.Code, codeParseError)
	}
	if len(resp.ID) != 0 {
		t.Errorf("parse-error id should be nil (marshals to null), got %s", string(resp.ID))
	}
}
