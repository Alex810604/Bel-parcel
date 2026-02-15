package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"bel-parcel/services/reference-service/internal/infra/outbox"
	"bel-parcel/services/reference-service/internal/metrics"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	db          *pgxpool.Pool
	producer    Publisher
	eventsTopic string
}

type Publisher interface {
	Publish(ctx context.Context, topic, key string, msg interface{}) error
}

func NewService(db *pgxpool.Pool, producer Publisher, eventsTopic string) *Service {
	return &Service{db: db, producer: producer, eventsTopic: eventsTopic}
}

type AuditInfo struct {
	OperatorID string    `json:"operator_id"`
	Reason     string    `json:"reason"`
	Timestamp  time.Time `json:"timestamp"`
}

func (s *Service) UpdatePVZHubFlag(ctx context.Context, id string, isHub bool, audit AuditInfo) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	ct, err := tx.Exec(ctx, `
		UPDATE pickup_points SET is_hub=$2, updated_at=NOW() WHERE id=$1
	`, id, isHub)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errors.New("pickup point not found")
	}

	eventID := uuid.New().String()
	now := time.Now().UTC()
	payload := map[string]interface{}{
		"event_id":       eventID,
		"event_type":     "events.reference_updated",
		"occurred_at":    now,
		"correlation_id": id,
		"data": map[string]interface{}{
			"update_type":  "pickup_point",
			"pickup_point": map[string]interface{}{"pvp_id": id, "is_hub": isHub},
			"operator_id":  audit.OperatorID,
			"reason":       audit.Reason,
			"updated_at":   now,
		},
	}
	if err := s.enqueueEvent(ctx, tx, "events.reference_updated", id, payload); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	slog.Info("pickup point updated", "pvp_id", id, "is_hub", isHub, "operator_id", audit.OperatorID, "reason", audit.Reason, "timestamp", now)
	return nil
}

func (s *Service) UpdateCarrierActive(ctx context.Context, id string, isActive bool, audit AuditInfo) error {
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	ct, err := tx.Exec(ctx, `
		UPDATE carriers SET is_active=$2, updated_at=NOW() WHERE id=$1
	`, id, isActive)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return errors.New("carrier not found")
	}
	eventID := uuid.New().String()
	now := time.Now().UTC()
	payload := map[string]interface{}{
		"event_id":       eventID,
		"event_type":     "events.reference_updated",
		"occurred_at":    now,
		"correlation_id": id,
		"data": map[string]interface{}{
			"update_type": "carrier",
			"carrier":     map[string]interface{}{"carrier_id": id, "is_active": isActive},
			"operator_id": audit.OperatorID,
			"reason":      audit.Reason,
			"updated_at":  now,
		},
	}
	if err := s.enqueueEvent(ctx, tx, "events.reference_updated", id, payload); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	slog.Info("carrier updated", "carrier_id", id, "is_active", isActive, "operator_id", audit.OperatorID, "reason", audit.Reason, "timestamp", now)
	return nil
}

func (s *Service) Search(ctx context.Context, typ string, q string, limit int, offset int) ([]map[string]interface{}, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	q = strings.TrimSpace(q)
	var rows pgx.Rows
	var err error
	start := time.Now()
	label := ""
	switch typ {
	case "pvz":
		label = "search_pvz"
		rows, err = s.db.Query(ctx, `
			SELECT id, name, address, location_lat, location_lng, is_hub
			FROM pickup_points
			WHERE to_tsvector('russian', name || ' ' || COALESCE(address, ''))
			      @@ plainto_tsquery('russian', $1)
			ORDER BY updated_at DESC
			LIMIT $2 OFFSET $3
		`, q, limit, offset)
	case "carriers":
		label = "search_carriers"
		rows, err = s.db.Query(ctx, `
			SELECT id, name, is_active, last_seen
			FROM carriers
			WHERE to_tsvector('russian', name)
			      @@ plainto_tsquery('russian', $1)
			ORDER BY updated_at DESC
			LIMIT $2 OFFSET $3
		`, q, limit, offset)
	case "warehouses":
		label = "search_warehouses"
		rows, err = s.db.Query(ctx, `
			SELECT id, name, address, location_lat, location_lng
			FROM warehouses
			WHERE to_tsvector('russian', name || ' ' || COALESCE(address, ''))
			      @@ plainto_tsquery('russian', $1)
			ORDER BY updated_at DESC
			LIMIT $2 OFFSET $3
		`, q, limit, offset)
	default:
		return nil, fmt.Errorf("unknown type: %s", typ)
	}
	if err != nil {
		return nil, err
	}
	metrics.DBQueryDuration.WithLabelValues(label).Observe(time.Since(start).Seconds())
	defer rows.Close()
	var res []map[string]interface{}
	for rows.Next() {
		values, _ := rows.Values()
		row := map[string]interface{}{}
		for i, fd := range rows.FieldDescriptions() {
			row[string(fd.Name)] = values[i]
		}
		res = append(res, row)
	}
	return res, rows.Err()
}

func (s *Service) enqueueEvent(ctx context.Context, tx pgx.Tx, eventType, partitionKey string, payload map[string]interface{}) error {
	data, _ := jsonMarshal(payload)
	return outbox.EnqueueTx(ctx, tx, outbox.Event{
		EventType:     eventType,
		CorrelationID: partitionKey,
		Topic:         s.eventsTopic,
		PartitionKey:  partitionKey,
		Payload:       data,
	})
}

func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
