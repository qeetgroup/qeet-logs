package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
)

// MCP server identity, reported via the initialize handshake.
const (
	// protocolVersion is the MCP revision advertised when the client does not
	// request one. When the client requests a version, we echo it back.
	protocolVersion = "2024-11-05"
	serverName      = "qeet-logs-mcp"
	serverVersion   = "0.1.0"
)

// server implements the MCP protocol subset over a stdio JSON-RPC transport.
type server struct {
	client *queryClient
	tools  []mcpTool
}

// newServer wires a server to a query-API client with the default tool set.
func newServer(client *queryClient) *server {
	return &server{client: client, tools: buildTools()}
}

// run reads newline-delimited JSON-RPC messages from in and writes responses to
// out until in is exhausted. Notifications produce no response.
func (s *server) run(in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	writer := bufio.NewWriter(out)
	defer writer.Flush() //nolint:errcheck

	for {
		line, readErr := reader.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(line); len(trimmed) > 0 {
			if resp := s.handleLine(context.Background(), trimmed); resp != nil {
				if err := writeMessage(writer, resp); err != nil {
					return err
				}
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				return nil
			}
			return readErr
		}
	}
}

// writeMessage marshals resp and writes it as a single newline-terminated line.
func writeMessage(w *bufio.Writer, resp *Response) error {
	b, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	if _, err := w.Write(b); err != nil {
		return err
	}
	if err := w.WriteByte('\n'); err != nil {
		return err
	}
	return w.Flush()
}

// handleLine decodes one JSON-RPC message and returns its response (or nil for
// notifications and for malformed notifications that must not be answered).
func (s *server) handleLine(ctx context.Context, line []byte) *Response {
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return newError(nil, codeParseError, "parse error: "+err.Error())
	}
	return s.handle(ctx, &req)
}

// handle dispatches a decoded request to the matching MCP method.
func (s *server) handle(ctx context.Context, req *Request) *Response {
	switch req.Method {
	case "initialize":
		return newResult(req.ID, s.initializeResult(req.Params))
	case "notifications/initialized":
		return nil // notification: no-op, no response
	case "ping":
		return newResult(req.ID, map[string]any{})
	case "tools/list":
		return newResult(req.ID, s.toolsListResult())
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	default:
		if req.IsNotification() {
			return nil // never respond to notifications, even unknown ones
		}
		return newError(req.ID, codeMethodNotFound, "method not found: "+req.Method)
	}
}

// initializeResult builds the initialize handshake response. It echoes the
// client's requested protocolVersion when supplied, else advertises the default.
func (s *server) initializeResult(params json.RawMessage) map[string]any {
	version := protocolVersion
	if len(params) > 0 {
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if err := json.Unmarshal(params, &p); err == nil && p.ProtocolVersion != "" {
			version = p.ProtocolVersion
		}
	}
	return map[string]any{
		"protocolVersion": version,
		"serverInfo": map[string]any{
			"name":    serverName,
			"version": serverVersion,
		},
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
	}
}

// toolsListResult advertises the available tools with their JSON-schema inputs.
func (s *server) toolsListResult() map[string]any {
	list := make([]map[string]any, 0, len(s.tools))
	for _, t := range s.tools {
		list = append(list, map[string]any{
			"name":        t.Name,
			"description": t.Description,
			"inputSchema": t.InputSchema,
		})
	}
	return map[string]any{"tools": list}
}

// handleToolsCall dispatches a tools/call request: it locates the tool, invokes
// it, and wraps the JSON result (or error) as MCP tool content.
func (s *server) handleToolsCall(ctx context.Context, req *Request) *Response {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if len(req.Params) > 0 {
		if err := json.Unmarshal(req.Params, &params); err != nil {
			return newError(req.ID, codeInvalidParams, "invalid params: "+err.Error())
		}
	}

	tool := s.findTool(params.Name)
	if tool == nil {
		return newError(req.ID, codeInvalidParams, "unknown tool: "+params.Name)
	}

	raw, err := tool.call(ctx, s.client, params.Arguments)
	if err != nil {
		return newResult(req.ID, toolErrorContent(err.Error()))
	}
	return newResult(req.ID, toolTextContent(string(raw)))
}

// findTool returns the tool with the given name, or nil.
func (s *server) findTool(name string) *mcpTool {
	for i := range s.tools {
		if s.tools[i].Name == name {
			return &s.tools[i]
		}
	}
	return nil
}

// toolTextContent wraps a text payload as a successful MCP tool result.
func toolTextContent(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
	}
}

// toolErrorContent wraps an error message as a failed MCP tool result so the
// agent can read what went wrong.
func toolErrorContent(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
		"isError": true,
	}
}
