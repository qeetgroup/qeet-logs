package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs/domains/aigateway"
	apimw "github.com/qeetgroup/qeet-logs/platform/api/middleware"
)

// AI Copilot GA — governed LLM gateway (PRD Module 12 / P2-G11).
//
// The LLM is the SAME Anthropic Messages API the NL-to-query path already uses;
// this handler adds no new model or runtime. What it adds is governance around
// that call: a per-tenant opt-in gate (ai_features), synchronous PII-masking of
// every prompt before it leaves the process, and an audit row per invocation
// (ai_decision_log). The masking + opt-in + audit pipeline itself lives in the
// pure domains/aigateway package and is unit-tested without a network.
//
// All package-level identifiers here are prefixed copilot*/aigw* so this file is
// self-contained and never collides with handler/nl_query.go's symbols.

// aigwModel is the Tier-1 model the gateway routes to, mirroring nl_query.go.
// Tier-2 (larger-model) routing is intentionally NOT wired here — still gated.
const aigwModel = "claude-sonnet-5"

// aigwSchemaContext is the system prompt: it pins the LogQL++ grammar and forces
// a strict JSON {loqlpp, explanation} answer. Mirrors nl_query.go's contract.
const aigwSchemaContext = `You are the LogQL++ copilot for the Qeet Logs platform.

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

type copilotRequest struct {
	Question string `json:"question"`
	Context  string `json:"context"`
}

type copilotResponse struct {
	LogQLPP     string `json:"loqlpp"`
	Explanation string `json:"explanation"`
}

// Copilot handles POST /v1/query/copilot.
//
//   - 403 if the caller lacks logs:query, OR the tenant has not opted in.
//   - 501 if ANTHROPIC_API_KEY is unset (same contract as NL query).
//   - else: mask the prompt, call Anthropic, audit the masked attempt, and
//     return the inspectable {loqlpp, explanation}.
func Copilot(pool *pgxpool.Pool) http.HandlerFunc {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")

	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:query") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "requires logs:query scope"})
			return
		}
		tenant := apimw.TenantID(ctx)

		// Opt-in gate — the copilot is off for every tenant until an admin enables it.
		if !copilotFeatureEnabled(ctx, pool, tenant) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "AI features are not enabled for this tenant (admin opt-in required)"})
			return
		}
		// Same 501-when-no-key discipline as the NL-query path.
		if apiKey == "" {
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "AI copilot is not configured (ANTHROPIC_API_KEY not set)"})
			return
		}

		var body copilotRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Question) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must be JSON with a non-empty \"question\" field"})
			return
		}

		res, entry, err := aigateway.Govern(ctx, aigateway.Request{
			TenantID: tenant,
			Enabled:  true, // gate already checked above
			Feature:  aigateway.FeatureCopilot,
			Question: body.Question,
			Context:  body.Context,
			Model:    aigwModel,
		}, aigwAnthropicLLM{apiKey: apiKey})

		// Best-effort governance audit: record the MASKED attempt whether or not
		// the model call succeeded. An audit-write failure must not fail the query
		// (same discipline as writeAudit in query.go).
		aigwLogDecision(ctx, pool, entry)

		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, copilotResponse{LogQLPP: res.LogQLPP, Explanation: res.Explanation})
	}
}

// copilotFeatureEnabled resolves the tenant's opt-in flag. Any error or missing
// row means OFF — governance defaults deny.
func copilotFeatureEnabled(ctx context.Context, pool *pgxpool.Pool, tenant string) bool {
	var enabled bool
	err := pool.QueryRow(ctx,
		`SELECT ai_features_enabled FROM ai_features WHERE tenant_id = $1::uuid`, tenant).Scan(&enabled)
	if err != nil {
		return false
	}
	return enabled
}

// aigwLogDecision writes one governance-trail row. Best-effort by design.
func aigwLogDecision(ctx context.Context, pool *pgxpool.Pool, e aigateway.AuditEntry) {
	if e.TenantID == "" {
		return
	}
	_, _ = pool.Exec(ctx,
		`INSERT INTO ai_decision_log (tenant_id, feature, prompt_masked, response_summary, model)
		 VALUES ($1::uuid, $2, $3, $4, $5)`,
		e.TenantID, e.Feature, e.PromptMasked, e.ResponseSummary, e.Model)
}

// aigwAnthropicLLM is the production aigateway.LLM: the existing Anthropic
// Messages call, mirrored from handler/nl_query.go (model, version, envelope
// parsing, code-fence stripping). Injectable so tests never hit the network.
type aigwAnthropicLLM struct {
	apiKey string
}

func (a aigwAnthropicLLM) Complete(ctx context.Context, prompt string) (aigateway.Result, error) {
	payload := map[string]any{
		"model":      aigwModel,
		"max_tokens": 512,
		"system":     aigwSchemaContext,
		"messages": []map[string]any{
			{"role": "user", "content": prompt},
		},
	}
	body, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return aigateway.Result{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	cl := &http.Client{Timeout: 30 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return aigateway.Result{}, fmt.Errorf("anthropic api: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return aigateway.Result{}, fmt.Errorf("anthropic api %d: %s", resp.StatusCode, string(raw))
	}

	var envelope struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || len(envelope.Content) == 0 {
		return aigateway.Result{}, fmt.Errorf("unexpected response: %s", string(raw))
	}

	text := strings.TrimSpace(envelope.Content[0].Text)
	if strings.HasPrefix(text, "```") {
		text = strings.Trim(text, "`")
		if after, ok := strings.CutPrefix(text, "json\n"); ok {
			text = after
		}
		text = strings.TrimSpace(text)
	}

	var parsed copilotResponse
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return aigateway.Result{}, fmt.Errorf("model returned non-JSON: %s", text)
	}
	if parsed.LogQLPP == "" {
		return aigateway.Result{}, fmt.Errorf("model returned empty loqlpp field")
	}
	return aigateway.Result{LogQLPP: parsed.LogQLPP, Explanation: parsed.Explanation}, nil
}

// --- Admin: per-tenant AI feature toggles (scope logs:admin) ----------------

type aigwFeaturesResponse struct {
	AIFeaturesEnabled          bool      `json:"ai_features_enabled"`
	CrossTenantTrainingConsent bool      `json:"cross_tenant_training_consent"`
	UpdatedAt                  time.Time `json:"updated_at"`
}

// GetAIFeatures handles GET /v1/admin/ai-features — returns the tenant's opt-in
// state (defaults, both false, when no row exists yet).
func GetAIFeatures(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := apimw.TenantID(r.Context())
		var resp aigwFeaturesResponse
		err := pool.QueryRow(r.Context(),
			`SELECT ai_features_enabled, cross_tenant_training_consent, updated_at
			 FROM ai_features WHERE tenant_id = $1::uuid`, tenant).
			Scan(&resp.AIFeaturesEnabled, &resp.CrossTenantTrainingConsent, &resp.UpdatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			writeJSON(w, http.StatusOK, aigwFeaturesResponse{UpdatedAt: time.Now().UTC()})
			return
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, resp)
	}
}

// UpdateAIFeatures handles PUT /v1/admin/ai-features. Fields are pointers so a
// partial PUT toggles just one flag; omitted flags keep their current value.
// Enabling the copilot never implies cross-tenant training consent.
func UpdateAIFeatures(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenant := apimw.TenantID(r.Context())
		var body struct {
			AIFeaturesEnabled          *bool `json:"ai_features_enabled"`
			CrossTenantTrainingConsent *bool `json:"cross_tenant_training_consent"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid JSON body"})
			return
		}

		// Merge onto current values (or defaults) so PUT is a partial update.
		enabled, consent := false, false
		_ = pool.QueryRow(r.Context(),
			`SELECT ai_features_enabled, cross_tenant_training_consent FROM ai_features WHERE tenant_id = $1::uuid`, tenant).
			Scan(&enabled, &consent)
		if body.AIFeaturesEnabled != nil {
			enabled = *body.AIFeaturesEnabled
		}
		if body.CrossTenantTrainingConsent != nil {
			consent = *body.CrossTenantTrainingConsent
		}

		_, err := pool.Exec(r.Context(),
			`INSERT INTO ai_features (tenant_id, ai_features_enabled, cross_tenant_training_consent, updated_at)
			 VALUES ($1::uuid, $2, $3, now())
			 ON CONFLICT (tenant_id) DO UPDATE SET
			     ai_features_enabled           = EXCLUDED.ai_features_enabled,
			     cross_tenant_training_consent = EXCLUDED.cross_tenant_training_consent,
			     updated_at                    = now()`,
			tenant, enabled, consent)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, aigwFeaturesResponse{
			AIFeaturesEnabled:          enabled,
			CrossTenantTrainingConsent: consent,
			UpdatedAt:                  time.Now().UTC(),
		})
	}
}
