-- Rollback for 001_update_schema.up.sql

DROP TABLE IF EXISTS failed_webhooks;

ALTER TABLE orders DROP COLUMN IF EXISTS destination_lng;
ALTER TABLE orders DROP COLUMN IF EXISTS destination_lat;
ALTER TABLE orders DROP COLUMN IF EXISTS destination_pvp_id;
ALTER TABLE orders DROP COLUMN IF EXISTS warehouse_lng;
ALTER TABLE orders DROP COLUMN IF EXISTS warehouse_lat;
ALTER TABLE orders DROP COLUMN IF EXISTS warehouse_id;
