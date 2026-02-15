-- Универсальный источник партии (склад или хаб)
ALTER TABLE batches ADD COLUMN IF NOT EXISTS origin_id TEXT;
ALTER TABLE batches ADD COLUMN IF NOT EXISTS origin_type TEXT DEFAULT 'warehouse';

-- Миграция существующих данных
UPDATE batches SET origin_id = seller_warehouse_id, origin_type = 'warehouse' WHERE origin_id IS NULL;

-- Разрешаем NULL для старого поля
ALTER TABLE batches ALTER COLUMN seller_warehouse_id DROP NOT NULL;
