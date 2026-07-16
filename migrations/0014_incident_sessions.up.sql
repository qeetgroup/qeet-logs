-- Incident war-room core (PRD Module 18, Phase 2 — non-collaboration slice). An
-- incident can be "declared" into a war-room SESSION with a command structure
-- (roles), a live investigation timeline (entries), and a post-incident handoff.
-- The two-way Slack/Teams sync (18 collaboration) is out of scope (needs the
-- collaboration app infra); this is the Postgres substrate + API. Explicit
-- tenant-filter convention (matches the incidents table; no RLS policy).
CREATE TABLE incident_sessions (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    incident_id UUID        NOT NULL,
    status      TEXT        NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved')),
    commander   TEXT        NOT NULL DEFAULT '',
    summary     TEXT        NOT NULL DEFAULT '',
    opened_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at   TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_incident_sessions_tenant ON incident_sessions (tenant_id, incident_id);
-- At most one open war room per incident.
CREATE UNIQUE INDEX idx_incident_sessions_open ON incident_sessions (incident_id) WHERE status = 'open';

CREATE TABLE incident_session_entries (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    session_id UUID        NOT NULL REFERENCES incident_sessions(id) ON DELETE CASCADE,
    kind       TEXT        NOT NULL DEFAULT 'note'
        CHECK (kind IN ('note', 'action', 'status_change', 'role')),
    author     TEXT        NOT NULL DEFAULT '',
    body       TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_incident_session_entries ON incident_session_entries (tenant_id, session_id, created_at);

CREATE TABLE incident_session_roles (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id  UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    session_id UUID        NOT NULL REFERENCES incident_sessions(id) ON DELETE CASCADE,
    role       TEXT        NOT NULL
        CHECK (role IN ('commander', 'comms', 'ops', 'scribe', 'liaison')),
    assignee   TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, session_id, role)
);
