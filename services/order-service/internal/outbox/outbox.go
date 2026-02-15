package outbox

import (
	"context"
	"encoding/json"
	"time"

	"bel-parcel/services/order-service/internal/infra/kafka"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Event struct {
	ID            string
	EventType     string
	CorrelationID string
	Topic         string
	PartitionKey  string
	Payload       []byte
	OccurredAt    time.Time
}

type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func EnsureSchema(db DB) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS outbox_events (
			id UUID PRIMARY KEY,
			event_type TEXT NOT NULL,
			correlation_id TEXT NOT NULL,
			topic TEXT NOT NULL,
			partition_key TEXT NOT NULL,
			payload JSONB NOT NULL,
			headers JSONB,
			occurred_at TIMESTAMPTZ NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			attempts INT NOT NULL DEFAULT 0,
			last_error TEXT,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(event_type, correlation_id)
		)
	`); err != nil {
		return err
	}
	_, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS published_events (
			event_type TEXT NOT NULL,
			correlation_id TEXT NOT NULL,
			occurred_at TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (event_type, correlation_id)
		)
	`)
	return err
}

func EnqueueTx(ctx context.Context, tx pgx.Tx, evt Event) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO outbox_events(id, event_type, correlation_id, topic, partition_key, payload, occurred_at, status, attempts)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending', 0)
	`, evt.ID, evt.EventType, evt.CorrelationID, evt.Topic, evt.PartitionKey, evt.Payload, evt.OccurredAt)
	return err
}

func fetchPending(ctx context.Context, db DB, limit int) ([]Event, error) {
	rows, err := db.Query(ctx, `
		SELECT id, event_type, correlation_id, topic, partition_key, payload, occurred_at
		FROM outbox_events
		WHERE status='pending'
		ORDER BY occurred_at
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []Event
	for rows.Next() {
		var e Event
		var payload []byte
		_ = rows.Scan(&e.ID, &e.EventType, &e.CorrelationID, &e.Topic, &e.PartitionKey, &payload, &e.OccurredAt)
		e.Payload = payload
		res = append(res, e)
	}
	return res, rows.Err()
}

func markPublished(ctx context.Context, db DB, id string, eventType, correlationID string) error {
	_, err := db.Exec(ctx, `
		UPDATE outbox_events SET status='published', attempts=attempts+1 WHERE id=$1
	`, id)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `
		INSERT INTO published_events(event_type, correlation_id, occurred_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (event_type, correlation_id) DO NOTHING
	`, eventType, correlationID)
	return err
}

func markFailed(ctx context.Context, db DB, id string, lastError string) error {
	_, err := db.Exec(ctx, `
		UPDATE outbox_events SET attempts=attempts+1, last_error=$2 WHERE id=$1
	`, id, lastError)
	return err
}

func StartPublisher(ctx context.Context, db DB, producer *kafka.Producer) {
	t := time.NewTicker(1 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			events, err := fetchPending(pctx, db, 50)
			cancel()
			if err != nil || len(events) == 0 {
				continue
			}
			for _, e := range events {
				var payload map[string]interface{}
				_ = json.Unmarshal(e.Payload, &payload)
				pubctx, pcancel := context.WithTimeout(ctx, 5*time.Second)
				err := producer.Publish(pubctx, e.Topic, e.PartitionKey, payload)
				pcancel()
				if err != nil {
					fctx, fcancel := context.WithTimeout(ctx, 5*time.Second)
					_ = markFailed(fctx, db, e.ID, err.Error())
					fcancel()
					continue
				}
				mctx, mcancel := context.WithTimeout(ctx, 5*time.Second)
				_ = markPublished(mctx, db, e.ID, e.EventType, e.CorrelationID)
				mcancel()
			}
		}
	}
}
