CREATE TABLE IF NOT EXISTS trips_cache (
    id TEXT PRIMARY KEY,
    origin_warehouse_id TEXT,
    pickup_point_id TEXT,
    carrier_id TEXT,
    assigned_at TIMESTAMPTZ,
    status TEXT,
    updated_at TIMESTAMPTZ DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS batches_cache (
    id TEXT PRIMARY KEY,
    trip_id TEXT,
    updated_at TIMESTAMPTZ DEFAULT NOW()
);
