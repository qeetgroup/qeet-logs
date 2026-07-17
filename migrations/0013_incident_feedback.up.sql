-- Continuous calibration against resolution outcomes (PRD Module 13.3, Phase 2).
-- Operators mark resolved incidents as actionable (true positive) or noise (false
-- positive); the alerter derives a per-(tenant, service) confidence multiplier from
-- that history, so services that page mostly noise get their confidence damped
-- (fewer pages) while genuinely-actionable services keep/raise theirs. Explicit
-- tenant-filter convention (matches the incidents table; no RLS policy).
CREATE TABLE incident_feedback (
    id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id   UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    incident_id UUID        NOT NULL,
    fingerprint TEXT        NOT NULL DEFAULT '',
    service     TEXT        NOT NULL DEFAULT '',
    verdict     TEXT        NOT NULL CHECK (verdict IN ('actionable', 'noise')),
    note        TEXT        NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_incident_feedback_cal ON incident_feedback (tenant_id, service, created_at);
