package auth

import (
	"errors"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Default fallback key for JWT signing. Can be overridden using JWT_SECRET env.
var defaultSecret = []byte("decotrack_super_secret_key_change_me_in_production")

type Claims struct {
	UID   string `json:"uid"`
	Email string `json:"email"`
	jwt.RegisteredClaims
}

// getSigningKey retrieves the JWT secret key from environmental variables or falls back to standard.
func getSigningKey() []byte {
	if secret := os.Getenv("JWT_SECRET"); secret != "" {
		return []byte(secret)
	}
	return defaultSecret
}

// GenerateJWT creates a custom session JWT for the authenticated user.
func GenerateJWT(uid, email string) (string, error) {
	if uid == "" || email == "" {
		return "", errors.New("invalid claims parameters")
	}

	claims := Claims{
		UID:   uid,
		Email: email,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(72 * time.Hour)), // 3 days expiry
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "DecoTrack-Backend",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signedToken, err := token.SignedString(getSigningKey())
	if err != nil {
		return "", err
	}

	return signedToken, nil
}

// ParseJWT validates the custom session JWT and returns the parsed Claims.
func ParseJWT(tokenString string) (*Claims, error) {
	if tokenString == "" {
		return nil, errors.New("empty jwt token")
	}

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Verify signature algorithm is HMAC-SHA256
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return getSigningKey(), nil
	})

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid or expired jwt token")
	}

	return claims, nil
}
