-- First-class deployment + Kubernetes enrichment fields (PRD Module 04.3 / Gap 7:
-- "treat git SHA, deploy ID, and PR number as first-class, automatically
-- propagated metadata on every log line and trace from ingestion time").
--
-- ALTER ... ADD COLUMN IF NOT EXISTS is a cheap metadata-only change on
-- MergeTree; existing rows read these as the column default. Idempotent.
ALTER TABLE qeet_logs.logs
    ADD COLUMN IF NOT EXISTS git_sha       String                 DEFAULT '',
    ADD COLUMN IF NOT EXISTS deploy_id     String                 DEFAULT '',
    ADD COLUMN IF NOT EXISTS pr_number     String                 DEFAULT '',
    ADD COLUMN IF NOT EXISTS k8s_namespace LowCardinality(String) DEFAULT '',
    ADD COLUMN IF NOT EXISTS k8s_pod       String                 DEFAULT '',
    ADD COLUMN IF NOT EXISTS k8s_node      LowCardinality(String) DEFAULT '';

ALTER TABLE qeet_logs.traces
    ADD COLUMN IF NOT EXISTS git_sha       String                 DEFAULT '',
    ADD COLUMN IF NOT EXISTS deploy_id     String                 DEFAULT '',
    ADD COLUMN IF NOT EXISTS pr_number     String                 DEFAULT '',
    ADD COLUMN IF NOT EXISTS k8s_namespace LowCardinality(String) DEFAULT '',
    ADD COLUMN IF NOT EXISTS k8s_pod       String                 DEFAULT '',
    ADD COLUMN IF NOT EXISTS k8s_node      LowCardinality(String) DEFAULT '';
