package reassignment

import (
	"context"
	"encoding/json"
	"time"

	"bel-parcel/services/reassignment-service/internal/metrics"
	"bel-parcel/services/reassignment-service/internal/outbox"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type WorkerDB interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

type Worker struct {
	db           WorkerDB
	interval     time.Duration
	commandTopic string
}

func NewWorker(db WorkerDB, interval time.Duration, commandTopic string) *Worker {
	return &Worker{db: db, interval: interval, commandTopic: commandTopic}
}

func (w *Worker) Start(ctx context.Context) {
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	tx, err := w.db.Begin(ctx)
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		SELECT trip_id, batch_id, carrier_id
		FROM pending_confirmations
		WHERE timeout_at <= NOW() AND status='pending'
		LIMIT 50
		FOR UPDATE SKIP LOCKED
	`)
	if err != nil {
		return
	}
	defer rows.Close()
	now := time.Now().UTC()
	for rows.Next() {
		var tripID, batchID, carrierID string
		if err := rows.Scan(&tripID, &batchID, &carrierID); err != nil {
			continue
		}
		metrics.ConfirmationTimeoutsTotal.Inc()
		envelope := map[string]interface{}{
			"event_id":       uuid.NewString(),
			"event_type":     "команды.переназначить",
			"occurred_at":    now,
			"correlation_id": tripID,
			"data": map[string]interface{}{
				"original_trip_id": tripID,
				"batch_id":         batchID,
				"reason":           "timeout_2h",
			},
		}
		payload, err := json.Marshal(envelope)
		if err != nil {
			continue
		}
		evt := outbox.Event{
			ID:            uuid.NewString(),
			EventType:     "команды.переназначить",
			CorrelationID: tripID,
			Topic:         w.commandTopic,
			PartitionKey:  tripID,
			Payload:       payload,
			OccurredAt:    now,
		}
		if err := outbox.EnqueueTx(ctx, tx, evt); err != nil {
			continue
		}
		metrics.ReassignmentsTotal.Inc()
		_, _ = tx.Exec(ctx, `
			UPDATE pending_confirmations SET status='reassigned' WHERE trip_id=$1
		`, tripID)
	}
	_ = tx.Commit(ctx)
}
