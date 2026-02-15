-- Rollback for 001_update_schema.up.sql

DROP INDEX IF EXISTS idx_ref_pickup_points_is_hub;
DROP TABLE IF EXISTS ref_pickup_points;
DROP TABLE IF EXISTS ref_warehouses;
DROP TABLE IF EXISTS batch_orders;

ALTER TABLE batches DROP COLUMN IF EXISTS destination_lng;
ALTER TABLE batches DROP COLUMN IF EXISTS destination_lat;
ALTER TABLE batches DROP COLUMN IF EXISTS origin_lng;
ALTER TABLE batches DROP COLUMN IF EXISTS origin_lat;
ALTER TABLE batches DROP COLUMN IF EXISTS is_hub_destination;
