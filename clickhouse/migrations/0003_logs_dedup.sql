-- Enable insert deduplication for the non-replicated (local/dev) logs table so
-- the writer's insert_deduplication_token makes redelivered batches idempotent
-- (PRD: at-least-once bus + idempotent writes). ReplicatedMergeTree in
-- production has block deduplication on by default.
ALTER TABLE qeet_logs.logs MODIFY SETTING non_replicated_deduplication_window = 1000;
