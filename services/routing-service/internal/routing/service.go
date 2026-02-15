package routing

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"sync"
	"time"

	"bel-parcel/services/routing-service/internal/infra/kafka"
	"bel-parcel/services/routing-service/internal/outbox"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	tripDB        *pgxpool.Pool
	producer      *kafka.Producer
	outTopic      string
	carriersCache sync.Map
}

func NewService(tripDB *pgxpool.Pool, producer *kafka.Producer, outTopic string) *Service {
	return &Service{tripDB: tripDB, producer: producer, outTopic: outTopic}
}

func (s *Service) Start(ctx context.Context) {
	go s.StartPendingReassignmentLoop(ctx)
}

func (s *Service) StartPendingReassignmentLoop(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := s.processPendingAssignments(ctx); err != nil {
				slog.Error("failed to process pending assignments", "error", err)
			}
		}
	}
}

func (s *Service) processPendingAssignments(ctx context.Context) error {
	tx, err := s.tripDB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	rows, err := tx.Query(ctx, `
		SELECT trip_id, attempt_count 
		FROM pending_assignments 
		WHERE timeout_at <= NOW() 
		ORDER BY timeout_at ASC 
		LIMIT 50 
		FOR UPDATE SKIP LOCKED
	`)
	if err != nil {
		return err
	}

	type item struct {
		tripID       string
		attemptCount int
	}
	var items []item
	for rows.Next() {
		var i item
		if err := rows.Scan(&i.tripID, &i.attemptCount); err != nil {
			rows.Close()
			return err
		}
		items = append(items, i)
	}
	rows.Close()

	for _, it := range items {
		slog.Info("Pending reassignment attempt", "attempt", it.attemptCount, "trip_id", it.tripID)

		var originLat, originLng, destLat, destLng float64
		var batchID string
		if err := tx.QueryRow(ctx, `
			SELECT t.origin_lat, t.origin_lng, t.dest_lat, t.dest_lng, tb.batch_id
			FROM trips t
			JOIN trip_batches tb ON t.id = tb.trip_id
			WHERE t.id = $1
		`, it.tripID).Scan(&originLat, &originLng, &destLat, &destLng, &batchID); err != nil {
			slog.Error("failed to load trip context", "trip_id", it.tripID, "error", err)
			continue
		}

		carrierID, dist, err := s.selectCarrier(ctx, batchID, originLat, originLng)
		now := time.Now().UTC()
		if err == nil {
			if _, err := tx.Exec(ctx, `
				UPDATE trips SET status='ASSIGNED', carrier_id=$1, assigned_at=$2, assigned_distance_meters=$3 WHERE id=$4
			`, carrierID, now, dist, it.tripID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM pending_assignments WHERE trip_id=$1`, it.tripID); err != nil {
				return err
			}

			envelope := map[string]interface{}{
				"event_id":       uuid.NewString(),
				"event_type":     "trips.assigned",
				"occurred_at":    now,
				"correlation_id": batchID,
				"data": map[string]interface{}{
					"trip_id":                  it.tripID,
					"batch_id":                 batchID,
					"carrier_id":               carrierID,
					"origin_lat":               originLat,
					"origin_lng":               originLng,
					"destination_lat":          destLat,
					"destination_lng":          destLng,
					"assigned_distance_meters": dist,
					"assigned_at":              now,
				},
			}
			payload, err := json.Marshal(envelope)
			if err != nil {
				return err
			}
			evt := outbox.Event{
				ID:            uuid.NewString(),
				EventType:     "trips.assigned",
				CorrelationID: batchID,
				Topic:         s.outTopic,
				PartitionKey:  it.tripID,
				Payload:       payload,
				OccurredAt:    now,
			}
			if err := outbox.EnqueueTx(ctx, tx, evt); err != nil {
				return err
			}

			slog.Info("Trip assigned to carrier", "trip_id", it.tripID, "carrier_id", carrierID, "attempts", it.attemptCount)
		} else {
			newCount := it.attemptCount + 1
			if newCount < 10 {
				if _, err := tx.Exec(ctx, `
					UPDATE pending_assignments 
					SET attempt_count=$1, timeout_at=NOW() + INTERVAL '5 minutes' 
					WHERE trip_id=$2
				`, newCount, it.tripID); err != nil {
					return err
				}
			} else {
				if _, err := tx.Exec(ctx, `
					UPDATE trips SET status='REQUIRES_MANUAL_ASSIGNMENT' WHERE id=$1
				`, it.tripID); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `DELETE FROM pending_assignments WHERE trip_id=$1`, it.tripID); err != nil {
					return err
				}

				envelope := map[string]interface{}{
					"event_id":       uuid.NewString(),
					"event_type":     "trip_requires_manual_assignment",
					"occurred_at":    now,
					"correlation_id": it.tripID,
					"data": map[string]interface{}{
						"trip_id": it.tripID,
						"reason":  "max_attempts_reached",
					},
				}
				payload, err := json.Marshal(envelope)
				if err != nil {
					return err
				}
				evt := outbox.Event{
					ID:            uuid.NewString(),
					EventType:     "trip_requires_manual_assignment",
					CorrelationID: it.tripID,
					Topic:         "alerts.trip_requires_manual_assignment",
					PartitionKey:  it.tripID,
					Payload:       payload,
					OccurredAt:    now,
				}
				if err := outbox.EnqueueTx(ctx, tx, evt); err != nil {
					return err
				}
				slog.Info("Trip requires manual assignment after failed attempts", "trip_id", it.tripID, "attempts", 10)
			}
		}
	}

	return tx.Commit(ctx)
}

func (s *Service) HandleEvent(ctx context.Context, topic string, key, value []byte) error {
	switch topic {
	case "batches.formed":
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
		var data struct {
			BatchID          string    `json:"batch_id"`
			OriginType       string    `json:"origin_type"`
			OriginID         string    `json:"origin_id"`
			OriginLat        float64   `json:"origin_lat"`
			OriginLng        float64   `json:"origin_lng"`
			DestinationType  string    `json:"destination_type"`
			DestinationID    string    `json:"destination_id"`
			DestinationLat   float64   `json:"destination_lat"`
			DestinationLng   float64   `json:"destination_lng"`
			IsHubDestination bool      `json:"is_hub_destination"`
			OrderIDs         []string  `json:"order_ids"`
			FormedAt         time.Time `json:"formed_at"`
		}
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			return err
		}
		// Idempotency check before heavy logic
		if s.isProcessed(ctx, envelope.EventID) {
			return nil
		}
		carrierID, dist, err := s.selectCarrier(ctx, data.BatchID, data.OriginLat, data.OriginLng)
		if err != nil {
			// Create PENDING trip when no suitable carriers are available (critical improvement)
			now := time.Now().UTC()
			tx, e := s.tripDB.Begin(ctx)
			if e != nil {
				return e
			}
			defer func() { _ = tx.Rollback(ctx) }()
			var inserted string
			if e := tx.QueryRow(ctx, `
				INSERT INTO processed_events(event_id, event_type, processed_at)
				VALUES ($1, $2, NOW())
				ON CONFLICT (event_id) DO NOTHING
				RETURNING event_id
			`, envelope.EventID, envelope.EventType).Scan(&inserted); e != nil {
				if e == sql.ErrNoRows {
					return nil
				}
				return e
			}
			var newTripID string
			if e := tx.QueryRow(ctx, `
				INSERT INTO trips (id, carrier_id, status, assigned_at, assigned_distance_meters, origin_lat, origin_lng, dest_lat, dest_lng) 
				VALUES (gen_random_uuid(), NULL, 'PENDING', NULL, 0, $1, $2, $3, $4) RETURNING id
			`, data.OriginLat, data.OriginLng, data.DestinationLat, data.DestinationLng).Scan(&newTripID); e != nil {
				return e
			}
			if _, e := tx.Exec(ctx, `
				INSERT INTO trip_batches (trip_id, batch_id) VALUES ($1, $2)
			`, newTripID, data.BatchID); e != nil {
				return e
			}
			if e := tx.Commit(ctx); e != nil {
				return e
			}
			_ = now
			return nil
		}
		return s.createTripWithEvent(ctx, envelope.EventID, envelope.EventType, carrierID, data.BatchID, dist, data.OriginLat, data.OriginLng, data.DestinationLat, data.DestinationLng)
	case "events.batch_picked_up":
		var envelope struct {
			EventID   string          `json:"event_id"`
			EventType string          `json:"event_type"`
			Data      json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(value, &envelope); err != nil {
			return err
		}
		var data struct {
			BatchID string `json:"batch_id"`
		}
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			return err
		}
		return s.handleBatchPickedUp(ctx, envelope.EventID, envelope.EventType, data.BatchID)
	case "events.batch_delivered_to_pvp":
		var envelope struct {
			EventID   string          `json:"event_id"`
			EventType string          `json:"event_type"`
			Data      json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(value, &envelope); err != nil {
			return err
		}
		var data struct {
			BatchID string `json:"batch_id"`
		}
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			return err
		}
		return s.handleBatchDelivered(ctx, envelope.EventID, envelope.EventType, data.BatchID)
	case "events.carrier_location":
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
		var data struct {
			CarrierID string    `json:"carrier_id"`
			Latitude  float64   `json:"latitude"`
			Longitude float64   `json:"longitude"`
			Timestamp time.Time `json:"timestamp"`
		}
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			// Try legacy fields if new ones missing
			var legacy struct {
				CarrierID string    `json:"carrier_id"`
				Lat       float64   `json:"lat"`
				Lng       float64   `json:"lng"`
				UpdatedAt time.Time `json:"updated_at"`
			}
			if err2 := json.Unmarshal(envelope.Data, &legacy); err2 == nil && legacy.CarrierID != "" {
				data.CarrierID = legacy.CarrierID
				data.Latitude = legacy.Lat
				data.Longitude = legacy.Lng
				data.Timestamp = legacy.UpdatedAt
			} else {
				return err
			}
		}
		return s.upsertCarrierLocation(ctx, envelope.EventID, envelope.EventType, data.CarrierID, data.Latitude, data.Longitude, data.Timestamp)
	case "events.reference_updated":
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
		var data struct {
			UpdateType string `json:"update_type"`
			Carrier    struct {
				ID       string `json:"id"`
				IsActive bool   `json:"is_active"`
			} `json:"carrier"`
			Reason string `json:"reason"`
		}
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			return err
		}
		if data.UpdateType == "carrier" && data.Carrier.ID != "" {
			return s.updateCarrierStatus(ctx, envelope.EventID, data.Carrier.ID, data.Carrier.IsActive, envelope.OccurredAt, data.Reason)
		}
		return nil
	case "commands.trip.reassign":
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
		var data struct {
			OriginalTripID string    `json:"original_trip_id"`
			BatchID        string    `json:"batch_id"`
			Reason         string    `json:"reason"`
			TimeoutAt      time.Time `json:"timeout_at"`
		}
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			return err
		}
		return s.handleReassign(ctx, envelope.EventID, envelope.EventType, data.OriginalTripID, data.BatchID, data.Reason)
	default:
		return nil
	}
}

func (s *Service) isProcessed(ctx context.Context, eventID string) bool {
	var exists bool
	err := s.tripDB.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM processed_events WHERE event_id=$1)", eventID).Scan(&exists)
	return err == nil && exists
}

func (s *Service) handleBatchPickedUp(ctx context.Context, eventID, eventType, batchID string) error {
	tx, err := s.tripDB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var inserted string
	if err := tx.QueryRow(ctx, `
		INSERT INTO processed_events(event_id, event_type, processed_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (event_id) DO NOTHING
		RETURNING event_id
	`, eventID, eventType).Scan(&inserted); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}

	// Update trip status to IN_PROGRESS (Algo 9)
	var tripID, carrierID string
	if err := tx.QueryRow(ctx, `
		UPDATE trips t
		SET status = 'IN_PROGRESS'
		FROM trip_batches tb
		WHERE t.id = tb.trip_id AND tb.batch_id = $1 AND t.status = 'ASSIGNED'
		RETURNING t.id, t.carrier_id
	`, batchID).Scan(&tripID, &carrierID); err != nil {
		if err == sql.ErrNoRows {
			// Trip might not be in ASSIGNED state or not found, ignore
			return tx.Commit(ctx)
		}
		return err
	}

	// Publish trip.started
	now := time.Now().UTC()
	envelope := map[string]interface{}{
		"event_id":       uuid.NewString(),
		"event_type":     "trips.started",
		"occurred_at":    now,
		"correlation_id": batchID,
		"data": map[string]interface{}{
			"trip_id":    tripID,
			"batch_id":   batchID,
			"carrier_id": carrierID,
			"started_at": now,
		},
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	evt := outbox.Event{
		ID:            uuid.NewString(),
		EventType:     "trips.started",
		CorrelationID: batchID,
		Topic:         s.outTopic,
		PartitionKey:  tripID,
		Payload:       payload,
		OccurredAt:    now,
	}
	if err := outbox.EnqueueTx(ctx, tx, evt); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Service) handleBatchDelivered(ctx context.Context, eventID, eventType, batchID string) error {
	tx, err := s.tripDB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var inserted string
	if err := tx.QueryRow(ctx, `
		INSERT INTO processed_events(event_id, event_type, processed_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (event_id) DO NOTHING
		RETURNING event_id
	`, eventID, eventType).Scan(&inserted); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}

	// Update trip status to COMPLETED (Algo 10)
	var tripID, carrierID string
	if err := tx.QueryRow(ctx, `
		UPDATE trips t
		SET status = 'COMPLETED'
		FROM trip_batches tb
		WHERE t.id = tb.trip_id AND tb.batch_id = $1 AND t.status = 'IN_PROGRESS'
		RETURNING t.id, t.carrier_id
	`, batchID).Scan(&tripID, &carrierID); err != nil {
		if err == sql.ErrNoRows {
			return tx.Commit(ctx)
		}
		return err
	}

	// Publish trip.completed
	now := time.Now().UTC()
	envelope := map[string]interface{}{
		"event_id":       uuid.NewString(),
		"event_type":     "trips.completed",
		"occurred_at":    now,
		"correlation_id": batchID,
		"data": map[string]interface{}{
			"trip_id":      tripID,
			"batch_id":     batchID,
			"carrier_id":   carrierID,
			"completed_at": now,
		},
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	evt := outbox.Event{
		ID:            uuid.NewString(),
		EventType:     "trips.completed",
		CorrelationID: batchID,
		Topic:         s.outTopic,
		PartitionKey:  tripID,
		Payload:       payload,
		OccurredAt:    now,
	}
	if err := outbox.EnqueueTx(ctx, tx, evt); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Service) selectCarrier(ctx context.Context, batchID string, originLat, originLng float64) (string, int, error) {
	rows, err := s.tripDB.Query(ctx, `
		SELECT a.carrier_id, p.latitude, p.longitude
		FROM carrier_activity_cache a
		LEFT JOIN carrier_positions p ON p.carrier_id = a.carrier_id
		WHERE a.is_active = true AND a.updated_at > NOW() - INTERVAL '1 hour'
	`)
	if err != nil {
		return "", 0, err
	}
	defer rows.Close()
	var cands []carrierCandidate
	for rows.Next() {
		var id string
		var lat, lng sql.NullFloat64
		_ = rows.Scan(&id, &lat, &lng)
		c := carrierCandidate{id: id}
		if lat.Valid && lng.Valid {
			c.hasPos = true
			c.lat = lat.Float64
			c.lng = lng.Float64
		}
		cands = append(cands, c)
	}
	return chooseCarrier(originLat, originLng, cands)
}

type carrierCandidate struct {
	id     string
	lat    float64
	lng    float64
	hasPos bool
}

func chooseCarrier(originLat, originLng float64, cands []carrierCandidate) (string, int, error) {
	if len(cands) == 0 {
		return "", 0, fmt.Errorf("no active carriers")
	}
	bestID := ""
	bestDist := math.MaxFloat64
	for _, c := range cands {
		if !c.hasPos {
			continue
		}
		d := haversine(originLat, originLng, c.lat, c.lng)
		if d > 5000 {
			continue
		}
		if d < bestDist {
			bestDist = d
			bestID = c.id
		}
	}
	if bestID == "" {
		return "", 0, fmt.Errorf("no active carriers within 5km")
	}
	return bestID, int(bestDist), nil
}

func (s *Service) createTripWithEvent(ctx context.Context, eventID, eventType, carrierID, batchID string, dist int, originLat, originLng, destLat, destLng float64) error {
	now := time.Now().UTC()
	tx, err := s.tripDB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var inserted string
	if err := tx.QueryRow(ctx, `
		INSERT INTO processed_events(event_id, event_type, processed_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (event_id) DO NOTHING
		RETURNING event_id
	`, eventID, eventType).Scan(&inserted); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}

	var tripID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO trips (id, carrier_id, status, assigned_at, assigned_distance_meters, origin_lat, origin_lng, dest_lat, dest_lng) 
		VALUES (gen_random_uuid(), $1, 'ASSIGNED', NOW(), $2, $3, $4, $5, $6) RETURNING id
	`, carrierID, dist, originLat, originLng, destLat, destLng).Scan(&tripID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO trip_batches (trip_id, batch_id) VALUES ($1, $2)
	`, tripID, batchID); err != nil {
		return err
	}
	envelope := map[string]interface{}{
		"event_id":       uuid.NewString(),
		"event_type":     "trips.assigned",
		"occurred_at":    now,
		"correlation_id": batchID,
		"data": map[string]interface{}{
			"trip_id":                  tripID,
			"batch_id":                 batchID,
			"carrier_id":               carrierID,
			"origin_lat":               originLat,
			"origin_lng":               originLng,
			"destination_lat":          destLat,
			"destination_lng":          destLng,
			"assigned_distance_meters": dist,
			"assigned_at":              now,
		},
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	evt := outbox.Event{
		ID:            uuid.NewString(),
		EventType:     "trips.assigned",
		CorrelationID: batchID,
		Topic:         s.outTopic,
		PartitionKey:  tripID,
		Payload:       payload,
		OccurredAt:    now,
	}
	if err := outbox.EnqueueTx(ctx, tx, evt); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Service) upsertCarrierLocation(ctx context.Context, eventID, eventType, carrierID string, lat, lng float64, updatedAt time.Time) error {
	tx, err := s.tripDB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var inserted string
	if err := tx.QueryRow(ctx, `
		INSERT INTO processed_events(event_id, event_type, processed_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (event_id) DO NOTHING
		RETURNING event_id
	`, eventID, eventType).Scan(&inserted); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO carrier_positions(carrier_id, latitude, longitude, last_seen)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (carrier_id)
		DO UPDATE SET latitude=EXCLUDED.latitude, longitude=EXCLUDED.longitude, last_seen=EXCLUDED.last_seen
	`, carrierID, lat, lng, updatedAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO carrier_activity_cache (carrier_id, is_active, updated_at)
		VALUES ($1, true, $2)
		ON CONFLICT (carrier_id) DO UPDATE
		SET is_active = true, updated_at = $2
	`, carrierID, updatedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) updateCarrierStatus(ctx context.Context, eventID, carrierID string, isActive bool, updatedAt time.Time, reason string) error {
	tx, err := s.tripDB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var inserted string
	if err := tx.QueryRow(ctx, `
		INSERT INTO processed_events(event_id, event_type, processed_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (event_id) DO NOTHING
		RETURNING event_id
	`, eventID, "events.reference_updated").Scan(&inserted); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO carrier_activity_cache (carrier_id, is_active, updated_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (carrier_id) DO UPDATE
		SET is_active = $2, updated_at = $3
	`, carrierID, isActive, updatedAt); err != nil {
		return err
	}

	if isActive {
		s.carriersCache.Store(carrierID, true)
	} else {
		s.carriersCache.Delete(carrierID)
	}

	slog.Info("Carrier status updated", "carrier_id", carrierID, "is_active", isActive, "reason", reason)

	return tx.Commit(ctx)
}

func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371000.0
	phi1 := lat1 * math.Pi / 180.0
	phi2 := lat2 * math.Pi / 180.0
	dphi := (lat2 - lat1) * math.Pi / 180.0
	dlam := (lon2 - lon1) * math.Pi / 180.0
	a := math.Sin(dphi/2)*math.Sin(dphi/2) + math.Cos(phi1)*math.Cos(phi2)*math.Sin(dlam/2)*math.Sin(dlam/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}

func (s *Service) handleReassign(ctx context.Context, eventID, eventType, tripID, batchID, reason string) error {
	// Idempotency
	now := time.Now().UTC()
	tx, err := s.tripDB.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var inserted string
	if err := tx.QueryRow(ctx, `
		INSERT INTO processed_events(event_id, event_type, processed_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (event_id) DO NOTHING
		RETURNING event_id
	`, eventID, eventType).Scan(&inserted); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if inserted == "" {
		return nil
	}
	// Load existing trip context (coordinates)
	var originLat, originLng, destLat, destLng sql.NullFloat64
	var status string
	if err := tx.QueryRow(ctx, `
		SELECT origin_lat, origin_lng, dest_lat, dest_lng, status
		FROM trips WHERE id=$1
	`, tripID).Scan(&originLat, &originLng, &destLat, &destLng, &status); err != nil {
		return err
	}

	if status == "PENDING" {
		if _, err := tx.Exec(ctx, "UPDATE pending_assignments SET timeout_at = NOW() WHERE trip_id = $1", tripID); err != nil {
			return err
		}
		slog.Info("Pending reassignment attempt triggered by command", "trip_id", tripID)
		return tx.Commit(ctx)
	}

	if !originLat.Valid || !originLng.Valid {
		return fmt.Errorf("trip %s missing origin coordinates", tripID)
	}

	// Select new carrier
	carrierID, dist, err := s.selectCarrier(ctx, batchID, originLat.Float64, originLng.Float64)
	if err != nil {
		// If carrier selection failed (likely no carrier found), create PENDING trip
		var newTripID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO trips (id, carrier_id, status, assigned_at, assigned_distance_meters, origin_lat, origin_lng, dest_lat, dest_lng) 
			VALUES (gen_random_uuid(), NULL, 'PENDING', NULL, 0, $1, $2, $3, $4) RETURNING id
		`, originLat.Float64, originLng.Float64, destLat.Float64, destLng.Float64).Scan(&newTripID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO trip_batches (trip_id, batch_id) VALUES ($1, $2)
		`, newTripID, batchID); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}

	// Create new trip and attach batch
	var newTripID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO trips (id, carrier_id, status, assigned_at, assigned_distance_meters, origin_lat, origin_lng, dest_lat, dest_lng) 
		VALUES (gen_random_uuid(), $1, 'ASSIGNED', NOW(), $2, $3, $4, $5, $6) RETURNING id
	`, carrierID, dist, originLat.Float64, originLng.Float64, destLat.Float64, destLng.Float64).Scan(&newTripID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO trip_batches (trip_id, batch_id) VALUES ($1, $2)
	`, newTripID, batchID); err != nil {
		return err
	}

	// Update old trip status to REASSIGNED
	if _, err := tx.Exec(ctx, "UPDATE trips SET status = 'REASSIGNED' WHERE id = $1", tripID); err != nil {
		return err
	}

	// Publish trips.assigned
	envelope := map[string]interface{}{
		"event_id":       uuid.NewString(),
		"event_type":     "trips.assigned",
		"occurred_at":    now,
		"correlation_id": batchID,
		"data": map[string]interface{}{
			"trip_id":                  newTripID,
			"batch_id":                 batchID,
			"carrier_id":               carrierID,
			"origin_lat":               originLat.Float64,
			"origin_lng":               originLng.Float64,
			"destination_lat":          destLat.Float64,
			"destination_lng":          destLng.Float64,
			"assigned_distance_meters": dist,
			"assigned_at":              now,
		},
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return err
	}
	evt := outbox.Event{
		ID:            uuid.NewString(),
		EventType:     "trips.assigned",
		CorrelationID: batchID,
		Topic:         s.outTopic,
		PartitionKey:  newTripID,
		Payload:       payload,
		OccurredAt:    now,
	}
	if err := outbox.EnqueueTx(ctx, tx, evt); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return nil
}
