package app

import (
	"bel-parcel/services/order-service/internal/infra/kafka"
	"bel-parcel/services/order-service/internal/outbox"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Order struct {
	ID        string    `json:"id"`
	SellerID  string    `json:"seller_id"`
	PVZID     string    `json:"pvz_id"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Ping(ctx context.Context) error
}

type OrderService struct {
	db       DB
	producer *kafka.Producer
	topic    string
}

func NewOrderService(db DB, producer *kafka.Producer, topic string) *OrderService {
	return &OrderService{db: db, producer: producer, topic: topic}
}

type CreateOrderParams struct {
	SellerID       string
	PVZID          string
	CustomerPhone  string
	CustomerEmail  string
	WarehouseLat   float64
	WarehouseLng   float64
	DestinationLat float64
	DestinationLng float64
}

func (s *OrderService) CreateOrder(ctx context.Context, params CreateOrderParams) (*Order, error) {
	id := uuid.New().String()
	now := time.Now().UTC()

	order := &Order{
		ID:        id,
		SellerID:  params.SellerID,
		PVZID:     params.PVZID,
		Status:    "CREATED",
		CreatedAt: now,
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx failed: %w", err)
	}
	_, err = tx.Exec(ctx, `INSERT INTO orders (id, seller_id, pvz_id, status, created_at) VALUES ($1, $2, $3, $4, $5)`,
		order.ID, order.SellerID, order.PVZID, order.Status, order.CreatedAt)
	if err != nil {
		tx.Rollback(ctx)
		return nil, fmt.Errorf("db insert failed: %w", err)
	}
	envelope := map[string]interface{}{
		"event_id":       uuid.NewString(),
		"event_type":     "orders.created",
		"occurred_at":    now,
		"correlation_id": order.ID,
		"data": map[string]interface{}{
			"order_id":           order.ID,
			"warehouse_id":       order.SellerID,
			"warehouse_lat":      params.WarehouseLat,
			"warehouse_lng":      params.WarehouseLng,
			"destination_pvp_id": order.PVZID,
			"destination_lat":    params.DestinationLat,
			"destination_lng":    params.DestinationLng,
			"created_at":         order.CreatedAt,
		},
	}
	payload, _ := json.Marshal(envelope)
	evt := outbox.Event{
		ID:            uuid.New().String(),
		EventType:     "orders.created",
		CorrelationID: order.ID,
		Topic:         s.topic,
		PartitionKey:  order.ID,
		Payload:       payload,
		OccurredAt:    now,
	}
	if err := outbox.EnqueueTx(ctx, tx, evt); err != nil {
		tx.Rollback(ctx)
		return nil, fmt.Errorf("outbox enqueue failed: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit failed: %w", err)
	}

	slog.InfoContext(ctx, "order created", "order_id", order.ID)
	return order, nil
}

func (s *OrderService) Ping(ctx context.Context) error {
	return s.db.Ping(ctx)
}

func (s *OrderService) HandleKafkaEvent(ctx context.Context, topic string, key, value []byte) error {
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
		if ok, _ := s.markEventProcessed(ctx, envelope.EventID); !ok {
			return nil
		}
		var data struct {
			BatchID  string    `json:"batch_id"`
			OrderIDs []string  `json:"order_ids"`
			FormedAt time.Time `json:"formed_at"`
		}
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			return err
		}
		if err := s.storeBatchOrders(ctx, data.BatchID, data.OrderIDs); err != nil {
			return err
		}
		return s.applyOrdersStatusForBatch(ctx, data.BatchID, "BATCHED")
	case "events.batch_picked_up":
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
		if ok, _ := s.markEventProcessed(ctx, envelope.EventID); !ok {
			return nil
		}
		return nil
	case "events.batch_delivered_to_pvp":
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
		if ok, _ := s.markEventProcessed(ctx, envelope.EventID); !ok {
			return nil
		}
		var data struct {
			BatchID    string    `json:"batch_id"`
			PickedUpAt time.Time `json:"picked_up_at"`
		}
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			return err
		}
		return s.applyOrdersStatusForBatch(ctx, data.BatchID, "DELIVERED_TO_PVP")
	case "events.batch_received_by_pvp":
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
		if ok, _ := s.markEventProcessed(ctx, envelope.EventID); !ok {
			return nil
		}
		var data struct {
			BatchID     string    `json:"batch_id"`
			ReceivedAt  time.Time `json:"received_at"`
			PVPWorkerID string    `json:"pvp_worker_id"`
		}
		if err := json.Unmarshal(envelope.Data, &data); err != nil {
			return err
		}
		return s.applyOrdersStatusForBatch(ctx, data.BatchID, "RECEIVED_BY_PVP")
	default:
		return nil
	}
}

func (s *OrderService) applyOrderStatus(ctx context.Context, orderID, next string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var current string
	err = tx.QueryRow(ctx, "SELECT status FROM orders WHERE id=$1 FOR UPDATE", orderID).Scan(&current)
	if err != nil {
		return err
	}
	allowed := map[string]string{
		"CREATED":          "BATCHED",
		"BATCHED":          "DELIVERED_TO_PVP",
		"DELIVERED_TO_PVP": "RECEIVED_BY_PVP",
	}
	target, ok := allowed[current]
	if !ok || target != next {
		// If already in target state, idempotent success
		if current == next {
			return nil
		}
		return fmt.Errorf("invalid transition %s -> %s", current, next)
	}
	now := time.Now().UTC()
	_, err = tx.Exec(ctx, "UPDATE orders SET status=$2, updated_at=$3 WHERE id=$1", orderID, next, now)
	if err != nil {
		return err
	}

	envelope := map[string]interface{}{
		"event_id":       uuid.NewString(),
		"event_type":     "orders.status_updated",
		"occurred_at":    now,
		"correlation_id": orderID,
		"data": map[string]interface{}{
			"order_id":        orderID,
			"previous_status": current,
			"new_status":      next,
			"updated_at":      now,
		},
	}
	payload, _ := json.Marshal(envelope)
	evt := outbox.Event{
		ID:            uuid.NewString(),
		EventType:     "orders.status_updated",
		CorrelationID: orderID,
		Topic:         "orders.status_updated",
		PartitionKey:  orderID,
		Payload:       payload,
		OccurredAt:    now,
	}
	if err := outbox.EnqueueTx(ctx, tx, evt); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (s *OrderService) applyOrdersStatusForBatch(ctx context.Context, batchID, next string) error {
	rows, err := s.db.Query(ctx, "SELECT order_id FROM order_batches WHERE batch_id=$1", batchID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var oid string
		if err := rows.Scan(&oid); err != nil {
			return err
		}
		if err := s.applyOrderStatus(ctx, oid, next); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *OrderService) storeBatchOrders(ctx context.Context, batchID string, orderIDs []string) error {
	for _, oid := range orderIDs {
		_, err := s.db.Exec(ctx, "INSERT INTO order_batches (batch_id, order_id) VALUES ($1, $2) ON CONFLICT DO NOTHING", batchID, oid)
		if err != nil {
			return err
		}
	}
	return nil
}
func (s *OrderService) markEventProcessed(ctx context.Context, eventID string) (bool, error) {
	var id string
	err := s.db.QueryRow(ctx, "INSERT INTO processed_events(event_id, occurred_at) VALUES ($1, NOW()) ON CONFLICT (event_id) DO NOTHING RETURNING event_id", eventID).Scan(&id)
	if err != nil {
		return false, nil
	}
	return id != "", nil
}

func (s *OrderService) markPublished(ctx context.Context, eventType, correlationID string) (bool, error) {
	var et string
	err := s.db.QueryRow(ctx, `
		INSERT INTO published_events(event_type, correlation_id, occurred_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (event_type, correlation_id) DO NOTHING
		RETURNING event_type
	`, eventType, correlationID).Scan(&et)
	if err != nil {
		return false, nil
	}
	return et != "", nil
}
