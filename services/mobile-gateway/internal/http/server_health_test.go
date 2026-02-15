package httpserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jackc/pgx/v5"
)

type healthDB struct{ pingErr error }

func (h *healthDB) Begin(ctx context.Context) (pgx.Tx, error) { return nil, nil }
func (h *healthDB) Ping(ctx context.Context) error            { return h.pingErr }

func TestHealthz_OK(t *testing.T) {
	s := NewServer(&healthDB{pingErr: nil}, nil, map[string]string{})
	mux := s.Routes()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestReadyz_Unready(t *testing.T) {
	s := NewServer(&healthDB{pingErr: errors.New("db down")}, nil, map[string]string{})
	mux := s.Routes()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rr.Code)
	}
}
