package reassignment

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/mock"
)

type mockDB struct {
	mock.Mock
}

func (m *mockDB) Begin(ctx context.Context) (pgx.Tx, error) {
	args := m.Called(ctx)
	tx, _ := args.Get(0).(pgx.Tx)
	return tx, args.Error(1)
}

type mockTx struct {
	mock.Mock
}

func (m *mockTx) Begin(ctx context.Context) (pgx.Tx, error) {
	args := m.Called(ctx)
	tx, _ := args.Get(0).(pgx.Tx)
	return tx, args.Error(1)
}

func (m *mockTx) Commit(ctx context.Context) error {
	return m.Called(ctx).Error(0)
}

func (m *mockTx) Rollback(ctx context.Context) error {
	return m.Called(ctx).Error(0)
}

func (m *mockTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	args := m.Called(ctx, tableName, columnNames, rowSrc)
	return args.Get(0).(int64), args.Error(1)
}

func (m *mockTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	args := m.Called(ctx, b)
	return args.Get(0).(pgx.BatchResults)
}

func (m *mockTx) LargeObjects() pgx.LargeObjects {
	return pgx.LargeObjects{}
}

func (m *mockTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	args := m.Called(ctx, name, sql)
	desc, _ := args.Get(0).(*pgconn.StatementDescription)
	return desc, args.Error(1)
}

func (m *mockTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	callArgs := []any{ctx, sql}
	callArgs = append(callArgs, arguments...)
	args := m.Called(callArgs...)
	tag, _ := args.Get(0).(pgconn.CommandTag)
	return tag, args.Error(1)
}

func (m *mockTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	callArgs := []any{ctx, sql}
	callArgs = append(callArgs, args...)
	a := m.Called(callArgs...)
	rows, _ := a.Get(0).(pgx.Rows)
	return rows, a.Error(1)
}

func (m *mockTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	callArgs := []any{ctx, sql}
	callArgs = append(callArgs, args...)
	a := m.Called(callArgs...)
	r, _ := a.Get(0).(pgx.Row)
	return r
}

func (m *mockTx) Conn() *pgx.Conn { return nil }

type mockRow struct {
	values []any
	err    error
}

func (r *mockRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		if i >= len(r.values) {
			break
		}
		switch d := dest[i].(type) {
		case *string:
			if v, ok := r.values[i].(string); ok {
				*d = v
			}
		case *time.Time:
			if v, ok := r.values[i].(time.Time); ok {
				*d = v
			}
		}
	}
	return nil
}

type mockRows struct {
	rows   [][]any
	idx    int
	closed bool
	err    error
}

func (r *mockRows) Close() {
	r.closed = true
}

func (r *mockRows) Err() error {
	return r.err
}

func (r *mockRows) CommandTag() pgconn.CommandTag {
	return pgconn.CommandTag{}
}

func (r *mockRows) FieldDescriptions() []pgconn.FieldDescription {
	return nil
}

func (r *mockRows) Conn() *pgx.Conn {
	return nil
}

func (r *mockRows) RawValues() [][]byte {
	return nil
}

func (r *mockRows) Values() ([]any, error) {
	if r.idx == 0 || r.idx > len(r.rows) {
		return nil, pgx.ErrNoRows
	}
	return r.rows[r.idx-1], nil
}

func (r *mockRows) Next() bool {
	if r.closed {
		return false
	}
	if r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}

func (r *mockRows) Scan(dest ...any) error {
	if r.idx == 0 || r.idx > len(r.rows) {
		return pgx.ErrNoRows
	}
	row := r.rows[r.idx-1]
	for i := range dest {
		if i >= len(row) {
			break
		}
		switch d := dest[i].(type) {
		case *string:
			if v, ok := row[i].(string); ok {
				*d = v
			}
		}
	}
	return nil
}

func TestHandleEvent_UnknownTopic(t *testing.T) {
	s := NewService(&mockDB{}, "commands.trip.reassign", 2*time.Hour)
	err := s.HandleEvent(context.Background(), "unknown.topic", nil, []byte(`{}`))
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestHandleTripAssigned_Success(t *testing.T) {
	ctx := context.Background()
	db := &mockDB{}
	tx := &mockTx{}
	s := NewService(db, "commands.trip.reassign", 2*time.Hour)

	assignedAt := time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC)
	occurredAt := assignedAt.Add(1 * time.Second)

	value, _ := json.Marshal(map[string]any{
		"event_id":    "evt-1",
		"occurred_at": occurredAt,
		"data": map[string]any{
			"trip_id":     "trip-1",
			"batch_id":    "batch-1",
			"carrier_id":  "carrier-1",
			"assigned_at": assignedAt,
		},
	})

	db.On("Begin", ctx).Return(tx, nil)
	tx.On("Rollback", ctx).Return(nil)
	tx.On("QueryRow", ctx, mock.AnythingOfType("string"), "evt-1", occurredAt).
		Return(&mockRow{values: []any{"evt-1"}})

	expectedTimeout := assignedAt.Add(2 * time.Hour)
	tx.On(
		"Exec",
		ctx,
		mock.AnythingOfType("string"),
		"trip-1",
		"batch-1",
		"carrier-1",
		assignedAt,
		expectedTimeout,
	).Return(pgconn.CommandTag{}, nil)
	tx.On("Commit", ctx).Return(nil)

	if err := s.HandleEvent(ctx, "трипы.назначены", nil, value); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleTripAssigned_DuplicateEvent(t *testing.T) {
	ctx := context.Background()
	db := &mockDB{}
	tx := &mockTx{}
	s := NewService(db, "commands.trip.reassign", 2*time.Hour)

	assignedAt := time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC)
	occurredAt := assignedAt.Add(1 * time.Second)

	value, _ := json.Marshal(map[string]any{
		"event_id":    "evt-1",
		"occurred_at": occurredAt,
		"data": map[string]any{
			"trip_id":     "trip-1",
			"batch_id":    "batch-1",
			"carrier_id":  "carrier-1",
			"assigned_at": assignedAt,
		},
	})

	db.On("Begin", ctx).Return(tx, nil)
	tx.On("Rollback", ctx).Return(nil)
	tx.On("QueryRow", ctx, mock.AnythingOfType("string"), "evt-1", occurredAt).
		Return(&mockRow{err: pgx.ErrNoRows})

	if err := s.HandleEvent(ctx, "трипы.назначены", nil, value); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleTripConfirmed_Success(t *testing.T) {
	ctx := context.Background()
	db := &mockDB{}
	tx := &mockTx{}
	s := NewService(db, "commands.trip.reassign", 2*time.Hour)

	occurredAt := time.Date(2026, 2, 15, 10, 0, 1, 0, time.UTC)
	value, _ := json.Marshal(map[string]any{
		"event_id":    "evt-2",
		"occurred_at": occurredAt,
		"data": map[string]any{
			"trip_id": "trip-1",
		},
	})

	db.On("Begin", ctx).Return(tx, nil)
	tx.On("Rollback", ctx).Return(nil)
	tx.On("QueryRow", ctx, mock.AnythingOfType("string"), "evt-2", occurredAt).
		Return(&mockRow{values: []any{"evt-2"}})
	tx.On("Exec", ctx, mock.AnythingOfType("string"), "trip-1").Return(pgconn.CommandTag{}, nil)
	tx.On("Commit", ctx).Return(nil)

	if err := s.HandleEvent(ctx, "трипы.подтверждены", nil, value); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleTripRejected_Success(t *testing.T) {
	ctx := context.Background()
	db := &mockDB{}
	tx := &mockTx{}
	s := NewService(db, "commands.trip.reassign", 2*time.Hour)

	occurredAt := time.Date(2026, 2, 15, 10, 0, 1, 0, time.UTC)
	value, _ := json.Marshal(map[string]any{
		"event_id":    "evt-3",
		"occurred_at": occurredAt,
		"data": map[string]any{
			"trip_id": "trip-1",
		},
	})

	db.On("Begin", ctx).Return(tx, nil)
	tx.On("Rollback", ctx).Return(nil)
	tx.On("QueryRow", ctx, mock.AnythingOfType("string"), "evt-3", occurredAt).
		Return(&mockRow{values: []any{"evt-3"}})
	tx.On("Exec", ctx, mock.AnythingOfType("string"), "trip-1").Return(pgconn.CommandTag{}, nil)
	tx.On("Commit", ctx).Return(nil)

	if err := s.HandleEvent(ctx, "трипы.отклонены", nil, value); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleReassignCommand_Success(t *testing.T) {
	ctx := context.Background()
	db := &mockDB{}
	tx := &mockTx{}
	s := NewService(db, "commands.trip.reassign", 2*time.Hour)

	occurredAt := time.Date(2026, 2, 15, 10, 0, 1, 0, time.UTC)
	value, _ := json.Marshal(map[string]any{
		"event_id":    "evt-4",
		"occurred_at": occurredAt,
		"data": map[string]any{
			"original_trip_id": "trip-1",
			"batch_id":         "batch-1",
			"reason":           "timeout_2h",
			"timeout_at":       occurredAt.Add(2 * time.Hour),
		},
	})

	db.On("Begin", ctx).Return(tx, nil)
	tx.On("Rollback", ctx).Return(nil)
	tx.On("QueryRow", ctx, mock.AnythingOfType("string"), "evt-4", occurredAt).
		Return(&mockRow{values: []any{"evt-4"}})

	tx.On(
		"Exec",
		ctx,
		mock.MatchedBy(func(sql string) bool { return len(sql) > 0 }),
		mock.Anything,
		"commands.trip.reassign",
		"trip-1",
		"commands.trip.reassign",
		"trip-1",
		mock.Anything,
		mock.Anything,
	).Return(pgconn.CommandTag{}, nil)

	tx.On("Commit", ctx).Return(nil)

	if err := s.HandleEvent(ctx, "команды.переназначить", nil, value); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleReassignCommand_DuplicateEvent(t *testing.T) {
	ctx := context.Background()
	db := &mockDB{}
	tx := &mockTx{}
	s := NewService(db, "commands.trip.reassign", 2*time.Hour)

	occurredAt := time.Date(2026, 2, 15, 10, 0, 1, 0, time.UTC)
	value, _ := json.Marshal(map[string]any{
		"event_id":    "evt-4",
		"occurred_at": occurredAt,
		"data": map[string]any{
			"original_trip_id": "trip-1",
			"batch_id":         "batch-1",
			"reason":           "timeout_2h",
			"timeout_at":       occurredAt.Add(2 * time.Hour),
		},
	})

	db.On("Begin", ctx).Return(tx, nil)
	tx.On("Rollback", ctx).Return(nil)
	tx.On("QueryRow", ctx, mock.AnythingOfType("string"), "evt-4", occurredAt).
		Return(&mockRow{err: pgx.ErrNoRows})

	if err := s.HandleEvent(ctx, "команды.переназначить", nil, value); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleEvent_InvalidJSON(t *testing.T) {
	s := NewService(&mockDB{}, "commands.trip.reassign", 2*time.Hour)
	err := s.HandleEvent(context.Background(), "трипы.назначены", nil, []byte(`{`))
	if err == nil {
		t.Fatalf("expected error")
	}
	if errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("unexpected ErrNoRows: %v", err)
	}
}

func TestHandleTripAssigned_BeginError(t *testing.T) {
	ctx := context.Background()
	db := &mockDB{}
	s := NewService(db, "commands.trip.reassign", 2*time.Hour)

	assignedAt := time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC)
	occurredAt := assignedAt.Add(1 * time.Second)
	value, _ := json.Marshal(map[string]any{
		"event_id":    "evt-1",
		"occurred_at": occurredAt,
		"data": map[string]any{
			"trip_id":     "trip-1",
			"batch_id":    "batch-1",
			"carrier_id":  "carrier-1",
			"assigned_at": assignedAt,
		},
	})

	db.On("Begin", ctx).Return((pgx.Tx)(nil), errors.New("begin failed"))
	if err := s.HandleEvent(ctx, "трипы.назначены", nil, value); err == nil {
		t.Fatalf("expected error")
	}
}

func TestHandleTripAssigned_ProcessedInsertError(t *testing.T) {
	ctx := context.Background()
	db := &mockDB{}
	tx := &mockTx{}
	s := NewService(db, "commands.trip.reassign", 2*time.Hour)

	assignedAt := time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC)
	occurredAt := assignedAt.Add(1 * time.Second)
	value, _ := json.Marshal(map[string]any{
		"event_id":    "evt-1",
		"occurred_at": occurredAt,
		"data": map[string]any{
			"trip_id":     "trip-1",
			"batch_id":    "batch-1",
			"carrier_id":  "carrier-1",
			"assigned_at": assignedAt,
		},
	})

	db.On("Begin", ctx).Return(tx, nil)
	tx.On("Rollback", ctx).Return(nil)
	tx.On("QueryRow", ctx, mock.AnythingOfType("string"), "evt-1", occurredAt).
		Return(&mockRow{err: errors.New("insert failed")})

	if err := s.HandleEvent(ctx, "трипы.назначены", nil, value); err == nil {
		t.Fatalf("expected error")
	}
}

func TestHandleTripAssigned_InsertPendingError(t *testing.T) {
	ctx := context.Background()
	db := &mockDB{}
	tx := &mockTx{}
	s := NewService(db, "commands.trip.reassign", 2*time.Hour)

	assignedAt := time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC)
	occurredAt := assignedAt.Add(1 * time.Second)
	value, _ := json.Marshal(map[string]any{
		"event_id":    "evt-1",
		"occurred_at": occurredAt,
		"data": map[string]any{
			"trip_id":     "trip-1",
			"batch_id":    "batch-1",
			"carrier_id":  "carrier-1",
			"assigned_at": assignedAt,
		},
	})

	db.On("Begin", ctx).Return(tx, nil)
	tx.On("Rollback", ctx).Return(nil)
	tx.On("QueryRow", ctx, mock.AnythingOfType("string"), "evt-1", occurredAt).
		Return(&mockRow{values: []any{"evt-1"}})
	tx.On(
		"Exec",
		ctx,
		mock.AnythingOfType("string"),
		"trip-1",
		"batch-1",
		"carrier-1",
		assignedAt,
		assignedAt.Add(2*time.Hour),
	).Return(pgconn.CommandTag{}, errors.New("exec failed"))

	if err := s.HandleEvent(ctx, "трипы.назначены", nil, value); err == nil {
		t.Fatalf("expected error")
	}
}

func TestHandleTripAssigned_CommitError(t *testing.T) {
	ctx := context.Background()
	db := &mockDB{}
	tx := &mockTx{}
	s := NewService(db, "commands.trip.reassign", 2*time.Hour)

	assignedAt := time.Date(2026, 2, 15, 10, 0, 0, 0, time.UTC)
	occurredAt := assignedAt.Add(1 * time.Second)
	value, _ := json.Marshal(map[string]any{
		"event_id":    "evt-1",
		"occurred_at": occurredAt,
		"data": map[string]any{
			"trip_id":     "trip-1",
			"batch_id":    "batch-1",
			"carrier_id":  "carrier-1",
			"assigned_at": assignedAt,
		},
	})

	db.On("Begin", ctx).Return(tx, nil)
	tx.On("Rollback", ctx).Return(nil)
	tx.On("QueryRow", ctx, mock.AnythingOfType("string"), "evt-1", occurredAt).
		Return(&mockRow{values: []any{"evt-1"}})
	tx.On(
		"Exec",
		ctx,
		mock.AnythingOfType("string"),
		"trip-1",
		"batch-1",
		"carrier-1",
		assignedAt,
		assignedAt.Add(2*time.Hour),
	).Return(pgconn.CommandTag{}, nil)
	tx.On("Commit", ctx).Return(errors.New("commit failed"))

	if err := s.HandleEvent(ctx, "трипы.назначены", nil, value); err == nil {
		t.Fatalf("expected error")
	}
}

func TestHandleTripConfirmed_DuplicateEvent(t *testing.T) {
	ctx := context.Background()
	db := &mockDB{}
	tx := &mockTx{}
	s := NewService(db, "commands.trip.reassign", 2*time.Hour)

	occurredAt := time.Date(2026, 2, 15, 10, 0, 1, 0, time.UTC)
	value, _ := json.Marshal(map[string]any{
		"event_id":    "evt-2",
		"occurred_at": occurredAt,
		"data": map[string]any{
			"trip_id": "trip-1",
		},
	})

	db.On("Begin", ctx).Return(tx, nil)
	tx.On("Rollback", ctx).Return(nil)
	tx.On("QueryRow", ctx, mock.AnythingOfType("string"), "evt-2", occurredAt).
		Return(&mockRow{err: pgx.ErrNoRows})

	if err := s.HandleEvent(ctx, "трипы.подтверждены", nil, value); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestHandleReassignCommand_EnqueueError(t *testing.T) {
	ctx := context.Background()
	db := &mockDB{}
	tx := &mockTx{}
	s := NewService(db, "commands.trip.reassign", 2*time.Hour)

	occurredAt := time.Date(2026, 2, 15, 10, 0, 1, 0, time.UTC)
	value, _ := json.Marshal(map[string]any{
		"event_id":    "evt-4",
		"occurred_at": occurredAt,
		"data": map[string]any{
			"original_trip_id": "trip-1",
			"batch_id":         "batch-1",
			"reason":           "timeout_2h",
			"timeout_at":       occurredAt.Add(2 * time.Hour),
		},
	})

	db.On("Begin", ctx).Return(tx, nil)
	tx.On("Rollback", ctx).Return(nil)
	tx.On("QueryRow", ctx, mock.AnythingOfType("string"), "evt-4", occurredAt).
		Return(&mockRow{values: []any{"evt-4"}})
	tx.On(
		"Exec",
		ctx,
		mock.MatchedBy(func(sql string) bool { return len(sql) > 0 }),
		mock.Anything,
		"commands.trip.reassign",
		"trip-1",
		"commands.trip.reassign",
		"trip-1",
		mock.Anything,
		mock.Anything,
	).Return(pgconn.CommandTag{}, errors.New("enqueue failed"))

	if err := s.HandleEvent(ctx, "команды.переназначить", nil, value); err == nil {
		t.Fatalf("expected error")
	}
}

func TestWorkerTick_EnqueuesAndMarksReassigned(t *testing.T) {
	ctx := context.Background()
	db := &mockDB{}
	tx := &mockTx{}
	w := NewWorker(db, 1*time.Second, "commands.trip.reassign")

	db.On("Begin", ctx).Return(tx, nil)
	tx.On("Rollback", ctx).Return(nil)

	rows := &mockRows{
		rows: [][]any{
			{"trip-1", "batch-1", "carrier-1"},
			{"trip-2", "batch-2", "carrier-2"},
		},
	}
	tx.On("Query", ctx, mock.MatchedBy(func(sql string) bool { return strings.Contains(sql, "FROM pending_confirmations") })).Return(rows, nil)

	tx.On(
		"Exec",
		ctx,
		mock.MatchedBy(func(sql string) bool { return strings.Contains(sql, "INSERT INTO outbox_events") }),
		mock.Anything,
		"команды.переназначить",
		mock.Anything,
		"commands.trip.reassign",
		mock.Anything,
		mock.Anything,
		mock.Anything,
	).Return(pgconn.CommandTag{}, nil)

	tx.On(
		"Exec",
		ctx,
		mock.MatchedBy(func(sql string) bool { return strings.Contains(sql, "UPDATE pending_confirmations") }),
		mock.Anything,
	).Return(pgconn.CommandTag{}, nil)

	tx.On("Commit", ctx).Return(nil)

	w.tick(ctx)
}

func TestWorkerTick_QueryError(t *testing.T) {
	ctx := context.Background()
	db := &mockDB{}
	tx := &mockTx{}
	w := NewWorker(db, 1*time.Second, "commands.trip.reassign")

	db.On("Begin", ctx).Return(tx, nil)
	tx.On("Rollback", ctx).Return(nil)
	tx.On("Query", ctx, mock.AnythingOfType("string")).Return((pgx.Rows)(nil), errors.New("query failed"))

	w.tick(ctx)
}

func TestWorkerTick_EnqueueError(t *testing.T) {
	ctx := context.Background()
	db := &mockDB{}
	tx := &mockTx{}
	w := NewWorker(db, 1*time.Second, "commands.trip.reassign")

	db.On("Begin", ctx).Return(tx, nil)
	tx.On("Rollback", ctx).Return(nil)

	rows := &mockRows{
		rows: [][]any{
			{"trip-1", "batch-1", "carrier-1"},
		},
	}
	tx.On("Query", ctx, mock.MatchedBy(func(sql string) bool { return strings.Contains(sql, "FROM pending_confirmations") })).Return(rows, nil)

	tx.On(
		"Exec",
		ctx,
		mock.MatchedBy(func(sql string) bool { return strings.Contains(sql, "INSERT INTO outbox_events") }),
		mock.Anything,
		"команды.переназначить",
		mock.Anything,
		"commands.trip.reassign",
		mock.Anything,
		mock.Anything,
		mock.Anything,
	).Return(pgconn.CommandTag{}, errors.New("enqueue failed"))

	// even if enqueue fails, worker continues; commit will still be attempted
	tx.On("Commit", ctx).Return(nil)

	w.tick(ctx)
}

func TestWorkerTick_CommitError(t *testing.T) {
	ctx := context.Background()
	db := &mockDB{}
	tx := &mockTx{}
	w := NewWorker(db, 1*time.Second, "commands.trip.reassign")

	db.On("Begin", ctx).Return(tx, nil)
	tx.On("Rollback", ctx).Return(nil)

	rows := &mockRows{
		rows: [][]any{
			{"trip-1", "batch-1", "carrier-1"},
		},
	}
	tx.On("Query", ctx, mock.MatchedBy(func(sql string) bool { return strings.Contains(sql, "FROM pending_confirmations") })).Return(rows, nil)

	tx.On(
		"Exec",
		ctx,
		mock.MatchedBy(func(sql string) bool { return strings.Contains(sql, "INSERT INTO outbox_events") }),
		mock.Anything,
		"команды.переназначить",
		mock.Anything,
		"commands.trip.reassign",
		mock.Anything,
		mock.Anything,
		mock.Anything,
	).Return(pgconn.CommandTag{}, nil)

	tx.On(
		"Exec",
		ctx,
		mock.MatchedBy(func(sql string) bool { return strings.Contains(sql, "UPDATE pending_confirmations") }),
		mock.Anything,
	).Return(pgconn.CommandTag{}, nil)

	tx.On("Commit", ctx).Return(errors.New("commit failed"))

	w.tick(ctx)
}
