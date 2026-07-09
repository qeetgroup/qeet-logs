-- Correlation-first incident objects (PRD Module 13.2). Instead of paging on
-- each rule independently, symptom signals for a (tenant, service) collapse into
-- ONE incident at generation time; repeated firings dedup into the same open
-- incident. Every incident carries a calibrated confidence + severity (13.1);
-- below the page threshold it lives here as a low-severity feed rather than a page.
CREATE TABLE incidents (
    id               UUID             PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id        UUID             NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    fingerprint      TEXT             NOT NULL,                 -- correlation key (tenant|service)
    title            TEXT             NOT NULL,
    service          TEXT,
    severity         TEXT             NOT NULL DEFAULT 'low',   -- low|medium|high|critical
    confidence       DOUBLE PRECISION NOT NULL DEFAULT 0,
    status           TEXT             NOT NULL DEFAULT 'open',  -- open|resolved
    signal_count     INT              NOT NULL DEFAULT 1,
    deploy_id        TEXT,                                      -- nearby deploy (change proximity)
    correlated_rules JSONB            NOT NULL DEFAULT '[]',
    paged            BOOLEAN          NOT NULL DEFAULT false,
    first_seen       TIMESTAMPTZ      NOT NULL DEFAULT now(),
    last_seen        TIMESTAMPTZ      NOT NULL DEFAULT now(),
    resolved_at      TIMESTAMPTZ
);

-- At most one OPEN incident per fingerprint — the ON CONFLICT target that makes
-- repeated firings dedup into the same incident.
CREATE UNIQUE INDEX idx_incidents_open_fp ON incidents (fingerprint) WHERE status = 'open';
CREATE INDEX idx_incidents_tenant ON incidents (tenant_id, last_seen);
