package tracking

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"bel-parcel/services/tracking-service/internal/outbox"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Service struct {
	db                   *pgxpool.Pool
	deviationThresholdM  float64
	lateThresholdMinutes int
}

func NewService(db *pgxpool.Pool, deviationThresholdMeters float64, lateThresholdMinutes int) *Service {
	return &Service{db: db, deviationThresholdM: deviationThresholdMeters, lateThresholdMinutes: lateThresholdMinutes}
}

func (s *Service) HandleEvent(ctx context.Context, topic string, key, value []byte) error {
	switch topic {
	case "трипы.назначены":
		return s.handleTripAssigned(ctx, value)
	case "трипы.подтверждены":
		return s.handleTripConfirmed(ctx, value)
	case "местоположение.перевозчика":
		return s.handleCarrierLocation(ctx, value)
	case "алерты.требуется_ручное_назначение":
		return s.handleManualAssignmentAlert(ctx, value)
	case "команды.переназначить":
		return s.handleReassignCommand(ctx, value)
	default:
		return nil
	}
}

func (s *Service) handleTripAssigned(ctx context.Context, value []byte) error {
	var envelope struct {
		EventID    string          `json:"event_id"`
		OccurredAt time.Time       `json:"occurred_at"`
		Data       json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(value, &envelope); err != nil {
		return err
	}
	var data struct {
		TripID            string    `json:"trip_id"`
		CarrierID         string    `json:"carrier_id"`
		OriginLat         float64   `json:"origin_lat"`
		OriginLng         float64   `json:"origin_lng"`
		DestinationLat    float64   `json:"destination_lat"`
		DestinationLng    float64   `json:"destination_lng"`
		AssignedAt        time.Time `json:"assigned_at"`
		EstimatedDuration string    `json:"estimated_duration"` // e.g., "2 hours"
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id string
	if err := tx.QueryRow(ctx, `
		INSERT INTO processed_events(event_id, occurred_at)
		VALUES ($1, $2)
		ON CONFLICT (event_id) DO NOTHING
		RETURNING event_id
	`, envelope.EventID, envelope.OccurredAt).Scan(&id); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if id == "" {
		return nil
	}
	totalRouteDist := haversine(data.OriginLat, data.OriginLng, data.DestinationLat, data.DestinationLng)
	_, err = tx.Exec(ctx, `
		INSERT INTO active_trips(trip_id, carrier_id, origin_lat, origin_lng, destination_lat, destination_lng, assigned_at, estimated_duration, status, total_route_distance_meters)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8::interval,'assigned',$9)
		ON CONFLICT (trip_id) DO UPDATE SET carrier_id=EXCLUDED.carrier_id, origin_lat=EXCLUDED.origin_lat, origin_lng=EXCLUDED.origin_lng, destination_lat=EXCLUDED.destination_lat, destination_lng=EXCLUDED.destination_lng, assigned_at=EXCLUDED.assigned_at, estimated_duration=EXCLUDED.estimated_duration, status='assigned', total_route_distance_meters=EXCLUDED.total_route_distance_meters
	`, data.TripID, data.CarrierID, data.OriginLat, data.OriginLng, data.DestinationLat, data.DestinationLng, data.AssignedAt, data.EstimatedDuration, totalRouteDist)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) handleTripConfirmed(ctx context.Context, value []byte) error {
	var envelope struct {
		EventID    string          `json:"event_id"`
		OccurredAt time.Time       `json:"occurred_at"`
		Data       json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(value, &envelope); err != nil {
		return err
	}
	var data struct {
		TripID string `json:"trip_id"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id string
	if err := tx.QueryRow(ctx, `
		INSERT INTO processed_events(event_id, occurred_at)
		VALUES ($1, $2)
		ON CONFLICT (event_id) DO NOTHING
		RETURNING event_id
	`, envelope.EventID, envelope.OccurredAt).Scan(&id); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if id == "" {
		return nil
	}
	_, err = tx.Exec(ctx, `UPDATE active_trips SET status='in_transit' WHERE trip_id=$1`, data.TripID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) handleCarrierLocation(ctx context.Context, value []byte) error {
	var envelope struct {
		EventID    string          `json:"event_id"`
		OccurredAt time.Time       `json:"occurred_at"`
		Data       json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(value, &envelope); err != nil {
		return err
	}
	var data struct {
		TripID    string    `json:"trip_id"`
		CarrierID string    `json:"carrier_id"`
		Lat       float64   `json:"lat"`
		Lng       float64   `json:"lng"`
		Timestamp time.Time `json:"timestamp"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id string
	if err := tx.QueryRow(ctx, `
		INSERT INTO processed_events(event_id, occurred_at)
		VALUES ($1, $2)
		ON CONFLICT (event_id) DO NOTHING
		RETURNING event_id
	`, envelope.EventID, envelope.OccurredAt).Scan(&id); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if id == "" {
		return nil
	}
	var originLat, originLng, destLat, destLng float64
	var assignedAt time.Time
	var estDur string
	var totalRouteDist float64
	if err := tx.QueryRow(ctx, `
		SELECT origin_lat, origin_lng, destination_lat, destination_lng, assigned_at, estimated_duration::text, total_route_distance_meters
		FROM active_trips WHERE trip_id=$1
	`, data.TripID).Scan(&originLat, &originLng, &destLat, &destLng, &assignedAt, &estDur, &totalRouteDist); err != nil {
		return err
	}
	deviation := calculateDeviation(data.Lat, data.Lng, originLat, originLng, destLat, destLng)
	eta := calculateEstimatedArrival(assignedAt, estDur, data.Lat, data.Lng, destLat, destLng, totalRouteDist)
	if _, err := tx.Exec(ctx, `
		INSERT INTO carrier_locations(trip_id, lat, lng, recorded_at, deviation_meters, estimated_arrival)
		VALUES ($1,$2,$3,$4,$5,$6)
	`, data.TripID, data.Lat, data.Lng, data.Timestamp, deviation, eta); err != nil {
		return err
	}
	if deviation > s.deviationThresholdM {
		_ = createAlertTx(ctx, tx, "route_deviation", data.TripID, data.CarrierID, "Отклонение от маршрута >500м", "warning")
	}
	delay := eta.Sub(assignedAt)
	if delay > parseDuration(estDur)+time.Duration(s.lateThresholdMinutes)*time.Minute {
		_ = createAlertTx(ctx, tx, "driver_late", data.TripID, data.CarrierID, "Опоздание >15 минут", "warning")
	}
	return tx.Commit(ctx)
}

func (s *Service) handleManualAssignmentAlert(ctx context.Context, value []byte) error {
	var envelope struct {
		EventID    string          `json:"event_id"`
		OccurredAt time.Time       `json:"occurred_at"`
		Data       json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(value, &envelope); err != nil {
		return err
	}
	var data struct {
		TripID    string `json:"trip_id"`
		CarrierID string `json:"carrier_id"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id string
	if err := tx.QueryRow(ctx, `
		INSERT INTO processed_events(event_id, occurred_at)
		VALUES ($1, $2)
		ON CONFLICT (event_id) DO NOTHING
		RETURNING event_id
	`, envelope.EventID, envelope.OccurredAt).Scan(&id); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if id == "" {
		return nil
	}
	if data.Message == "" {
		data.Message = "Требуется ручное назначение"
	}
	if err := createAlertTx(ctx, tx, "manual_assignment", data.TripID, data.CarrierID, data.Message, "warning"); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) handleReassignCommand(ctx context.Context, value []byte) error {
	var envelope struct {
		EventID    string          `json:"event_id"`
		OccurredAt time.Time       `json:"occurred_at"`
		Data       json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(value, &envelope); err != nil {
		return err
	}
	var data struct {
		TripID    string `json:"original_trip_id"`
		CarrierID string `json:"new_carrier_id"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		return err
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var id string
	if err := tx.QueryRow(ctx, `
		INSERT INTO processed_events(event_id, occurred_at)
		VALUES ($1, $2)
		ON CONFLICT (event_id) DO NOTHING
		RETURNING event_id
	`, envelope.EventID, envelope.OccurredAt).Scan(&id); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	if id == "" {
		return nil
	}
	if _, err := tx.Exec(ctx, `UPDATE active_trips SET carrier_id=$2, status='reassigned' WHERE trip_id=$1`, data.TripID, data.CarrierID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE active_alerts SET resolved_at=NOW() WHERE trip_id=$1 AND resolved_at IS NULL`, data.TripID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func createAlertTx(ctx context.Context, tx pgx.Tx, alertType, tripID, carrierID, message, severity string) error {
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx, `
		INSERT INTO active_alerts(alert_id, alert_type, trip_id, carrier_id, message, created_at, severity)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
	`, uuid.NewString(), alertType, tripID, carrierID, message, now, severity); err != nil {
		return err
	}
	envelope := map[string]interface{}{
		"event_id":       uuid.NewString(),
		"event_type":     "operator.alert",
		"occurred_at":    now,
		"correlation_id": tripID,
		"data": map[string]interface{}{
			"alert_type": alertType,
			"trip_id":    tripID,
			"carrier_id": carrierID,
			"message":    message,
			"severity":   severity,
		},
	}
	payload, _ := json.Marshal(envelope)
	return outbox.EnqueueTx(ctx, tx, outbox.Event{
		ID:            uuid.NewString(),
		EventType:     "operator.alert",
		CorrelationID: tripID,
		Topic:         "operator.alerts",
		PartitionKey:  tripID,
		Payload:       payload,
		OccurredAt:    now,
	})
}
