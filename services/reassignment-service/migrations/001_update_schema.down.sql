-- Rollback for 001_update_schema.up.sql

DROP INDEX IF EXISTS idx_pending_assignments_timeout_at;
DROP TABLE IF EXISTS pending_assignments;
