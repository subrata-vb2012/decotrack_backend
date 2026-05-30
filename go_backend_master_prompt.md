# 🚀 DecoTrack Go Backend: Developer Master Prompt

This document contains a production-grade, highly comprehensive **Developer Master Prompt**. You can copy the raw text from this document and feed it directly into any advanced AI coding assistant (like Gemini, Claude, or GPT) in a new session to generate the entire **DecoTrack Go Backend codebase** from scratch.

---

## 📋 Copy-Paste Master Prompt Text

Copy everything inside the box below to start your backend coding session:

```text
You are an expert principal software engineer specializing in Go (Golang) and production-grade REST APIs. 

I want to build a complete backend service for an app named "DecoTrack" (a club inventory and lending manager) from scratch using Go. You must generate the full, production-ready, and compilation-safe Go codebase matching the exact folder layout, database schemas, and business rules specified below.

---

### 🛠️ 1. Technology Stack
1. **Language:** Go (Golang) 1.21+
2. **HTTP Router / Framework:** Gin-Gonic (`github.com/gin-gonic/gin`)
3. **Database & Connection Pool:** PostgreSQL using `github.com/jackc/pgx/v5/pgxpool` for direct high-performance SQL execution (No GORM, write clean, raw SQL queries).
4. **Authentication:**
   - Direct Google ID Token verification (using the official Google API library `google.golang.org/api/idtoken`) to authenticate users (bypassing Firebase Auth entirely).
   - Custom, server-signed session JWTs using `github.com/golang-jwt/jwt/v5` for securing subsequent API endpoints.
5. **Push Notifications:** Firebase Cloud Messaging (FCM) using the official `firebase.google.com/go/v4` SDK *only* for sending push notifications (not for database or login).

---

### 📂 2. Project Directory Structure
Generate and structure the files matching this exact standard layout:
- cmd/api/main.go                 # App entry point, Router setup, database connection bootstrap
- config/serviceAccountKey.json   # Placed here manually (loaded for FCM push)
- internal/auth/google.go         # Direct Google ID Token verification logic
- internal/auth/jwt.go            # Custom JWT generation, parsing, and signing
- internal/auth/middleware.go     # HTTP Middleware interceptor validating Bearer custom JWT
- internal/database/db.go         # pgxpool connection pool initializers and migrations
- internal/database/fcm.go        # Firebase Messaging Client initializing and push sending
- internal/models/models.go       # Go Structs matching database rows with JSON tags
- internal/handlers/auth.go       # Handlers: Direct Google login & profile updates
- internal/handlers/club.go       # Handlers: Creating clubs, joining, and listing members
- internal/handlers/inventory.go  # Handlers: Catalog CRUD
- internal/handlers/lending.go    # Handlers: Relational transaction-safe checkouts & returns
- internal/handlers/notices.go    # Handlers: Notice board, RSVP events, polls & voting
- internal/handlers/accounts.go   # Handlers: Financial ledger credit/debit entries

---

### 💾 3. PostgreSQL Database Schema
All database queries must match these exact relational table designs:

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

-- 3. Club Members Table
CREATE TABLE club_members (
    club_id UUID REFERENCES clubs(id) ON DELETE CASCADE,
    user_id VARCHAR(128) REFERENCES users(id) ON DELETE CASCADE,
    role member_role NOT NULL DEFAULT 'member',
    status member_status NOT NULL DEFAULT 'pending',
    joined_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (club_id, user_id)
);

-- 4. Device FCM Tokens Table
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

---

### ⚡ 4. Critical Business Logic to Implement

1. **Direct Google Sign-in Verification (`internal/auth/google.go`):**
   Use Google's `idtoken` package to validate incoming ID tokens against Google's public certificates. Validate that the token was issued to Client ID `594179901385-ab1g3eblhjkjokktcrr11ab9cd9jp35f.apps.googleusercontent.com`. Extract `Subject` (UID), `Email`, `Name`, and `Picture`.
   
2. **Auth Middleware (`internal/auth/middleware.go`):**
   Intercept protected HTTP requests. Verify the custom session JWT from the `Authorization: Bearer <token>` header. Extract the `userUID` and `email` claims, injecting them into Gin's context (`c.Set("userUID", claim.UID)`).

3. **Transaction-Safe Checkout / Lending (`internal/handlers/lending.go`):**
   When creating a lending record, execute inside an atomic SQL transaction:
   - For every checked-out item, query stock from `inventory` with row locking (`SELECT available_qty FROM inventory WHERE id = $1 FOR UPDATE`).
   - Throw a `400 Bad Request` if `available_qty < requested_quantity`.
   - Update `available_qty = available_qty - requested_quantity` and record the lending contract in a `lendings` log.

4. **Background FCM Notifications (`internal/handlers/notices.go`):**
   When a user posts a new notice or RSVP event inside a club:
   - Save the notice in the database.
   - Run a background goroutine that queries all active member UIDs in that club from `club_members`, retrieves their registered tokens from `user_fcm_tokens`, and triggers `FCMClient.Send()` to send push notifications to their devices.

---

### 📡 5. REST API Endpoints Specification

Write standard Gin endpoints matching this specification:
* **`POST /api/v1/auth/google`** - Payload: `{ "idToken" }`. Returns: `{ "token", "user" }`.
* **`GET /api/v1/users/me`** - Returns logged-in user profile.
* **`POST /api/v1/users/me/fcm-token`** - Payload: `{ "token", "platform" }`.
* **`POST /api/v1/clubs`** - Payload: `{ "name", "address", "registrationNumber", "email" }`.
* **`GET /api/v1/clubs`** - Lists user clubs.
* **`GET /api/v1/clubs/:clubId/members`** - Lists members (filter by `status` query).
* **`GET /api/v1/clubs/:clubId/inventory`** - Lists assets (filter by `type` query).
* **`POST /api/v1/clubs/:clubId/lendings`** - Executes stock checkout transaction.
* **`POST /api/v1/clubs/:clubId/lendings/:lendingId/returns`** - Records returns and restores stock.
* **`GET /api/v1/clubs/:clubId/accounts`** - Lists financial ledger (filter by `status` query for current vs pending).
* **`POST /api/v1/clubs/:clubId/accounts`** - Adds expense or income transaction entry.
* **`PUT /api/v1/clubs/:clubId/accounts/:entryId`** - Modifies details of a transaction.
* **`PATCH /api/v1/clubs/:clubId/accounts/:entryId/status`** - Approves/completes a pending transaction.
* **`GET /api/v1/clubs/:clubId/accounts/balance`** - Fetch credit, debit, running balance, and pending calculations.

Please write clean, well-commented, complete, and production-ready Go code for these files. Focus on proper error handling, logging, and performance.
```
