-- trips: расстояние и координаты
ALTER TABLE trips ADD COLUMN IF NOT EXISTS assigned_distance_meters INTEGER;
ALTER TABLE trips ADD COLUMN IF NOT EXISTS origin_lat DOUBLE PRECISION;
ALTER TABLE trips ADD COLUMN IF NOT EXISTS origin_lng DOUBLE PRECISION;
ALTER TABLE trips ADD COLUMN IF NOT EXISTS destination_lat DOUBLE PRECISION;
ALTER TABLE trips ADD COLUMN IF NOT EXISTS destination_lng DOUBLE PRECISION;

-- Кэш локаций перевозчиков
CREATE TABLE IF NOT EXISTS carrier_positions (
    carrier_id UUID PRIMARY KEY,
    latitude DOUBLE PRECISION NOT NULL,
    longitude DOUBLE PRECISION NOT NULL,
    last_seen TIMESTAMPTZ NOT NULL
);

-- Кэш активности перевозчиков
CREATE TABLE IF NOT EXISTS carrier_activity_cache (
    carrier_id UUID PRIMARY KEY,
    is_active BOOLEAN NOT NULL DEFAULT true,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Идемпотентность обработки событий
CREATE TABLE IF NOT EXISTS processed_events (
    event_id UUID PRIMARY KEY,
    event_type TEXT NOT NULL,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Индексы
CREATE INDEX IF NOT EXISTS idx_carrier_positions_last_seen ON carrier_positions(last_seen);
