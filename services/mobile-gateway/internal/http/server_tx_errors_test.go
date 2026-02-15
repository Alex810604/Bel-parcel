package httpserver

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bel-parcel/services/mobile-gateway/internal/auth"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func jwtCarrier4(t *testing.T) string {
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

type errDB struct {
	tx       pgx.Tx
	beginErr error
}

func (e *errDB) Begin(ctx context.Context) (pgx.Tx, error) { return e.tx, e.beginErr }
func (e *errDB) Ping(ctx context.Context) error            { return nil }

type errTx struct {
	failAt int
	calls  int
}

func (t *errTx) Begin(ctx context.Context) (pgx.Tx, error) { return t, nil }
func (t *errTx) Commit(ctx context.Context) error {
	t.calls++
	if t.failAt == t.calls {
		return errors.New("commit failed")
	}
	return nil
}
func (t *errTx) Rollback(ctx context.Context) error { return nil }
func (t *errTx) Conn() *pgx.Conn                    { return nil }
func (t *errTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (t *errTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.calls++
	if t.failAt == t.calls {
		return pgconn.CommandTag{}, errors.New("exec failed")
	}
	return pgconn.CommandTag{}, nil
}
func (t *errTx) LargeObjects() pgx.LargeObjects { var lo pgx.LargeObjects; return lo }
func (t *errTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (t *errTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return &mgRows{}, nil
}
func (t *errTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return &struct{ pgx.Row }{}
}
func (t *errTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	var br pgx.BatchResults
	return br
}

func TestEvents_BeginError_500(t *testing.T) {
	db := &errDB{beginErr: errors.New("begin failed")}
	s := NewServer(db, auth.NewValidator("secret", "bp", "mobile"), map[string]string{
		"events.batch_picked_up": "T",
	})
	mux := s.Routes()
	body := `{"event_id":"e1","event_type":"events.batch_picked_up","data":{"trip_id":"t1","batch_id":"b1","carrier_id":"c1","pickup_point_id":"p1","picked_up_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+jwtCarrier4(t))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

func TestEvents_LogInsertError_500(t *testing.T) {
	tx := &errTx{failAt: 1}
	db := &errDB{tx: tx}
	s := NewServer(db, auth.NewValidator("secret", "bp", "mobile"), map[string]string{
		"events.batch_picked_up": "T",
	})
	mux := s.Routes()
	body := `{"event_id":"e1","event_type":"events.batch_picked_up","data":{"trip_id":"t1","batch_id":"b1","carrier_id":"c1","pickup_point_id":"p1","picked_up_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+jwtCarrier4(t))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

func TestEvents_EnqueueError_500(t *testing.T) {
	tx := &errTx{failAt: 2} // first Exec ok (log), second Exec fails (enqueue)
	db := &errDB{tx: tx}
	s := NewServer(db, auth.NewValidator("secret", "bp", "mobile"), map[string]string{
		"events.batch_picked_up": "T",
	})
	mux := s.Routes()
	body := `{"event_id":"e1","event_type":"events.batch_picked_up","data":{"trip_id":"t1","batch_id":"b1","carrier_id":"c1","pickup_point_id":"p1","picked_up_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+jwtCarrier4(t))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

func TestEvents_CommitError_500(t *testing.T) {
	tx := &errTx{failAt: 3} // two Exec ok, Commit fails
	db := &errDB{tx: tx}
	s := NewServer(db, auth.NewValidator("secret", "bp", "mobile"), map[string]string{
		"events.batch_picked_up": "T",
	})
	mux := s.Routes()
	body := `{"event_id":"e1","event_type":"events.batch_picked_up","data":{"trip_id":"t1","batch_id":"b1","carrier_id":"c1","pickup_point_id":"p1","picked_up_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+jwtCarrier4(t))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

func TestLocation_BeginError_500(t *testing.T) {
	db := &errDB{beginErr: errors.New("begin failed")}
	s := NewServer(db, auth.NewValidator("secret", "bp", "mobile"), map[string]string{"events.carrier_location": "T"})
	mux := s.Routes()
	body := `{"event_id":"e1","carrier_id":"c1","latitude":1.0,"longitude":2.0,"timestamp":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/location", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+jwtCarrier4(t))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

func TestLocation_LogInsertError_500(t *testing.T) {
	tx := &errTx{failAt: 1}
	db := &errDB{tx: tx}
	s := NewServer(db, auth.NewValidator("secret", "bp", "mobile"), map[string]string{"events.carrier_location": "T"})
	mux := s.Routes()
	body := `{"event_id":"e1","carrier_id":"c1","latitude":1.0,"longitude":2.0,"timestamp":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/location", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+jwtCarrier4(t))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

func TestLocation_EnqueueError_500(t *testing.T) {
	tx := &errTx{failAt: 2}
	db := &errDB{tx: tx}
	s := NewServer(db, auth.NewValidator("secret", "bp", "mobile"), map[string]string{"events.carrier_location": "T"})
	mux := s.Routes()
	body := `{"event_id":"e1","carrier_id":"c1","latitude":1.0,"longitude":2.0,"timestamp":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/location", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+jwtCarrier4(t))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}

func TestLocation_CommitError_500(t *testing.T) {
	tx := &errTx{failAt: 3}
	db := &errDB{tx: tx}
	s := NewServer(db, auth.NewValidator("secret", "bp", "mobile"), map[string]string{"events.carrier_location": "T"})
	mux := s.Routes()
	body := `{"event_id":"e1","carrier_id":"c1","latitude":1.0,"longitude":2.0,"timestamp":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/location", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+jwtCarrier2(t))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rr.Code)
	}
}
