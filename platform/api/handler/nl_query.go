package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// schemaContext is injected into every NL-to-query prompt so the model knows
// the exact LogQL++ grammar and available tables/columns.
const schemaContext = `You are a LogQL++ query assistant for the Qeet Logs platform.

## LogQL++ grammar
SELECT <columns|*> FROM <table> [WHERE <predicates>] [ORDER BY <col> [ASC|DESC]] [LIMIT n]

Tables and key columns:
- logs: id, timestamp, service, environment, level (trace/debug/info/warn/error/fatal), message, trace_id, span_id, user_linkage_key, git_sha, deploy_id, k8s_namespace, k8s_pod, extra_fields (JSON)
- traces: id, timestamp, trace_id, span_id, parent_span_id, service, name, kind, duration_ns, status_code (ok/error), attributes (JSON), git_sha, deploy_id
- metrics: id, timestamp, service, metric_name, metric_type (gauge/sum/histogram), value, count, sum, min, max, attributes (Map)
- auth_events: id, timestamp, event_type, user_id, auth_method, error_code, risk_score
- change_events: id, timestamp, service, kind (deploy/flag/config/rollback), title, git_sha, deploy_id, author

Predicates support: =, !=, >, <, >=, <=, IN (...), LIKE, ILIKE, AND, OR, NOT
Time helpers: now()-3600 (seconds), parseDateTime64BestEffort('2024-01-01T00:00:00Z')
JSON access: JSONExtractString(extra_fields, 'key'), attr.<label> for metrics map
Aggregate: COUNT(*), SUM(col), AVG(col), MIN(col), MAX(col), GROUP BY

## Rules
- Never include tenant_id in the WHERE clause — it is always injected by the platform.
- Prefer timestamp > now()-N over absolute dates unless the user specifies them.
- Return only a JSON object with keys "loqlpp" and "explanation". No prose outside the JSON.

## Example
Input: "errors from payments in the last hour"
Output: {"loqlpp":"SELECT timestamp, level, message, service FROM logs WHERE service = 'payments' AND level = 'error' AND timestamp > now()-3600 ORDER BY timestamp DESC LIMIT 100","explanation":"Selects error-level log rows from the payments service in the last hour, newest first."}`

type nlQueryRequest struct {
	Query string `json:"query"`
}

type nlQueryResponse struct {
	LogQLPP     string `json:"loqlpp"`
	Explanation string `json:"explanation"`
}

// NLQuery handles POST /v1/query/nl.
// Translates a natural-language string to an inspectable, editable LogQL++ query.
// Requires ANTHROPIC_API_KEY env var; returns 501 if unset.
func NLQuery() http.HandlerFunc {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")

	return func(w http.ResponseWriter, r *http.Request) {
		if apiKey == "" {
			http.Error(w, `{"error":"NL query is not configured (ANTHROPIC_API_KEY not set)"}`, http.StatusNotImplemented)
			return
		}

		var body nlQueryRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Query) == "" {
			http.Error(w, `{"error":"request body must be JSON with a non-empty \"query\" field"}`, http.StatusBadRequest)
			return
		}

		result, err := callClaude(r.Context(), apiKey, body.Query)
		if err != nil {
			http.Error(w, fmt.Sprintf(`{"error":%q}`, err.Error()), http.StatusBadGateway)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

// callClaude sends the user query to the Anthropic Messages API and returns
// the parsed LogQL++ + explanation. Uses claude-sonnet-5 (latest capable model).
func callClaude(ctx context.Context, apiKey, userQuery string) (*nlQueryResponse, error) {
	payload := map[string]any{
		"model":      "claude-sonnet-5",
		"max_tokens": 512,
		"system":     schemaContext,
		"messages": []map[string]any{
			{"role": "user", "content": userQuery},
		},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	cl := &http.Client{Timeout: 30 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic api: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("anthropic api %d: %s", resp.StatusCode, string(raw))
	}

	// Decode the Anthropic response envelope.
	var envelope struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || len(envelope.Content) == 0 {
		return nil, fmt.Errorf("unexpected response: %s", string(raw))
	}

	// Extract the text block and parse the JSON the model returned.
	text := strings.TrimSpace(envelope.Content[0].Text)
	// Strip markdown code fences if the model wraps in ```json...```
	if strings.HasPrefix(text, "```") {
		text = strings.Trim(text, "`")
		if after, ok := strings.CutPrefix(text, "json\n"); ok {
			text = after
		}
		text = strings.TrimSpace(text)
	}

	var result nlQueryResponse
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return nil, fmt.Errorf("model returned non-JSON: %s", text)
	}
	if result.LogQLPP == "" {
		return nil, fmt.Errorf("model returned empty loqlpp field")
	}
	return &result, nil
}
