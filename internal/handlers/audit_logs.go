package handlers

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"decotrack-backend/internal/models"
	"github.com/gin-gonic/gin"
)

// LogActionHelper records an action into audit_logs.
func (app *App) LogActionHelper(ctx context.Context, clubID string, actionType string, performedBy string, targetEntity string, oldValue any, newValue any) {
	var oldJSON, newJSON []byte
	var err error

	if oldValue != nil {
		oldJSON, err = json.Marshal(oldValue)
		if err != nil {
			log.Printf("[AUDIT LOG ERROR] Failed to marshal oldValue: %v", err)
		}
	}
	if newValue != nil {
		newJSON, err = json.Marshal(newValue)
		if err != nil {
			log.Printf("[AUDIT LOG ERROR] Failed to marshal newValue: %v", err)
		}
	}

	query := `
		INSERT INTO audit_logs (club_id, action_type, performed_by, target_entity, old_value, new_value, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err = app.DB.Exec(ctx, query, clubID, actionType, performedBy, targetEntity, oldJSON, newJSON, time.Now())
	if err != nil {
		log.Printf("[AUDIT LOG ERROR] Failed to record audit log in DB: %v", err)
	}
}

// ListAuditLogs lists all audit logs for a club, sorted by timestamp descending.
func (app *App) ListAuditLogs(c *gin.Context) {
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
		SELECT id, club_id, action_type, performed_by, target_entity, old_value, new_value, timestamp
		FROM audit_logs
		WHERE club_id = $1
		ORDER BY timestamp DESC`

	rows, err := app.DB.Query(c.Request.Context(), query, clubID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to fetch audit logs: " + err.Error()})
		return
	}
	defer rows.Close()

	logsList := []models.AuditLog{}
	for rows.Next() {
		var l models.AuditLog
		var oldValBytes, newValBytes []byte
		err := rows.Scan(
			&l.ID, &l.ClubID, &l.ActionType, &l.PerformedBy, &l.TargetEntity, &oldValBytes, &newValBytes, &l.Timestamp,
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to read audit log record: " + err.Error()})
			return
		}
		if len(oldValBytes) > 0 {
			var unmarshaledOld any
			if err := json.Unmarshal(oldValBytes, &unmarshaledOld); err == nil {
				l.OldValue = unmarshaledOld
			}
		}
		if len(newValBytes) > 0 {
			var unmarshaledNew any
			if err := json.Unmarshal(newValBytes, &unmarshaledNew); err == nil {
				l.NewValue = unmarshaledNew
			}
		}
		logsList = append(logsList, l)
	}

	c.JSON(http.StatusOK, logsList)
}
