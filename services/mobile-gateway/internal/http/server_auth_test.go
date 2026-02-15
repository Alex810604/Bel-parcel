package httpserver

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bel-parcel/services/mobile-gateway/internal/auth"
	"github.com/golang-jwt/jwt/v5"
)

func TestEvents_Unauthorized(t *testing.T) {
	validator := auth.NewValidator("secret", "bp", "mobile")
	s := NewServer(nil, validator, map[string]string{})
	mux := s.Routes()
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(`{"event_id":"e1","event_type":"batch_picked_up","data":{}}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestLocation_Unauthorized(t *testing.T) {
	validator := auth.NewValidator("secret", "bp", "mobile")
	s := NewServer(nil, validator, map[string]string{})
	mux := s.Routes()
	req := httptest.NewRequest(http.MethodPost, "/location", bytes.NewBufferString(`{"carrier_id":"c1","latitude":1.0,"longitude":2.0,"timestamp":"`+time.Now().UTC().Format(time.RFC3339)+`"}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestEvents_AuthorizedBadPayload(t *testing.T) {
	validator := auth.NewValidator("secret", "bp", "mobile")
	s := NewServer(nil, validator, map[string]string{
		"events.batch_picked_up": "events.batch_picked_up",
	})
	mux := s.Routes()
	claims := jwt.RegisteredClaims{
		Issuer:    "bp",
		Subject:   "u1",
		Audience:  jwt.ClaimStrings{"mobile"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{RegisteredClaims: claims, Role: "carrier"})
	signed, _ := tok.SignedString([]byte("secret"))
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(`{"event_id":"","event_type":"events.batch_picked_up","data":{}}`))
	req.Header.Set("Authorization", "Bearer "+signed)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}
