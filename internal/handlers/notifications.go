package handlers

import (
	"errors"
	"net/http"
	"time"

	"decotrack-backend/internal/models"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
)

type AddNotificationRequest struct {
	ID    string `json:"id"`
	Title string `json:"title" binding:"required"`
	Body  string `json:"body" binding:"required"`
}

type UpdateNotificationRequest struct {
	Title  string `json:"title"`
	Body   string `json:"body"`
	IsRead bool   `json:"isRead"`
}

// ListNotifications lists all in-app notifications for a club.
func (app *App) ListNotifications(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")

	// Verify membership
	role, status, err := app.getRequesterRoleAndStatus(c, clubID, userUID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if role == "" || status != "active" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. Active club membership required."})
		return
	}

	query := `
		SELECT id, club_id, title, body, is_read, created_at
		FROM notifications
		WHERE club_id = $1
		ORDER BY created_at DESC`

	rows, err := app.DB.Query(c.Request.Context(), query, clubID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query notifications: " + err.Error()})
		return
	}
	defer rows.Close()

	notifications := []models.Notification{}
	for rows.Next() {
		var n models.Notification
		err := rows.Scan(&n.ID, &n.ClubID, &n.Title, &n.Body, &n.IsRead, &n.CreatedAt)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read notification: " + err.Error()})
			return
		}
		notifications = append(notifications, n)
	}

	c.JSON(http.StatusOK, notifications)
}

// AddNotification registers a new notification.
func (app *App) AddNotification(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")

	var req AddNotificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify membership permissions (only secretary, admin, or owner can create notifications)
	role, status, err := app.getRequesterRoleAndStatus(c, clubID, userUID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if status != "active" || (role != "owner" && role != "admin" && role != "secretary") {
		c.JSON(http.StatusForbidden, gin.H{"error": "Unauthorized. Owner, Admin, or Secretary permissions required."})
		return
	}

	var n models.Notification
	var query string
	if req.ID != "" {
		query = `
			INSERT INTO notifications (id, club_id, title, body, is_read, created_at)
			VALUES ($1, $2, $3, $4, $5, $6)
			RETURNING id, club_id, title, body, is_read, created_at`
		err = app.DB.QueryRow(c.Request.Context(), query,
			req.ID, clubID, req.Title, req.Body, false, time.Now(),
		).Scan(&n.ID, &n.ClubID, &n.Title, &n.Body, &n.IsRead, &n.CreatedAt)
	} else {
		query = `
			INSERT INTO notifications (club_id, title, body, is_read, created_at)
			VALUES ($1, $2, $3, $4, $5)
			RETURNING id, club_id, title, body, is_read, created_at`
		err = app.DB.QueryRow(c.Request.Context(), query,
			clubID, req.Title, req.Body, false, time.Now(),
		).Scan(&n.ID, &n.ClubID, &n.Title, &n.Body, &n.IsRead, &n.CreatedAt)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to create notification: " + err.Error()})
		return
	}

	c.JSON(http.StatusCreated, n)
}

// UpdateNotification updates notification parameters (like isRead).
func (app *App) UpdateNotification(c *gin.Context) {
	userUID, exists := c.Get("userUID")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "User context not found"})
		return
	}

	clubID := c.Param("clubId")
	notificationID := c.Param("id")

	var req UpdateNotificationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify membership
	role, status, err := app.getRequesterRoleAndStatus(c, clubID, userUID.(string))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if role == "" || status != "active" {
		c.JSON(http.StatusForbidden, gin.H{"error": "Access denied. Active club membership required."})
		return
	}

	// Check if notification exists
	var currentIsRead bool
	err = app.DB.QueryRow(c.Request.Context(), `SELECT is_read FROM notifications WHERE id = $1 AND club_id = $2`, notificationID, clubID).Scan(&currentIsRead)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Notification not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to query notification: " + err.Error()})
		return
	}

	var n models.Notification
	query := `
		UPDATE notifications
		SET title = COALESCE(NULLIF($1, ''), title), body = COALESCE(NULLIF($2, ''), body), is_read = $3
		WHERE id = $4 AND club_id = $5
		RETURNING id, club_id, title, body, is_read, created_at`

	err = app.DB.QueryRow(c.Request.Context(), query,
		req.Title, req.Body, req.IsRead, notificationID, clubID,
	).Scan(&n.ID, &n.ClubID, &n.Title, &n.Body, &n.IsRead, &n.CreatedAt)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update notification: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, n)
}
