-- Rollback for 001_update_schema.up.sql

ALTER TABLE carriers DROP COLUMN IF EXISTS is_active;
ALTER TABLE pickup_points DROP COLUMN IF EXISTS is_hub;
