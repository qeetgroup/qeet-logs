package main

import "encoding/json"

// jsonrpcVersion is the only JSON-RPC version this server speaks.
const jsonrpcVersion = "2.0"

// Standard JSON-RPC 2.0 error codes.
const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// Request is a decoded JSON-RPC 2.0 request. When ID is absent the message is a
// notification and MUST NOT be answered with a response.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// IsNotification reports whether the request omits an id (JSON-RPC notification).
func (r *Request) IsNotification() bool { return len(r.ID) == 0 }

// Response is a JSON-RPC 2.0 response. Exactly one of Result / Error is set; ID
// echoes the request id (or null when the id could not be determined).
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// normalizeID collapses an empty id to nil so it marshals as JSON null (a
// zero-length, non-nil json.RawMessage would otherwise emit invalid JSON).
func normalizeID(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return nil
	}
	return id
}

// newResult builds a success response echoing the request id.
func newResult(id json.RawMessage, result any) *Response {
	return &Response{JSONRPC: jsonrpcVersion, ID: normalizeID(id), Result: result}
}

// newError builds an error response echoing the request id.
func newError(id json.RawMessage, code int, message string) *Response {
	return &Response{
		JSONRPC: jsonrpcVersion,
		ID:      normalizeID(id),
		Error:   &RPCError{Code: code, Message: message},
	}
}
