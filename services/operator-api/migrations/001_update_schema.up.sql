-- pickup_points: is_hub
ALTER TABLE pickup_points ADD COLUMN IF NOT EXISTS is_hub BOOLEAN NOT NULL DEFAULT false;

-- carriers: is_active
ALTER TABLE carriers ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT true;
