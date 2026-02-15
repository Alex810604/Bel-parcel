package reassignment

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type wtMockDB struct {
	tx pgx.Tx
}

func (m *wtMockDB) Begin(ctx context.Context) (pgx.Tx, error) {
	return m.tx, nil
}

type wtRows struct {
	cur  int
	data [][]any
}

func (r *wtRows) Next() bool {
	if r.cur >= len(r.data) {
		return false
	}
	r.cur++
	return true
}

func (r *wtRows) Scan(dest ...any) error {
	row := r.data[r.cur-1]
	for i := range dest {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(row[i]))
	}
	return nil
}

func (r *wtRows) Close()                                       {}
func (r *wtRows) Err() error                                   { return nil }
func (r *wtRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *wtRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *wtRows) RawValues() [][]byte                          { return nil }
func (r *wtRows) Values() ([]any, error)                       { return nil, nil }
func (r *wtRows) Conn() *pgx.Conn                              { return nil }

type wtTx struct {
	execs []string
	rows  pgx.Rows
}

func (t *wtTx) Begin(ctx context.Context) (pgx.Tx, error) { return t, nil }
func (t *wtTx) Commit(ctx context.Context) error          { return nil }
func (t *wtTx) Rollback(ctx context.Context) error        { return nil }
func (t *wtTx) Conn() *pgx.Conn                           { return nil }
func (t *wtTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (t *wtTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.execs = append(t.execs, sql)
	return pgconn.CommandTag{}, nil
}
func (t *wtTx) LargeObjects() pgx.LargeObjects { var lo pgx.LargeObjects; return lo }
func (t *wtTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (t *wtTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return t.rows, nil
}
func (t *wtTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row { return nil }
func (t *wtTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	var br pgx.BatchResults
	return br
}

func TestWorkerTick_EnqueuesAndUpdates(t *testing.T) {
	tx := &wtTx{
		rows: &wtRows{
			data: [][]any{
				{"trip-1", "batch-1", "carrier-1"},
			},
		},
	}
	db := &wtMockDB{tx: tx}
	w := NewWorker(db, time.Millisecond, "commands.trip.reassign")
	w.tick(context.Background())
	foundOutbox := false
	foundUpdate := false
	for _, s := range tx.execs {
		if contains(s, "INSERT INTO outbox_events") {
			foundOutbox = true
		}
		if contains(s, "UPDATE pending_confirmations") {
			foundUpdate = true
		}
	}
	if !foundOutbox || !foundUpdate {
		t.Fatalf("expected outbox insert and pending update")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && indexOf(s, sub) >= 0
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
