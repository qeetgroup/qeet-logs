package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
)

// mcpTool is one tool advertised via tools/list and dispatched via tools/call.
type mcpTool struct {
	Name        string
	Description string
	InputSchema map[string]any
	// call executes the tool against the query API using the decoded arguments.
	call func(ctx context.Context, c *queryClient, args map[string]any) (json.RawMessage, error)
}

// buildTools returns the Qeet Logs tools exposed to MCP clients.
func buildTools() []mcpTool {
	return []mcpTool{
		{
			Name:        "qeet_logs_query",
			Description: "Run a LogQL++ query against Qeet Logs and return matching log records for the authenticated tenant.",
			InputSchema: objectSchema(props{
				"q": stringProp(`LogQL++ query string, e.g. {service="api"} |= "error".`),
			}, []string{"q"}),
			call: func(ctx context.Context, c *queryClient, args map[string]any) (json.RawMessage, error) {
				q, err := requireString(args, "q")
				if err != nil {
					return nil, err
				}
				return c.get(ctx, "/v1/query", url.Values{"q": {q}})
			},
		},
		{
			Name:        "qeet_logs_incidents",
			Description: "List incidents detected by Qeet Logs, optionally filtered by status.",
			InputSchema: objectSchema(props{
				"status": stringProp("Optional incident status filter, e.g. open, acknowledged, resolved."),
			}, nil),
			call: func(ctx context.Context, c *queryClient, args map[string]any) (json.RawMessage, error) {
				params := url.Values{}
				if s := optString(args, "status"); s != "" {
					params.Set("status", s)
				}
				return c.get(ctx, "/v1/incidents", params)
			},
		},
		{
			Name:        "qeet_logs_rca",
			Description: "Run automated root-cause analysis for a service over a time window.",
			InputSchema: objectSchema(props{
				"service": stringProp("Service name to analyze."),
				"since":   stringProp("Optional lookback window, e.g. 15m, 1h, 24h."),
			}, []string{"service"}),
			call: func(ctx context.Context, c *queryClient, args map[string]any) (json.RawMessage, error) {
				service, err := requireString(args, "service")
				if err != nil {
					return nil, err
				}
				params := url.Values{"service": {service}}
				if s := optString(args, "since"); s != "" {
					params.Set("since", s)
				}
				return c.get(ctx, "/v1/rca", params)
			},
		},
		{
			Name:        "qeet_logs_topology",
			Description: "Return the observed service dependency topology, optionally scoped to a service and time window.",
			InputSchema: objectSchema(props{
				"service": stringProp("Optional service name to center the topology on."),
				"since":   stringProp("Optional lookback window, e.g. 1h, 24h."),
			}, nil),
			call: func(ctx context.Context, c *queryClient, args map[string]any) (json.RawMessage, error) {
				params := url.Values{}
				if s := optString(args, "service"); s != "" {
					params.Set("service", s)
				}
				if s := optString(args, "since"); s != "" {
					params.Set("since", s)
				}
				return c.get(ctx, "/v1/topology", params)
			},
		},
		{
			Name:        "qeet_logs_deploy_culprits",
			Description: "Identify recent deploys likely responsible for a service's error spike.",
			InputSchema: objectSchema(props{
				"service": stringProp("Service name to investigate."),
				"since":   stringProp("Optional lookback window, e.g. 1h, 24h."),
			}, []string{"service"}),
			call: func(ctx context.Context, c *queryClient, args map[string]any) (json.RawMessage, error) {
				service, err := requireString(args, "service")
				if err != nil {
					return nil, err
				}
				params := url.Values{"service": {service}}
				if s := optString(args, "since"); s != "" {
					params.Set("since", s)
				}
				return c.get(ctx, "/v1/deploy/culprits", params)
			},
		},
	}
}

// props is a JSON-schema "properties" map.
type props map[string]any

// stringProp builds a string-typed JSON-schema property with a description.
func stringProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}

// objectSchema builds a JSON-schema object with the given properties and, when
// non-empty, a "required" list.
func objectSchema(properties props, required []string) map[string]any {
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any(properties),
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

// requireString extracts a non-empty string argument, returning an error the
// caller surfaces to the agent as an isError tool result.
func requireString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required argument %q", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %q must be a string", key)
	}
	if s == "" {
		return "", fmt.Errorf("argument %q must not be empty", key)
	}
	return s, nil
}

// optString returns a string argument if present and well-typed, else "".
func optString(args map[string]any, key string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
