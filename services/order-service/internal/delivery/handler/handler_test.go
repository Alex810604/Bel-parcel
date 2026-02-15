package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHealthz_OK(t *testing.T) {
	h := &Handler{service: nil}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.HealthCheck)
	req := httptest.NewRequest("GET", "/healthz", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestCreateOrder_BadJSON(t *testing.T) {
	h := &Handler{service: nil}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders", h.CreateOrder)
	req := httptest.NewRequest("POST", "/orders", strings.NewReader("{"))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestCreateOrder_MissingParams(t *testing.T) {
	h := &Handler{service: nil}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /orders", h.CreateOrder)
	req := httptest.NewRequest("POST", "/orders", strings.NewReader(`{"seller_id":"","pvz_id":""}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}
