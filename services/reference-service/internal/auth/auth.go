package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Validator struct {
	secret   []byte
	issuer   string
	audience string
}

func NewValidator(secret, issuer, audience string) *Validator {
	return &Validator{
		secret:   []byte(secret),
		issuer:   issuer,
		audience: audience,
	}
}

type Claims struct {
	jwt.RegisteredClaims
	Role string `json:"role"`
}

func (v *Validator) verify(tokenString string) (*Claims, error) {
	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return v.secret, nil
	})
	if err != nil || !token.Valid {
		return nil, errors.New("invalid token")
	}
	if claims.ExpiresAt != nil && time.Now().After(claims.ExpiresAt.Time) {
		return nil, errors.New("token expired")
	}
	if v.issuer != "" && claims.Issuer != v.issuer {
		return nil, errors.New("invalid issuer")
	}
	if v.audience != "" {
		ok := false
		for _, a := range claims.Audience {
			if a == v.audience {
				ok = true
				break
			}
		}
		if !ok {
			return nil, errors.New("invalid audience")
		}
	}
	return claims, nil
}

type ctxKey string

const userKey ctxKey = "auth_user"

type User struct {
	ID   string
	Role string
}

func FromContext(r *http.Request) *User {
	if u, ok := r.Context().Value(userKey).(*User); ok {
		return u
	}
	return nil
}

func RequireRoles(v *Validator, roles []string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		parts := strings.SplitN(header, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		claims, err := v.verify(parts[1])
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if !roleAllowed(claims.Role, roles) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		u := &User{ID: claims.Subject, Role: claims.Role}
		ctx := context.WithValue(r.Context(), userKey, u)
		next(w, r.WithContext(ctx))
	}
}

func roleAllowed(role string, allowed []string) bool {
	for _, a := range allowed {
		if role == a {
			return true
		}
	}
	return false
}
