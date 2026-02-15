package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type stubDB struct{}

func (s *stubDB) Begin(ctx context.Context) (pgx.Tx, error) { return nil, nil }
func (s *stubDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (s *stubDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) { return nil, nil }

type stubRow struct {
	val string
}

func (r stubRow) Scan(dest ...any) error {
	if len(dest) > 0 {
		*(dest[0].(*string)) = r.val
	}
	return nil
}

func (s *stubDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return stubRow{val: "event_id"}
}
func (s *stubDB) Ping(ctx context.Context) error { return nil }

func TestHandleKafkaEvent_PickedUp_Success(t *testing.T) {
	db := &stubDB{}
	svc := NewOrderService(db, nil, "topic")
	envelope := map[string]interface{}{
		"event_id":       "e1",
		"event_type":     "events.batch_picked_up",
		"occurred_at":    time.Now(),
		"correlation_id": "batch1",
		"data":           map[string]interface{}{},
	}
	body, _ := json.Marshal(envelope)
	err := svc.HandleKafkaEvent(context.Background(), "events.batch_picked_up", []byte("batch1"), body)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleKafkaEvent_InvalidJSON(t *testing.T) {
	db := &stubDB{}
	svc := NewOrderService(db, nil, "topic")
	err := svc.HandleKafkaEvent(context.Background(), "events.batch_picked_up", []byte("batch1"), []byte("{"))
	if err == nil {
		t.Fatalf("expected error")
	}
}
