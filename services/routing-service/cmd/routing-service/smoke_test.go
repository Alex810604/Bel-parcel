package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func routingMuxForTest(pingErr error) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if pingErr != nil {
			http.Error(w, "Service Unavailable", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})
	return mux
}

func TestCmd_HealthAndReady_Routing(t *testing.T) {
	mux := routingMuxForTest(nil)
	t.Run("healthz 200", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/healthz", nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
	})
	t.Run("readyz 200", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/readyz", nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rr.Code)
		}
	})
	t.Run("readyz 503 on ping error", func(t *testing.T) {
		mux := routingMuxForTest(errors.New("db down"))
		req := httptest.NewRequest("GET", "/readyz", nil)
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusServiceUnavailable {
			t.Fatalf("expected 503, got %d", rr.Code)
		}
	})
}
