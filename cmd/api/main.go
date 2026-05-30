package main

import (
	"log"
	"net/http"
	"os"

	"decotrack-backend/internal/auth"
	"decotrack-backend/internal/database"
	"decotrack-backend/internal/handlers"
	"github.com/gin-gonic/gin"
)

func main() {
	// 1. Initialize Gin Router
	router := gin.Default()

	// 2. Setup CORS Middleware
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, PATCH, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})

	// 3. Load Configurations
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		// Default local development URI
		dbURL = "postgres://postgres:postgres@localhost:5432/decotrack?sslmode=disable"
		log.Printf("DATABASE_URL environment variable is empty. Defaulting to local dev URI: %s", dbURL)
	}

	fcmCredentialsPath := os.Getenv("FIREBASE_CREDENTIALS_PATH")
	if fcmCredentialsPath == "" {
		// Standard project fallback location
		fcmCredentialsPath = "config/serviceAccountKey.json"
		if _, err := os.Stat(fcmCredentialsPath); err != nil {
			// Try secondary fallback location
			fcmCredentialsPath = "config/firebaseServiceAccount.json"
			if _, err := os.Stat(fcmCredentialsPath); err != nil {
				fcmCredentialsPath = "" // No credentials found
			}
		}
	}

	// 4. Initialize Database Connection Pool
	dbPool, err := database.InitDB(dbURL)
	if err != nil {
		log.Printf("DATABASE CONNECTION ERROR: %v", err)
		log.Println("WARNING: The database connection failed. Endpoints requiring database queries will fail. Running in offline/debug mode...")
	} else {
		defer dbPool.Close()
	}

	// 5. Initialize FCM Push Notifications Client
	fcmEngine, err := database.InitFCM(fcmCredentialsPath)
	if err != nil {
		log.Printf("FCM INITIALIZATION ERROR: %v", err)
		log.Println("WARNING: The Firebase Messaging Client failed to boot. Push notification dispatches will be mocked.")
	}

	// 6. Ingest Dependencies into Handlers App
	app := &handlers.App{
		DB:  dbPool,
		FCM: fcmEngine,
	}

	// 7. Database Connection Pool Status Middleware
	dbCheck := func(c *gin.Context) {
		if dbPool == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"error": "Database service is offline. Please ensure your PostgreSQL server is active, a database exists, and the DATABASE_URL environment variable is configured correctly.",
			})
			c.Abort()
			return
		}
		c.Next()
	}

	// 8. Wire API Route groups
	v1 := router.Group("/api/v1")
	{
		// Public Routes
		v1.GET("/health", func(c *gin.Context) {
			c.JSON(http.StatusOK, gin.H{
				"status":  "healthy",
				"message": "DecoTrack Backend API is fully operational",
			})
		})

		v1.POST("/auth/google", dbCheck, app.AuthenticateGoogleUser)

		// Protected Route Group (Interceptors mapped under AuthMiddleware and dbCheck)
		protected := v1.Group("")
		protected.Use(auth.AuthMiddleware(), dbCheck)
		{
			// User Settings / Profile
			protected.GET("/users/me", app.GetMe)
			protected.PUT("/users/me", app.UpdateProfile)
			protected.POST("/users/me/fcm-token", app.RegisterFCMToken)

			// Club Management
			protected.POST("/clubs", app.CreateClub)
			protected.GET("/clubs", app.ListUserClubs)
			protected.GET("/clubs/invite/:code", app.VerifyInviteCode)
			protected.POST("/clubs/join", app.RequestToJoinClub)

			// Member Configurations
			protected.GET("/clubs/:clubId/members", app.ListClubMembers)
			protected.PUT("/clubs/:clubId/members/:userId/role", app.UpdateMemberRole)
			protected.PUT("/clubs/:clubId/members/:userId/status", app.UpdateMemberStatus)

			// Inventory Catalog
			protected.GET("/clubs/:clubId/inventory", app.FetchClubInventory)
			protected.POST("/clubs/:clubId/inventory", app.AddInventoryAsset)
			protected.PUT("/clubs/:clubId/inventory/:itemId", app.UpdateInventoryAsset)

			// Relational lending & returns (ACID)
			protected.POST("/clubs/:clubId/lendings", app.CreateLending)
			protected.POST("/clubs/:clubId/lendings/:lendingId/returns", app.RecordReturn)

			// Notice board posts & voting polls
			protected.POST("/clubs/:clubId/notices", app.CreateNotice)
			protected.POST("/clubs/:clubId/notices/:noticeId/vote", app.CastVote)

			// Ledger accounts and running balance summaries
			protected.GET("/clubs/:clubId/accounts", app.FetchClubAccounts)
			protected.POST("/clubs/:clubId/accounts", app.AddLedgerEntry)
			protected.PUT("/clubs/:clubId/accounts/:entryId", app.UpdateLedgerEntry)
			protected.PATCH("/clubs/:clubId/accounts/:entryId/status", app.ApproveLedgerEntry)
			protected.GET("/clubs/:clubId/accounts/balance", app.FetchRunningBalance)
		}
	}

	// 8. Run Server
	port := os.Getenv("PORT")
	if port == "" {
		port = "7070"
	}

	log.Printf("Starting DecoTrack Backend server on port %s...", port)
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
