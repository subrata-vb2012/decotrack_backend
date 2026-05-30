# 🚀 DecoTrack Go Backend

This is the Go-lang production-ready REST API backend for **DecoTrack**, a club inventory and lending manager app.

## 🛠️ Tech Stack
- **Go 1.21+**
- **Gin-Gonic** for HTTP Routing
- **PostgreSQL** (via `pgxpool`) for high-performance direct SQL execution
- **Direct Google Auth Token Verification** (bypassing Firebase Auth for authentication)
- **Custom JWT Sessions** for API request authorization
- **Firebase Cloud Messaging (FCM)** for background push notifications

## 📂 Project Structure

```text
Decotrack_backend/
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
│   │   └── models.go               # PostgreSQL database structures
│   └── handlers/
│       ├── auth_handler.go         # Direct Google Login & registration handlers
│       ├── inventory_handler.go    # SQL Transaction-safe Inventory CRUD
│       ├── lending_handler.go      # Transaction-safe lending & returns
│       └── notification_handler.go # Registering device FCM tokens
├── go.mod
└── go.sum
```

## 🚀 Getting Started

### 📋 Prerequisites
- **Go** installed on your system (v1.21 or later recommended).
- **PostgreSQL** database up and running.

### ⚙️ Setup
1. Clone or navigate to this folder.
2. Download dependencies:
   ```bash
   go mod tidy
   ```
3. Run the development server:
   ```bash
   go run cmd/api/main.go
   ```

### 🔨 Build
To build a production binary:
```bash
go build -o decotrack-api ./cmd/api
```
