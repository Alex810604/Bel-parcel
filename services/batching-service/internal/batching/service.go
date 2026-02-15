package batching

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"bel-parcel/services/batching-service/internal/infra/kafka"
	"bel-parcel/services/batching-service/internal/metrics"
	"bel-parcel/services/batching-service/internal/outbox"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	db            *pgxpool.Pool
	producer      *kafka.Producer
	outTopic      string
	dlqTopic      string
	maxSize       int
	flushInterval time.Duration
	mu            sync.Mutex
	groups        map[string]*group
}

type group struct {
	warehouseID string
	orderIDs    []string
	updatedAt   time.Time
	contacts    map[string]struct {
		Phone string
		Email string
	}
}

func EnsureSchema(db *pgxpool.Pool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS processed_events (
			event_id TEXT PRIMARY KEY,
			occurred_at TIMESTAMPTZ NOT NULL
		)
	`); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS batch_group_items (
			warehouse_id TEXT NOT NULL,
			pvp_id TEXT NOT NULL,
			order_id TEXT NOT NULL,
			customer_phone TEXT,
			customer_email TEXT,
			updated_at TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (warehouse_id, pvp_id, order_id)
		)
	`); err != nil {
		return err
	}
	// Ensure new columns exist in case table was created earlier
	if _, err := db.Exec(ctx, `
		ALTER TABLE IF EXISTS batch_group_items
		ADD COLUMN IF NOT EXISTS customer_phone TEXT,
		ADD COLUMN IF NOT EXISTS customer_email TEXT
	`); err != nil {
		return err
	}

	// Reference tables
	if _, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS ref_warehouses (
			warehouse_id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			latitude DOUBLE PRECISION NOT NULL,
			longitude DOUBLE PRECISION NOT NULL,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE TABLE IF NOT EXISTS ref_pickup_points (
			pvp_id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			latitude DOUBLE PRECISION NOT NULL,
			longitude DOUBLE PRECISION NOT NULL,
			is_hub BOOLEAN NOT NULL DEFAULT false,
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);
		CREATE TABLE IF NOT EXISTS batches (
			id TEXT PRIMARY KEY,
			origin_id TEXT,
			origin_type TEXT,
			seller_warehouse_id TEXT,
			pickup_point_id TEXT,
			origin_lat DOUBLE PRECISION,
			origin_lng DOUBLE PRECISION,
			destination_lat DOUBLE PRECISION,
			destination_lng DOUBLE PRECISION,
			is_hub_destination BOOLEAN,
			formed_at TIMESTAMPTZ
		);
		CREATE TABLE IF NOT EXISTS batch_orders (
			batch_id TEXT NOT NULL,
			order_id TEXT NOT NULL,
			destination_pvp_id TEXT,
			PRIMARY KEY (batch_id, order_id)
		);
	`); err != nil {
		return err
	}

	return nil
}

func NewService(db *pgxpool.Pool, producer *kafka.Producer, outTopic string, maxSize int, flushInterval time.Duration) *Service {
	return &Service{
		db:            db,
		producer:      producer,
		outTopic:      outTopic,
		maxSize:       maxSize,
		flushInterval: flushInterval,
		groups:        make(map[string]*group),
	}
}

func (s *Service) WithDLQ(topic string) *Service {
	s.dlqTopic = topic
	return s
}

func (s *Service) HandleEvent(ctx context.Context, topic string, key, value []byte) error {
	if topic == "events.reference_updated" {
		return s.handleReferenceUpdate(ctx, value)
	}
	if topic == "events.batch_delivered_to_pvp" {
		return s.handleBatchDelivered(ctx, value)
	}
	if topic != "orders.created" {
		return nil
	}
	var envelope struct {
		EventID       string          `json:"event_id"`
		EventType     string          `json:"event_type"`
		OccurredAt    time.Time       `json:"occurred_at"`
		CorrelationID string          `json:"correlation_id"`
		Data          json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(value, &envelope); err != nil {
		return err
	}
	added, err := s.addItemTx(ctx, envelope.EventID, envelope.Data)
	if err != nil {
		return err
	}
	if !added {
		return nil
	}
	var data struct {
		OrderID           string    `json:"order_id"`
		SellerWarehouseID string    `json:"seller_warehouse_id"`
		PickupPointID     string    `json:"pickup_point_id"`
		CreatedAt         time.Time `json:"created_at"`
		CustomerPhone     string    `json:"customer_phone"`
		CustomerEmail     string    `json:"customer_email"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return err
	}
	s.addToGroup(ctx, data.SellerWarehouseID, data.PickupPointID, data.OrderID, data.CustomerPhone, data.CustomerEmail)
	return nil
}

func (s *Service) handleReferenceUpdate(ctx context.Context, value []byte) error {
	var envelope struct {
		EventID string          `json:"event_id"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(value, &envelope); err != nil {
		return err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var id string
	if err := tx.QueryRow(ctx, `
		INSERT INTO processed_events(event_id, occurred_at)
		VALUES ($1, NOW())
		ON CONFLICT (event_id) DO NOTHING
		RETURNING event_id
	`, envelope.EventID).Scan(&id); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if id == "" {
		return nil
	}

	var data struct {
		UpdateType string `json:"update_type"`
		Warehouse  *struct {
			ID        string  `json:"warehouse_id"`
			Name      string  `json:"name"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
		} `json:"warehouse"`
		PickupPoint *struct {
			ID        string  `json:"pvp_id"`
			Name      string  `json:"name"`
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			IsHub     bool    `json:"is_hub"`
		} `json:"pickup_point"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return err
	}

	switch data.UpdateType {
	case "warehouse":
		if w := data.Warehouse; w != nil {
			if _, err := tx.Exec(ctx, `
				INSERT INTO ref_warehouses (warehouse_id, name, latitude, longitude, updated_at)
				VALUES ($1, $2, $3, $4, NOW())
				ON CONFLICT (warehouse_id) DO UPDATE
				SET name=EXCLUDED.name, latitude=EXCLUDED.latitude, longitude=EXCLUDED.longitude, updated_at=NOW()
			`, w.ID, w.Name, w.Latitude, w.Longitude); err != nil {
				return err
			}
		}
	case "pickup_point":
		if p := data.PickupPoint; p != nil {
			if _, err := tx.Exec(ctx, `
				INSERT INTO ref_pickup_points (pvp_id, name, latitude, longitude, is_hub, updated_at)
				VALUES ($1, $2, $3, $4, $5, NOW())
				ON CONFLICT (pvp_id) DO UPDATE
				SET name=EXCLUDED.name, latitude=EXCLUDED.latitude, longitude=EXCLUDED.longitude, is_hub=EXCLUDED.is_hub, updated_at=NOW()
			`, p.ID, p.Name, p.Latitude, p.Longitude, p.IsHub); err != nil {
				return err
			}
		}
	}

	return tx.Commit(ctx)
}

func (s *Service) handleBatchDelivered(ctx context.Context, value []byte) error {
	var envelope struct {
		EventID string          `json:"event_id"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(value, &envelope); err != nil {
		return err
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Idempotency
	var id string
	if err := tx.QueryRow(ctx, `
		INSERT INTO processed_events(event_id, occurred_at)
		VALUES ($1, NOW())
		ON CONFLICT (event_id) DO NOTHING
		RETURNING event_id
	`, envelope.EventID).Scan(&id); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if id == "" {
		return nil
	}

	var data struct {
		BatchID     string    `json:"batch_id"`
		IsHub       bool      `json:"is_hub"`
		PvpID       string    `json:"pvp_id"`
		DeliveredAt time.Time `json:"delivered_at"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return err
	}

	if !data.IsHub {
		return tx.Commit(ctx)
	}

	// Algorithm 7: Disbanding at Hub
	rows, err := tx.Query(ctx, `
		SELECT order_id, destination_pvp_id
		FROM batch_orders
		WHERE batch_id=$1
	`, data.BatchID)
	if err != nil {
		return err
	}
	defer rows.Close()

	ordersByDest := make(map[string][]string)
	for rows.Next() {
		var oid string
		var destID *string
		if err := rows.Scan(&oid, &destID); err != nil {
			return err
		}
		if destID != nil {
			ordersByDest[*destID] = append(ordersByDest[*destID], oid)
		}
	}
	rows.Close()

	fmt.Printf("Found %d destinations for batch %s\n", len(ordersByDest), data.BatchID)

	originID := data.PvpID
	var originLat, originLng float64
	if err := tx.QueryRow(ctx, "SELECT latitude, longitude FROM ref_pickup_points WHERE pvp_id=$1", originID).Scan(&originLat, &originLng); err != nil {
		fmt.Printf("Hub %s not found in ref_pickup_points: %v\n", originID, err)
		return fmt.Errorf("hub %s not found in ref_pickup_points", originID)
	}

	for destID, orderIDs := range ordersByDest {
		if len(orderIDs) == 0 {
			continue
		}
		fmt.Printf("Processing destination %s with %d orders\n", destID, len(orderIDs))
		var destLat, destLng float64
		var isHubDest bool
		if err := tx.QueryRow(ctx, "SELECT latitude, longitude, is_hub FROM ref_pickup_points WHERE pvp_id=$1", destID).Scan(&destLat, &destLng, &isHubDest); err != nil {
			// Skip unknown destination
			fmt.Printf("Destination %s not found in ref_pickup_points: %v\n", destID, err)
			continue
		}

		newBatchID := uuid.NewString()
		now := time.Now().UTC()

		// Create new batch
		if _, err := tx.Exec(ctx, `
			INSERT INTO batches (id, origin_id, origin_type, seller_warehouse_id, pickup_point_id, origin_lat, origin_lng, destination_lat, destination_lng, is_hub_destination, formed_at)
			VALUES ($1, $2, 'pvp', NULL, $3, $4, $5, $6, $7, $8, $9)
		`, newBatchID, originID, destID, originLat, originLng, destLat, destLng, false, now); err != nil {
			return err
		}

		for _, oid := range orderIDs {
			if _, err := tx.Exec(ctx, `INSERT INTO batch_orders (batch_id, order_id, destination_pvp_id) VALUES ($1, $2, $3)`, newBatchID, oid, destID); err != nil {
				return err
			}
		}

		// Publish event
		envelope := map[string]interface{}{
			"event_id":       uuid.NewString(),
			"event_type":     "batches.formed",
			"occurred_at":    now,
			"correlation_id": originID + "/" + destID,
			"data": map[string]interface{}{
				"batch_id":           newBatchID,
				"origin_type":        "pvp",
				"origin_id":          originID,
				"origin_lat":         originLat,
				"origin_lng":         originLng,
				"destination_type":   "pvp",
				"destination_id":     destID,
				"destination_lat":    destLat,
				"destination_lng":    destLng,
				"is_hub_destination": false,
				"order_ids":          orderIDs,
				"formed_at":          now,
			},
		}
		payload, err := json.Marshal(envelope)
		if err != nil {
			return err
		}
		evt := outbox.Event{
			ID:            uuid.NewString(),
			EventType:     "batches.formed",
			CorrelationID: newBatchID,
			Topic:         s.outTopic,
			PartitionKey:  newBatchID,
			Payload:       payload,
			OccurredAt:    now,
		}
		if err := outbox.EnqueueTx(ctx, tx, evt); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

func (s *Service) PublishDLQ(ctx context.Context, originalTopic string, key []byte, value []byte, err error) {
	if s.dlqTopic == "" {
		return
	}
	envelope := map[string]interface{}{
		"event_id":       uuid.NewString(),
		"event_type":     "dlq",
		"occurred_at":    time.Now().UTC(),
		"correlation_id": string(key),
		"data": map[string]interface{}{
			"original_topic": originalTopic,
			"error":          err.Error(),
			"payload":        string(value),
		},
	}
	_ = s.producer.Publish(ctx, s.dlqTopic, string(key), envelope)
}

func (s *Service) addToGroup(ctx context.Context, warehouseID, pvpID, orderID, phone, email string) {
	k := warehouseID + ":" + pvpID
	now := time.Now().UTC()
	s.mu.Lock()
	g, ok := s.groups[k]
	if !ok {
		g = &group{warehouseID: warehouseID, orderIDs: make([]string, 0, s.maxSize), updatedAt: now, contacts: make(map[string]struct {
			Phone string
			Email string
		})}
		s.groups[k] = g
		metrics.BatchActiveGroups.Inc()
	}
	g.orderIDs = append(g.orderIDs, orderID)
	metrics.BatchOrdersAdded.WithLabelValues(warehouseID).Inc()
	g.contacts[orderID] = struct {
		Phone string
		Email string
	}{Phone: phone, Email: email}
	g.updatedAt = now
	s.mu.Unlock()
	_ = s.tryFlushBySizeDB(ctx, warehouseID, pvpID)
}

func (s *Service) FlushExpired(ctx context.Context) {
	_ = s.flushExpiredDB(ctx)
}

func (s *Service) flushGroup(ctx context.Context, key string) error {
	// Legacy method, not used
	return nil
}

func (s *Service) DisbandBatch(ctx context.Context, batchID string) error {
	// Legacy disband method (if needed for rollback or cleanup)
	// Currently Algorithm 7 handles "Disbanding at Hub" by creating new batches.
	// We do NOT delete the old batch immediately? Spec doesn't say "delete old batch".
	// But usually "disband" implies the old batch is done.
	// Algorithm 7 doesn't explicitly say "delete old batch".
	// It says "Save event in processed".
	// The orders are now in NEW batches.
	// We can leave the old batch as is (history).
	return nil
}

func (s *Service) addItemTx(ctx context.Context, eventID string, raw json.RawMessage) (bool, error) {
	var data struct {
		OrderID           string    `json:"order_id"`
		SellerWarehouseID string    `json:"seller_warehouse_id"`
		PickupPointID     string    `json:"pickup_point_id"`
		CreatedAt         time.Time `json:"created_at"`
		CustomerPhone     string    `json:"customer_phone"`
		CustomerEmail     string    `json:"customer_email"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		return false, err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()
	var id string
	if err := tx.QueryRow(ctx, `
		INSERT INTO processed_events(event_id, occurred_at)
		VALUES ($1, NOW())
		ON CONFLICT (event_id) DO NOTHING
		RETURNING event_id
	`, eventID).Scan(&id); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, err
	}
	if id == "" {
		return false, nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO batch_group_items(warehouse_id, pvp_id, order_id, customer_phone, customer_email, updated_at)
		VALUES ($1, $2, $3, $4, $5, NOW())
		ON CONFLICT (warehouse_id, pvp_id, order_id)
		DO UPDATE SET customer_phone=EXCLUDED.customer_phone, customer_email=EXCLUDED.customer_email, updated_at=EXCLUDED.updated_at
	`, data.SellerWarehouseID, data.PickupPointID, data.OrderID, data.CustomerPhone, data.CustomerEmail); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Service) tryFlushBySizeDB(ctx context.Context, warehouseID, pvpID string) error {
	var cnt int
	if err := s.db.QueryRow(ctx, `
		SELECT COUNT(*) FROM batch_group_items
		WHERE warehouse_id=$1 AND pvp_id=$2
	`, warehouseID, pvpID).Scan(&cnt); err != nil {
		return err
	}
	if cnt >= s.maxSize {
		return s.flushGroupDB(ctx, warehouseID, pvpID)
	}
	return nil
}

func (s *Service) flushExpiredDB(ctx context.Context) error {
	interval := fmt.Sprintf("%d seconds", int64(s.flushInterval.Seconds()))
	rows, err := s.db.Query(ctx, `
		SELECT warehouse_id, pvp_id
		FROM (
			SELECT warehouse_id, pvp_id, COUNT(*) AS cnt, MAX(updated_at) AS last_upd
			FROM batch_group_items
			GROUP BY warehouse_id, pvp_id
		) t
		WHERE cnt >= $1 OR last_upd <= NOW() - $2::interval
	`, s.maxSize, interval)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var wid, pid string
		if err := rows.Scan(&wid, &pid); err != nil {
			return err
		}
		if err := s.flushGroupDB(ctx, wid, pid); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Service) flushGroupDB(ctx context.Context, warehouseID, pvpID string) error {
	now := time.Now().UTC()
	start := time.Now()
	var orderIDs []string
	var orderContacts []map[string]string
	pvpSet := make(map[string]struct{})
	orderDestMap := make(map[string]string)

	rows, err := s.db.Query(ctx, `
		SELECT order_id, pvp_id, COALESCE(customer_phone,''), COALESCE(customer_email,'')
		FROM batch_group_items
		WHERE warehouse_id=$1 AND pvp_id=$2
		ORDER BY updated_at
	`, warehouseID, pvpID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var oid, pid, phone, email string
		if err := rows.Scan(&oid, &pid, &phone, &email); err != nil {
			return err
		}
		orderIDs = append(orderIDs, oid)
		pvpSet[pid] = struct{}{}
		orderDestMap[oid] = pid
		orderContacts = append(orderContacts, map[string]string{
			"order_id":       oid,
			"customer_phone": phone,
			"customer_email": email,
		})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(orderIDs) == 0 {
		return nil
	}

	var destType, destID string
	var destLat, destLng float64
	var isHubDest bool

	var originLat, originLng float64
	if err := s.db.QueryRow(ctx, "SELECT latitude, longitude FROM ref_warehouses WHERE warehouse_id=$1", warehouseID).Scan(&originLat, &originLng); err != nil {
		return fmt.Errorf("warehouse %s not found in ref_warehouses: %w", warehouseID, err)
	}

	useSinglePVP := false
	if len(pvpSet) == 1 {
		var singlePVPID string
		for pid := range pvpSet {
			singlePVPID = pid
		}

		var pvpLat, pvpLng float64
		var pvpIsHub bool
		if err := s.db.QueryRow(ctx, "SELECT latitude, longitude, is_hub FROM ref_pickup_points WHERE pvp_id=$1", singlePVPID).Scan(&pvpLat, &pvpLng, &pvpIsHub); err != nil {
			return fmt.Errorf("pvp %s not found: %w", singlePVPID, err)
		}

		dist := haversine(originLat, originLng, pvpLat, pvpLng)
		if dist <= 200000 {
			useSinglePVP = true
			destType = "pvp"
			destID = singlePVPID
			destLat = pvpLat
			destLng = pvpLng
			isHubDest = pvpIsHub
		}
	}

	if !useSinglePVP {
		rows, err := s.db.Query(ctx, "SELECT pvp_id, latitude, longitude FROM ref_pickup_points WHERE is_hub=true")
		if err != nil {
			return err
		}
		defer rows.Close()

		minDist := math.MaxFloat64
		foundHub := false

		for rows.Next() {
			var hid string
			var hlat, hlng float64
			if err := rows.Scan(&hid, &hlat, &hlng); err != nil {
				continue
			}
			dist := haversine(originLat, originLng, hlat, hlng)
			if dist < minDist {
				minDist = dist
				destType = "pvp"
				destID = hid
				destLat = hlat
				destLng = hlng
				isHubDest = true
				foundHub = true
			}
		}

		if !foundHub {
			return fmt.Errorf("no hubs found in ref_pickup_points")
		}
	}

	batchID := uuid.NewString()
	envelope := map[string]interface{}{
		"event_id":       uuid.NewString(),
		"event_type":     "batches.formed",
		"occurred_at":    now,
		"correlation_id": warehouseID + "/" + destID,
		"data": map[string]interface{}{
			"batch_id":           batchID,
			"origin_type":        "warehouse",
			"origin_id":          warehouseID,
			"origin_lat":         originLat,
			"origin_lng":         originLng,
			"destination_type":   destType,
			"destination_id":     destID,
			"destination_lat":    destLat,
			"destination_lng":    destLng,
			"is_hub_destination": isHubDest,
			"order_ids":          orderIDs,
			"order_contacts":     orderContacts,
			"formed_at":          now,
		},
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(ctx, `
		DELETE FROM batch_group_items
		WHERE warehouse_id=$1 AND pvp_id=$2
	`, warehouseID, pvpID); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO batches (id, origin_id, origin_type, seller_warehouse_id, pickup_point_id, origin_lat, origin_lng, destination_lat, destination_lng, is_hub_destination, formed_at)
		VALUES ($1, $2, 'warehouse', $2, $3, $4, $5, $6, $7, $8, $9)
	`, batchID, warehouseID, destID, originLat, originLng, destLat, destLng, isHubDest, now); err != nil {
		return err
	}

	for _, oid := range orderIDs {
		// Save destination_pvp_id for potential disbanding at hub
		destPVP := orderDestMap[oid]
		if _, err := tx.Exec(ctx, `INSERT INTO batch_orders (batch_id, order_id, destination_pvp_id) VALUES ($1, $2, $3)`, batchID, oid, destPVP); err != nil {
			return err
		}
	}

	payload, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	evt := outbox.Event{
		ID:            uuid.NewString(),
		EventType:     "batches.formed",
		CorrelationID: warehouseID + "/" + destID + "/" + batchID,
		Topic:         s.outTopic,
		PartitionKey:  batchID,
		Payload:       payload,
		OccurredAt:    now,
	}
	if err := outbox.EnqueueTx(ctx, tx, evt); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	metrics.BatchFlushDuration.Observe(time.Since(start).Seconds())
	s.mu.Lock()
	if g, ok := s.groups[warehouseID+":"+pvpID]; ok {
		g.orderIDs = g.orderIDs[:0]
	}
	s.mu.Unlock()
	metrics.BatchActiveGroups.Dec()
	return nil
}

func haversine(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371000 // Earth radius in meters
	phi1 := lat1 * math.Pi / 180
	phi2 := lat2 * math.Pi / 180
	dphi := (lat2 - lat1) * math.Pi / 180
	dlmn := (lng2 - lng1) * math.Pi / 180

	a := math.Sin(dphi/2)*math.Sin(dphi/2) +
		math.Cos(phi1)*math.Cos(phi2)*
			math.Sin(dlmn/2)*math.Sin(dlmn/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))

	return R * c
}
