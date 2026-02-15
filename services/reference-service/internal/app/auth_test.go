package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bel-parcel/services/reference-service/internal/auth"
	"github.com/golang-jwt/jwt/v5"
)

func makeToken(t *testing.T, secret, issuer, audience, sub, role string, exp time.Time) string {
	t.Helper()
	claims := &auth.Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   sub,
			Audience:  jwt.ClaimStrings{audience},
			ExpiresAt: jwt.NewNumericDate(exp),
		},
		Role: role,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := token.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return s
}

func TestAuthorization_RoleChecks(t *testing.T) {
	secret := "test-secret"
	issuer := "bel-parcel"
	audience := "ops-ui"
	val := auth.NewValidator(secret, issuer, audience)

	svc := &Service{}
	h := NewHandlers(svc, val)
	mux := http.NewServeMux()
	h.Routes(mux)

	// user trying to update pvz -> 403
	userToken := makeToken(t, secret, issuer, audience, "user-1", "user", time.Now().Add(time.Hour))
	req := httptest.NewRequest(http.MethodPut, "/pvp/pvp-123", strings.NewReader(`{"is_hub":true,"reason":"test"}`))
	req.Header.Set("Authorization", "Bearer "+userToken)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w.Code)
	}

	// moderator trying to update carrier -> 403
	modToken := makeToken(t, secret, issuer, audience, "mod-1", "moderator", time.Now().Add(time.Hour))
	req2 := httptest.NewRequest(http.MethodPut, "/carriers/car-1", strings.NewReader(`{"is_active":false,"reason":"test"}`))
	req2.Header.Set("Authorization", "Bearer "+modToken)
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)
	if w2.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", w2.Code)
	}

	// user searching references -> 200 (empty q handled without DB)
	req3 := httptest.NewRequest(http.MethodGet, "/pvz?q=", nil)
	req3.Header.Set("Authorization", "Bearer "+userToken)
	w3 := httptest.NewRecorder()
	mux.ServeHTTP(w3, req3)
	if w3.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w3.Code)
	}
}
