package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"google.golang.org/api/idtoken"
)

const DefaultGoogleClientID = "586583690399-el00enka8a457ik0aj6h9m5film3nvig.apps.googleusercontent.com"

func getGoogleClientID() string {
	if clientID := os.Getenv("GOOGLE_CLIENT_ID"); clientID != "" {
		return clientID
	}
	return DefaultGoogleClientID
}

// getRawAudience parses the unverified JWT payload to inspect its "aud" claim for debugging.
func getRawAudience(tokenString string) string {
	parts := strings.Split(tokenString, ".")
	if len(parts) < 2 {
		return "invalid-jwt-format"
	}

	payloadStr := parts[1]
	// Pad base64 standard padding if needed
	if l := len(payloadStr) % 4; l > 0 {
		payloadStr += strings.Repeat("=", 4-l)
	}

	decoded, err := base64.URLEncoding.DecodeString(payloadStr)
	if err != nil {
		// Fallback to standard base64 decoding
		decoded, err = base64.StdEncoding.DecodeString(payloadStr)
		if err != nil {
			return "failed-base64-decode"
		}
	}

	var claims map[string]interface{}
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return "failed-json-unmarshal"
	}

	if aud, ok := claims["aud"].(string); ok {
		return aud
	}
	return "missing-aud-claim"
}

type GoogleUser struct {
	Subject string // Google UID
	Email   string
	Name    string
	Picture string
}

// VerifyGoogleIDToken validates the token directly with Google Auth servers.
func VerifyGoogleIDToken(ctx context.Context, tokenString string) (*GoogleUser, error) {
	if tokenString == "" {
		return nil, errors.New("empty token")
	}

	clientID := getGoogleClientID()

	// 🛠️ Multi-Audience Validation List:
	// Loops through all configured client IDs in the DecoTrack ecosystem (mobile app, playground, backend)
	// to guarantee instant validation across all testing and production devices.
	allowedClientIDs := []string{
		clientID,
		"407408718192.apps.googleusercontent.com",                                 // Google OAuth Playground
		"586583690399-el00enka8a457ik0aj6h9m5film3nvig.apps.googleusercontent.com", // Flutter App Active Android Client ID
		"594179901385-796854p6185s6g2q82i2jcc38bof45j0.apps.googleusercontent.com", // Flutter App Alternative Client ID
	}

	var payload *idtoken.Payload
	var err error

	for _, aud := range allowedClientIDs {
		if aud == "" {
			continue
		}
		payload, err = idtoken.Validate(ctx, tokenString, aud)
		if err == nil {
			break // Validation succeeded!
		}
	}

	if err != nil {
		rawAud := getRawAudience(tokenString)
		return nil, fmt.Errorf("idtoken validation failed: %w (Token payload 'aud': %q | Backend checking list: %v)", err, rawAud, allowedClientIDs)
	}

	email, _ := payload.Claims["email"].(string)
	name, _ := payload.Claims["name"].(string)
	picture, _ := payload.Claims["picture"].(string)

	return &GoogleUser{
		Subject: payload.Subject,
		Email:   email,
		Name:    name,
		Picture: picture,
	}, nil
}
