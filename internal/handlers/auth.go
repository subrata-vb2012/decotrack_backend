package handlers

import (
	"log"
	"net/http"
	"time"

	"decotrack-backend/internal/auth"
	"decotrack-backend/internal/database"
	"decotrack-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgxpool"
)

// App holds the shared application dependencies (Database Pool & FCM Engine)
type App struct {
	DB  *pgxpool.Pool
	FCM *database.NotificationEngine
}

type GoogleLoginRequest struct {
	IDToken string `json:"idToken" binding:"required"`
}

// AuthenticateGoogleUser verifies the Google ID Token and returns a custom session JWT.
func (app *App) AuthenticateGoogleUser(c *gin.Context) {
	var req GoogleLoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 1. Verify token directly with Google Auth servers
	googleUser, err := auth.VerifyGoogleIDToken(c.Request.Context(), req.IDToken)
	if err != nil {
		log.Printf("[GOOGLE AUTH ERROR] ID Token verification failed: %v", err)
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	// 2. Query / Upsert User in PostgreSQL database
	var user models.User
	var mobileNum *string
	query := `
		INSERT INTO users (id, name, email, photo_url, created_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE 
		SET name = EXCLUDED.name, photo_url = EXCLUDED.photo_url
		RETURNING id, name, email, photo_url, mobile_number, created_at`

	err = app.DB.QueryRow(c.Request.Context(), query,
		googleUser.Subject,
		googleUser.Name,
		googleUser.Email,
		googleUser.Picture,
		time.Now(),
	).Scan(&user.ID, &user.Name, &user.Email, &user.PhotoURL, &mobileNum, &user.CreatedAt)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database registration failed: " + err.Error()})
		return
	}
	user.MobileNumber = mobileNum

	// 3. Generate a secure, server-signed Custom session JWT
	jwtToken, err := auth.GenerateJWT(user.ID, user.Email)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "JWT generation failed"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"token": jwtToken,
		"user":  user,
	})
}

// GetMe retrieves the currently logged-in user profile.
func (app *App) GetMe(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	var user models.User
	var mobileNum *string
	query := `SELECT id, name, email, photo_url, mobile_number, created_at FROM users WHERE id = $1`
	
	err := app.DB.QueryRow(c.Request.Context(), query, userUID).Scan(
		&user.ID, &user.Name, &user.Email, &user.PhotoURL, &mobileNum, &user.CreatedAt,
	)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User profile not found in database"})
		return
	}
	user.MobileNumber = mobileNum

	c.JSON(http.StatusOK, user)
}

type UpdateProfileRequest struct {
	Name         string `json:"name" binding:"required"`
	MobileNumber string `json:"mobileNumber" binding:"required"`
}

// UpdateProfile modifies the user's name and mobile number.
func (app *App) UpdateProfile(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	var req UpdateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var user models.User
	var mobileNum *string
	query := `
		UPDATE users 
		SET name = $1, mobile_number = $2 
		WHERE id = $3 
		RETURNING id, name, email, photo_url, mobile_number, created_at`

	err := app.DB.QueryRow(c.Request.Context(), query, req.Name, req.MobileNumber, userUID).Scan(
		&user.ID, &user.Name, &user.Email, &user.PhotoURL, &mobileNum, &user.CreatedAt,
	)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update profile: " + err.Error()})
		return
	}
	user.MobileNumber = mobileNum

	c.JSON(http.StatusOK, user)
}

type FCMTokenRequest struct {
	Token    string `json:"token" binding:"required"`
	Platform string `json:"platform" binding:"required"`
}

// RegisterFCMToken maps device push tokens to the active user profile.
func (app *App) RegisterFCMToken(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	var req FCMTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	query := `
		INSERT INTO user_fcm_tokens (user_id, token, platform, last_updated)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (token) DO UPDATE 
		SET user_id = EXCLUDED.user_id, platform = EXCLUDED.platform, last_updated = EXCLUDED.last_updated`

	_, err := app.DB.Exec(c.Request.Context(), query, userUID, req.Token, req.Platform, time.Now())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to register FCM token: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":  "registered",
		"message": "FCM token updated successfully",
	})
}
