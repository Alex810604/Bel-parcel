package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bel-parcel/services/reference-service/internal/auth"
)

func TestGetInvalidType(t *testing.T) {
	secret := "s"
	v := auth.NewValidator(secret, "iss", "aud")
	h := NewHandlers(&Service{}, v)
	mux := http.NewServeMux()
	h.Routes(mux)

	req := httptest.NewRequest("GET", "/unknown?q=x", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken(t, secret, "iss", "aud", "u1", "user", time.Now().Add(time.Minute)))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "invalid type") {
		t.Fatalf("expected invalid type")
	}
}

func TestGetEmptyQuery(t *testing.T) {
	secret := "s"
	v := auth.NewValidator(secret, "iss", "aud")
	h := NewHandlers(&Service{}, v)
	mux := http.NewServeMux()
	h.Routes(mux)

	req := httptest.NewRequest("GET", "/pvz?q=", nil)
	req.Header.Set("Authorization", "Bearer "+makeToken(t, secret, "iss", "aud", "u1", "user", time.Now().Add(time.Minute)))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "[]") {
		t.Fatalf("expected empty list")
	}
}

func TestPutPvzReasonRequired(t *testing.T) {
	secret := "s"
	v := auth.NewValidator(secret, "iss", "aud")
	h := NewHandlers(&Service{}, v)
	mux := http.NewServeMux()
	h.Routes(mux)

	req := httptest.NewRequest("PUT", "/pvp/123", strings.NewReader(`{"is_hub":true,"reason":""}`))
	req.Header.Set("Authorization", "Bearer "+makeToken(t, secret, "iss", "aud", "u1", "moderator", time.Now().Add(time.Minute)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "reason is required") {
		t.Fatalf("expected reason validation error")
	}
}
