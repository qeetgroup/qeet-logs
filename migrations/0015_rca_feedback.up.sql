-- RCA root-cause labels — the training corpus for the deferred learned-to-rank
-- model (PRD Module 11.2 GA). The Phase-2 non-gated slice ships a transparent
-- feature-weighted LINEAR ranker (domains/rca/ranker.go); the trained model is
-- gated on having enough labelled outcomes to train on. This table collects those
-- labels: for a given incident an operator marks which retrieved RCA candidate
-- was (or was not) the actual root cause. Once the corpus is large enough, these
-- rows + the ranker's interpretable features train the GA model.
-- Explicit tenant-filter convention (matches the incidents / incident_feedback
-- tables; no RLS policy).
CREATE TABLE rca_feedback (
    id                UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    tenant_id         UUID        NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    incident_id       UUID,                                  -- optional link to the incident under investigation
    candidate_subject TEXT        NOT NULL,                  -- the RCA candidate being labelled (Candidate.Subject)
    candidate_type    TEXT,                                  -- deploy | dependency | ... (Candidate.Type)
    was_root_cause    BOOLEAN     NOT NULL,                  -- the label: true = confirmed root cause
    note              TEXT        DEFAULT '',
    created_at        TIMESTAMPTZ DEFAULT now()
);
CREATE INDEX idx_rca_feedback_incident ON rca_feedback (tenant_id, incident_id);
