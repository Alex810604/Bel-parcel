package httpserver

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bel-parcel/services/mobile-gateway/internal/auth"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type mgMockDB struct{ tx pgx.Tx }

func (m *mgMockDB) Begin(ctx context.Context) (pgx.Tx, error) { return m.tx, nil }
func (m *mgMockDB) Ping(ctx context.Context) error            { return nil }

type mgRows struct{}

func (r *mgRows) Next() bool                                   { return false }
func (r *mgRows) Scan(dest ...any) error                       { return nil }
func (r *mgRows) Close()                                       {}
func (r *mgRows) Err() error                                   { return nil }
func (r *mgRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *mgRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *mgRows) RawValues() [][]byte                          { return nil }
func (r *mgRows) Values() ([]any, error)                       { return nil, nil }
func (r *mgRows) Conn() *pgx.Conn                              { return nil }

type mgTx struct{ execs []string }

func (t *mgTx) Begin(ctx context.Context) (pgx.Tx, error) { return t, nil }
func (t *mgTx) Commit(ctx context.Context) error          { return nil }
func (t *mgTx) Rollback(ctx context.Context) error        { return nil }
func (t *mgTx) Conn() *pgx.Conn                           { return nil }
func (t *mgTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (t *mgTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.execs = append(t.execs, sql)
	return pgconn.CommandTag{}, nil
}
func (t *mgTx) LargeObjects() pgx.LargeObjects { var lo pgx.LargeObjects; return lo }
func (t *mgTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (t *mgTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return &mgRows{}, nil
}
func (t *mgTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return &struct{ pgx.Row }{}
}
func (t *mgTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	var br pgx.BatchResults
	return br
}

func jwtFor(t *testing.T) string {
	claims := jwt.RegisteredClaims{
		Issuer:    "bp",
		Subject:   "u1",
		Audience:  jwt.ClaimStrings{"mobile"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{RegisteredClaims: claims, Role: "carrier"})
	signed, _ := tok.SignedString([]byte("secret"))
	return signed
}

func TestEvents_PickedUp_Accepted(t *testing.T) {
	tx := &mgTx{}
	db := &mgMockDB{tx: tx}
	s := NewServer(db, auth.NewValidator("secret", "bp", "mobile"), map[string]string{
		"events.batch_picked_up": "events.batch_picked_up",
	})
	mux := s.Routes()
	body := `{"event_id":"e1","event_type":"events.batch_picked_up","data":{"trip_id":"t1","batch_id":"b1","carrier_id":"c1","pickup_point_id":"p1","picked_up_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+jwtFor(t))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}
}

func TestEvents_DeliveredToPVP_Accepted(t *testing.T) {
	tx := &mgTx{}
	db := &mgMockDB{tx: tx}
	s := NewServer(db, auth.NewValidator("secret", "bp", "mobile"), map[string]string{
		"events.batch_delivered_to_pvp": "events.batch_delivered_to_pvp",
	})
	mux := s.Routes()
	body := `{"event_id":"e2","event_type":"events.batch_delivered_to_pvp","data":{"trip_id":"t1","batch_id":"b1","carrier_id":"c1","pvp_id":"p1","is_hub":false,"delivered_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+jwtFor(t))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}
}

func TestEvents_ReceivedByPVP_Accepted(t *testing.T) {
	tx := &mgTx{}
	db := &mgMockDB{tx: tx}
	s := NewServer(db, auth.NewValidator("secret", "bp", "mobile"), map[string]string{
		"events.batch_received_by_pvp": "events.batch_received_by_pvp",
	})
	mux := s.Routes()
	body := `{"event_id":"e3","event_type":"events.batch_received_by_pvp","data":{"batch_id":"b1","pvp_id":"p1","order_ids":["o1","o2"],"received_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+jwtFor(t))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}
}
