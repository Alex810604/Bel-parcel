package httpserver

import (
	"bel-parcel/services/mobile-gateway/internal/auth"
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func jwtCarrier3(t *testing.T) string {
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

func TestLocation_InvalidJSON(t *testing.T) {
	s := NewServer(nil, auth.NewValidator("secret", "bp", "mobile"), map[string]string{"events.carrier_location": "T"})
	mux := s.Routes()
	req := httptest.NewRequest(http.MethodPost, "/location", bytes.NewBufferString(`{`))
	req.Header.Set("Authorization", "Bearer "+jwtCarrier3(t))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLocation_MissingCarrier(t *testing.T) {
	s := NewServer(nil, auth.NewValidator("secret", "bp", "mobile"), map[string]string{"events.carrier_location": "T"})
	mux := s.Routes()
	req := httptest.NewRequest(http.MethodPost, "/location", bytes.NewBufferString(`{"event_id":"x","latitude":1,"longitude":2}`))
	req.Header.Set("Authorization", "Bearer "+jwtCarrier3(t))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestLocation_TimestampDefault(t *testing.T) {
	tx := &locTx{}
	db := &locMockDB{tx: tx}
	mux := NewServer(db, auth.NewValidator("secret", "bp", "mobile"), map[string]string{"events.carrier_location": "T"}).Routes()
	req := httptest.NewRequest(http.MethodPost, "/location", bytes.NewBufferString(`{"event_id":"x","carrier_id":"c1","latitude":1,"longitude":2}`))
	req.Header.Set("Authorization", "Bearer "+jwtCarrier3(t))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}
}
