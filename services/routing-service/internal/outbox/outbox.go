package outbox

import (
	"context"
	"encoding/json"
	"time"

	"bel-parcel/services/routing-service/internal/metrics"

	"github.com/google/uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Pgx interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Begin(ctx context.Context) (pgx.Tx, error)
}

type Producer interface {
	Publish(ctx context.Context, topic string, key string, msg interface{}) error
}

type Event struct {
	ID            string
	EventType     string
	CorrelationID string
	Topic         string
	PartitionKey  string
	Payload       []byte
	OccurredAt    time.Time
	Attempts      int
}

func EnsureSchema(db Pgx) error {
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
	if _, err := db.Exec(ctx, `
		ALTER TABLE outbox_events 
		ADD COLUMN IF NOT EXISTS next_attempt_time TIMESTAMPTZ NOT NULL DEFAULT NOW()
	`); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `
		CREATE INDEX IF NOT EXISTS idx_outbox_next_attempt 
		ON outbox_events(next_attempt_time) 
		WHERE status IN ('pending','error')
	`); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS published_events (
			event_type TEXT NOT NULL,
			correlation_id TEXT NOT NULL,
			occurred_at TIMESTAMPTZ NOT NULL,
			PRIMARY KEY (event_type, correlation_id)
		)
	`); err != nil {
		return err
	}
	_, err := db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS dead_letter_queue (
			id UUID PRIMARY KEY,
			source_id UUID NOT NULL,
			event_type TEXT NOT NULL,
			topic TEXT NOT NULL,
			partition_key TEXT NOT NULL,
			payload JSONB NOT NULL,
			last_error TEXT,
			attempts INT NOT NULL,
			inserted_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			process_status TEXT NOT NULL DEFAULT 'new'
		)
	`)
	return err
}

func EnqueueTx(ctx context.Context, tx pgx.Tx, evt Event) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO outbox_events(id, event_type, correlation_id, topic, partition_key, payload, occurred_at, status, attempts, next_attempt_time)
		VALUES ($1, $2, $3, $4, $5, $6, $7, 'pending', 0, NOW())
	`, evt.ID, evt.EventType, evt.CorrelationID, evt.Topic, evt.PartitionKey, evt.Payload, evt.OccurredAt)
	return err
}

func fetchPending(ctx context.Context, db Pgx, limit int) ([]Event, error) {
	rows, err := db.Query(ctx, `
		SELECT id, event_type, correlation_id, topic, partition_key, payload, occurred_at, attempts
		FROM outbox_events
		WHERE status IN ('pending','error') AND next_attempt_time <= NOW()
		ORDER BY next_attempt_time
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var res []Event
	for rows.Next() {
		var e Event
		var payload []byte
		_ = rows.Scan(&e.ID, &e.EventType, &e.CorrelationID, &e.Topic, &e.PartitionKey, &payload, &e.OccurredAt, &e.Attempts)
		e.Payload = payload
		res = append(res, e)
	}
	return res, rows.Err()
}

func markPublished(ctx context.Context, db Pgx, id string, eventType, correlationID string) error {
	_, err := db.Exec(ctx, `
		UPDATE outbox_events SET status='published' WHERE id=$1
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

func markFailed(ctx context.Context, db Pgx, id string, lastError string) error {
	tx, err := db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var attempts int
	var topic string
	if err := tx.QueryRow(ctx, `SELECT attempts, topic FROM outbox_events WHERE id=$1 FOR UPDATE`, id).Scan(&attempts, &topic); err != nil {
		return err
	}
	newAttempts := attempts + 1
	if newAttempts >= 10 {
		metrics.IncOutboxError(topic)
		metrics.ObserveRetryAttempt(newAttempts)
		dlqID := uuid.NewString()
		if _, err := tx.Exec(ctx, `
			INSERT INTO dead_letter_queue(id, source_id, event_type, topic, partition_key, payload, last_error, attempts)
			SELECT $4, id, event_type, topic, partition_key, payload, $2, $3
			FROM outbox_events
			WHERE id=$1
		`, id, lastError, newAttempts, dlqID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM outbox_events WHERE id=$1`, id); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	var interval string
	switch {
	case newAttempts <= 3:
		interval = "1 second"
	case newAttempts <= 6:
		interval = "5 seconds"
	default:
		interval = "30 seconds"
	}
	if _, err := tx.Exec(ctx, `
		UPDATE outbox_events 
		SET status='error', attempts=$2, last_error=$3, next_attempt_time=NOW() + $4::interval
		WHERE id=$1
	`, id, newAttempts, lastError, interval); err != nil {
		return err
	}
	metrics.IncOutboxError(topic)
	metrics.ObserveRetryAttempt(newAttempts)
	return tx.Commit(ctx)
}

func StartPublisher(ctx context.Context, db Pgx, producer Producer) {
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
