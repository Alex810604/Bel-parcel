package app

import (
	"bel-parcel/services/operator-api/internal/clients"
	"bel-parcel/services/operator-api/internal/infra/kafka"
	"bel-parcel/services/operator-api/internal/outbox"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Trip struct {
	ID                string    `json:"id"`
	OriginWarehouseID string    `json:"origin_warehouse_id"`
	PickupPointID     string    `json:"pickup_point_id"`
	CarrierID         string    `json:"carrier_id"`
	AssignedAt        time.Time `json:"assigned_at"`
	Status            string    `json:"status"`
}

type Reference struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

type Service struct {
	db           *pgxpool.Pool
	clients      *clients.Clients
	producer     *kafka.Producer
	commandTopic string
	cache        sync.Map
}

func NewService(db *pgxpool.Pool, clients *clients.Clients, producer *kafka.Producer, commandTopic string) *Service {
	return &Service{
		db:           db,
		clients:      clients,
		producer:     producer,
		commandTopic: commandTopic,
	}
}

type cacheEntry struct {
	data      interface{}
	expiresAt time.Time
}

func (s *Service) getCache(key string) (interface{}, bool) {
	val, ok := s.cache.Load(key)
	if !ok {
		return nil, false
	}
	entry := val.(cacheEntry)
	if time.Now().After(entry.expiresAt) {
		s.cache.Delete(key)
		return nil, false
	}
	return entry.data, true
}

func (s *Service) setCache(key string, data interface{}) {
	s.cache.Store(key, cacheEntry{
		data:      data,
		expiresAt: time.Now().Add(10 * time.Second),
	})
}

func (s *Service) ListTrips(ctx context.Context, status, pvz, carrier string, date *time.Time) ([]Trip, error) {
	cacheKey := fmt.Sprintf("list_trips:%s:%s:%s:%v", status, pvz, carrier, date)
	if val, ok := s.getCache(cacheKey); ok {
		return val.([]Trip), nil
	}

	params := url.Values{}
	if status != "" {
		params.Add("status", status)
	}
	if pvz != "" {
		params.Add("pvz", pvz)
	}
	if carrier != "" {
		params.Add("carrier", carrier)
	}
	if date != nil {
		params.Add("date", date.Format("2006-01-02"))
	}

	path := "/trips"
	if len(params) > 0 {
		path += "?" + params.Encode()
	}

	body, err := s.clients.Routing.Get(ctx, path)
	if err != nil {
		slog.Error("failed to call routing-service", "error", err)
		return nil, fmt.Errorf("routing service unavailable: %w", err)
	}

	var trips []Trip
	if err := json.Unmarshal(body, &trips); err != nil {
		return nil, err
	}

	s.setCache(cacheKey, trips)
	return trips, nil
}

func (s *Service) TripDetails(ctx context.Context, tripID string) (map[string]interface{}, error) {
	cacheKey := "trip_details:" + tripID
	if val, ok := s.getCache(cacheKey); ok {
		return val.(map[string]interface{}), nil
	}

	res := map[string]interface{}{}

	// 1. Get Trip from routing-service
	tripBody, err := s.clients.Routing.Get(ctx, "/trips/"+tripID)
	if err != nil {
		slog.Error("failed to get trip details", "trip_id", tripID, "error", err)
		return nil, fmt.Errorf("routing service unavailable: %w", err)
	}
	var trip Trip
	if err := json.Unmarshal(tripBody, &trip); err != nil {
		return nil, err
	}
	res["trip"] = trip

	// 2. Get Batches from batching-service
	batchBody, err := s.clients.Batching.Get(ctx, "/batches?trip_id="+tripID)
	if err != nil {
		slog.Warn("failed to get batches for trip", "trip_id", tripID, "error", err)
		res["batches"] = []string{}
	} else {
		var batchIDs []string
		if err := json.Unmarshal(batchBody, &batchIDs); err != nil {
			slog.Warn("failed to unmarshal batches", "error", err)
			res["batches"] = []string{}
		} else {
			res["batches"] = batchIDs
		}
	}

	// 3. Get Orders from order-service (if we have batches)
	var allOrders []string
	if batchIDs, ok := res["batches"].([]string); ok && len(batchIDs) > 0 {
		for _, bid := range batchIDs {
			orderBody, err := s.clients.Orders.Get(ctx, "/orders?batch_id="+bid)
			if err != nil {
				slog.Warn("failed to get orders for batch", "batch_id", bid, "error", err)
				continue
			}
			var oids []string
			if err := json.Unmarshal(orderBody, &oids); err == nil {
				allOrders = append(allOrders, oids...)
			}
		}
	}
	res["orders"] = allOrders

	s.setCache(cacheKey, res)
	return res, nil
}

func (s *Service) ReassignTrip(ctx context.Context, tripID, newCarrierID, reason, operatorID string) error {
	now := time.Now().UTC()
	envelope := map[string]interface{}{
		"event_id":       uuid.NewString(),
		"event_type":     "commands.trip.reassign",
		"occurred_at":    now,
		"correlation_id": tripID,
		"data": map[string]interface{}{
			"trip_id":        tripID,
			"new_carrier_id": newCarrierID,
			"reason":         reason,
			"requested_at":   now,
			"operator_id":    operatorID,
		},
	}
	payload, _ := json.Marshal(envelope)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	evt := outbox.Event{
		ID:            uuid.New().String(),
		EventType:     "commands.trip.reassign",
		CorrelationID: tripID,
		Topic:         s.commandTopic,
		PartitionKey:  tripID,
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

func (s *Service) MonitorDelays(ctx context.Context, threshold time.Duration) ([]Trip, error) {
	cacheKey := fmt.Sprintf("delays:%v", threshold)
	if val, ok := s.getCache(cacheKey); ok {
		return val.([]Trip), nil
	}

	path := fmt.Sprintf("/trips/delayed?threshold=%s", threshold.String())
	body, err := s.clients.Routing.Get(ctx, path)
	if err != nil {
		slog.Error("failed to call routing-service for delays", "error", err)
		return nil, fmt.Errorf("routing service unavailable: %w", err)
	}

	var trips []Trip
	if err := json.Unmarshal(body, &trips); err != nil {
		return nil, err
	}

	s.setCache(cacheKey, trips)
	return trips, nil
}

func (s *Service) SearchReferences(ctx context.Context, typ, q string) ([]Reference, error) {
	cacheKey := fmt.Sprintf("refs:%s:%s", typ, q)
	if val, ok := s.getCache(cacheKey); ok {
		return val.([]Reference), nil
	}

	path := fmt.Sprintf("/%s?q=%s", typ, url.QueryEscape(q))
	body, err := s.clients.Reference.Get(ctx, path)
	if err != nil {
		slog.Error("failed to call reference-service", "type", typ, "error", err)
		return nil, fmt.Errorf("reference service unavailable: %w", err)
	}

	var refs []Reference
	if err := json.Unmarshal(body, &refs); err != nil {
		return nil, err
	}

	s.setCache(cacheKey, refs)
	return refs, nil
}

func (s *Service) markPublished(ctx context.Context, eventType, correlationID string) (bool, error) {
	var et string
	err := s.db.QueryRow(ctx, `
		INSERT INTO published_events(event_type, correlation_id, occurred_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (event_type, correlation_id) DO NOTHING
		RETURNING event_type
	`, eventType, correlationID).Scan(&et)
	if err != nil {
		return false, err
	}
	return et != "", nil
}

func (s *Service) UpdatePickupPoint(ctx context.Context, id string, isHub bool, reason string, operatorID string) error {
	// 1. Call reference-service API
	body := map[string]interface{}{
		"is_hub":      isHub,
		"reason":      reason,
		"operator_id": operatorID,
	}
	payload, _ := json.Marshal(body)
	slog.Info("sending reference update", "path", "/pvp/"+id, "payload", string(payload))
	_, err := s.clients.Reference.Put(ctx, "/pvp/"+id, bytes.NewReader(payload))
	if err != nil {
		slog.Error("failed to update pickup point via reference-service", "id", id, "error", err)
		return fmt.Errorf("reference service unavailable: %w", err)
	}

	// 2. Publish event via outbox (if required to maintain existing behavior)
	now := time.Now().UTC()
	envelope := map[string]interface{}{
		"event_id":       uuid.NewString(),
		"event_type":     "events.reference_updated",
		"occurred_at":    now,
		"correlation_id": id,
		"data": map[string]interface{}{
			"update_type": "pickup_point",
			"pickup_point": map[string]interface{}{
				"pvp_id": id,
				"is_hub": isHub,
			},
			"operator_id": operatorID,
		},
	}

	payload, _ = json.Marshal(envelope)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	evt := outbox.Event{
		ID:            uuid.NewString(),
		EventType:     "events.reference_updated",
		CorrelationID: id,
		Topic:         "events.reference_updated",
		PartitionKey:  id,
		Payload:       payload,
		OccurredAt:    now,
	}

	if err := outbox.EnqueueTx(ctx, tx, evt); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Service) UpdateCarrierStatus(ctx context.Context, carrierID string, isActive bool, reason string, operatorID string) error {
	// 1. Call reference-service API
	body := map[string]interface{}{
		"is_active":   isActive,
		"reason":      reason,
		"operator_id": operatorID,
	}
	payload, _ := json.Marshal(body)
	_, err := s.clients.Reference.Put(ctx, "/carriers/"+carrierID, bytes.NewReader(payload))
	if err != nil {
		slog.Error("failed to update carrier status via reference-service", "id", carrierID, "error", err)
		return fmt.Errorf("reference service unavailable: %w", err)
	}

	// 2. Publish event via outbox
	now := time.Now().UTC()
	envelope := map[string]interface{}{
		"event_id":       uuid.NewString(),
		"event_type":     "events.reference_updated",
		"occurred_at":    now,
		"correlation_id": carrierID,
		"data": map[string]interface{}{
			"update_type": "carrier",
			"carrier": map[string]interface{}{
				"id":        carrierID,
				"is_active": isActive,
			},
			"reason":      reason,
			"operator_id": operatorID,
		},
	}

	payload, _ = json.Marshal(envelope)
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	evt := outbox.Event{
		ID:            uuid.NewString(),
		EventType:     "events.reference_updated",
		CorrelationID: carrierID,
		Topic:         "events.reference_updated",
		PartitionKey:  carrierID,
		Payload:       payload,
		OccurredAt:    now,
	}

	if err := outbox.EnqueueTx(ctx, tx, evt); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *Service) itoa(i int) string {
	return strconv.Itoa(i)
}

func (s *Service) SyncCache(ctx context.Context, topic string, value []byte) error {
	var event struct {
		EventType string          `json:"event_type"`
		Data      json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(value, &event); err != nil {
		return err
	}

	switch topic {
	case "trips.updated":
		var trip Trip
		if err := json.Unmarshal(event.Data, &trip); err != nil {
			return err
		}
		_, err := s.db.Exec(ctx, `
			INSERT INTO trips_cache (id, origin_warehouse_id, pickup_point_id, carrier_id, assigned_at, status, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, NOW())
			ON CONFLICT (id) DO UPDATE SET
				origin_warehouse_id = EXCLUDED.origin_warehouse_id,
				pickup_point_id = EXCLUDED.pickup_point_id,
				carrier_id = EXCLUDED.carrier_id,
				assigned_at = EXCLUDED.assigned_at,
				status = EXCLUDED.status,
				updated_at = NOW()
		`, trip.ID, trip.OriginWarehouseID, trip.PickupPointID, trip.CarrierID, trip.AssignedAt, trip.Status)
		return err

	case "batches.updated":
		var batch struct {
			ID     string `json:"id"`
			TripID string `json:"trip_id"`
		}
		if err := json.Unmarshal(event.Data, &batch); err != nil {
			return err
		}
		_, err := s.db.Exec(ctx, `
			INSERT INTO batches_cache (id, trip_id, updated_at)
			VALUES ($1, $2, NOW())
			ON CONFLICT (id) DO UPDATE SET
				trip_id = EXCLUDED.trip_id,
				updated_at = NOW()
		`, batch.ID, batch.TripID)
		return err
	}

	return nil
}
