-- Rollback for 001_update_schema.up.sql

ALTER TABLE carrier_tracks DROP COLUMN IF EXISTS trip_id;
