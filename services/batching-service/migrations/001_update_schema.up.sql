-- batches: флаг хаба и координаты
ALTER TABLE batches ADD COLUMN IF NOT EXISTS is_hub_destination BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE batches ADD COLUMN IF NOT EXISTS origin_lat DOUBLE PRECISION;
ALTER TABLE batches ADD COLUMN IF NOT EXISTS origin_lng DOUBLE PRECISION;
ALTER TABLE batches ADD COLUMN IF NOT EXISTS destination_lat DOUBLE PRECISION;
ALTER TABLE batches ADD COLUMN IF NOT EXISTS destination_lng DOUBLE PRECISION;

-- Связь партия ↔ заказы (для расформирования)
CREATE TABLE IF NOT EXISTS batch_orders (
    batch_id UUID NOT NULL REFERENCES batches(id) ON DELETE CASCADE,
    order_id UUID NOT NULL,
    destination_pvp_id UUID,
    PRIMARY KEY (batch_id, order_id)
);

-- Локальная проекция справочников
CREATE TABLE IF NOT EXISTS ref_warehouses (
    warehouse_id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    latitude DOUBLE PRECISION NOT NULL,
    longitude DOUBLE PRECISION NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ref_pickup_points (
    pvp_id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    latitude DOUBLE PRECISION NOT NULL,
    longitude DOUBLE PRECISION NOT NULL,
    is_hub BOOLEAN NOT NULL DEFAULT false,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Индексы
CREATE INDEX IF NOT EXISTS idx_ref_pickup_points_is_hub ON ref_pickup_points(is_hub);
