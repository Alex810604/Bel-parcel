package outbox

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type mockRow struct {
	attempts int
	topic    string
	err      error
}

func (r *mockRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 2 {
		return fmt.Errorf("expected 2 dests, got %d", len(dest))
	}
	if a, ok := dest[0].(*int); ok {
		*a = r.attempts
	} else {
		return fmt.Errorf("dest[0] not *int")
	}
	if s, ok := dest[1].(*string); ok {
		*s = r.topic
	} else {
		return fmt.Errorf("dest[1] not *string")
	}
	return nil
}

type mockRows struct {
	items []Event
	idx   int
	err   error
}

func (m *mockRows) Next() bool {
	if m.idx < len(m.items) {
		return true
	}
	return false
}

func (m *mockRows) Scan(dest ...any) error {
	if m.err != nil {
		return m.err
	}
	e := m.items[m.idx]
	m.idx++
	if len(dest) != 8 {
		return fmt.Errorf("expected 8 dests, got %d", len(dest))
	}
	if id, ok := dest[0].(*string); ok {
		*id = e.ID
	} else {
		return fmt.Errorf("dest[0] not *string")
	}
	if et, ok := dest[1].(*string); ok {
		*et = e.EventType
	} else {
		return fmt.Errorf("dest[1] not *string")
	}
	if corr, ok := dest[2].(*string); ok {
		*corr = e.CorrelationID
	} else {
		return fmt.Errorf("dest[2] not *string")
	}
	if topic, ok := dest[3].(*string); ok {
		*topic = e.Topic
	} else {
		return fmt.Errorf("dest[3] not *string")
	}
	if pk, ok := dest[4].(*string); ok {
		*pk = e.PartitionKey
	} else {
		return fmt.Errorf("dest[4] not *string")
	}
	if payload, ok := dest[5].(*[]byte); ok {
		*payload = e.Payload
	} else {
		return fmt.Errorf("dest[5] not *[]byte")
	}
	if occurred, ok := dest[6].(*time.Time); ok {
		*occurred = e.OccurredAt
	} else {
		return fmt.Errorf("dest[6] not *time.Time")
	}
	if attempts, ok := dest[7].(*int); ok {
		*attempts = e.Attempts
	} else {
		return fmt.Errorf("dest[7] not *int")
	}
	return nil
}

func (m *mockRows) Err() error                                   { return m.err }
func (m *mockRows) Close()                                       {}
func (m *mockRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (m *mockRows) Conn() *pgx.Conn                              { return nil }
func (m *mockRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (m *mockRows) RawValues() [][]byte                          { return nil }
func (m *mockRows) Values() ([]any, error)                       { return nil, nil }

type mockTx struct {
	execCalls   []string
	execArgs    [][]any
	retAttempts int
	retTopic    string
	committed   bool
	commitErr   error
}

func (tx *mockTx) Begin(ctx context.Context) (pgx.Tx, error) { return tx, nil }
func (tx *mockTx) Commit(ctx context.Context) error {
	tx.committed = true
	return tx.commitErr
}
func (tx *mockTx) Conn() *pgx.Conn { return nil }
func (tx *mockTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (tx *mockTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	tx.execCalls = append(tx.execCalls, sql)
	tx.execArgs = append(tx.execArgs, args)
	return pgconn.CommandTag{}, nil
}
func (tx *mockTx) LargeObjects() pgx.LargeObjects { return pgx.LargeObjects{} }
func (tx *mockTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (tx *mockTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}
func (tx *mockTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return &mockRow{attempts: tx.retAttempts, topic: tx.retTopic}
}
func (tx *mockTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults { return nil }
func (tx *mockTx) Rollback(ctx context.Context) error                           { return nil }

type mockDB struct {
	tx        *mockTx
	rows      *mockRows
	execCalls []string
	execArgs  [][]any
}

func (db *mockDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	db.execCalls = append(db.execCalls, sql)
	db.execArgs = append(db.execArgs, args)
	return pgconn.CommandTag{}, nil
}
func (db *mockDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return db.rows, nil
}
func (db *mockDB) Begin(ctx context.Context) (pgx.Tx, error) {
	return db.tx, nil
}

func TestEnsureSchemaExecutesStatements(t *testing.T) {
	db := &mockDB{tx: &mockTx{}}
	if err := EnsureSchema(db); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(db.execCalls) < 4 {
		t.Fatalf("expected >=4 schema statements, got %d", len(db.execCalls))
	}
}

func TestFetchPendingScansRows(t *testing.T) {
	now := time.Now().UTC()
	items := []Event{
		{
			ID:            uuid.NewString(),
			EventType:     "events.batch_picked_up",
			CorrelationID: "c1",
			Topic:         "picked.up",
			PartitionKey:  "k1",
			Payload:       []byte(`{"a":1}`),
			OccurredAt:    now,
			Attempts:      0,
		},
		{
			ID:            uuid.NewString(),
			EventType:     "events.carrier_location",
			CorrelationID: "c2",
			Topic:         "carrier.location",
			PartitionKey:  "carrier-1",
			Payload:       []byte(`{"lat":1,"lng":2}`),
			OccurredAt:    now.Add(1 * time.Second),
			Attempts:      1,
		},
	}
	db := &mockDB{
		rows: &mockRows{items: items},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	evts, err := fetchPending(ctx, db, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(evts) != 2 {
		t.Fatalf("expected 2 events, got %d", len(evts))
	}
	if string(evts[0].Payload) != `{"a":1}` || evts[1].Attempts != 1 {
		t.Fatalf("unexpected payload/attempts in scanned events")
	}
}

func TestMarkPublishedUpdatesStatusAndPublishesRecord(t *testing.T) {
	db := &mockDB{tx: &mockTx{}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := markPublished(ctx, db, "id-1", "events.batch_picked_up", "corr-1"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(db.execCalls) != 2 {
		t.Fatalf("expected 2 exec calls, got %d", len(db.execCalls))
	}
}

func TestMarkFailedMovesToDLQOnMaxAttempts(t *testing.T) {
	tx := &mockTx{retAttempts: 9, retTopic: "picked.up"}
	db := &mockDB{tx: tx}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := markFailed(ctx, db, "id-1", "boom"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !tx.committed {
		t.Fatalf("expected commit to be called")
	}
	if len(tx.execCalls) != 2 {
		t.Fatalf("expected 2 tx exec calls (insert DLQ + delete outbox), got %d", len(tx.execCalls))
	}
}

func TestMarkFailedSetsRetryIntervalBuckets(t *testing.T) {
	// attempts=0 => newAttempts=1 => 1 second
	tx := &mockTx{retAttempts: 0, retTopic: "carrier.location"}
	db := &mockDB{tx: tx}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := markFailed(ctx, db, "id-2", "temporary error"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !tx.committed {
		t.Fatalf("expected commit")
	}
	if len(tx.execArgs) != 1 {
		t.Fatalf("expected 1 update call, got %d", len(tx.execArgs))
	}
	// args: id, newAttempts, lastError, interval
	if interval, ok := tx.execArgs[0][3].(string); !ok || interval != "1 second" {
		t.Fatalf("expected interval '1 second', got %#v", tx.execArgs[0][3])
	}
}

type stubProducer struct {
	retErrByKey map[string]error
	calls       int
}

func (p *stubProducer) Publish(ctx context.Context, topic string, key string, msg interface{}) error {
	p.calls++
	if err, ok := p.retErrByKey[key]; ok {
		return err
	}
	return nil
}

func TestStartPublisherProcessesSuccessAndError(t *testing.T) {
	now := time.Now().UTC()
	items := []Event{
		{
			ID:            "id-success",
			EventType:     "events.batch_picked_up",
			CorrelationID: "corr-success",
			Topic:         "picked.up",
			PartitionKey:  "k-success",
			Payload:       []byte(`{"x":1}`),
			OccurredAt:    now,
			Attempts:      0,
		},
		{
			ID:            "id-error",
			EventType:     "events.carrier_location",
			CorrelationID: "corr-error",
			Topic:         "carrier.location",
			PartitionKey:  "k-error",
			Payload:       []byte(`{"lat":1,"lng":2}`),
			OccurredAt:    now,
			Attempts:      0,
		},
	}
	tx := &mockTx{retAttempts: 0, retTopic: "carrier.location"}
	db := &mockDB{
		tx:   tx,
		rows: &mockRows{items: items},
	}
	prod := &stubProducer{
		retErrByKey: map[string]error{
			"k-error": fmt.Errorf("publish failed"),
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go StartPublisher(ctx, db, prod)
	time.Sleep(1200 * time.Millisecond)
	cancel()
	time.Sleep(100 * time.Millisecond)
	if prod.calls < 2 {
		t.Fatalf("expected at least 2 publish calls, got %d", prod.calls)
	}
	if len(db.execCalls) < 2 {
		t.Fatalf("expected markPublished to execute 2 statements, got %d", len(db.execCalls))
	}
	if len(tx.execArgs) == 0 {
		t.Fatalf("expected markFailed to execute retry update")
	}
}
