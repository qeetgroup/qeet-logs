-- Governed LLM gateway (PRD Module 12 / P2-G11, AI Copilot GA). The LLM itself
-- is the EXISTING Anthropic Messages call already used by the NL-to-query path
-- (handler/nl_query.go) — no new model, no vendored runtime. What lands here is
-- the GOVERNANCE substrate around that call: per-tenant opt-in, a masked-prompt
-- audit trail, and an explicit cross-tenant-training consent flag (default OFF).
--
-- ai_features is the opt-in gate: the AI copilot is DISABLED for every tenant
-- until an admin (logs:admin) flips ai_features_enabled. cross_tenant_training_
-- consent is a separate, independently-off toggle — enabling the copilot never
-- implies consenting to cross-tenant training. Mirrors the incidents table's
-- convention: queries scope by tenant_id explicitly; NO RLS policy on this table.
CREATE TABLE ai_features (
    tenant_id                     UUID        PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    ai_features_enabled           BOOLEAN     NOT NULL DEFAULT false,  -- opt-in gate for the copilot
    cross_tenant_training_consent BOOLEAN     NOT NULL DEFAULT false,  -- independent; never implied by the above
    updated_at                    TIMESTAMPTZ DEFAULT now()
);

-- ai_decision_log is the governance trail: one row per governed LLM invocation.
-- prompt_masked is the PII-masked prompt actually sent to the model (raw prompts
-- never land here); response_summary is a short digest of the model's decision.
-- Written best-effort by the copilot handler — an audit-write failure must not
-- fail the request, same discipline as audit_log in handler/query.go.
CREATE TABLE ai_decision_log (
    id               UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    feature          TEXT        NOT NULL,              -- e.g. 'copilot'
    prompt_masked    TEXT,                              -- PII-masked prompt sent to the model
    response_summary TEXT,                              -- short digest of the model decision
    model            TEXT,                              -- model id the gateway routed to
    created_at       TIMESTAMPTZ DEFAULT now()
);

-- Audit lookups fan out per tenant, newest first.
CREATE INDEX idx_ai_decision_log_tenant ON ai_decision_log (tenant_id, created_at);
