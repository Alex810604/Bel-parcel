package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bel-parcel/services/reference-service/internal/auth"
)

func TestHandlers_InvalidJSON(t *testing.T) {
	val := auth.NewValidator("s", "", "")
	svc := &Service{}
	h := NewHandlers(svc, val)
	mux := http.NewServeMux()
	h.Routes(mux)

	token := "invalid" // will fail auth, but we need to pass auth -> make bare validator accept by empty issuer/audience
	// create a valid token quickly
	token = makeToken(t, "s", "", "", "op-1", "admin", time.Now().Add(time.Hour))

	// invalid JSON body
	req := httptest.NewRequest(http.MethodPut, "/pvp/pvp-1", strings.NewReader(`{"is_hub": true`))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandlers_MissingReason(t *testing.T) {
	val := auth.NewValidator("s", "", "")
	svc := &Service{}
	h := NewHandlers(svc, val)
	mux := http.NewServeMux()
	h.Routes(mux)

	token := makeToken(t, "s", "", "", "op-1", "admin", time.Now().Add(time.Hour))

	// missing reason
	req := httptest.NewRequest(http.MethodPut, "/carriers/car-1", strings.NewReader(`{"is_active": true}`))
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}

	// empty reason
	req2 := httptest.NewRequest(http.MethodPut, "/carriers/car-1", strings.NewReader(`{"is_active": true, "reason": ""}`))
	req2.Header.Set("Authorization", "Bearer "+token)
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	if w2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w2.Code)
	}
}
