-- orders: координаты склада и ПВЗ
ALTER TABLE orders ADD COLUMN IF NOT EXISTS warehouse_id UUID;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS warehouse_lat DOUBLE PRECISION;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS warehouse_lng DOUBLE PRECISION;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS destination_pvp_id UUID;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS destination_lat DOUBLE PRECISION;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS destination_lng DOUBLE PRECISION;

-- Вебхуки магазинам (фоновая отправка)
CREATE TABLE IF NOT EXISTS failed_webhooks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id UUID NOT NULL REFERENCES orders(id),
    webhook_url TEXT NOT NULL,
    payload JSONB NOT NULL,
    attempt_count INTEGER NOT NULL DEFAULT 0,
    max_attempts INTEGER NOT NULL DEFAULT 5,
    next_attempt_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
