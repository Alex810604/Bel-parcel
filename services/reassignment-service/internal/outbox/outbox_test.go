package outbox

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type mockRow struct {
	vals []any
	err  error
}

func (r *mockRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(r.vals[i]))
	}
	return nil
}

type mockRows struct {
	cur  int
	data [][]any
	err  error
}

func (r *mockRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *mockRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *mockRows) RawValues() [][]byte                          { return nil }
func (r *mockRows) Conn() *pgx.Conn                              { return nil }
func (r *mockRows) Values() ([]any, error) {
	if r.cur == 0 || r.cur > len(r.data) {
		return nil, errors.New("no current row")
	}
	return r.data[r.cur-1], nil
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

func (r *mockRows) Close()     {}
func (r *mockRows) Err() error { return r.err }

type mockDB struct {
	execCalls    []string
	execArgs     [][]any
	querySQL     string
	queryArgs    []any
	queryRows    pgx.Rows
	queryErr     error
	queryRowVals []any
	queryRowErr  error
}

func (m *mockDB) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	m.execCalls = append(m.execCalls, sql)
	m.execArgs = append(m.execArgs, args)
	return pgconn.CommandTag{}, nil
}

func (m *mockDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	m.querySQL = sql
	m.queryArgs = args
	return m.queryRows, m.queryErr
}

func (m *mockDB) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return &mockRow{vals: m.queryRowVals, err: m.queryRowErr}
}

func TestMarkFailed_RetryBuckets(t *testing.T) {
	db := &mockDB{queryRowVals: []any{0}}
	err := markFailed(context.Background(), db, "e1", "oops")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(db.execCalls) == 0 || !containsSQL(db.execCalls, "UPDATE outbox_events") {
		t.Fatalf("expected update outbox_events")
	}
}

func TestMarkFailed_DLQ(t *testing.T) {
	db := &mockDB{queryRowVals: []any{9}}
	err := markFailed(context.Background(), db, "e1", "oops")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSQL(db.execCalls, "INSERT INTO dead_letter_queue") {
		t.Fatalf("expected insert into dead_letter_queue")
	}
}

func TestMarkPublished_Writes(t *testing.T) {
	db := &mockDB{}
	if err := markPublished(context.Background(), db, "id", "t", "corr"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !containsSQL(db.execCalls, "UPDATE outbox_events") || !containsSQL(db.execCalls, "INSERT INTO published_events") {
		t.Fatalf("expected update outbox and insert published")
	}
}

func TestFetchPending_Reads(t *testing.T) {
	now := time.Now()
	rows := &mockRows{
		data: [][]any{
			{"id1", "t1", "c1", "topic", "pk", []byte(`{}`), now},
			{"id2", "t2", "c2", "topic", "pk", []byte(`{}`), now},
		},
	}
	db := &mockDB{queryRows: rows}
	evts, err := fetchPending(context.Background(), db, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(evts) != 2 {
		t.Fatalf("expected 2 events")
	}
}

func containsSQL(list []string, sub string) bool {
	for _, s := range list {
		if stringsContains(s, sub) {
			return true
		}
	}
	return false
}

func stringsContains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool { return (len(sub) == 0) || (len(s) >= len(sub) && indexOf(s, sub) >= 0) })()
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			if s[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func TestEnsureSchema_Executes(t *testing.T) {
	db := &mockDB{}
	err := EnsureSchema(db)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(db.execCalls) < 3 {
		t.Fatalf("expected multiple schema execs")
	}
}
