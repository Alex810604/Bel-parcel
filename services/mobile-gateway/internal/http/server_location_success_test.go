package httpserver

import (
	"context"
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bel-parcel/services/mobile-gateway/internal/auth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type locMockDB struct{ tx pgx.Tx }

func (m *locMockDB) Begin(ctx context.Context) (pgx.Tx, error) { return m.tx, nil }
func (m *locMockDB) Ping(ctx context.Context) error             { return nil }

type locTx struct{ execs []string }

func (t *locTx) Begin(ctx context.Context) (pgx.Tx, error) { return t, nil }
func (t *locTx) Commit(ctx context.Context) error          { return nil }
func (t *locTx) Rollback(ctx context.Context) error        { return nil }
func (t *locTx) Conn() *pgx.Conn                           { return nil }
func (t *locTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (t *locTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.execs = append(t.execs, sql)
	return pgconn.CommandTag{}, nil
}
func (t *locTx) LargeObjects() pgx.LargeObjects { var lo pgx.LargeObjects; return lo }
func (t *locTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (t *locTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) { return nil, nil }
func (t *locTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row        { return &struct{ pgx.Row }{} }
func (t *locTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults         { var br pgx.BatchResults; return br }

func jwtCarrier(t *testing.T) string {
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

func TestLocation_Accepted(t *testing.T) {
	tx := &locTx{}
	db := &locMockDB{tx: tx}
	s := NewServer(db, auth.NewValidator("secret", "bp", "mobile"), map[string]string{
		"events.carrier_location": "events.carrier_location",
	})
	mux := s.Routes()
	body := `{"event_id":"l1","carrier_id":"c1","latitude":53.9,"longitude":27.56,"timestamp":"` + time.Now().UTC().Format(time.RFC3339) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/location", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+jwtCarrier(t))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}
}
