# 🚀 Go Backend: PostgreSQL, Direct Google Auth & FCM Integration

This technical guide outlines the architecture, database schema, REST API endpoints, and core code implementations for a custom **Go (Golang)** backend for **DecoTrack**. 

This system completely **bypasses Firebase Auth and Firebase Firestore**. Instead, it uses **PostgreSQL** for relational data persistence, **direct Google ID Token verification** for authentication, issues **custom JWTs** for session security, and integrates **Firebase Cloud Messaging (FCM)** *only* for sending push notifications.

---

## 🏛️ 1. System Architecture

```mermaid
graph TD
    Client[Flutter Client] -->|1. Authenticates| Google[Google Sign-In Account]
    Google -->|2. Returns Google ID Token| Client
    Client -->|3. POST /auth/google {idToken}| Go[Go Backend API]
    Go -->|4. Verifies Token with Google Keys| GoogleAPI[google.golang.org/api/idtoken]
    Go -->|5. Registers / Finds User| DB[(PostgreSQL Database)]
    Go -->|6. Generates custom JWT| Client
    Client -->|7. Calls APIs with Bearer JWT| Go
    Go -->|Sends Push Notifications| FCM[Firebase Cloud Messaging API]
    FCM -->|Push Notification Alerts| Client
```

---

## 📂 2. Go Backend Project Structure

```text
decotrack-backend/
├── cmd/
│   └── api/
│       └── main.go                 # Server entry point & routes setup
├── config/
│   └── firebaseServiceAccount.json # FCM service credentials (EXCLUDE FROM GIT)
├── internal/
│   ├── auth/
│   │   ├── google.go               # Direct Google ID token verifier
│   │   ├── jwt.go                  # Custom JWT generator and parser
│   │   └── middleware.go           # Custom JWT auth middleware
│   ├── database/
│   │   ├── db.go                   # PostgreSQL connection pool manager
│   │   └── fcm.go                  # Firebase Messaging client initializer
│   ├── models/
│   │   ├── models.go               # PostgreSQL database structures
│   └── handlers/
│       ├── auth_handler.go         # Direct Google Login & registration handlers
│       ├── inventory_handler.go    # SQL Transaction-safe Inventory CRUD
│       ├── lending_handler.go      # Transaction-safe lending & returns
│       └── notification_handler.go # Registering device FCM tokens
├── go.mod
└── go.sum
```

---

## 💾 3. PostgreSQL Database Schema

To guarantee double-entry accounting integrity and exact stock allocations, we use relational schemas with foreign keys and quantity constraints:

```sql
-- Enums
CREATE TYPE member_role AS ENUM ('owner', 'admin', 'secretary', 'member');
CREATE TYPE member_status AS ENUM ('active', 'pending', 'blocked');
CREATE TYPE inventory_type AS ENUM ('lent', 'catering');
CREATE TYPE lending_status AS ENUM ('active', 'partiallyReturned', 'closed');
CREATE TYPE account_entry_type AS ENUM ('openingBalance', 'credit', 'debit');
CREATE TYPE account_entry_status AS ENUM ('completed', 'pending');

-- 1. Users Table
CREATE TABLE users (
    id VARCHAR(128) PRIMARY KEY, -- Matches Google Subject ID / Sub
    name VARCHAR(255) NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    photo_url TEXT,
    mobile_number VARCHAR(20),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 2. Clubs Table
CREATE TABLE clubs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    address TEXT NOT NULL,
    registration_number VARCHAR(100),
    email VARCHAR(255) NOT NULL,
    photo_url TEXT,
    invite_code VARCHAR(10) UNIQUE NOT NULL,
    owner_id VARCHAR(128) REFERENCES users(id),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 3. Club Members Table (Pivot Table)
CREATE TABLE club_members (
    club_id UUID REFERENCES clubs(id) ON DELETE CASCADE,
    user_id VARCHAR(128) REFERENCES users(id) ON DELETE CASCADE,
    role member_role NOT NULL DEFAULT 'member',
    status member_status NOT NULL DEFAULT 'pending',
    joined_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (club_id, user_id)
);

-- 4. Device FCM Tokens Table (Linked to User for Push Notifications)
CREATE TABLE user_fcm_tokens (
    user_id VARCHAR(128) REFERENCES users(id) ON DELETE CASCADE,
    token TEXT PRIMARY KEY,
    platform VARCHAR(50) DEFAULT 'mobile',
    last_updated TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

-- 5. Inventory Table
CREATE TABLE inventory (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    club_id UUID REFERENCES clubs(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    total_qty INT NOT NULL CHECK (total_qty >= 0),
    available_qty INT NOT NULL CHECK (available_qty >= 0),
    category VARCHAR(100) NOT NULL,
    is_active BOOLEAN DEFAULT TRUE,
    type inventory_type NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT chk_available_qty CHECK (available_qty <= total_qty)
);

-- 6. Club Account Entries Table (Financial Ledger)
CREATE TABLE club_account_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    club_id UUID REFERENCES clubs(id) ON DELETE CASCADE,
    purpose VARCHAR(255) NOT NULL,
    type account_entry_type NOT NULL,
    status account_entry_status NOT NULL DEFAULT 'completed',
    amount NUMERIC(12, 2) NOT NULL CHECK (amount >= 0),
    entry_date TIMESTAMP WITH TIME ZONE NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    created_by VARCHAR(128) REFERENCES users(id) ON DELETE SET NULL,
    updated_at TIMESTAMP WITH TIME ZONE,
    updated_by VARCHAR(128) REFERENCES users(id) ON DELETE SET NULL
);
```

---

## 🛠️ 4. Core Implementations (Go Code Snippets)

### A. Direct Google ID Token Verification (`google.go`)
This function validates the Google ID Token sent by the Flutter client using Google's public certificate keys, bypassing Firebase Auth completely.

```go
package auth

import (
	"context"
	"errors"
	
	"google.golang.org/api/idtoken"
)

const GoogleClientID = "594179901385-ab1g3eblhjkjokktcrr11ab9cd9jp35f.apps.googleusercontent.com"

type GoogleUser struct {
	Subject       string // Google UID
	Email         string
	Name          string
	Picture       string
}

// VerifyGoogleIDToken validates the token directly with Google Auth servers
func VerifyGoogleIDToken(ctx context.Context, tokenString string) (*GoogleUser, error) {
	payload, err := idtoken.Validate(ctx, tokenString, GoogleClientID)
	if err != nil {
		return nil, errors.New("invalid google token: " + err.Error())
	}

	return &GoogleUser{
		Subject: payload.Subject,
		Email:   payload.Claims["email"].(string),
		Name:    payload.Claims["name"].(string),
		Picture: payload.Claims["picture"].(string),
	}, nil
}
```

---

### B. Google Authentication & Custom JWT Issuance (`auth_handler.go`)
Upon successful verification of the Google ID Token, the backend searches for the user in PostgreSQL. If new, it registers them; it then generates a custom **JWT token** to secure subsequent requests.

```go
package handlers

import (
	"database/sql"
	"net/http"
	"time"

	"decotrack-backend/internal/auth"
	"github.com/gin-gonic/gin"
)

type GoogleLoginRequest struct {
	IDToken string `json:"idToken" binding:"required"`
}

func (app *App) AuthenticateGoogleUser(c *gin.Context) {
	var req GoogleLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 1. Verify token directly with Google
	googleUser, err := auth.VerifyGoogleIDToken(c.Request.Context(), req.IDToken)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	// 2. Query / Insert User into PostgreSQL
	var userUID, email, name, photoURL string
	query := `
		INSERT INTO users (id, name, email, photo_url, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE 
		SET name = EXCLUDED.name, photo_url = EXCLUDED.photo_url
		RETURNING id, email, name, photo_url`

	err = app.DB.QueryRow(c.Request.Context(), query, 
		googleUser.Subject, 
		googleUser.Name, 
		googleUser.Email, 
		googleUser.Picture, 
		time.Now(),
	).Scan(&userUID, &email, &name, &photoURL)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database registration failed"})
		return
	}

	// 3. Generate a secure, server-signed Custom JWT
	jwtToken, err := auth.GenerateJWT(userUID, email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "JWT generation failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": jwtToken,
		"user": gin.H{
			"uid":      userUID,
			"name":     name,
			"email":    email,
			"photoUrl": photoURL,
		},
	})
}
```

---

### C. Sending Push Notifications via FCM (`fcm.go`)
This function securely connects to Firebase Cloud Messaging (FCM) using your service account credentials to dispatch push notifications to specific users.

```go
package database

import (
	"context"
	"log"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"
)

type NotificationEngine struct {
	MsgClient *messaging.Client
}

func InitFCM(serviceAccountPath string) *NotificationEngine {
	ctx := context.Background()
	opt := option.WithCredentialsFile(serviceAccountPath)
	
	app, err := firebase.NewApp(ctx, nil, opt)
	if err != nil {
		log.Fatalf("Error initializing Firebase App for FCM: %v", err)
	}

	msgClient, err := app.Messaging(ctx)
	if err != nil {
		log.Fatalf("Error initializing FCM messaging client: %v", err)
	}

	return &NotificationEngine{MsgClient: msgClient}
}

// SendPushNotification dispatches a push notification to a device token
func (ne *NotificationEngine) SendPushNotification(ctx context.Context, deviceToken, title, body string) error {
	message := &messaging.Message{
		Token: deviceToken,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
		Android: &messaging.AndroidConfig{
			Priority: "high",
		},
	}

	_, err := ne.MsgClient.Send(ctx, message)
	return err
}
```

---

## 📡 5. Updated REST API Endpoints Specification

### 🔑 Authentication Endpoints
* **`POST /api/v1/auth/google`**
  * **Payload:** `{ "idToken": "google_id_token_from_flutter" }`
  * **Response:** `{ "token": "custom_server_signed_jwt", "user": { "uid", "name", "email" } }`

### 📱 FCM Push Token Registration
* **`POST /api/v1/users/me/fcm-token`**
  * **Payload:** `{ "token": "fcm_token_from_device", "platform": "android/ios" }`
  * **Response:** `{ "status": "registered" }`

### 🏢 Operations Endpoints (Protected by Bearer custom JWT)
* **`GET /api/v1/clubs/:clubId/inventory`** - Get cataloged assets.
* **`POST /api/v1/clubs/:clubId/lendings`** - Verify stock and record checkouts.
* **`POST /api/v1/clubs/:clubId/lendings/:lendingId/returns`** - Log partial or full checkins, immediately restoring stock levels.
* **`POST /api/v1/clubs/:clubId/notices`** - Publish announcements.
  * **Business Rule:** When a new notice is published, the Go backend queries the database for all active members' FCM tokens and automatically calls the **FCM Notification Engine** to push a notification alert to their devices!
