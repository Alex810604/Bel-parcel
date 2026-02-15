-- Rollback for 001_update_schema.up.sql

DROP INDEX IF EXISTS idx_carrier_positions_last_seen;
DROP TABLE IF EXISTS processed_events;
DROP TABLE IF EXISTS carrier_activity_cache;
DROP TABLE IF EXISTS carrier_positions;

ALTER TABLE trips DROP COLUMN IF EXISTS destination_lng;
ALTER TABLE trips DROP COLUMN IF EXISTS destination_lat;
ALTER TABLE trips DROP COLUMN IF EXISTS origin_lng;
ALTER TABLE trips DROP COLUMN IF EXISTS origin_lat;
ALTER TABLE trips DROP COLUMN IF EXISTS assigned_distance_meters;
