-- Rollback for 001_update_schema.up.sql

DROP TABLE IF EXISTS published_events;
DROP TABLE IF EXISTS outbox_events;
