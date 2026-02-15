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

func TestMarkPublished_UpdatesAndInserts(t *testing.T) {
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
				{"id1", "et", "c1", "topic", "pk", []byte(`{}`), time.Now().UTC()},
			},
		},
	}
	evts, err := fetchPending(context.Background(), db, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(evts) != 1 {
		t.Fatalf("expected 1 event")
	}
}
