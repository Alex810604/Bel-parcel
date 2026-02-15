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

func jwtRole(t *testing.T, role string) string {
	claims := jwt.RegisteredClaims{
		Issuer:    "bp",
		Subject:   "u1",
		Audience:  jwt.ClaimStrings{"mobile"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{RegisteredClaims: claims, Role: role})
	signed, _ := tok.SignedString([]byte("secret"))
	return signed
}

func TestEvents_InvalidJSON(t *testing.T) {
	s := NewServer(nil, auth.NewValidator("secret", "bp", "mobile"), map[string]string{})
	mux := s.Routes()
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(`{`))
	req.Header.Set("Authorization", "Bearer "+jwtRole(t, "carrier"))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestEvents_MissingFields(t *testing.T) {
	s := NewServer(nil, auth.NewValidator("secret", "bp", "mobile"), map[string]string{})
	mux := s.Routes()
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(`{"event_id":"","event_type":"","data":{}}`))
	req.Header.Set("Authorization", "Bearer "+jwtRole(t, "carrier"))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestResolveTopic_Aliases(t *testing.T) {
	s := NewServer(nil, auth.NewValidator("secret", "bp", "mobile"), map[string]string{
		"batch_picked_up":        "T1",
		"batch_delivered_to_pvp": "T2",
		"batch_received_by_pvp":  "T3",
		"carrier.location":       "T4",
	})
	cases := []string{
		"events.batch_picked_up",
		"events.batch_delivered_to_pvp",
		"events.batch_received_by_pvp",
		"events.carrier_location",
	}
	for _, in := range cases {
		if _, ok := s.resolveTopic(in); !ok {
			t.Fatalf("expected alias resolve for %s", in)
		}
	}
}

func TestEvents_PvpWorker_Role(t *testing.T) {
	tx := &mgTx{}
	db := &mgMockDB{tx: tx}
	s := NewServer(db, auth.NewValidator("secret", "bp", "mobile"), map[string]string{
		"events.batch_picked_up": "T",
	})
	mux := s.Routes()
	body := `{"event_id":"e1","event_type":"events.batch_picked_up","data":{"trip_id":"t1","batch_id":"b1","carrier_id":"c1","pickup_point_id":"p1","picked_up_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}}`
	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+jwtRole(t, "pvp_worker"))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}
}
