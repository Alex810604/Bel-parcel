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

func jwtCarrier2(t *testing.T) string {
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

func TestEvents_InvalidEventType(t *testing.T) {
	s := NewServer(nil, auth.NewValidator("secret", "bp", "mobile"), map[string]string{})
	mux := s.Routes()
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(`{"event_id":"x","event_type":"unknown","data":{}}`))
	req.Header.Set("Authorization", "Bearer "+jwtCarrier2(t))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestEvents_PickedUp_InvalidTime(t *testing.T) {
	s := NewServer(nil, auth.NewValidator("secret", "bp", "mobile"), map[string]string{"events.batch_picked_up": "T"})
	mux := s.Routes()
	body := `{"event_id":"e1","event_type":"events.batch_picked_up","data":{"trip_id":"t1","batch_id":"b1","carrier_id":"c1","pickup_point_id":"p1","picked_up_at":"0001-01-01T00:00:00Z"}}`
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+jwtCarrier2(t))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestEvents_DeliveredToPVP_InvalidData(t *testing.T) {
	s := NewServer(nil, auth.NewValidator("secret", "bp", "mobile"), map[string]string{"events.batch_delivered_to_pvp": "T"})
	mux := s.Routes()
	body := `{"event_id":"e2","event_type":"events.batch_delivered_to_pvp","data":{"trip_id":"","batch_id":"b1","carrier_id":"c1","pvp_id":"p1","is_hub":false,"delivered_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+jwtCarrier2(t))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestEvents_ReceivedByPVP_InvalidTime(t *testing.T) {
	s := NewServer(nil, auth.NewValidator("secret", "bp", "mobile"), map[string]string{"events.batch_received_by_pvp": "T"})
	mux := s.Routes()
	body := `{"event_id":"e3","event_type":"events.batch_received_by_pvp","data":{"batch_id":"b1","pvp_id":"p1","order_ids":["o1"],"received_at":"0001-01-01T00:00:00Z"}}`
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+jwtCarrier2(t))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}
