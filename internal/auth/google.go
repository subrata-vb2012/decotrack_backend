package auth

import (
	"context"
	"errors"
	"fmt"
	"os"

	"google.golang.org/api/idtoken"
)

const DefaultGoogleClientID = "594179901385-ab1g3eblhjkjokktcrr11ab9cd9jp35f.apps.googleusercontent.com"

func getGoogleClientID() string {
	if clientID := os.Getenv("GOOGLE_CLIENT_ID"); clientID != "" {
		return clientID
	}
	return DefaultGoogleClientID
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
	payload, err := idtoken.Validate(ctx, tokenString, clientID)
	if err != nil {
		return nil, fmt.Errorf("invalid google token: %w", err)
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
