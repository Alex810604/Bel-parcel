package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"bel-parcel/services/operator-api/internal/auth"
	"github.com/golang-jwt/jwt/v5"
)

func TestReassign_Unauthorized(t *testing.T) {
	validator := auth.NewValidator("secret", "bp", "operator")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /trips/{trip_id}/reassign", auth.RequireRoles(validator, []string{"moderator", "admin"}, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))

	req := httptest.NewRequest(http.MethodPost, "/trips/tr-1/reassign", bytes.NewBufferString(`{"new_carrier_id":"c1","reason":"test"}`))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestReassign_Authorized(t *testing.T) {
	validator := auth.NewValidator("secret", "bp", "operator")
	claims := jwt.RegisteredClaims{
		Issuer:    "bp",
		Subject:   "u1",
		Audience:  jwt.ClaimStrings{"operator"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{RegisteredClaims: claims, Role: "admin"})
	signed, _ := tok.SignedString([]byte("secret"))
	mux := http.NewServeMux()
	mux.HandleFunc("POST /trips/{trip_id}/reassign", auth.RequireRoles(validator, []string{"moderator", "admin"}, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			NewCarrierID string `json:"new_carrier_id"`
			Reason       string `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.NewCarrierID == "" || body.Reason == "" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))

	req := httptest.NewRequest(http.MethodPost, "/trips/tr-1/reassign", bytes.NewBufferString(`{"new_carrier_id":"c1","reason":"test"}`))
	req.Header.Set("Authorization", "Bearer "+signed)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", rr.Code)
	}
}
