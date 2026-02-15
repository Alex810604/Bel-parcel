-- Индексы для ПВЗ
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_pickup_points_name
  ON pickup_points USING gin (to_tsvector('russian', name));
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_pickup_points_address
  ON pickup_points USING gin (to_tsvector('russian', COALESCE(address, '')));
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_pickup_points_updated_at
  ON pickup_points (updated_at DESC);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_pickup_points_text
  ON pickup_points USING gin (to_tsvector('russian', name || ' ' || COALESCE(address, '')));

-- Индексы для перевозчиков
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_carriers_name
  ON carriers USING gin (to_tsvector('russian', name));
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_carriers_updated_at
  ON carriers (updated_at DESC);

-- Индексы для складов
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_warehouses_name
  ON warehouses USING gin (to_tsvector('russian', name));
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_warehouses_address
  ON warehouses USING gin (to_tsvector('russian', COALESCE(address, '')));
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_warehouses_updated_at
  ON warehouses (updated_at DESC);
CREATE INDEX CONCURRENTLY IF NOT EXISTS idx_warehouses_text
  ON warehouses USING gin (to_tsvector('russian', name || ' ' || COALESCE(address, '')));
