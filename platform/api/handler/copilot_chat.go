package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/qeetgroup/qeet-logs/domains/aigateway"
	apimw "github.com/qeetgroup/qeet-logs/platform/api/middleware"
)

// Conversational multi-turn AI Copilot (PRD Module 12.2 / P2-G11). Builds on the
// single-shot handler/copilot.go: it reuses that file's opt-in gate
// (copilotFeatureEnabled), Anthropic LLM (aigwAnthropicLLM + aigwModel), and
// governance audit (aigwLogDecision) unchanged. What it adds is durable turn
// history (copilot_conversations / copilot_messages, migration 0019): a follow-up
// question is answered with the prior turns replayed as context via
// aigateway.BuildThreadPrompt, and every turn still flows through the SAME
// masked, audited aigateway.Govern pipeline.
//
// Identifiers are prefixed copilotChat* so this file never collides with
// handler/copilot.go.

type copilotChatStartRequest struct {
	Title string `json:"title"`
}

type copilotChatMessageRequest struct {
	Question string `json:"question"`
	Context  string `json:"context"`
}

type copilotChatMessageResponse struct {
	ConversationID string `json:"conversation_id"`
	LogQLPP        string `json:"loqlpp"`
	Explanation    string `json:"explanation"`
}

// StartCopilotConversation handles POST /v1/query/copilot/conversations. Creates
// an empty conversation for the tenant and returns its id. Same gates as the
// single-shot copilot: logs:query scope + tenant opt-in.
func StartCopilotConversation(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:query") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "requires logs:query scope"})
			return
		}
		tenant := apimw.TenantID(ctx)
		if !copilotFeatureEnabled(ctx, pool, tenant) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "AI features are not enabled for this tenant (admin opt-in required)"})
			return
		}
		var body copilotChatStartRequest
		_ = json.NewDecoder(r.Body).Decode(&body)

		var id string
		var createdAt time.Time
		err := pool.QueryRow(ctx,
			`INSERT INTO copilot_conversations (tenant_id, title) VALUES ($1::uuid, $2)
			 RETURNING id::text, created_at`,
			tenant, copilotChatTruncate(body.Title, 120)).Scan(&id, &createdAt)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{"id": id, "created_at": createdAt})
	}
}

// CopilotConversationMessage handles POST /v1/query/copilot/conversations/{id}/messages.
// It replays the conversation history into a governed LLM call and appends both
// the user turn and the assistant turn.
//
//   - 403 without logs:query or if the tenant has not opted in.
//   - 404 if the conversation is not the tenant's.
//   - 501 if ANTHROPIC_API_KEY is unset (same contract as the single-shot path).
func CopilotConversationMessage(pool *pgxpool.Pool) http.HandlerFunc {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:query") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "requires logs:query scope"})
			return
		}
		tenant := apimw.TenantID(ctx)
		if !copilotFeatureEnabled(ctx, pool, tenant) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "AI features are not enabled for this tenant (admin opt-in required)"})
			return
		}
		if apiKey == "" {
			writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "AI copilot is not configured (ANTHROPIC_API_KEY not set)"})
			return
		}
		convID := chi.URLParam(r, "id")
		if !copilotChatOwns(ctx, pool, tenant, convID) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "conversation not found"})
			return
		}

		var body copilotChatMessageRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Question) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request body must be JSON with a non-empty \"question\" field"})
			return
		}

		history := copilotChatHistory(ctx, pool, tenant, convID)
		prompt := aigateway.BuildThreadPrompt(history, body.Question, aigateway.DefaultHistoryTurns)

		// Persist the user turn (verbatim question, not the assembled prompt).
		_, _ = pool.Exec(ctx,
			`INSERT INTO copilot_messages (tenant_id, conversation_id, role, content)
			 VALUES ($1::uuid, $2::uuid, 'user', $3)`, tenant, convID, body.Question)

		res, entry, err := aigateway.Govern(ctx, aigateway.Request{
			TenantID: tenant,
			Enabled:  true,
			Feature:  aigateway.FeatureCopilot,
			Question: prompt,
			Context:  body.Context,
			Model:    aigwModel,
		}, aigwAnthropicLLM{apiKey: apiKey})

		aigwLogDecision(ctx, pool, entry) // best-effort governance audit

		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}

		// Persist the assistant turn.
		_, _ = pool.Exec(ctx,
			`INSERT INTO copilot_messages (tenant_id, conversation_id, role, content, loqlpp)
			 VALUES ($1::uuid, $2::uuid, 'assistant', $3, $4)`,
			tenant, convID, res.Explanation, res.LogQLPP)

		writeJSON(w, http.StatusOK, copilotChatMessageResponse{
			ConversationID: convID, LogQLPP: res.LogQLPP, Explanation: res.Explanation,
		})
	}
}

// GetCopilotConversation handles GET /v1/query/copilot/conversations/{id} —
// returns the conversation's turn history (tenant-scoped).
func GetCopilotConversation(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if !apimw.HasScope(ctx, "logs:query") {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "requires logs:query scope"})
			return
		}
		tenant := apimw.TenantID(ctx)
		convID := chi.URLParam(r, "id")
		if !copilotChatOwns(ctx, pool, tenant, convID) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "conversation not found"})
			return
		}
		history := copilotChatHistory(ctx, pool, tenant, convID)
		turns := make([]map[string]string, 0, len(history))
		for _, t := range history {
			turns = append(turns, map[string]string{"role": t.Role, "content": t.Content, "loqlpp": t.LogQLPP})
		}
		writeJSON(w, http.StatusOK, map[string]any{"conversation_id": convID, "turns": turns})
	}
}

// copilotChatOwns reports whether the conversation exists and belongs to tenant.
func copilotChatOwns(ctx context.Context, pool *pgxpool.Pool, tenant, convID string) bool {
	var ok bool
	err := pool.QueryRow(ctx,
		`SELECT true FROM copilot_conversations WHERE id = $1::uuid AND tenant_id = $2::uuid`,
		convID, tenant).Scan(&ok)
	return err == nil && ok
}

// copilotChatHistory loads the conversation's turns oldest-first for the tenant.
func copilotChatHistory(ctx context.Context, pool *pgxpool.Pool, tenant, convID string) []aigateway.Turn {
	rows, err := pool.Query(ctx,
		`SELECT role, COALESCE(content, ''), COALESCE(loqlpp, '') FROM copilot_messages
		 WHERE tenant_id = $1::uuid AND conversation_id = $2::uuid ORDER BY created_at`, tenant, convID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []aigateway.Turn
	for rows.Next() {
		var t aigateway.Turn
		if err := rows.Scan(&t.Role, &t.Content, &t.LogQLPP); err == nil {
			out = append(out, t)
		}
	}
	return out
}

func copilotChatTruncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n]
}
