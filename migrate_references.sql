-- Перенос ПВЗ из batching-service
INSERT INTO reference_db.pickup_points (
    id, name, address, location_lat, location_lng, is_hub, created_at, updated_at
) 
SELECT 
    pvp_id, 
    name, 
    address, 
    location_lat, 
    location_lng, 
    is_hub, 
    NOW(), 
    NOW() 
FROM batching_db.ref_pickup_points 
ON CONFLICT (id) DO NOTHING;

-- Перенос складов из batching-service
INSERT INTO reference_db.warehouses (
    id, name, address, location_lat, location_lng, created_at, updated_at
) 
SELECT 
    warehouse_id, 
    name, 
    address, 
    location_lat, 
    location_lng, 
    NOW(), 
    NOW() 
FROM batching_db.ref_warehouses 
ON CONFLICT (id) DO NOTHING;

-- Перенос перевозчиков из routing-service
INSERT INTO reference_db.carriers (
    id, name, is_active, last_seen, created_at, updated_at
) 
SELECT 
    carrier_id, 
    name, 
    is_active, 
    last_seen, 
    NOW(), 
    NOW() 
FROM routing_db.carriers 
ON CONFLICT (id) DO NOTHING;
