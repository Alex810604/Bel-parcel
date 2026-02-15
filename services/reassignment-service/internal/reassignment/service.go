package reassignment

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"bel-parcel/services/reassignment-service/internal/outbox"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type DB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type Service struct {
	db                  DB
	commandTopic        string
	confirmationTimeout time.Duration
}

func NewService(db DB, commandTopic string, confirmationTimeout time.Duration) *Service {
	return &Service{db: db, commandTopic: commandTopic, confirmationTimeout: confirmationTimeout}
}

func (s *Service) HandleEvent(ctx context.Context, topic string, key, value []byte) error {
	switch topic {
	case "трипы.назначены":
		return s.handleTripAssigned(ctx, value)
	case "трипы.подтверждены":
		return s.handleTripConfirmed(ctx, value)
	case "трипы.отклонены":
		return s.handleTripRejected(ctx, value)
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
		TripID     string    `json:"trip_id"`
		BatchID    string    `json:"batch_id"`
		CarrierID  string    `json:"carrier_id"`
		AssignedAt time.Time `json:"assigned_at"`
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
	timeoutAt := data.AssignedAt.Add(s.confirmationTimeout)
	_, err = tx.Exec(ctx, `
		INSERT INTO pending_confirmations(trip_id, batch_id, carrier_id, assigned_at, timeout_at, status)
		VALUES ($1, $2, $3, $4, $5, 'pending')
		ON CONFLICT (trip_id) DO UPDATE SET batch_id=EXCLUDED.batch_id, carrier_id=EXCLUDED.carrier_id, assigned_at=EXCLUDED.assigned_at, timeout_at=EXCLUDED.timeout_at, status='pending'
	`, data.TripID, data.BatchID, data.CarrierID, data.AssignedAt, timeoutAt)
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
	_, err = tx.Exec(ctx, `
		DELETE FROM pending_confirmations WHERE trip_id=$1
	`, data.TripID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *Service) handleTripRejected(ctx context.Context, value []byte) error {
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
	_, err = tx.Exec(ctx, `
		DELETE FROM pending_confirmations WHERE trip_id=$1
	`, data.TripID)
	if err != nil {
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
		OriginalTripID string    `json:"original_trip_id"`
		BatchID        string    `json:"batch_id"`
		Reason         string    `json:"reason"`
		TimeoutAt      time.Time `json:"timeout_at"`
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
	now := time.Now().UTC()
	out := outbox.Event{
		ID:            uuid.NewString(),
		EventType:     "commands.trip.reassign",
		CorrelationID: data.OriginalTripID,
		Topic:         s.commandTopic,
		PartitionKey:  data.OriginalTripID,
		Payload:       value,
		OccurredAt:    now,
	}
	if err := outbox.EnqueueTx(ctx, tx, out); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
