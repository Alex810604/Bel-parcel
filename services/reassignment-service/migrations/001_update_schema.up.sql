-- Таймер 2 часа для переназначения
CREATE TABLE IF NOT EXISTS pending_assignments (
    trip_id UUID PRIMARY KEY,
    batch_id UUID NOT NULL,
    carrier_id UUID NOT NULL,
    timeout_at TIMESTAMPTZ NOT NULL, -- created_at + 2 часа
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Индексы
CREATE INDEX IF NOT EXISTS idx_pending_assignments_timeout_at ON pending_assignments(timeout_at);
