package main

import (
	"encoding/json"
	"testing"
)

func TestRequestDecode(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"x"}}`)
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if req.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want 2.0", req.JSONRPC)
	}
	if string(req.ID) != "7" {
		t.Errorf("id = %q, want 7", string(req.ID))
	}
	if req.Method != "tools/call" {
		t.Errorf("method = %q, want tools/call", req.Method)
	}
	if req.IsNotification() {
		t.Error("request with id should not be a notification")
	}
}

func TestRequestDecodeNotification(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !req.IsNotification() {
		t.Error("request without id should be a notification")
	}
}

func TestResponseEncodeResult(t *testing.T) {
	resp := newResult(json.RawMessage(`42`), map[string]any{"ok": true})
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %v, want 2.0", got["jsonrpc"])
	}
	if got["id"] != float64(42) {
		t.Errorf("id = %v, want 42", got["id"])
	}
	if _, ok := got["result"]; !ok {
		t.Error("result member missing")
	}
	if _, ok := got["error"]; ok {
		t.Error("success response must not carry an error member")
	}
}

func TestResponseEncodeError(t *testing.T) {
	resp := newError(json.RawMessage(`"abc"`), codeMethodNotFound, "method not found: foo")
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   *RPCError       `json:"error"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.JSONRPC != "2.0" {
		t.Errorf("jsonrpc = %q, want 2.0", got.JSONRPC)
	}
	if string(got.ID) != `"abc"` {
		t.Errorf("id = %s, want \"abc\"", string(got.ID))
	}
	if len(got.Result) != 0 {
		t.Errorf("error response must omit result, got %s", string(got.Result))
	}
	if got.Error == nil || got.Error.Code != codeMethodNotFound {
		t.Fatalf("error = %+v, want code %d", got.Error, codeMethodNotFound)
	}
}

func TestResponseEncodeNullID(t *testing.T) {
	// A parse-error response has no known id and must emit JSON null.
	resp := newError(nil, codeParseError, "parse error")
	b, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got map[string]json.RawMessage
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if string(got["id"]) != "null" {
		t.Errorf("id = %s, want null", string(got["id"]))
	}
}
