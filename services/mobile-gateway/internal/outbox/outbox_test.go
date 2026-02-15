package outbox

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type mockDB struct {
	execCalls []string
	execArgs  [][]any
	qRows     pgx.Rows
	qErr      error
}

func (m *mockDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	m.execCalls = append(m.execCalls, sql)
	m.execArgs = append(m.execArgs, args)
	return pgconn.CommandTag{}, nil
}
func (m *mockDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return m.qRows, m.qErr
}
func (m *mockDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return &struct{ pgx.Row }{}
}

type mockRows struct {
	cur  int
	data [][]any
}

func (r *mockRows) Next() bool {
	if r.cur >= len(r.data) {
		return false
	}
	r.cur++
	return true
}
func (r *mockRows) Scan(dest ...any) error {
	row := r.data[r.cur-1]
	for i := range dest {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(row[i]))
	}
	return nil
}
func (r *mockRows) Close() {}
func (r *mockRows) Err() error { return nil }
func (r *mockRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *mockRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *mockRows) RawValues() [][]byte                          { return nil }
func (r *mockRows) Values() ([]any, error)                       { return nil, nil }
func (r *mockRows) Conn() *pgx.Conn                              { return nil }

type mockTx struct{ execs []string }

func (t *mockTx) Begin(ctx context.Context) (pgx.Tx, error) { return t, nil }
func (t *mockTx) Commit(ctx context.Context) error          { return nil }
func (t *mockTx) Rollback(ctx context.Context) error        { return nil }
func (t *mockTx) Conn() *pgx.Conn                           { return nil }
func (t *mockTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (t *mockTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.execs = append(t.execs, sql)
	return pgconn.CommandTag{}, nil
}
func (t *mockTx) LargeObjects() pgx.LargeObjects { var lo pgx.LargeObjects; return lo }
func (t *mockTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (t *mockTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return &mockRows{}, nil
}
func (t *mockTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row { return &struct{ pgx.Row }{} }
func (t *mockTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults { var br pgx.BatchResults; return br }

func TestMarkPublished_InsertsAndUpdates(t *testing.T) {
	db := &mockDB{}
	err := markPublished(context.Background(), db, "id1", "et", "corr")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(db.execCalls) < 2 {
		t.Fatalf("expected two execs")
	}
}

func TestMarkFailed_Updates(t *testing.T) {
	db := &mockDB{}
	err := markFailed(context.Background(), db, "id1", "oops")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(db.execCalls) == 0 {
		t.Fatalf("expected update")
	}
}

func TestFetchPending_ReadsRows(t *testing.T) {
	db := &mockDB{
		qRows: &mockRows{
			data: [][]any{
				{"id1", "et", "ev", "corr", "top", "part", []byte(`{}`), time.Now().UTC()},
			},
		},
	}
	events, err := fetchPending(context.Background(), db, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event")
	}
}

func TestEnsureSchema_Executes(t *testing.T) {
	db := &mockDB{}
	err := EnsureSchema(db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(db.execCalls) < 2 {
		t.Fatalf("expected schema execs")
	}
}
