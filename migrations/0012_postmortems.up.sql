-- Postmortem & Knowledge Graph (PRD Module 20). Once an incident is resolved,
-- teams author a structured postmortem — summary, timeline, root cause, impact —
-- and capture remediation commitments (action items), each optionally wired to
-- an alert rule so the fix is verifiable. These records also back the CERT-In
-- 6-hour incident export (PRD Module 27.2). Tenant isolation is enforced with an
-- explicit tenant_id predicate at the query layer (no RLS), matching the
-- incidents-table convention (migration 0008).
CREATE TABLE postmortems (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id    UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    incident_id  UUID,                                       -- optional link to incidents(id)
    title        TEXT        NOT NULL,
    summary      TEXT,
    timeline     TEXT,
    root_cause   TEXT,
    impact       TEXT,
    status       TEXT        NOT NULL DEFAULT 'draft',       -- draft | published
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at TIMESTAMPTZ
);
CREATE INDEX idx_postmortems_tenant ON postmortems (tenant_id, created_at);

-- Remediation commitments (action items) belonging to a postmortem. alert_rule_id
-- optionally binds a commitment to the alert rule that proves the fix landed.
CREATE TABLE remediation_commitments (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id     UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    postmortem_id UUID        NOT NULL REFERENCES postmortems(id) ON DELETE CASCADE,
    description   TEXT        NOT NULL,
    due_date      DATE,
    alert_rule_id UUID,                                      -- optional link to alert_rules(id)
    status        TEXT        NOT NULL DEFAULT 'open',       -- open | done
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_remediation_commitments_tenant ON remediation_commitments (tenant_id, postmortem_id);
