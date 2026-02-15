package outbox

import (
	"context"
	"encoding/json"
	"time"

	"bel-parcel/services/tracking-service/internal/websocket"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
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

func EnsureSchema(db *pgxpool.Pool) error {
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
			occurred_at TIMESTAMPTZ NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			attempts INT NOT NULL DEFAULT 0,
			last_error TEXT,
			next_attempt_time TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(event_type, correlation_id)
		)
	`); err != nil {
		return err
	}
	if _, err := db.Exec(ctx, `CREATE INDEX IF NOT EXISTS idx_outbox_next_attempt ON outbox_events(next_attempt_time)`); err != nil {
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
			process_status TEXT NOT NULL DEFAULT 'pending'
		)
	`)
	return err
}

func EnqueueTx(ctx context.Context, tx pgx.Tx, evt Event) error {
	_, err := tx.Exec(ctx, `
		INSERT INTO outbox_events(id, event_type, correlation_id, topic, partition_key, payload, occurred_at, status, attempts, next_attempt_time)
		VALUES ($1,$2,$3,$4,$5,$6,$7,'pending',0,NOW())
	`, evt.ID, evt.EventType, evt.CorrelationID, evt.Topic, evt.PartitionKey, evt.Payload, evt.OccurredAt)
	return err
}

func fetchPending(ctx context.Context, db *pgxpool.Pool, limit int) ([]Event, error) {
	rows, err := db.Query(ctx, `
		SELECT id, event_type, correlation_id, topic, partition_key, payload, occurred_at
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
		_ = rows.Scan(&e.ID, &e.EventType, &e.CorrelationID, &e.Topic, &e.PartitionKey, &payload, &e.OccurredAt)
		e.Payload = payload
		res = append(res, e)
	}
	return res, rows.Err()
}

func markPublished(ctx context.Context, db *pgxpool.Pool, id string, eventType, correlationID string) error {
	if _, err := db.Exec(ctx, `UPDATE outbox_events SET status='published', attempts=attempts+1 WHERE id=$1`, id); err != nil {
		return err
	}
	_, err := db.Exec(ctx, `
		INSERT INTO published_events(event_type, correlation_id, occurred_at)
		VALUES ($1,$2,NOW())
		ON CONFLICT (event_type, correlation_id) DO NOTHING
	`, eventType, correlationID)
	return err
}

func markFailed(ctx context.Context, db *pgxpool.Pool, id string, lastError string) error {
	var attempts int
	if err := db.QueryRow(ctx, `SELECT attempts FROM outbox_events WHERE id=$1`, id).Scan(&attempts); err != nil {
		return err
	}
	var nextSeconds int
	switch {
	case attempts < 3:
		nextSeconds = 1
	case attempts < 6:
		nextSeconds = 5
	case attempts < 9:
		nextSeconds = 30
	default:
		nextSeconds = 0
	}
	if attempts >= 9 {
		if _, err := db.Exec(ctx, `
			INSERT INTO dead_letter_queue(id, source_id, event_type, topic, partition_key, payload, last_error, attempts)
			SELECT $1, id, event_type, topic, partition_key, payload, $2, attempts+1
			FROM outbox_events WHERE id=$3
		`, uuid.NewString(), lastError, id); err != nil {
			return err
		}
		_, err := db.Exec(ctx, `UPDATE outbox_events SET status='published', attempts=attempts+1, last_error=$2 WHERE id=$1`, id, lastError)
		return err
	}
	_, err := db.Exec(ctx, `
		UPDATE outbox_events
		SET attempts=attempts+1, last_error=$2, status='error', next_attempt_time=NOW() + ($3 * INTERVAL '1 second')
		WHERE id=$1
	`, id, lastError, nextSeconds)
	return err
}

func StartPublisher(ctx context.Context, db *pgxpool.Pool, hub *websocket.Hub) {
	t := time.NewTicker(1 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, 5*time.Second)
			events, err := fetchPending(pctx, db, 100)
			cancel()
			if err != nil || len(events) == 0 {
				continue
			}
			for _, e := range events {
				var msg map[string]interface{}
				_ = json.Unmarshal(e.Payload, &msg)
				if err := hub.BroadcastJSON(msg); err != nil {
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
