-- carrier_tracks: trip_id
ALTER TABLE carrier_tracks ADD COLUMN IF NOT EXISTS trip_id UUID;
