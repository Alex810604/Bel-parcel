package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestRoleAllowed(t *testing.T) {
	if !roleAllowed("admin", []string{"user", "admin"}) {
		t.Fatalf("expected admin allowed")
	}
	if roleAllowed("guest", []string{"user", "admin"}) {
		t.Fatalf("unexpected guest allowed")
	}
}

func TestValidatorVerify(t *testing.T) {
	secret := "s"
	v := NewValidator(secret, "iss", "aud")
	claims := jwt.RegisteredClaims{
		Issuer:    "iss",
		Subject:   "u1",
		Audience:  jwt.ClaimStrings{"aud"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Minute)),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{RegisteredClaims: claims, Role: "admin"})
	s, _ := tok.SignedString([]byte(secret))
	c, err := v.verify(s)
	if err != nil || c.Subject != "u1" || c.Role != "admin" {
		t.Fatalf("verify failed")
	}
	_, err = v.verify("invalid")
	if err == nil {
		t.Fatalf("expected error for invalid token")
	}
}

func TestRequireRoles(t *testing.T) {
	secret := "s"
	v := NewValidator(secret, "iss", "aud")
	claims := jwt.RegisteredClaims{
		Issuer:    "iss",
		Subject:   "u1",
		Audience:  jwt.ClaimStrings{"aud"},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(1 * time.Minute)),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, Claims{RegisteredClaims: claims, Role: "admin"})
	s, _ := tok.SignedString([]byte(secret))

	ok := false
	h := RequireRoles(v, []string{"admin"}, func(w http.ResponseWriter, r *http.Request) {
		ok = FromContext(r) != nil
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+s)
	rr := httptest.NewRecorder()
	h(rr, req)
	if rr.Code != http.StatusOK || !ok {
		t.Fatalf("RequireRoles expected OK")
	}

	req2 := httptest.NewRequest("GET", "/", nil)
	req2.Header.Set("Authorization", "Bearer "+s)
	rr2 := httptest.NewRecorder()
	RequireRoles(v, []string{"user"}, func(w http.ResponseWriter, r *http.Request) {})(rr2, req2)
	if rr2.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden")
	}

	req3 := httptest.NewRequest("GET", "/", nil)
	req3.Header.Set("Authorization", "Bearer "+strings.Repeat("x", 5))
	rr3 := httptest.NewRecorder()
	RequireRoles(v, []string{"admin"}, func(w http.ResponseWriter, r *http.Request) {})(rr3, req3)
	if rr3.Code != http.StatusUnauthorized {
		t.Fatalf("expected unauthorized")
	}
}
