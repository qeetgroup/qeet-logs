-- Conversational multi-turn for the AI Copilot (PRD Module 12.2 / P2-G11). The
-- single-shot governed path (handler/copilot.go + domains/aigateway + migration
-- 0016) already exists; this adds durable conversation threading so a follow-up
-- question ("now scope that to prod", "only 5xx") carries the prior turns as
-- context. The LLM, opt-in gate, PII-masking, and audit trail are UNCHANGED —
-- every turn still flows through aigateway.Govern and lands in ai_decision_log.
--
-- Same convention as ai_features/incidents: queries scope by tenant_id
-- explicitly; NO RLS policy. Messages cascade with their conversation and tenant.
CREATE TABLE copilot_conversations (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    title      TEXT,                                        -- first question, truncated (UI label)
    created_at TIMESTAMPTZ DEFAULT now()
);

CREATE TABLE copilot_messages (
    id              UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id       UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    conversation_id UUID        NOT NULL REFERENCES copilot_conversations(id) ON DELETE CASCADE,
    role            TEXT        NOT NULL CHECK (role IN ('user', 'assistant')),
    content         TEXT,                                   -- user question, OR assistant explanation
    loqlpp          TEXT,                                   -- assistant turns only: the generated LogQL++
    created_at      TIMESTAMPTZ DEFAULT now()
);

-- Turn replay for a conversation is a tenant-scoped, chronological scan.
CREATE INDEX idx_copilot_messages_conversation
    ON copilot_messages (tenant_id, conversation_id, created_at);
