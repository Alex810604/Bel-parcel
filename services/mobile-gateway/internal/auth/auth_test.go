package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestRequireBearerAndRoles(t *testing.T) {
	secret := "s"
	v := NewValidator(secret, "iss", "aud")
	claims := jwt.RegisteredClaims{
		Issuer:    "iss",
		Subject:   "u1",
		Audience:  jwt.ClaimStrings{"aud"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Minute)),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{RegisteredClaims: claims, Role: "admin"})
	s, _ := tok.SignedString([]byte(secret))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+s)
	rr := httptest.NewRecorder()
	RequireBearer(v, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected OK")
	}

	rr2 := httptest.NewRecorder()
	RequireRoles(v, []string{"admin"}, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})(rr2, req)
	if rr2.Code != http.StatusOK {
		t.Fatalf("expected roles OK")
	}
}
